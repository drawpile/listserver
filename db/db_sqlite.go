package db

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"crawshaw.io/sqlite"
	"crawshaw.io/sqlite/sqlitex"
)

type sqliteDb struct {
	pool           *sqlitex.Pool
	timeoutString  string
	timeoutMinutes int
}

func sqliteExec(conn *sqlite.Conn, statement string) {
	stmt, _, err := conn.PrepareTransient(statement)
	if err != nil {
		panic(err)
	}

	_, err = stmt.Step()
	stmt.Finalize()
}

// The SQLite library leaves (potentially large) WAL files laying around.
// https://github.com/crawshaw/sqlite/issues/119
func sqliteCleanUpWal(ctx context.Context, dbname string) error {
	dbpool, err := sqlitex.Open(dbname, 0, 1)
	if err != nil {
		return err
	}
	return dbpool.Close()
}

func sqliteTableExists(conn *sqlite.Conn, tableName string) bool {
	stmt, _, err := conn.PrepareTransient(`
		SELECT EXISTS (
			SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?
		)
	`)
	if err != nil {
		panic(err)
	}

	defer stmt.Finalize()
	stmt.BindText(1, tableName)

	if hasRow, err := stmt.Step(); err != nil {
		panic(err)
	} else {
		return hasRow && stmt.ColumnInt(0) != 0
	}
}

func sqliteMigrationExists(conn *sqlite.Conn, migration int64) bool {
	stmt, _, err := conn.PrepareTransient(
		`SELECT EXISTS (SELECT 1 FROM migrations WHERE version = ?)`)
	if err != nil {
		panic(err)
	}

	defer stmt.Finalize()
	stmt.BindInt64(1, migration)

	if hasRow, err := stmt.Step(); err != nil {
		panic(err)
	} else {
		return hasRow && stmt.ColumnInt(0) != 0
	}
}

func sqliteCreateSessionsTable(conn *sqlite.Conn) {
	sqliteExec(conn, `CREATE TABLE sessions (
		id INTEGER PRIMARY KEY NOT NULL,
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
		unlist_reason TEXT,
		max_users INTEGER NOT NULL,
		closed INTEGER NOT NULL,
		active_drawing_users INTEGER NOT NULL DEFAULT -1,
		allow_web INTEGER NOT NULL DEFAULT 0
		);`)
}

func sqliteInitDb(conn *sqlite.Conn) {
	sqliteExec(conn, `CREATE TABLE migrations (
		version INTEGER PRIMARY KEY NOT NULL
		);`)
	sqliteCreateSessionsTable(conn)
	sqliteExec(conn, `CREATE TABLE hostbans (
		id INTEGER PRIMARY KEY NOT NULL,
		host TEXT NOT NULL,
		expires TEXT,
		notes TEXT NOT NULL DEFAULT ''
		);`)
	sqliteExec(conn, `CREATE TABLE accesslevels (
		id INTEGER PRIMARY KEY NOT NULL,
		description TEXT NOT NULL
		);`)
	sqliteExec(conn, `INSERT INTO accesslevels (id, description) VALUES
		(0, 'none'), (1, 'view'), (2, 'manage');`)
	sqliteExec(conn, `CREATE TABLE roles (
		id INTEGER PRIMARY KEY NOT NULL,
		name TEXT UNIQUE NOT NULL,
		admin INTEGER NOT NULL,
		access_sessions INTEGER NOT NULL REFERENCES accesslevels (id),
		access_hostbans INTEGER NOT NULL REFERENCES accesslevels (id),
		access_roles INTEGER NOT NULL REFERENCES accesslevels (id),
		access_users INTEGER NOT NULL REFERENCES accesslevels (id)
		);`)
	sqliteExec(conn, `CREATE TABLE users (
		id INTEGER PRIMARY KEY NOT NULL,
		name TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		role INTEGER NOT NULL REFERENCES roles (id)
		);`)
	sqliteExec(conn, `INSERT INTO migrations (version) VALUES (1), (2), (3), (4);`)
}

func sqliteMigrateFromLegacyFormat(conn *sqlite.Conn) {
	sqliteExec(conn, `ALTER TABLE sessions RENAME TO sessions_old;`)
	sqliteExec(conn, `ALTER TABLE hostbans RENAME TO hostbans_old;`)
	sqliteInitDb(conn)
	sqliteExec(conn, `INSERT INTO sessions SELECT
		rowid, host, port, session_id, protocol, title, users, usernames,
		password, nsfm, owner, started, last_active, unlisted, update_key,
		client_ip, NULL, 0, 0, -1, 0 FROM sessions_old;`)
	sqliteExec(conn, `INSERT INTO hostbans SELECT rowid, * FROM hostbans_old;`)
	sqliteExec(conn, `DROP TABLE sessions_old;`)
	sqliteExec(conn, `DROP TABLE hostbans_old;`)
}

func sqliteMigrateActiveDrawingUsers(conn *sqlite.Conn) {
	sqliteExec(conn, `ALTER TABLE sessions ADD active_drawing_users INTEGER NOT NULL DEFAULT -1;`)
	sqliteExec(conn, `INSERT INTO migrations (version) VALUES (2);`)
}

func sqliteMigrateAllowWeb(conn *sqlite.Conn) {
	sqliteExec(conn, `ALTER TABLE sessions ADD allow_web INTEGER NOT NULL DEFAULT 0;`)
	sqliteExec(conn, `INSERT INTO migrations (version) VALUES (3);`)
}

func sqliteMigrateRoomcodeRemoval(conn *sqlite.Conn) {
	sqliteExec(conn, `ALTER TABLE sessions RENAME TO sessions_old;`)
	sqliteCreateSessionsTable(conn)
	sqliteExec(conn, `INSERT INTO sessions SELECT
		id, host, port, session_id, protocol, title, users, usernames, password,
		nsfm, owner, started, last_active, unlisted, update_key, client_ip,
		unlist_reason, max_users, closed, active_drawing_users, allow_web
		FROM sessions_old;`)
	sqliteExec(conn, `DROP TABLE sessions_old;`)
	sqliteExec(conn, `INSERT INTO migrations (version) VALUES (4);`)
}

func sqliteEnableForeignKeys(dbpool *sqlitex.Pool, poolsize int) error {
	for i := 0; i < poolsize; i++ {
		conn := dbpool.Get(nil)
		defer dbpool.Put(conn)
		err := sqlitex.Exec(conn, `PRAGMA foreign_keys = ON;`, nil)
		if err != nil {
			return err
		}
	}
	return nil
}

func newSqliteDb(dbname string, sessionTimeout int) (*sqliteDb, error) {
	ctx := context.Background()
	poolsize := 5
	if dbname == "memory" {
		dbname = "file:memory:?mode=memory"
		poolsize = 1 // memory database is not shared between connections
	} else {
		sqliteCleanUpWal(ctx, dbname)
	}

	dbpool, err := sqlitex.Open(dbname, 0, poolsize)
	if err != nil {
		return nil, err
	}

	err = sqliteEnableForeignKeys(dbpool, poolsize)
	if err != nil {
		return nil, fmt.Errorf("Error enabling foreign keys: %s", err.Error())
	}

	conn := dbpool.Get(ctx)
	if conn == nil {
		return nil, fmt.Errorf("No connection")
	}
	defer dbpool.Put(conn)

	sqliteExec(conn, "BEGIN TRANSACTION;")
	if sqliteTableExists(conn, "migrations") {
		if !sqliteMigrationExists(conn, 2) {
			log.Println("Applying database migration 2: active drawing users")
			sqliteMigrateActiveDrawingUsers(conn)
		}
		if !sqliteMigrationExists(conn, 3) {
			log.Println("Applying database migration 3: allow web")
			sqliteMigrateAllowWeb(conn)
		}
		if !sqliteMigrationExists(conn, 4) {
			log.Println("Applying database migration 4: remove roomcodes")
			sqliteMigrateRoomcodeRemoval(conn)
		}
	} else if sqliteTableExists(conn, "sessions") {
		log.Println("Applying database migrations")
		sqliteMigrateFromLegacyFormat(conn) // Pre-migrations table format.
	} else {
		log.Println("Initializing database")
		sqliteInitDb(conn)
	}
	sqliteExec(conn, "COMMIT;")

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
	sqliteExec(conn, "DELETE FROM sessions WHERE unlisted!=0 OR last_active < DATETIME('now', '-1 day')")
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
	SELECT host, port, session_id, protocol, title, users, password, nsfm, owner,
	started, max_users, closed, active_drawing_users, allow_web
	FROM sessions
	WHERE last_active >= DATETIME('now', $timeout) AND unlisted=false`

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
			Host:               stmt.GetText("host"),
			Port:               int(stmt.GetInt64("port")),
			Id:                 stmt.GetText("session_id"),
			Protocol:           stmt.GetText("protocol"),
			Title:              stmt.GetText("title"),
			Users:              int(stmt.GetInt64("users")),
			Usernames:          []string{},
			Password:           stmt.GetInt64("password") != 0,
			Nsfm:               stmt.GetInt64("nsfm") != 0,
			Owner:              stmt.GetText("owner"),
			Started:            stmt.GetText("started"),
			MaxUsers:           int(stmt.GetInt64("max_users")),
			Closed:             stmt.GetInt64("closed") != 0,
			ActiveDrawingUsers: int(stmt.GetInt64("active_drawing_users")),
			AllowWeb:           stmt.GetInt64("allow_web") != 0,
		})
	}

	return sessions, nil
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
	WHERE host=$host COLLATE NOCASE AND (expires IS NULL OR expires > DATETIME('now'))
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
	owner, started, last_active, unlisted, update_key, client_ip, max_users,
	closed, active_drawing_users, allow_web)
	VALUES (?, ?, ?, ?, ?, ?, '', ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'),
	CURRENT_TIMESTAMP, 0, ?, ?, ?, ?, ?, ?)
	`)

	i := sqlite.BindIncrementor()
	stmt.BindText(i(), session.Host)
	stmt.BindInt64(i(), int64(session.Port))
	stmt.BindText(i(), session.Id)
	stmt.BindText(i(), session.Protocol)
	stmt.BindText(i(), session.Title)
	stmt.BindInt64(i(), int64(session.Users))
	stmt.BindBool(i(), session.Password)
	stmt.BindBool(i(), session.Nsfm)
	stmt.BindText(i(), session.Owner)
	stmt.BindText(i(), updateKey)
	stmt.BindText(i(), clientIp)
	stmt.BindInt64(i(), int64(session.MaxUsers))
	stmt.BindBool(i(), session.Closed)
	stmt.BindInt64(i(), int64(session.ActiveDrawingUsers))
	stmt.BindBool(i(), session.AllowWeb)

	if _, err := stmt.Step(); err != nil {
		return NewSessionInfo{}, err
	}

	return NewSessionInfo{
		conn.LastInsertRowID(),
		updateKey,
	}, nil
}

// Refresh an announcement
func (db *sqliteDb) RefreshSession(refreshFields map[string]interface{}, listingId int64, updateKey string, ctx context.Context) error {
	conn := db.pool.Get(ctx)
	if conn == nil {
		return fmt.Errorf("Connection not available")
	}
	defer db.pool.Put(conn)

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

	if val, ok := optBool(refreshFields, "password"); ok {
		querySql += ", password=?"
		params = append(params, val)
	}

	if val, ok := optBool(refreshFields, "nsfm"); ok {
		querySql += ", nsfm=?"
		params = append(params, val)
	}

	if val, ok := optInt(refreshFields, "maxusers"); ok {
		querySql += ", max_users=?"
		params = append(params, val)
	}

	if val, ok := optBool(refreshFields, "closed"); ok {
		querySql += ", closed=?"
		params = append(params, val)
	}

	var activeDrawingUsers int
	if val, ok := optInt(refreshFields, "activedrawingusers"); ok {
		activeDrawingUsers = val
	} else {
		activeDrawingUsers = -1
	}
	querySql += ", active_drawing_users=?"
	params = append(params, activeDrawingUsers)

	if val, ok := optBool(refreshFields, "allowweb"); ok {
		querySql += ", allow_web=?"
		params = append(params, val)
	}

	querySql += `
		WHERE id = ?
		AND update_key = ?
		AND COALESCE(unlist_reason, '') = ''
		AND last_active >= DATETIME('now', ?)
	`
	params = append(params, listingId, updateKey, db.timeoutString)

	updateStmt := conn.Prep(querySql)
	defer updateStmt.Reset()
	for i, v := range params {
		switch val := v.(type) {
		case string:
			updateStmt.BindText(i+1, val)
		case int:
			updateStmt.BindInt64(i+1, int64(val))
		case int64:
			updateStmt.BindInt64(i+1, val)
		case bool:
			updateStmt.BindBool(i+1, val)
		default:
			panic("Unhandled type")
		}
	}

	// Execute update
	_, err := updateStmt.Step()
	if err != nil {
		return err
	}

	// If we actually executed an update, we're done
	if conn.Changes() > 0 {
		return nil
	}

	// We didn't update anything, figure out what the error was.
	selectStmt := conn.Prep(`
		SELECT update_key, unlisted, unlist_reason
		FROM sessions
		WHERE id = $id
	`)
	defer selectStmt.Reset()

	selectStmt.SetInt64("$id", listingId)

	if hasRow, err := selectStmt.Step(); err != nil {
		return err
	} else if !hasRow {
		return RefreshError{"no such session"}
	} else if selectStmt.GetText("update_key") != updateKey {
		return RefreshError{"invalid session key"}
	} else if selectStmt.GetInt64("unlisted") != 0 {
		reason := selectStmt.GetText("unlist_reason")
		if reason == "" {
			return RefreshError{"already unlisted"}
		} else {
			return RefreshError{reason}
		}
	} else {
		return RefreshError{"timed out"} // (Probably.)
	}
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
	WHERE id=$id AND update_key=$key AND unlisted=false`)

	stmt.SetInt64("$id", listingId)
	stmt.SetText("$key", updateKey)

	if _, err := stmt.Step(); err != nil {
		return false, err
	}

	return conn.Changes() > 0, nil
}

func (db *sqliteDb) AdminUpdateSessions(ids []int64, unlisted bool, unlistReason string, ctx context.Context) ([]int64, error) {
	conn := db.pool.Get(ctx)
	if conn == nil {
		return []int64{}, fmt.Errorf("Connection not available")
	}
	defer db.pool.Put(conn)

	var stmt *sqlite.Stmt
	stmt = conn.Prep(`UPDATE sessions SET unlisted = $unlisted, unlist_reason = $reason WHERE id = $id`)
	stmt.SetBool("$unlisted", unlisted)
	stmt.SetText("$reason", unlistReason)

	changedIds := make([]int64, len(ids))
	handledIds := map[int64]bool{}
	for _, id := range ids {
		if !handledIds[id] {
			stmt.Reset()
			stmt.SetInt64("$id", id)
			if _, err := stmt.Step(); err != nil {
				return changedIds, err
			} else if conn.Changes() > 0 {
				changedIds = append(changedIds, id)
			}
			handledIds[id] = true
		}
	}
	return changedIds, nil
}

func (db *sqliteDb) AdminQuerySessions(ctx context.Context) ([]AdminSession, error) {
	conn := db.pool.Get(ctx)
	if conn == nil {
		return []AdminSession{}, fmt.Errorf("Connection not available")
	}
	defer db.pool.Put(conn)

	stmt := conn.Prep(`
		SELECT id, host, port, session_id, protocol, title, users,
			password, nsfm, owner, started, last_active, unlisted, update_key,
			client_ip, unlist_reason, max_users, closed,
			last_active < DATETIME('now', $timeout) AS timed_out,
			unlist_reason IS NOT NULL as kicked, active_drawing_users, allow_web
		FROM sessions
		ORDER BY host, id
	`)
	stmt.SetText("$timeout", db.timeoutString)

	sessions := []AdminSession{}
	for {
		if hasRow, err := stmt.Step(); err != nil {
			return sessions, err
		} else if !hasRow {
			break
		}

		unlisted := stmt.GetInt64("unlisted") != 0
		timedOut := stmt.GetInt64("timed_out") != 0
		kicked := stmt.GetInt64("kicked") != 0
		var unlistReason string
		if unlisted {
			if kicked {
				unlistReason = stmt.GetText("unlist_reason")
			} else {
				unlistReason = "unlisted by owner"
			}
		} else if timedOut {
			unlistReason = "timed out"
		}

		sessions = append(sessions, AdminSession{
			Id:                 stmt.GetInt64("id"),
			Host:               stmt.GetText("host"),
			Port:               int(stmt.GetInt64("port")),
			SessionId:          stmt.GetText("session_id"),
			Protocol:           stmt.GetText("protocol"),
			Title:              stmt.GetText("title"),
			Users:              int(stmt.GetInt64("users")),
			Usernames:          []string{},
			Password:           stmt.GetInt64("password") != 0,
			Nsfm:               stmt.GetInt64("nsfm") != 0,
			Owner:              stmt.GetText("owner"),
			Started:            stmt.GetText("started"),
			LastActive:         stmt.GetText("last_active"),
			Unlisted:           unlisted || timedOut,
			UpdateKey:          stmt.GetText("update_key"),
			ClientIp:           stmt.GetText("client_ip"),
			UnlistReason:       unlistReason,
			MaxUsers:           int(stmt.GetInt64("max_users")),
			Closed:             stmt.GetInt64("closed") != 0,
			Kicked:             kicked,
			TimedOut:           timedOut,
			ActiveDrawingUsers: int(stmt.GetInt64("active_drawing_users")),
			AllowWeb:           stmt.GetInt64("allow_web") != 0,
		})
	}

	return sessions, nil
}

func (db *sqliteDb) AdminCreateHostBan(host string, expires string, notes string, ctx context.Context) (int64, error) {
	conn := db.pool.Get(ctx)
	if conn == nil {
		return 0, fmt.Errorf("Connection not available")
	}
	defer db.pool.Put(conn)

	var stmt *sqlite.Stmt
	if expires == "" {
		stmt = conn.Prep(`INSERT INTO hostbans (host, expires, notes) VALUES ($host, NULL, $notes)`)
	} else {
		stmt = conn.Prep(`INSERT INTO hostbans (host, expires, notes) VALUES ($host, DATETIME($expires), $notes)`)
		stmt.SetText("$expires", expires)
	}
	stmt.SetText("$host", host)
	stmt.SetText("$notes", notes)

	if _, err := stmt.Step(); err != nil {
		return 0, err
	} else {
		return conn.LastInsertRowID(), nil
	}
}

func (db *sqliteDb) AdminUpdateHostBan(id int64, host string, expires string, notes string, ctx context.Context) (bool, error) {
	conn := db.pool.Get(ctx)
	if conn == nil {
		return false, fmt.Errorf("Connection not available")
	}
	defer db.pool.Put(conn)

	var stmt *sqlite.Stmt
	if expires == "" {
		stmt = conn.Prep(`UPDATE hostbans SET host = $host, expires = NULL, notes = $notes WHERE id = $id`)
	} else {
		stmt = conn.Prep(`UPDATE hostbans SET host = $host, expires = $expires, notes = $notes WHERE id = $id`)
		stmt.SetText("$expires", expires)
	}
	stmt.SetText("$host", host)
	stmt.SetText("$notes", notes)
	stmt.SetInt64("$id", id)

	if _, err := stmt.Step(); err != nil {
		return false, err
	} else {
		return conn.Changes() > 0, nil
	}
}

func (db *sqliteDb) AdminDeleteHostBan(id int64, ctx context.Context) (bool, error) {
	conn := db.pool.Get(ctx)
	if conn == nil {
		return false, fmt.Errorf("Connection not available")
	}
	defer db.pool.Put(conn)

	stmt := conn.Prep(`DELETE FROM hostbans WHERE id = ?`)
	stmt.BindInt64(1, id)

	if _, err := stmt.Step(); err != nil {
		return false, err
	} else {
		return conn.Changes() > 0, nil
	}
}

func (db *sqliteDb) AdminQueryHostBans(ctx context.Context) ([]AdminHostBan, error) {
	conn := db.pool.Get(ctx)
	if conn == nil {
		return []AdminHostBan{}, fmt.Errorf("Connection not available")
	}
	defer db.pool.Put(conn)

	stmt := conn.Prep(`
		SELECT id, host, expires, expires IS NULL OR expires > DATETIME('now') AS active, notes
		FROM hostbans
		ORDER BY id DESC
	`)

	hostBans := []AdminHostBan{}
	for {
		if hasRow, err := stmt.Step(); err != nil {
			return hostBans, err
		} else if !hasRow {
			break
		}

		hostBans = append(hostBans, AdminHostBan{
			Id:      stmt.GetInt64("id"),
			Host:    stmt.GetText("host"),
			Expires: stmt.GetText("expires"),
			Active:  stmt.GetInt64("active") != 0,
			Notes:   stmt.GetText("notes"),
		})
	}

	return hostBans, nil
}

func (db *sqliteDb) AdminCreateRole(
	name string, admin bool, accessSessions int64, accessHostbans int64,
	accessRoles int64, accessUsers int64, ctx context.Context) (int64, error) {

	conn := db.pool.Get(ctx)
	if conn == nil {
		return 0, fmt.Errorf("Connection not available")
	}
	defer db.pool.Put(conn)

	var stmt *sqlite.Stmt = conn.Prep(`
		INSERT INTO roles (
			name, admin, access_sessions, access_hostbans, access_roles, access_users)
		VALUES ($name, $admin, $sessions, $hostbans, $roles, $users)
	`)
	stmt.SetText("$name", name)
	stmt.SetBool("$admin", admin)
	stmt.SetInt64("$sessions", accessSessions)
	stmt.SetInt64("$hostbans", accessHostbans)
	stmt.SetInt64("$roles", accessRoles)
	stmt.SetInt64("$users", accessUsers)

	if _, err := stmt.Step(); err != nil {
		return 0, err
	} else {
		return conn.LastInsertRowID(), nil
	}
}

func (db *sqliteDb) AdminUpdateRole(
	id int64, name string, admin bool, accessSessions int64, accessHostbans int64,
	accessRoles int64, accessUsers int64, ctx context.Context) (bool, error) {

	conn := db.pool.Get(ctx)
	if conn == nil {
		return false, fmt.Errorf("Connection not available")
	}
	defer db.pool.Put(conn)

	var stmt *sqlite.Stmt = conn.Prep(`
		UPDATE roles SET name = $name, admin = $admin,
			access_sessions = $sessions, access_hostbans = $hostbans,
			access_roles = $roles, access_users = $users
		WHERE id = $id
	`)
	stmt.SetText("$name", name)
	stmt.SetBool("$admin", admin)
	stmt.SetInt64("$sessions", accessSessions)
	stmt.SetInt64("$hostbans", accessHostbans)
	stmt.SetInt64("$roles", accessRoles)
	stmt.SetInt64("$users", accessUsers)
	stmt.SetInt64("$id", id)

	if _, err := stmt.Step(); err != nil {
		return false, err
	} else {
		return conn.Changes() > 0, nil
	}
}

func (db *sqliteDb) AdminDeleteRole(id int64, ctx context.Context) (bool, error) {
	conn := db.pool.Get(ctx)
	if conn == nil {
		return false, fmt.Errorf("Connection not available")
	}
	defer db.pool.Put(conn)

	var stmt *sqlite.Stmt = conn.Prep(`DELETE FROM roles WHERE id = $id`)
	stmt.SetInt64("$id", id)

	if _, err := stmt.Step(); err != nil {
		return false, err
	} else {
		return conn.Changes() > 0, nil
	}
}

func (db *sqliteDb) AdminQueryRoles(ctx context.Context) ([]AdminRole, error) {
	conn := db.pool.Get(ctx)
	if conn == nil {
		return []AdminRole{}, fmt.Errorf("Connection not available")
	}
	defer db.pool.Put(conn)

	stmt := conn.Prep(`
		SELECT
			r.id, r.name, r.admin, r.access_sessions, r.access_hostbans,
			r.access_roles, r.access_users,
			(SELECT EXISTS (SELECT 1 FROM users u WHERE u.role = r.id)) AS used
		FROM roles r
		ORDER BY name
	`)

	roles := []AdminRole{}
	for {
		if hasRow, err := stmt.Step(); err != nil {
			return roles, err
		} else if !hasRow {
			break
		}

		roles = append(roles, AdminRole{
			Id:             stmt.GetInt64("id"),
			Name:           stmt.GetText("name"),
			Admin:          stmt.GetInt64("admin") != 0,
			AccessSessions: int(stmt.GetInt64("access_sessions")),
			AccessHostBans: int(stmt.GetInt64("access_hostbans")),
			AccessRoles:    int(stmt.GetInt64("access_roles")),
			AccessUsers:    int(stmt.GetInt64("access_users")),
			Used:           stmt.GetInt64("used") != 0,
		})
	}

	return roles, nil
}

func (db *sqliteDb) AdminQueryRoleByName(name string, ctx context.Context) (AdminRole, error) {
	conn := db.pool.Get(ctx)
	if conn == nil {
		return AdminRole{}, fmt.Errorf("Connection not available")
	}
	defer db.pool.Put(conn)

	stmt := conn.Prep(`
		SELECT
			r.id, r.name, r.admin, r.access_sessions, r.access_hostbans,
			r.access_roles, r.access_users,
			(SELECT EXISTS (SELECT 1 FROM users u WHERE u.role = r.id)) AS used
		FROM roles r
		WHERE r.name = $name
	`)
	defer stmt.Reset()
	stmt.SetText("$name", name)

	if hasRow, err := stmt.Step(); err != nil {
		return AdminRole{}, err
	} else if !hasRow {
		return AdminRole{}, nil
	}

	return AdminRole{
		Id:             stmt.GetInt64("id"),
		Name:           stmt.GetText("name"),
		Admin:          stmt.GetInt64("admin") != 0,
		AccessSessions: int(stmt.GetInt64("access_sessions")),
		AccessHostBans: int(stmt.GetInt64("access_hostbans")),
		AccessRoles:    int(stmt.GetInt64("access_roles")),
		AccessUsers:    int(stmt.GetInt64("access_users")),
		Used:           stmt.GetInt64("used") != 0,
	}, nil
}

func (db *sqliteDb) AdminCreateUser(name string, passwordHash string, role int64, ctx context.Context) (int64, error) {
	conn := db.pool.Get(ctx)
	if conn == nil {
		return 0, fmt.Errorf("Connection not available")
	}
	defer db.pool.Put(conn)

	var stmt *sqlite.Stmt = conn.Prep(`
		INSERT INTO users (name, password_hash, role)
		VALUES ($name, $hash, $role)
	`)
	stmt.SetText("$name", name)
	stmt.SetText("$hash", passwordHash)
	stmt.SetInt64("$role", role)

	if _, err := stmt.Step(); err != nil {
		return 0, err
	} else {
		return conn.LastInsertRowID(), nil
	}
}

func (db *sqliteDb) AdminUpdateUser(id int64, name string, passwordHash string, role int64, ctx context.Context) (bool, error) {
	conn := db.pool.Get(ctx)
	if conn == nil {
		return false, fmt.Errorf("Connection not available")
	}
	defer db.pool.Put(conn)

	sql := `UPDATE users SET name = $name, role = $role`
	if passwordHash != "" {
		sql += `, password_hash = $hash`
	}
	sql += ` WHERE id = $id`

	var stmt *sqlite.Stmt = conn.Prep(sql)
	stmt.SetText("$name", name)
	stmt.SetInt64("$role", role)
	stmt.SetInt64("$id", id)
	if passwordHash != "" {
		stmt.SetText("$hash", passwordHash)
	}

	if _, err := stmt.Step(); err != nil {
		return false, err
	} else {
		return conn.Changes() > 0, nil
	}
}

func (db *sqliteDb) AdminUpdateUserPassword(id int64, passwordHash string, ctx context.Context) (bool, error) {
	conn := db.pool.Get(ctx)
	if conn == nil {
		return false, fmt.Errorf("Connection not available")
	}
	defer db.pool.Put(conn)

	var stmt *sqlite.Stmt = conn.Prep(
		`UPDATE users SET password_hash = $hash WHERE id = $id`)
	stmt.SetText("$hash", passwordHash)
	stmt.SetInt64("$id", id)

	if _, err := stmt.Step(); err != nil {
		return false, err
	} else {
		return conn.Changes() > 0, nil
	}
}

func (db *sqliteDb) AdminDeleteUser(id int64, ctx context.Context) (bool, error) {
	conn := db.pool.Get(ctx)
	if conn == nil {
		return false, fmt.Errorf("Connection not available")
	}
	defer db.pool.Put(conn)

	var stmt *sqlite.Stmt = conn.Prep(
		`DELETE FROM users WHERE id = $id`)
	stmt.SetInt64("$id", id)

	if _, err := stmt.Step(); err != nil {
		return false, err
	} else {
		return conn.Changes() > 0, nil
	}
}

func (db *sqliteDb) AdminQueryUserByName(name string, ctx context.Context) (AdminUserDetail, error) {
	conn := db.pool.Get(ctx)
	if conn == nil {
		return AdminUserDetail{}, fmt.Errorf("Connection not available")
	}
	defer db.pool.Put(conn)

	stmt := conn.Prep(`
		SELECT
			u.id as user_id, u.name as user_name, r.id as role_id,
			r.name as role_name, r.admin, r.access_sessions, r.access_hostbans,
			r.access_roles, r.access_users, u.password_hash
		FROM users u
		JOIN roles r ON r.id = u.role
		WHERE u.name = $name
	`)
	defer stmt.Reset()
	stmt.SetText("$name", name)

	if hasRow, err := stmt.Step(); err != nil {
		return AdminUserDetail{}, err
	} else if hasRow {
		user := AdminUserDetail{
			Id:   stmt.GetInt64("user_id"),
			Name: stmt.GetText("user_name"),
			Role: AdminRole{
				Id:             stmt.GetInt64("role_id"),
				Name:           stmt.GetText("role_name"),
				Admin:          stmt.GetInt64("admin") != 0,
				AccessSessions: int(stmt.GetInt64("access_sessions")),
				AccessHostBans: int(stmt.GetInt64("access_hostbans")),
				AccessRoles:    int(stmt.GetInt64("access_roles")),
				AccessUsers:    int(stmt.GetInt64("access_users")),
			},
			PasswordHash: stmt.GetText("password_hash"),
		}
		return user, nil
	} else {
		return AdminUserDetail{Id: 0}, nil
	}
}

func (db *sqliteDb) AdminQueryUsers(ctx context.Context) ([]AdminUser, error) {
	conn := db.pool.Get(ctx)
	if conn == nil {
		return []AdminUser{}, fmt.Errorf("Connection not available")
	}
	defer db.pool.Put(conn)

	stmt := conn.Prep(`
		SELECT
			u.id as user_id, u.name as user_name, r.name as role_name
		FROM users u
		JOIN roles r ON r.id = u.role
		ORDER BY u.name
	`)

	users := []AdminUser{}
	for {
		if hasRow, err := stmt.Step(); err != nil {
			return users, err
		} else if !hasRow {
			break
		}

		users = append(users, AdminUser{
			Id:   stmt.GetInt64("user_id"),
			Name: stmt.GetText("user_name"),
			Role: stmt.GetText("role_name"),
		})
	}

	return users, nil
}

func (db *sqliteDb) Close() error {
	return db.pool.Close()
}
