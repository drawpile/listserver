package db

import (
	"context"
	"reflect"
	"testing"
)

func initDb() *sqliteDb {
	db, err := newSqliteDb("memory", 5)
	if err != nil {
		panic(err)
	}
	return db
}

func insertTest(db *sqliteDb, title string, id string) NewSessionInfo {
	ses, err := db.InsertSession(
		SessionInfo{
			Host:      "example.com",
			Port:      27750,
			Id:        id,
			Protocol:  "dp:4.21.2",
			Title:     title,
			Users:     2,
			Usernames: []string{"User1", "Other one"},
			Password:  false,
			Nsfm:      false,
			Owner:     "User1",
			Started:   "",
			Roomcode:  "",
			Private:   false,
		},
		"192.168.1.1",
		context.TODO(),
	)

	if err != nil {
		panic(err)
	}

	return ses
}

func TestInsert(t *testing.T) {
	db := initDb()
	insertTest(db, "test", "demo")
}

func TestQueryList(t *testing.T) {
	db := initDb()
	sessions, err := db.QuerySessionList(QueryOptions{}, context.TODO())
	if err != nil {
		panic(err)
	}

	if len(sessions) != 0 {
		t.Fatalf("Did not receive zero listings (got %d)", len(sessions))
	}

	insertTest(db, "Test", "demo1")
	insertTest(db, "Example", "demo2")

	sessions, err = db.QuerySessionList(QueryOptions{}, context.TODO())
	if err != nil {
		panic(err)
	}

	if len(sessions) != 2 {
		t.Fatalf("Did not receive 2 listings (got %d)", len(sessions))
	}

	// Sessions are sorted by title
	if sessions[0].Title != "Example" {
		t.Fatal("First item was not Example")
	}
	if sessions[0].Id != "demo2" {
		t.Fatal("Wrong ID for first session!")
	}

	if sessions[1].Title != "Test" {
		t.Fatal("Second item was not Test")
	}

	// Filter by title
	sessions, err = db.QuerySessionList(QueryOptions{Title: "Ex"}, context.TODO())
	if err != nil {
		panic(err)
	}

	if len(sessions) != 1 {
		t.Fatalf("Received more than one listing (got %d)", len(sessions))
	}

	if sessions[0].Title != "Example" {
		t.Fatalf("Expected Example, got %s", sessions[0].Title)
	}
}

func TestQueryRoomCode(t *testing.T) {
	db := initDb()
	s1 := insertTest(db, "Test", "demo1")
	insertTest(db, "Example", "demo2")

	code, err := db.AssignRoomCode(s1.ListingId, context.TODO())
	if err != nil {
		panic(err)
	}

	active, err := db.IsActiveSession("example.com", "demo1", 27750, context.TODO())
	if err != nil {
		panic(err)
	}
	if !active {
		t.Fatal("demo1 should be active")
	}

	count, err := db.GetHostSessionCount("example.com", context.TODO())
	if err != nil {
		panic(err)
	}
	if count != 2 {
		t.Fatalf("Expected 2 active sessions, got %d", count)
	}

	join, err := db.QuerySessionByRoomcode(code, context.TODO())
	if err != nil {
		panic(err)
	}

	if join.Id != "demo1" {
		t.Fatalf("Expected demo1, got %s", join.Id)
	}
}

func TestSessionRefreshing(t *testing.T) {
	db := initDb()
	ses := insertTest(db, "test", "demo1")

	err := db.RefreshSession(map[string]interface{}{
		"title":     "Hello",
		"users":     10,
		"usernames": []string{"a", "b"},
		"password":  true,
		"nsfm":      true,
		"private":   false,
	}, ses.ListingId, ses.UpdateKey, context.TODO())
	if err != nil {
		panic(err)
	}

	sessions, err := db.QuerySessionList(QueryOptions{Nsfm: true}, context.TODO())
	if err != nil {
		panic(err)
	}

	if len(sessions) != 1 {
		t.Fatalf("Did not receive 1 listing (got %d)", len(sessions))
	}

	if sessions[0].Title != "Hello" {
		t.Fatal("Title did not change!")
	}

	if sessions[0].Users != 10 {
		t.Fatal("User count did not change!")
	}

	if !reflect.DeepEqual(sessions[0].Usernames, []string{"a", "b"}) {
		t.Fatal("Username list did not change!")
	}

	if !sessions[0].Password {
		t.Fatal("Password flag did not change!")
	}

	if !sessions[0].Nsfm {
		t.Fatal("Nsfm flag did not change!")
	}
}

func TestExpiredSession(t *testing.T) {
	db := initDb()
	ses := insertTest(db, "test", "demo1")

	sessions, err := db.QuerySessionList(QueryOptions{Nsfm: true}, context.TODO())
	if err != nil {
		panic(err)
	}

	if len(sessions) != 1 {
		t.Fatalf("Did not receive 1 listing (got %d)", len(sessions))
	}

	// Now, let's change the last active time...
	conn := db.pool.Get(context.TODO())
	stmt := conn.Prep("UPDATE sessions SET last_active='2000-01-01T00:00:00Z' WHERE rowid=?")
	stmt.BindInt64(1, ses.ListingId)
	if _, err = stmt.Step(); err != nil {
		panic(err)
	}
	db.pool.Put(conn)

	// Now there should be nothing visible
	sessions, err = db.QuerySessionList(QueryOptions{Nsfm: true}, context.TODO())
	if err != nil {
		panic(err)
	}

	if len(sessions) != 0 {
		t.Fatalf("Did not receive 0 listing (got %d)", len(sessions))
	}

	// Can't refresh either
	if err := db.RefreshSession(map[string]interface{}{}, ses.ListingId, ses.UpdateKey, context.TODO()); err != nil {
		if _, ok := err.(RefreshError); !ok {
			panic(err)
		}
	}
}

func tryIsBanned(t *testing.T, db *sqliteDb, host string, expected bool) {
	banned, err := db.IsBannedHost(host, context.TODO())
	if err != nil {
		t.Fatalf(err.Error())
	}

	if banned != expected {
		t.Errorf("%s banned=%t, expected=%t", host, banned, expected)
	}
}

func TestBanList(t *testing.T) {
	db := initDb()

	// Insert a few ban entries
	conn := db.pool.Get(context.TODO())
	sqlite_exec(conn, `INSERT INTO hostbans (host, expires) VALUES
		('banned1.com', '3000-01-01T00:00:00Z'),
		('banned2.com', NULL),
		('expired.com', '2000-01-01T00:00:00Z'),
		('rebanned.com', '2000-01-01T00:00:00Z'),
		('rebanned.com', '3000-01-01T00:00:00Z')
	`)
	db.pool.Put(conn)

	tryIsBanned(t, db, "banned1.com", true)
	tryIsBanned(t, db, "banned2.com", true)
	tryIsBanned(t, db, "rebanned.com", true)
	tryIsBanned(t, db, "expired.com", false)
	tryIsBanned(t, db, "not-banned.com", false)
}

func TestCleanup(t *testing.T) {
	db := initDb()

	// Insert a few test entries
	conn := db.pool.Get(context.TODO())
	sqlite_exec(conn, `INSERT INTO sessions VALUES
		('example.com', 27750, 'abc1', 'dp:0.1.2', 'Test1', 0, '', 0, 0, 'X', '0000-00-00', DATETIME('now'), 0, 'x', '127.0.0.1', NULL, 0),
		('example.com', 27750, 'abc1', 'dp:0.1.2', 'Test1', 0, '', 0, 0, 'X', '0000-00-00', DATETIME('now'), 1, 'x', '127.0.0.1', NULL, 0),
		('example.com', 27750, 'abc1', 'dp:0.1.2', 'Test1', 0, '', 0, 0, 'X', '0000-00-00', DATETIME('now', '-10 days'), 0, 'x', '127.0.0.1', NULL, 0)
	`)
	db.pool.Put(conn)

	db.cleanup()

	conn = db.pool.Get(context.TODO())
	stmt := conn.Prep("SELECT COUNT(*) FROM sessions")
	stmt.Step()

	if stmt.ColumnInt(0) != 1 {
		t.Errorf("Expected only one row after cleanup, got %d", stmt.ColumnInt(0))
	}
}
