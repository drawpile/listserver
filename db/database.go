package db

import (
	"database/sql"
	"log"
)

func InitDatabase(dbname string) (db *sql.DB) {
	if len(dbname) == 0 {
		log.Fatal("No database connection configuration")
	}

	db, err := sql.Open("postgres", dbname)
	if err != nil {
		log.Fatal(err)
	}

	err = db.Ping()
	if err != nil {
		log.Fatal(err)
	}

	return
}
