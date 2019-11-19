package main

import (
	"database/sql"
	"github.com/drawpile/listserver/ratelimit"
	"github.com/patrickmn/go-cache"
	"log"
	"net/http"
	"time"
)

func StartServer(cfg *config, db *sql.DB) {
	mux := &apiMux{
		http.NewServeMux(),
		cfg,
		db,
		cache.New(15*time.Second, 10*time.Minute),
		ratelimit.NewBucketMap(),
	}

	mux.HandleApiEndpoint("/", RootEndpoint)
	mux.HandleApiEndpoint("/sessions/", SessionListEndpoint)
	mux.HandleApiEndpoint("/join/", RoomCodeEndpoint)

	log.Println("Starting list server at", cfg.Listen)
	log.Fatal(http.ListenAndServe(cfg.Listen, mux))
}
