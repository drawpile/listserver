package db

import (
	"context"
	"log"
)

type Database interface {
	SessionTimeoutMinutes() int
	QuerySessionList(opts QueryOptions, ctx context.Context) ([]SessionInfo, error)
	QuerySessionByRoomcode(roomcode string, ctx context.Context) (JoinSessionInfo, error)
	IsActiveSession(host, id string, port int, ctx context.Context) (bool, error)
	GetHostSessionCount(host string, ctx context.Context) (int, error)
	IsBannedHost(host string, ctx context.Context) (bool, error)
	InsertSession(session SessionInfo, clientIp string, ctx context.Context) (NewSessionInfo, error)
	AssignRoomCode(listingId int64, ctx context.Context) (string, error)
	RefreshSession(refreshFields map[string]interface{}, listingId int64, updateKey string, ctx context.Context) error
	DeleteSession(listingId int64, updateKey string, ctx context.Context) (bool, error)
}

func InitDatabase(dbname string, sessionTimeout int) Database {
	if len(dbname) == 0 {
		log.Println("No datatabase used")
		return nil
	}

	if sessionTimeout < 2 {
		log.Fatal("Session timeout should be at least 2 minutes")
		return nil
	}

	log.Println("Using database:", dbname)
	db, err := newSqliteDb(dbname, sessionTimeout)
	if err != nil {
		log.Fatal(err)
		return nil
	}

	return db
}
