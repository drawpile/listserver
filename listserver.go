package main

import (
	"context"
	"flag"
	"github.com/drawpile/listserver/db"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"log"
	"net/http"
	"os"
)

func main() {
	// Command line arguments (can be set in configuration file as well)
	cfgFile := flag.String("c", "", "configuration file")
	listenAddr := flag.String("l", "", "listening address")
	dbName := flag.String("d", "", "database path")
	inclServer := flag.String("s", "", "include session from server")

	flag.Parse()

	// Load configuration file
	var cfg *config
	var err error
	if len(*cfgFile) > 0 {
		cfg, err = readConfigFile(*cfgFile)
	} else {
		cfg, err = readEnv()
	}

	if err != nil {
		log.Fatal(err)
	}

	// Overridable settings
	if len(*listenAddr) > 0 {
		cfg.Listen = *listenAddr
	}

	if len(*dbName) > 0 {
		cfg.Database = *dbName
	}

	if len(*inclServer) > 0 {
		cfg.IncludeServers = []string{*inclServer}
	}

	if cfg.Database == "none" {
		cfg.Database = ""
	}

	// Start the server
	startServer(cfg, db.InitDatabase(cfg.Database, cfg.SessionTimeout))
}

type apiContext struct {
	cfg *config
	db  db.Database
}

type apiContextKey = int

const apiCtxKey = apiContextKey(0)

func startServer(cfg *config, database db.Database) {
	apictx := apiContext{cfg, database}
	router := mux.NewRouter()

	router.Handle("/", ResponseHandler(apiRootHandler)).Methods(http.MethodGet, http.MethodOptions)
	router.Handle("/sessions/", handlers.MethodHandler{
		"GET":  ResponseHandler(apiSessionListHandler),
		"POST": ResponseHandler(apiAnnounceSessionHandler),
		"PUT":  ResponseHandler(apiBatchRefreshHandler),
	})
	router.Handle("/sessions/{id:[0-9]+}", handlers.MethodHandler{
		"PUT":    ResponseHandler(apiRefreshHandler),
		"DELETE": ResponseHandler(apiUnlistHandler),
	})
	router.Handle("/join/{code:[A-Z]{5}}", ResponseHandler(apiRoomCodeHandler)).Methods(http.MethodGet, http.MethodOptions)

	if cfg.ProxyHeaders {
		router.Use(handlers.ProxyHeaders)
	}

	router.Use(
		handlers.CORS(
			handlers.AllowedOrigins(cfg.AllowOrigins),
			handlers.AllowedMethods([]string{"GET"}),
		),
		func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), apiCtxKey, apictx)))
			})
		},
	)

	var server http.Handler
	if cfg.LogRequests {
		server = handlers.LoggingHandler(os.Stdout, router)
	} else {
		server = router
	}

	if len(cfg.IncludeServers) > 0 {
		log.Println("Including session from:", cfg.IncludeServers)
	}
	log.Println("Listening at", cfg.Listen)
	log.Fatal(http.ListenAndServe(cfg.Listen, server))
}
