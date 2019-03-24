package main

import (
	"flag"
	"github.com/drawpile/listserver/db"
	"github.com/drawpile/listserver/drawpile"
	_ "github.com/lib/pq"
	"log"
	"os"
)

func main() {
	// Command line arguments (can be set in configuration file as well)
	cfgFile := flag.String("c", "", "configuration file")
	listenAddr := flag.String("l", "", "listening address")
	dbName := flag.String("d", "", "database URL")
	connectionTest := flag.String("t", "", "server URL (connection test mode)")

	flag.Parse()

	// Test mode
	if len(*connectionTest) > 0 {
		log.Print(*connectionTest, ": Test mode. Trying to connect...")
		err := drawpile.TryProtoV4Login(*connectionTest)
		if err != nil {
			log.Print(*connectionTest, ": Couldn't connect: ", err)
			os.Exit(1)
		} else {
			log.Print(*connectionTest, ": OK!")
		}
		return
	}

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
		cfg.Listen = *listenAddr
	}

	if len(*dbName) > 0 {
		cfg.Database = *dbName
	}

	// Start the server
	db := db.InitDatabase(cfg.Database)
	StartServer(cfg, db)
}
