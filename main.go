package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v4/stdlib"
	"github.com/joho/godotenv"
	"github.com/stianeikeland/go-rpio"
)

var (
	// Uses BCM address.
	// 11 is 17
	led = rpio.Pin(17)
	// 12 is 18
	meter = rpio.Pin(18)
)

func main() {
	fmt.Println("starting up")

	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
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

	//handleData(db, time.Now().UTC())

	var wg sync.WaitGroup

	quit := make(chan os.Signal)
	signal.Notify(quit, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	pulse := make(chan time.Time, 50)

	wmTick := time.Tick(200 * time.Millisecond)

	wg.Add(1)
	go func() {
		defer wg.Done()

		// Open and map memory to access gpio, check for errors
		if err := rpio.Open(); err != nil {
			fmt.Println(err)
			return
		}

		// Set led to output mode
		led.Output()

		meter.Input()
		meter.PullUp()
		//meter.Detect(rpio.FallEdge)     // enable falling edge event detection
		//defer meter.Detect(rpio.NoEdge) // disable edge event detection

		lastState := rpio.High
		for {
			select {
			case <-quit:
				fmt.Println("shutting down!")
				close(pulse)
				return
			case <-wmTick:
				// look at https://github.com/stianeikeland/go-rpio/issues/46#issuecomment-524267649
				state := meter.Read()
				if state == rpio.Low && state != lastState {
					now := time.Now().UTC()
					fmt.Printf("wm pulse @ %s\n", now.Format(time.RFC3339))
					pulse <- now
					led.Toggle()
				}
				lastState = state
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for recordedAt := range pulse {
			handleData(db, recordedAt)
		}

	}()

	wg.Wait()
}

func handleData(db *sql.DB, recordedAt time.Time) {
	if _, err := db.Exec("insert into meter (recorded_at) values ($1)", recordedAt); err != nil {
		log.Printf("error inserting into db, continueing. %s\n", err.Error())
	}
	/*
		conn, err := pgx.Connect(context.Background(), os.Getenv("DATABASE_CONNECTION"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Unable to connect to database: %v\n", err)
			os.Exit(1)
		}
		defer conn.Close(context.Background())
	*/

	/*
		var name string
		var weight int64
		err = conn.QueryRow(context.Background(), "select id, recorded_at from meter").Scan(&name, &weight)
		if err != nil {
			fmt.Fprintf(os.Stderr, "QueryRow failed: %v\n", err)
			os.Exit(1)
		}
	*/

}
