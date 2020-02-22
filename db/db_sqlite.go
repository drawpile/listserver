package db

import (
	"context"
	"crawshaw.io/sqlite"
	"crawshaw.io/sqlite/sqlitex"
	"fmt"
	"strings"
	"time"
)

type sqliteDb struct {
	pool           *sqlitex.Pool
	timeoutString  string
	timeoutMinutes int
}

func sqlite_exec(conn *sqlite.Conn, statement string) {
	stmt, _, err := conn.PrepareTransient(statement)
	if err != nil {
		panic(err)
	}

	_, err = stmt.Step()
	stmt.Finalize()
}

func newSqliteDb(dbname string, sessionTimeout int) (*sqliteDb, error) {
	poolsize := 5
	if dbname == "memory" {
		dbname = "file:memory:?mode=memory"
		poolsize = 1 // memory database is not shared between connections
	}

	dbpool, err := sqlitex.Open(dbname, 0, poolsize)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	conn := dbpool.Get(ctx)
	if conn == nil {
		return nil, fmt.Errorf("No connection")
	}
	defer dbpool.Put(conn)

	// Prepare the database
	sqlite_exec(conn, `CREATE TABLE IF NOT EXISTS sessions (
		host TEXT NOT NULL,
		port INTEGER NOT NULL,
		session_id TEXT NOT NULL,
		protocol TEXT NOT NULL,
		title TEXT NOT NULL,
		users INTEGER NOT NULL,
		usernames TEXT NOT NULL,
		password INTEGER NOT NULL,
		nsfm INTEGER NOT NULL,
		owner TEXT NOT NULL,
		started TEXT NOT NULL,
		last_active TEXT NOT NULL,
		unlisted INTEGER NOT NULL,
		update_key TEXT NOT NULL,
		client_ip TEXT NOT NULL,
		roomcode TEXT UNIQUE,
		private INTEGER NOT NULL
		);`)
	sqlite_exec(conn, `CREATE TABLE IF NOT EXISTS hostbans (
		host TEXT NOT NULL,
		expires TEXT,
		notes TEXT NOT NULL DEFAULT ''
		);`)

	db := &sqliteDb{
		pool:           dbpool,
		timeoutString:  fmt.Sprintf("-%d minutes", sessionTimeout),
		timeoutMinutes: sessionTimeout,
	}

	go sqliteCleanupTask(db)

	return db, nil
}

func (db *sqliteDb) cleanup() {
	conn := db.pool.Get(context.TODO())
	defer db.pool.Put(conn)
	sqlite_exec(conn, "DELETE FROM sessions WHERE unlisted!=0 OR last_active < DATETIME('now', '-1 day')")
}

func sqliteCleanupTask(db *sqliteDb) {
	for {
		time.Sleep(24 * time.Hour)
		db.cleanup()
	}
}

func (db *sqliteDb) SessionTimeoutMinutes() int {
	return db.timeoutMinutes
}

// Get a list of sessions that match the given query parameters
func (db *sqliteDb) QuerySessionList(opts QueryOptions, ctx context.Context) ([]SessionInfo, error) {
	querySql := `
	SELECT host, port, session_id, roomcode, protocol, title, users, usernames, password, nsfm, owner,
	started
	FROM sessions
	WHERE last_active >= DATETIME('now', $timeout) AND unlisted=false AND private=false`

	if len(opts.Title) > 0 {
		querySql += " AND title LIKE '%' || $title || '%'"
	}

	if !opts.Nsfm {
		querySql += " AND nsfm=0"
	}

	var protocols []string

	if len(opts.Protocol) > 0 {
		protocols = strings.Split(opts.Protocol, ",")
		placeholders := make([]string, len(protocols))
		for i, _ := range protocols {
			placeholders[i] = fmt.Sprintf("$proto%d", i)
		}
		querySql += ` AND protocol IN (` + strings.Join(placeholders, ",") + `)`
	}

	querySql += ` ORDER BY title, users ASC`

	conn := db.pool.Get(ctx)
	if conn == nil {
		return []SessionInfo{}, fmt.Errorf("Connection not available")
	}
	defer db.pool.Put(conn)

	stmt := conn.Prep(querySql)

	stmt.SetText("$timeout", db.timeoutString)

	if len(opts.Title) > 0 {
		stmt.SetText("$title", opts.Title)
	}

	if len(protocols) > 0 {
		for i, v := range protocols {
			stmt.SetText(fmt.Sprintf("$proto%d", i), v)
		}
	}

	sessions := []SessionInfo{}
	for {
		if hasRow, err := stmt.Step(); err != nil {
			return sessions, err
		} else if !hasRow {
			break
		}

		sessions = append(sessions, SessionInfo{
			Host:      stmt.GetText("host"),
			Port:      int(stmt.GetInt64("port")),
			Id:        stmt.GetText("session_id"),
			Protocol:  stmt.GetText("protocol"),
			Title:     stmt.GetText("title"),
			Users:     int(stmt.GetInt64("users")),
			Usernames: strings.Split(stmt.GetText("usernames"), ","), // TODO split this properly
			Password:  stmt.GetInt64("password") != 0,
			Nsfm:      stmt.GetInt64("nsfm") != 0,
			Owner:     stmt.GetText("owner"),
			Started:   stmt.GetText("started"),
			Roomcode:  stmt.GetText("roomcode"),
			Private:   false,
		})
	}

	return sessions, nil
}

// Get the session matching the given room code (if it is still active)
func (db *sqliteDb) QuerySessionByRoomcode(roomcode string, ctx context.Context) (JoinSessionInfo, error) {
	conn := db.pool.Get(ctx)
	if conn == nil {
		return JoinSessionInfo{}, fmt.Errorf("Connection not available")
	}
	defer db.pool.Put(conn)

	stmt := conn.Prep(`SELECT host, port, session_id
	FROM sessions
	WHERE last_active >= DATETIME('now', $timeout) AND unlisted=false
	AND roomcode=$roomcode
	`)
	defer stmt.Reset()

	stmt.SetText("$timeout", db.timeoutString)
	stmt.SetText("$roomcode", roomcode)

	ses := JoinSessionInfo{}

	if hasRow, err := stmt.Step(); err != nil {
		return ses, err
	} else if !hasRow {
		return ses, nil
	}

	ses.Host = stmt.GetText("host")
	ses.Port = int(stmt.GetInt64("port"))
	ses.Id = stmt.GetText("session_id")

	return ses, nil
}

// Is there an active announcement for this session
func (db *sqliteDb) IsActiveSession(host, id string, port int, ctx context.Context) (bool, error) {
	conn := db.pool.Get(ctx)
	if conn == nil {
		return false, fmt.Errorf("Connection not available")
	}
	defer db.pool.Put(conn)

	stmt := conn.Prep(`SELECT EXISTS(SELECT 1
	FROM sessions
	WHERE host=$host AND port=$port AND session_id=$id
	AND last_active >= DATETIME('now', $timeout) AND unlisted=0
	)`)
	defer stmt.Reset()

	stmt.SetText("$host", host)
	stmt.SetText("$id", id)
	stmt.SetInt64("$port", int64(port))
	stmt.SetText("$timeout", db.timeoutString)

	if hasRow, err := stmt.Step(); err != nil {
		return false, err
	} else if !hasRow {
		return false, nil
	}

	active := stmt.ColumnInt(0) != 0
	return active, nil
}

// Get the number of active announcements on this server (all ports)
func (db *sqliteDb) GetHostSessionCount(host string, ctx context.Context) (int, error) {
	conn := db.pool.Get(ctx)
	if conn == nil {
		return 0, fmt.Errorf("Connection not available")
	}
	defer db.pool.Put(conn)

	stmt := conn.Prep(`SELECT COUNT(*) FROM sessions
	WHERE host=$host AND last_active >= DATETIME('now', $timeout) AND unlisted=false
	`)
	defer stmt.Reset()

	stmt.SetText("$host", host)
	stmt.SetText("$timeout", db.timeoutString)

	if hasRow, err := stmt.Step(); err != nil {
		return 0, err
	} else if !hasRow {
		return 0, fmt.Errorf("No row returned!")
	}

	count := stmt.ColumnInt(0)
	return count, nil
}

// Check if the given host is on the ban list
func (db *sqliteDb) IsBannedHost(host string, ctx context.Context) (bool, error) {
	conn := db.pool.Get(ctx)
	if conn == nil {
		return false, fmt.Errorf("Connection not available")
	}
	defer db.pool.Put(conn)

	stmt := conn.Prep(`SELECT EXISTS(SELECT 1
	FROM hostbans
	WHERE host=$host AND (expires IS NULL OR expires > DATETIME('now'))
	)`)
	defer stmt.Reset()

	stmt.SetText("$host", host)

	if hasRow, err := stmt.Step(); err != nil {
		return false, err
	} else if !hasRow {
		return false, fmt.Errorf("No row returned")
	}

	isBanned := stmt.ColumnInt(0) != 0
	return isBanned, nil
}

// Insert a new session to the database
// Note: this function does not validate the data;
// that must be done before calling this
func (db *sqliteDb) InsertSession(session SessionInfo, clientIp string, ctx context.Context) (NewSessionInfo, error) {
	var updateKey string
	updateKey, err := generateUpdateKey()
	if err != nil {
		return NewSessionInfo{}, err
	}

	conn := db.pool.Get(ctx)
	if conn == nil {
		return NewSessionInfo{}, fmt.Errorf("Connection not available")
	}
	defer db.pool.Put(conn)

	stmt := conn.Prep(`INSERT INTO sessions
	(host, port, session_id, protocol, title, users, usernames, password, nsfm,
	owner, started, last_active, unlisted, update_key, client_ip, private)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'), CURRENT_TIMESTAMP, 0, ?, ?, ?)
	`)

	i := sqlite.BindIncrementor()
	stmt.BindText(i(), session.Host)
	stmt.BindInt64(i(), int64(session.Port))
	stmt.BindText(i(), session.Id)
	stmt.BindText(i(), session.Protocol)
	stmt.BindText(i(), session.Title)
	stmt.BindInt64(i(), int64(session.Users))
	stmt.BindText(i(), strings.Join(session.Usernames, ","))
	stmt.BindBool(i(), session.Password)
	stmt.BindBool(i(), session.Nsfm)
	stmt.BindText(i(), session.Owner)
	stmt.BindText(i(), updateKey)
	stmt.BindText(i(), clientIp)
	stmt.BindBool(i(), session.Private)

	if _, err := stmt.Step(); err != nil {
		return NewSessionInfo{}, err
	}

	return NewSessionInfo{
		conn.LastInsertRowID(),
		updateKey,
		session.Private,
		"",
	}, nil
}

// Assign a random room code to a listing and return the code
func (db *sqliteDb) AssignRoomCode(listingId int64, ctx context.Context) (string, error) {
	conn := db.pool.Get(ctx)
	if conn == nil {
		return "", fmt.Errorf("Connection not available")
	}
	defer db.pool.Put(conn)

	stmt := conn.Prep("UPDATE sessions SET roomcode=$roomcode WHERE rowid=$id")

	for retry := 0; retry < 10; retry += 1 {
		roomcode, err := generateRoomcode(5)
		if err != nil {
			return "", err
		}
		stmt.SetText("$roomcode", roomcode)
		stmt.SetInt64("$id", listingId)

		if _, err = stmt.Step(); err != nil {
			if sqerr, ok := err.(sqlite.Error); ok {
				// Unlikely, but can happen
				if sqerr.Code == sqlite.SQLITE_CONSTRAINT_UNIQUE {
					continue
				}
			}

			return "", err

		} else {
			return roomcode, nil
		}
	}
	return "", fmt.Errorf("Couldn't generate a unique room code")
}

// Refresh an announcement
func (db *sqliteDb) RefreshSession(refreshFields map[string]interface{}, listingId int64, updateKey string, ctx context.Context) error {
	conn := db.pool.Get(ctx)
	if conn == nil {
		return fmt.Errorf("Connection not available")
	}
	defer db.pool.Put(conn)

	// First, make sure an active listing exists
	stmt := conn.Prep(`SELECT EXISTS(SELECT 1
	FROM sessions
	WHERE rowid=$id AND update_key=$key AND last_active >= DATETIME('now', $timeout)
	AND unlisted=false
	)`)
	defer stmt.Reset()

	stmt.SetInt64("$id", listingId)
	stmt.SetText("$key", updateKey)
	stmt.SetText("$timeout", db.timeoutString)

	if hasRow, err := stmt.Step(); err != nil {
		return err
	} else if !hasRow {
		return fmt.Errorf("No row returned")
	}
	if stmt.ColumnInt(0) == 0 {
		return RefreshError{"not found"}
	}

	// Construct update query
	querySql := "UPDATE sessions SET last_active=CURRENT_TIMESTAMP"
	params := []interface{}{}

	if val, ok := optString(refreshFields, "title"); ok {
		querySql += ", title=?"
		params = append(params, val)
	}

	if val, ok := optInt(refreshFields, "users"); ok {
		querySql += ", users=?"
		params = append(params, val)
	}

	if val, ok := optStringList(refreshFields, "usernames"); ok {
		querySql += ", usernames=?"
		params = append(params, strings.Join(val, ",")) // TODO
	}

	if val, ok := optBool(refreshFields, "password"); ok {
		querySql += ", password=?"
		params = append(params, val)
	}

	if val, ok := optBool(refreshFields, "nsfm"); ok {
		querySql += ", nsfm=?"
		params = append(params, val)
	}

	if val, ok := optBool(refreshFields, "private"); ok {
		querySql += ", private=?"
		params = append(params, val)
	}

	querySql += " WHERE rowid=?"
	params = append(params, listingId)

	stmt2 := conn.Prep(querySql)
	defer stmt2.Reset()
	for i, v := range params {
		switch val := v.(type) {
		case string:
			stmt2.BindText(i+1, val)
		case int:
			stmt2.BindInt64(i+1, int64(val))
		case int64:
			stmt2.BindInt64(i+1, val)
		case bool:
			stmt2.BindBool(i+1, val)
		default:
			panic("Unhandled type")
		}
	}

	// Execute update
	_, err := stmt2.Step()
	return err
}

// Delete an announcement
func (db *sqliteDb) DeleteSession(listingId int64, updateKey string, ctx context.Context) (bool, error) {
	conn := db.pool.Get(ctx)
	if conn == nil {
		return false, fmt.Errorf("Connection not available")
	}
	defer db.pool.Put(conn)

	stmt := conn.Prep(`UPDATE sessions
	SET unlisted=1
	WHERE rowid=$id AND update_key=$key AND unlisted=false`)

	stmt.SetInt64("$id", listingId)
	stmt.SetText("$key", updateKey)

	if _, err := stmt.Step(); err != nil {
		return false, err
	}

	return conn.Changes() > 0, nil
}
