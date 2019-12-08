package db

import (
	"log"
	"strings"
)

type Database interface {
	init(name string, sessionTimeout int) error

	SessionTimeoutMinutes() int
	QuerySessionList(opts QueryOptions) ([]SessionInfo, error)
	QuerySessionByRoomcode(roomcode string) (JoinSessionInfo, error)
	IsActiveSession(host, id string, port int) (bool, error)
	GetHostSessionCount(host string) (int, error)
	IsBannedHost(host string) (bool, error)
	InsertSession(session SessionInfo, clientIp string) (NewSessionInfo, error)
	AssignRoomcode(listingId int) (string, error)
	RefreshSession(refreshFields map[string]interface{}, listingId int, updateKey string) error
	DeleteSession(listingId int, updateKey string) (bool, error)
}

func InitDatabase(dbname string, sessionTimeout int) Database {
	if len(dbname) == 0 || dbname == "none" {
		return nil
	}

	if sessionTimeout < 2 {
		log.Fatal("Session timeout should be at least 2 minutes")
		return nil
	}

	var db Database
	if strings.HasPrefix(dbname, "postgres:") {
		db = &postgresDb{}
	} else if dbname == "memory" {
		db = &memoryDb{}
	} else {
		log.Fatal("Unsupported database type: " + dbname)
		return nil
	}

	if err := db.init(dbname, sessionTimeout); err != nil {
		log.Fatal(err)
		return nil
	}

	return db
}
