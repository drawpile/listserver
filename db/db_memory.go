package db

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

type session struct {
	info      SessionInfo
	id        int
	expires   time.Time
	updateKey string
}

func (s *session) isActive() bool {
	return s.expires.After(time.Now())
}

type memoryDb struct {
	mutex          sync.Mutex
	sessions       []session
	lastId         int
	sessionTimeout time.Duration
}

func (db *memoryDb) init(name string, sessionTimeout int) error {
	db.sessionTimeout = time.Duration(sessionTimeout) * time.Minute

	// Note: this never ends, but since we only create one
	// db object per application instance and it lasts the
	// whole process lifetime, it's OK.
	go cleanupRoutine(db)

	return nil
}

func (db *memoryDb) SessionTimeoutMinutes() int {
	return int(db.sessionTimeout / time.Minute)
}

func cleanupRoutine(db *memoryDb) {
	for {
		time.Sleep(15 * time.Minute)
		db.mutex.Lock()

		count := len(db.sessions)
		for i := 0; i < count; i += 1 {
			if !db.sessions[i].isActive() {
				db.sessions[i] = db.sessions[count-1]
				count -= 1
			}
		}

		if count != len(db.sessions) {
			db.sessions = db.sessions[:count]
		}
		db.mutex.Unlock()
	}
}

func matchTitle(title, substring string) bool {
	if substring == "" {
		return true
	}

	return strings.Contains(strings.ToLower(title), strings.ToLower(substring))
}

func matchProtocol(protocol string, protocols []string) bool {
	if len(protocols) == 0 || (len(protocols) == 1 && protocols[0] == "") {
		return true
	}

	for _, p := range protocols {
		if protocol == p {
			return true
		}
	}

	return false
}

// Get a list of sessions that match the given query parameters
func (db *memoryDb) QuerySessionList(opts QueryOptions) ([]SessionInfo, error) {
	sessions := []SessionInfo{}
	protocols := strings.Split(opts.Protocol, ",")

	db.mutex.Lock()
	for _, session := range db.sessions {
		if !session.info.Private && session.isActive() &&
			matchTitle(session.info.Title, opts.Title) &&
			(opts.Nsfm || !session.info.Nsfm) &&
			matchProtocol(session.info.Protocol, protocols) {
			sessions = append(sessions, session.info)
		}
	}
	db.mutex.Unlock()

	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].Title < sessions[j].Title {
			return true
		}
		return sessions[i].Users < sessions[j].Users
	})

	return sessions, nil
}

// Get the session matching the given room code (if it is still active)
func (db *memoryDb) QuerySessionByRoomcode(roomcode string) (JoinSessionInfo, error) {
	db.mutex.Lock()
	defer db.mutex.Unlock()
	for i := range db.sessions {
		ses := &db.sessions[i]
		if ses.isActive() && ses.info.Roomcode == roomcode {
			return JoinSessionInfo{
				ses.info.Host,
				ses.info.Port,
				ses.info.Id,
			}, nil
		}
	}
	return JoinSessionInfo{}, nil
}

// Is there an active announcement for this session
func (db *memoryDb) IsActiveSession(host, id string, port int) (bool, error) {
	db.mutex.Lock()
	defer db.mutex.Unlock()
	for _, s := range db.sessions {
		if s.info.Host == host && s.info.Id == id && s.info.Port == port && s.isActive() {
			return true, nil
		}
	}
	return false, nil
}

// Get the number of active announcements on this server (all ports)
func (db *memoryDb) GetHostSessionCount(host string) (int, error) {
	db.mutex.Lock()
	defer db.mutex.Unlock()

	count := 0
	for _, s := range db.sessions {
		if s.info.Host == host && s.isActive() {
			count += 1
		}
	}

	return count, nil
}

// Check if the given host is on the ban list
func (db *memoryDb) IsBannedHost(host string) (bool, error) {
	// TODO use configuration file?
	return false, nil
}

// Insert a new session to the database
// Note: this function does not validate the data;
// that must be done before calling this
func (db *memoryDb) InsertSession(sessionInfo SessionInfo, clientIp string) (NewSessionInfo, error) {
	updateKey, err := generateUpdateKey()
	if err != nil {
		return NewSessionInfo{}, err
	}

	db.lastId += 1
	listingId := db.lastId

	db.mutex.Lock()
	db.sessions = append(db.sessions, session{
		info:      sessionInfo,
		id:        listingId,
		expires:   time.Now().Add(db.sessionTimeout),
		updateKey: updateKey,
	})
	db.mutex.Unlock()

	return NewSessionInfo{listingId, updateKey, sessionInfo.Private, ""}, nil
}

func (db *memoryDb) AssignRoomcode(listingId int) (string, error) {
	db.mutex.Lock()
	defer db.mutex.Unlock()

RETRY:
	for retry := 0; retry < 10; retry += 1 {
		roomcode, err := generateRoomcode(5)
		if err != nil {
			return "", err
		}

		for i := range db.sessions {
			if db.sessions[i].info.Roomcode == roomcode {
				// Very unlikely
				continue RETRY
			}
		}

		for i := range db.sessions {
			if db.sessions[i].id == listingId {
				db.sessions[i].info.Roomcode = roomcode
				return roomcode, nil
			}
		}
	}

	return "", errors.New("Couldn't generate a unique room code")
}

// Refresh an announcement
func (db *memoryDb) RefreshSession(refreshFields map[string]interface{}, listingId int, updateKey string) error {
	db.mutex.Lock()
	defer db.mutex.Unlock()

	var ses *session
	for i := range db.sessions {
		if db.sessions[i].id == listingId {
			ses = &db.sessions[i]
			break
		}
	}

	if ses == nil || ses.updateKey != updateKey {
		return RefreshError{"not found"}
	}

	ses.expires = time.Now().Add(db.sessionTimeout)

	if val, ok := optString(refreshFields, "title"); ok {
		ses.info.Title = val
	}

	if val, ok := optInt(refreshFields, "users"); ok {
		ses.info.Users = val
	}

	if val, ok := optStringList(refreshFields, "usernames"); ok {
		ses.info.Usernames = val
	}

	if val, ok := optBool(refreshFields, "password"); ok {
		ses.info.Password = val
	}

	if val, ok := optBool(refreshFields, "nsfm"); ok {
		ses.info.Nsfm = val
	}

	if val, ok := optBool(refreshFields, "private"); ok {
		ses.info.Private = val
	}

	return nil
}

// Delete an announcement
func (db *memoryDb) DeleteSession(listingId int, updateKey string) (bool, error) {
	db.mutex.Lock()
	defer db.mutex.Unlock()

	for i := range db.sessions {
		if db.sessions[i].id == listingId {
			ses := &db.sessions[i]
			if ses.updateKey != updateKey {
				return false, nil
			}

			// Quick delete by swapping with last item.
			// We can do this, because we don't care about the order
			// in the session list
			db.sessions[i] = db.sessions[len(db.sessions)-1]
			db.sessions = db.sessions[:len(db.sessions)-1]

			return true, nil
		}
	}

	return false, nil
}
