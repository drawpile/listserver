package db

import (
	"database/sql"
	"errors"
	"fmt"
	"github.com/lib/pq"
	"strings"
)

type postgresDb struct {
	connection     *sql.DB
	timeoutString  string
	timeoutMinutes int
}

func (db *postgresDb) init(name string, sessionTimeout int) (err error) {
	db.connection, err = sql.Open("postgres", name)

	if err != nil {
		return
	}

	db.timeoutMinutes = sessionTimeout
	db.timeoutString = fmt.Sprintf("'%d minutes'", sessionTimeout)

	err = db.connection.Ping()
	return
}

func (db *postgresDb) SessionTimeoutMinutes() int {
	return db.timeoutMinutes
}

// Get a list of sessions that match the given query parameters
func (db *postgresDb) QuerySessionList(opts QueryOptions) ([]SessionInfo, error) {
	querySql := `
	SELECT host, port, session_id, coalesce(roomcode, '') as roomcode, protocol, title, users, usernames, password, nsfm, owner,
	to_char(started at time zone 'UTC', 'YYYY-MM-DD HH24:MI:ss') as started
	FROM sessions
	WHERE last_active >= current_timestamp - $1::interval AND unlisted=false AND private=false
	`

	params := []interface{}{db.timeoutString}

	if len(opts.Title) > 0 {
		querySql += fmt.Sprintf(" AND title ILIKE '%%' || $%d || '%%'", len(params)+1)
		params = append(params, opts.Title)
	}

	if !opts.Nsfm {
		querySql += ` AND nsfm=false`
	}

	if len(opts.Protocol) > 0 {
		protocols := strings.Split(opts.Protocol, ",")
		placeholders := make([]string, len(protocols))
		for i, v := range protocols {
			placeholders[i] = fmt.Sprintf("$%d", len(params)+1)
			params = append(params, v)
		}
		querySql += ` AND protocol IN (` + strings.Join(placeholders, ",") + `)`
	}

	querySql += ` ORDER BY title, users ASC`
	rows, err := db.connection.Query(querySql, params...)

	if err != nil {
		return []SessionInfo{}, err
	}
	defer rows.Close()

	sessions := []SessionInfo{}

	for rows.Next() {
		ses := SessionInfo{}
		var usernames string
		err := rows.Scan(&ses.Host, &ses.Port, &ses.Id, &ses.Roomcode, &ses.Protocol, &ses.Title, &ses.Users, &usernames, &ses.Password, &ses.Nsfm, &ses.Owner, &ses.Started)
		if len(usernames) > 0 {
			ses.Usernames = strings.Split(usernames, ",") // TODO user array type when pq driver supports it
		} else {
			ses.Usernames = []string{}
		}

		if err != nil {
			return []SessionInfo{}, err
		}

		sessions = append(sessions, ses)
	}

	return sessions, nil
}

// Get the session matching the given room code (if it is still active)
func (db *postgresDb) QuerySessionByRoomcode(roomcode string) (JoinSessionInfo, error) {
	querySql := `
	SELECT host, port, session_id
	FROM sessions
	WHERE last_active >= current_timestamp - $1::interval AND unlisted=false
	AND roomcode=$2
	`

	ses := JoinSessionInfo{}
	rows, err := db.connection.Query(querySql, db.timeoutString, roomcode)

	if err != nil {
		return ses, err
	}
	defer rows.Close()

	if !rows.Next() {
		// Not found
		return ses, nil
	}

	err = rows.Scan(&ses.Host, &ses.Port, &ses.Id)
	if err != nil {
		return JoinSessionInfo{}, err
	}

	return ses, nil
}

// Is there an active announcement for this session
func (db *postgresDb) IsActiveSession(host, id string, port int) (bool, error) {
	querySql := `
	SELECT exists(SELECT 1
	FROM sessions
	WHERE host=$1 AND port=$2 AND session_id=$3 AND last_active >= current_timestamp - $4::interval
	AND unlisted=false
	)`

	params := []interface{}{host, port, id, db.timeoutString}

	var exists bool
	if err := db.connection.QueryRow(querySql, params...).Scan(&exists); err != nil {
		return false, err
	}

	return exists, nil
}

// Get the number of active announcements on this server (all ports)
func (db *postgresDb) GetHostSessionCount(host string) (int, error) {
	querySql := `
	SELECT COUNT(*) FROM sessions
	WHERE host=$1 AND last_active >= current_timestamp - $2::interval AND unlisted=false
	`

	var count int
	if err := db.connection.QueryRow(querySql, host, db.timeoutString).Scan(&count); err != nil {
		return 0, err
	}

	return count, nil
}

// Check if the given host is on the ban list
func (db *postgresDb) IsBannedHost(host string) (bool, error) {
	querySql := `
	SELECT EXISTS(SELECT 1
	FROM hostbans
	WHERE host=$1 AND (expires IS NULL OR expires > CURRENT_TIMESTAMP)
	)`

	var banned bool
	if err := db.connection.QueryRow(querySql, host).Scan(&banned); err != nil {
		return false, err
	}
	return banned, nil
}

// Insert a new session to the database
// Note: this function does not validate the data;
// that must be done before calling this
func (db *postgresDb) InsertSession(session SessionInfo, clientIp string) (NewSessionInfo, error) {
	var updateKey string
	updateKey, err := generateUpdateKey()
	if err != nil {
		return NewSessionInfo{}, err
	}

	insertSql := `
	INSERT INTO sessions
	(host, port, session_id, protocol, title, users, usernames, password, nsfm, private, owner, started, last_active, update_key, client_ip)
	VALUES
	($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, current_timestamp, current_timestamp, $12, $13)
	RETURNING id
	`
	var listingId int
	usernames := strings.Join(session.Usernames, ",")
	err = db.connection.QueryRow(insertSql,
		&session.Host,
		&session.Port,
		&session.Id,
		&session.Protocol,
		&session.Title,
		&session.Users,
		&usernames,
		&session.Password,
		&session.Nsfm,
		&session.Private,
		&session.Owner,
		&updateKey,
		&clientIp,
	).Scan(&listingId)

	if err != nil {
		return NewSessionInfo{}, err
	}

	return NewSessionInfo{listingId, updateKey, session.Private, ""}, nil
}

func (db *postgresDb) AssignRoomcode(listingId int) (string, error) {
	for retry := 0; retry < 10; retry += 1 {
		roomcode, err := generateRoomcode(5)
		if err != nil {
			return "", err
		}
		_, err = db.connection.Exec("UPDATE sessions SET roomcode=$1 WHERE id=$2", roomcode, listingId)
		if err != nil {
			if pgerr, ok := err.(*pq.Error); ok {
				// Unlikely, but can happen
				if pgerr.Code.Name() == "unique_violation" {
					continue
				}
			}

			return "", err

		} else {
			return roomcode, nil
		}
	}
	return "", errors.New("Couldn't generate a unique room code")
}

// Refresh an announcement
func (db *postgresDb) RefreshSession(refreshFields map[string]interface{}, listingId int, updateKey string) error {
	// First, make sure an active listing exists
	querySql := `
	SELECT exists(SELECT 1
	FROM sessions
	WHERE id=$1 AND update_key=$2 AND last_active >= current_timestamp - $3::interval
	AND unlisted=false
	)`

	params := []interface{}{listingId, updateKey, db.timeoutString}

	var exists bool
	if err := db.connection.QueryRow(querySql, params...).Scan(&exists); err != nil {
		return err
	}

	if !exists {
		return RefreshError{"not found"}
	}

	// Construct update query
	querySql = `
	UPDATE sessions
	SET last_active=current_timestamp`
	params = []interface{}{}

	if val, ok := optString(refreshFields, "title"); ok {
		querySql += fmt.Sprintf(", title=$%d", len(params)+1)
		params = append(params, val)
	}

	if val, ok := optInt(refreshFields, "users"); ok {
		querySql += fmt.Sprintf(", users=$%d", len(params)+1)
		params = append(params, val)
	}

	if val, ok := optStringList(refreshFields, "usernames"); ok {
		querySql += fmt.Sprintf(", usernames=$%d", len(params)+1)
		params = append(params, strings.Join(val, ","))
	}

	if val, ok := optBool(refreshFields, "password"); ok {
		querySql += fmt.Sprintf(", password=$%d", len(params)+1)
		params = append(params, val)
	}

	if val, ok := optBool(refreshFields, "nsfm"); ok {
		querySql += fmt.Sprintf(", nsfm=$%d", len(params)+1)
		params = append(params, val)
	}

	if val, ok := optBool(refreshFields, "private"); ok {
		querySql += fmt.Sprintf(", private=$%d", len(params)+1)
		params = append(params, val)
	}

	querySql += fmt.Sprintf(` WHERE id=$%d`, len(params)+1)
	params = append(params, listingId)

	// Execute update
	_, err := db.connection.Exec(querySql, params...)
	return err
}

// Delete an announcement
func (db *postgresDb) DeleteSession(listingId int, updateKey string) (bool, error) {
	querySql := `
	UPDATE sessions
	SET unlisted=true
	WHERE id=$1 AND update_key=$2 AND unlisted=false`

	res, err := db.connection.Exec(querySql, listingId, updateKey)
	if err != nil {
		return false, err
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}
