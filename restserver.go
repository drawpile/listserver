package main

import (
	"database/sql"
	"net/http"
	"log"
	"github.com/drawpile/listserver/ratelimit"
)

func StartServer(cfg *config, db *sql.DB) {
	mux := &apiMux{http.NewServeMux(), cfg, db, ratelimit.NewBucketMap()}

	mux.HandleApiEndpoint("/", RootEndpoint)
	mux.HandleApiEndpoint("/sessions/", SessionListEndpoint)

	log.Println("Starting list server at", cfg.Listen)
	http.ListenAndServe(cfg.Listen, mux)
}

