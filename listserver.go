package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/drawpile/listserver/db"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
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

	var handler http.Handler
	if cfg.LogRequests {
		handler = handlers.LoggingHandler(os.Stdout, router)
	} else {
		handler = router
	}

	if len(cfg.IncludeServers) > 0 {
		log.Println("Including session from:", cfg.IncludeServers)
	}

	c := make(chan os.Signal, 1)
	srv := &http.Server{
		Addr:    cfg.Listen,
		Handler: handler,
	}

	go func() {
		log.Println("Listening at", cfg.Listen)
		if err := srv.ListenAndServe(); err != nil {
			log.Println(err)
		}
		c <- os.Kill
	}()

	signal.Notify(c, os.Interrupt)
	sig := <-c

	// Interrupt is a clean shutdown, anything else should be an error.
	if sig == os.Interrupt {
		log.Println("Ate interrupt signal, shutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.SessionTimeout)*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
		if err := database.Close(); err != nil {
			log.Println("Error closing database:", err)
		}
		os.Exit(0)
	} else {
		log.Fatal("Fatal server error")
	}
}
