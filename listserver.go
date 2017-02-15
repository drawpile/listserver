package main

import (
	"flag"
	"log"
	_ "github.com/lib/pq"
	"github.com/drawpile/listserver/db"
)

func main() {
	// Command line arguments (can be set in configuration file as well)
	cfgFile := flag.String("c", "", "configuration file")
	listenAddr := flag.String("l", "localhost:8080", "listening address")
	dbName := flag.String("d", "", "database URL")

	flag.Parse()

	// Load configuration file
	var cfg *config
	if len(*cfgFile) > 0 {
		var err error
		cfg, err = readConfigFile(*cfgFile)
		if err != nil {
			log.Fatal(err)
			return
		}

	} else {
		cfg = defaultConfig()
	}

	// Overridable settings
	if len(*listenAddr) > 0 {
		cfg.Listen = *listenAddr;
	}

	if len(*dbName) > 0 {
		cfg.Database = *dbName
	}

	// Start the server
	db := db.InitDatabase(cfg.Database)
	StartServer(cfg, db)
}

