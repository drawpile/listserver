package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"github.com/lib/pq"
	"fmt"
	"log"
	"strings"
	"errors"
)

const SessionTimeout = 10
const SessionTimeoutString = "'10 minutes'"

type QueryOptions struct {
	Title    string // filter by title
	Nsfm     bool   // show NSFM sessions
	Protocol string // filter by protocol version (comma separated list accepted)
}

// Get a list of sessions that match the given query parameters
// Parameters:
func QuerySessionList(opts QueryOptions, db *sql.DB) ([]SessionInfo, error) {
	querySql := `
	SELECT host, port, session_id, coalesce(roomcode, '') as roomcode, protocol, title, users, usernames, password, nsfm, owner,
	to_char(started at time zone 'UTC', 'YYYY-MM-DD HH24:MI:ss') as started
	FROM sessions
	WHERE last_active >= current_timestamp - $1::interval AND unlisted=false AND private=false
	`

	params := []interface{}{SessionTimeoutString}

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
	rows, err := db.Query(querySql, params...)

	if err != nil {
		log.Println("Sesion list query error:", err)
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
			log.Println("Session list row error:", err)
			return []SessionInfo{}, err
		}

		sessions = append(sessions, ses)
	}

	return sessions, nil
}

// Get the session matching the given room code (if it is still active)
func QuerySessionByRoomcode(roomcode string, db *sql.DB) (JoinSessionInfo, error) {
	querySql := `
	SELECT host, port, session_id
	FROM sessions
	WHERE last_active >= current_timestamp - $1::interval AND unlisted=false
	AND roomcode=$2
	`

	ses := JoinSessionInfo{}
	rows, err := db.Query(querySql, SessionTimeoutString, roomcode)

	if err != nil {
		log.Println("Sesion (roomcode) query error:", err)
		return ses, err
	}
	defer rows.Close()

	if !rows.Next() {
		// Not found
		return ses, nil
	}

	err = rows.Scan(&ses.Host, &ses.Port, &ses.Id)
	if err != nil {
		log.Println("Session (roomcode) row error:", err)
		return JoinSessionInfo{}, err
	}

	return ses, nil
}

// Is there an active announcement for this session
func IsActiveSession(host, id string, port int, db *sql.DB) (bool, error) {
	querySql := `
	SELECT exists(SELECT 1
	FROM sessions
	WHERE host=$1 AND port=$2 AND session_id=$3 AND last_active >= current_timestamp - $4::interval
	AND unlisted=false
	)`

	params := []interface{}{host, port, id, SessionTimeoutString}

	var exists bool
	if err := db.QueryRow(querySql, params...).Scan(&exists); err != nil {
		log.Println("Active session query error:", err)
		return false, err
	}

	return exists, nil
}

// Get the number of active announcements on this server (all ports)
func GetHostSessionCount(host string, db *sql.DB) (int, error) {
	querySql := `
	SELECT COUNT(*) FROM sessions
	WHERE host=$1 AND last_active >= current_timestamp - $2::interval AND unlisted=false
	`

	var count int
	if err := db.QueryRow(querySql, host, SessionTimeoutString).Scan(&count); err != nil {
		log.Println("Session count query error:", err)
		return 0, err
	}

	return count, nil
}

// Generate a secure random string
func generateUpdateKey() (string, error) {
	keybytes := make([]byte, 128/8)
	_, err := rand.Read(keybytes)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(keybytes), nil
}

func generateRoomcode(length int) (string, error) {
	randbytes := make([]byte, length)
	_, err := rand.Read(randbytes)
	if err != nil {
		return "", err
	}
	code := make([]rune, length)
	for i := range randbytes {
		code[i] = rune(randbytes[i] % 26) + 'A'
	}
	return string(code), nil
}

// Insert a new session to the database
// Note: this function does not validate the data;
// that must be done before calling this
func InsertSession(session SessionInfo, clientIp string, db *sql.DB) (NewSessionInfo, error) {
	var updateKey string
	updateKey, err := generateUpdateKey()
	if err != nil {
		log.Println("Secure random generator failed:", err)
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
	err = db.QueryRow(insertSql,
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
		log.Println("Session insert error:", err)
		return NewSessionInfo{}, err
	}

	return NewSessionInfo{listingId, updateKey, session.Private, ""}, nil
}

func AssignRoomcode(listingId int, db *sql.DB) (string, error) {
	for retry := 0; retry < 10; retry += 1 {
		roomcode, err := generateRoomcode(5)
		if err != nil {
			return "", err
		}
		_, err = db.Exec("UPDATE sessions SET roomcode=$1 WHERE id=$2", roomcode, listingId)
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

type RefreshError struct {
	message string
}

func (e RefreshError) Error() string {
	return e.message
}

func optStringList(fields map[string]interface{}, name string) (string, bool) {
	if value, ok := fields[name]; ok {
		val, ok := value.([]string)
		return strings.Join(val, ", "), ok
	}
	return "", false
}
func optString(fields map[string]interface{}, name string) (string, bool) {
	if value, ok := fields[name]; ok {
		val, ok := value.(string)
		return val, ok
	}
	return "", false
}

func optInt(fields map[string]interface{}, name string) (int, bool) {
	if value, ok := fields[name]; ok {
		val, ok := value.(float64)
		return int(val), ok
	}
	return 0, false
}

func optBool(fields map[string]interface{}, name string) (bool, bool) {
	if value, ok := fields[name]; ok {
		val, ok := value.(bool)
		return val, ok
	}
	return false, false
}

// Refresh an announcement
func RefreshSession(refreshFields map[string]interface{}, listingId int, updateKey string, db *sql.DB) error {
	// First, make sure an active listing exists
	querySql := `
	SELECT exists(SELECT 1
	FROM sessions
	WHERE id=$1 AND update_key=$2 AND last_active >= current_timestamp - $3::interval
	AND unlisted=false
	)`

	params := []interface{}{listingId, updateKey, SessionTimeoutString}

	var exists bool
	if err := db.QueryRow(querySql, params...).Scan(&exists); err != nil {
		log.Println("Session refresh query error:", err)
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
		params = append(params, val)
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
	_, err := db.Exec(querySql, params...)
	if err != nil {
		log.Println("Session refresh error:", err)
	}
	return err
}

// Delete an announcement
func DeleteSession(listingId int, updateKey string, db *sql.DB) (bool, error) {
	querySql := `
	UPDATE sessions
	SET unlisted=true
	WHERE id=$1 AND update_key=$2 AND unlisted=false`

	res, err := db.Exec(querySql, listingId, updateKey)
	if err != nil {
		log.Println("Session delete error:", err)
		return false, err
	}

	rows, err := res.RowsAffected()
	if err != nil {
		log.Println("Session delete error:", err)
		return false, err
	}
	return rows > 0, nil
}
