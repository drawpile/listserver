package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"time"

	"github.com/drawpile/listserver/db"
	"github.com/drawpile/listserver/inclsrv"
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

	adminUser, _ := os.LookupEnv("DRAWPILE_LISTSERVER_USER")
	adminPass, _ := os.LookupEnv("DRAWPILE_LISTSERVER_PASS")

	inclsrv.CacheTtl = time.Duration(cfg.IncludeCacheTtl) * time.Second
	inclsrv.StatusCacheTtl = time.Duration(cfg.IncludeStatusCacheTtl) * time.Second
	inclsrv.Timeout = time.Duration(cfg.IncludeTimeout) * time.Second

	// Start the server
	startServer(cfg, db.InitDatabase(cfg.Database, cfg.SessionTimeout), adminUser, adminPass)
}

type apiContext struct {
	cfg *config
	db  db.Database
}

type apiContextKey = int

const apiCtxKey = apiContextKey(0)
const adminCtxKey = apiContextKey(1)

func normalizeSlashesHandler(next http.Handler) http.Handler {
	collapseSlashesRegexp := regexp.MustCompile("/{2,}")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := collapseSlashesRegexp.ReplaceAllString(r.URL.Path, "/")
		if strings.HasSuffix(path, "/") {
			r.URL.Path = path
		} else {
			r.URL.Path = path + "/"
		}
		next.ServeHTTP(w, r)
	})
}

func startServer(cfg *config, database db.Database, adminUser string, adminPass string) {
	apictx := apiContext{cfg, database}
	router := mux.NewRouter()

	if cfg.ProxyHeaders {
		router.Use(handlers.ProxyHeaders)
	}

	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), apiCtxKey, apictx)))
		})
	})

	mainRouter := router.NewRoute().Subrouter()
	mainRouter.Handle("/", ResponseHandler(apiRootHandler)).Methods(http.MethodGet, http.MethodOptions)
	mainRouter.Handle("/sessions/", handlers.MethodHandler{
		"GET":  ResponseHandler(apiSessionListHandler),
		"POST": ResponseHandler(apiAnnounceSessionHandler),
		"PUT":  ResponseHandler(apiBatchRefreshHandler),
	})
	mainRouter.Handle("/sessions/{id:[0-9]+}/", handlers.MethodHandler{
		"PUT":    ResponseHandler(apiRefreshHandler),
		"DELETE": ResponseHandler(apiUnlistHandler),
	})
	mainRouter.Handle("/join/{code:[A-Z]{5}}/",
		ResponseHandler(apiRoomCodeHandler)).Methods(http.MethodGet, http.MethodOptions)

	mainRouter.Use(handlers.CORS(
		handlers.AllowedOrigins(cfg.AllowOrigins),
		handlers.AllowedMethods([]string{http.MethodGet}),
	))

	if cfg.EnableAdminApi && database != nil {
		adminRouter := router.PathPrefix("/admin").Subrouter()
		adminRouter.Handle("/", handlers.MethodHandler{
			"GET": ResponseHandler(apiAdminRootHandler),
		})
		adminRouter.Handle("/sessions/", handlers.MethodHandler{
			"GET": ResponseHandler(apiAdminSessionListHandler),
			"PUT": ResponseHandler(apiAdminSessionPutHandler),
		})
		adminRouter.Handle("/bans/", handlers.MethodHandler{
			"GET":  ResponseHandler(apiAdminBanListHandler),
			"POST": ResponseHandler(apiAdminBanCreateHandler),
		})
		adminRouter.Handle("/bans/{id:[0-9]+}/", handlers.MethodHandler{
			"PUT":    ResponseHandler(apiAdminBanPutHandler),
			"DELETE": ResponseHandler(apiAdminBanDeleteHandler),
		})
		adminRouter.Handle("/roles/", handlers.MethodHandler{
			"GET":  ResponseHandler(apiAdminRoleListHandler),
			"POST": ResponseHandler(apiAdminRoleCreateHandler),
		})
		adminRouter.Handle("/roles/{id:[0-9]+}/", handlers.MethodHandler{
			"PUT":    ResponseHandler(apiAdminRolePutHandler),
			"DELETE": ResponseHandler(apiAdminRoleDeleteHandler),
		})
		adminRouter.Handle("/users/", handlers.MethodHandler{
			"GET":  ResponseHandler(apiAdminUserListHandler),
			"POST": ResponseHandler(apiAdminUserCreateHandler),
		})
		adminRouter.Handle("/users/{id:[0-9]+}/", handlers.MethodHandler{
			"PUT":    ResponseHandler(apiAdminUserPutHandler),
			"DELETE": ResponseHandler(apiAdminUserDeleteHandler),
		})
		adminRouter.Handle("/users/self/password/", handlers.MethodHandler{
			"PUT": ResponseHandler(apiAdminUserSelfPasswordPutHandler),
		})

		adminRouter.Use(handlers.CORS(
			handlers.AllowedOrigins(cfg.AllowOrigins),
			handlers.AllowedMethods([]string{
				http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete}),
			handlers.AllowedHeaders([]string{"Authorization", "Content-Type"}),
		))

		aam := adminAuthMiddleware{
			adminUser: adminUser,
			adminPass: adminPass,
		}
		adminRouter.Use(aam.Middleware)
	}

	var handler http.Handler = normalizeSlashesHandler(router)
	if cfg.LogRequests {
		handler = handlers.LoggingHandler(os.Stdout, handler)
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
