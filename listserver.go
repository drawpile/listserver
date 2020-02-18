package main

import (
	"flag"
	"github.com/drawpile/listserver/db"
	"log"
)

func main() {
	// Command line arguments (can be set in configuration file as well)
	cfgFile := flag.String("c", "", "configuration file")
	listenAddr := flag.String("l", "", "listening address")
	dbName := flag.String("d", "", "database path")

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
		return
	}

	// Overridable settings
	if len(*listenAddr) > 0 {
		cfg.Listen = *listenAddr
	}

	if len(*dbName) > 0 {
		cfg.Database = *dbName
	}

	if cfg.Database == "none" {
		cfg.Database = ""
	}

	// Start the server
	StartServer(cfg, db.InitDatabase(cfg.Database, cfg.SessionTimeout))
}
