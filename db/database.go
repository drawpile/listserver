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
	AdminUpdateSessions(ids []int64, unlisted bool, unlistReason string, ctx context.Context) ([]int64, error)
	AdminQuerySessions(ctx context.Context) ([]AdminSession, error)
	AdminCreateHostBan(host string, expires string, notes string, ctx context.Context) (int64, error)
	AdminUpdateHostBan(id int64, host string, expires string, notes string, ctx context.Context) (bool, error)
	AdminDeleteHostBan(id int64, ctx context.Context) (bool, error)
	AdminQueryHostBans(ctx context.Context) ([]AdminHostBan, error)
	AdminCreateRole(name string, admin bool, accessSessions int64, accessHostbans int64,
		accessRoles int64, accessUsers int64, ctx context.Context) (int64, error)
	AdminUpdateRole(id int64, name string, admin bool, accessSessions int64, accessHostbans int64,
		accessRoles int64, accessUsers int64, ctx context.Context) (bool, error)
	AdminDeleteRole(id int64, ctx context.Context) (bool, error)
	AdminQueryRoles(ctx context.Context) ([]AdminRole, error)
	AdminQueryRoleByName(name string, ctx context.Context) (AdminRole, error)
	AdminCreateUser(name string, passwordHash string, role int64, ctx context.Context) (int64, error)
	AdminUpdateUser(id int64, name string, paswordHash string, role int64, ctx context.Context) (bool, error)
	AdminUpdateUserPassword(id int64, passwordHash string, ctx context.Context) (bool, error)
	AdminDeleteUser(id int64, ctx context.Context) (bool, error)
	AdminQueryUserByName(name string, ctx context.Context) (AdminUserDetail, error)
	AdminQueryUsers(ctx context.Context) ([]AdminUser, error)
	Close() error
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
