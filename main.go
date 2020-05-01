package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/anthonywittig/watermeter/watermeter"
	"github.com/anthonywittig/watermeter/watermeter/pulselisteners"
	_ "github.com/jackc/pgx/v4/stdlib"
	"github.com/joho/godotenv"
)

func main() {
	fmt.Println("starting up")

	ex, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	dir := path.Dir(ex)
	if strings.HasPrefix(dir, "/tmp/go-build") {
		if err := godotenv.Load(); err != nil {
			log.Fatal("Error loading .env file")
		}
	} else {
		if err := godotenv.Load(dir + "/.env"); err != nil {
			log.Fatal("Error loading .env file")
		}
	}

	db, err := sql.Open("pgx", os.Getenv("DATABASE_CONNECTION"))
	if err != nil {
		log.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Need context to handle cleaning up DB?

	wg := &sync.WaitGroup{}

	pulse, err := watermeter.StartHardware(wg)
	if err := db.Ping(); err != nil {
		log.Fatal(err)
	}

	pulselisteners.HandlePulses(pulse, wg, db)

	wg.Wait()
}
