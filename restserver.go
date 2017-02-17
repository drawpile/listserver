package main

import (
	"database/sql"
	"github.com/drawpile/listserver/ratelimit"
	"log"
	"net/http"
)

func StartServer(cfg *config, db *sql.DB) {
	mux := &apiMux{http.NewServeMux(), cfg, db, ratelimit.NewBucketMap()}

	mux.HandleApiEndpoint("/", RootEndpoint)
	mux.HandleApiEndpoint("/sessions/", SessionListEndpoint)

	log.Println("Starting list server at", cfg.Listen)
	log.Fatal(http.ListenAndServe(cfg.Listen, mux))
}
