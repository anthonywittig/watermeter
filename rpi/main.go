package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strings"
	"sync"
	"syscall"

	"github.com/anthonywittig/watermeter/watermeter"
	"github.com/anthonywittig/watermeter/watermeter/pulselisteners"
	_ "github.com/jackc/pgx/v4/stdlib"
	"github.com/joho/godotenv"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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

	ctx, cancelCtx := context.WithCancel(context.Background())
	go cancelContextOnInterrupt(ctx, cancelCtx)

	db, err := sql.Open("pgx", os.Getenv("DATABASE_CONNECTION"))
	if err != nil {
		log.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Need to shut this down nicely?
	go handlePrometheus()

	// Need context to handle cleaning up DB?

	wg := &sync.WaitGroup{}

	pulse, valve, err := watermeter.StartHardware(ctx, wg)
	if err := db.Ping(); err != nil {
		log.Fatal(err)
	}

	if err := pulselisteners.HandlePulses(
		ctx,
		pulse,
		wg,
		db,
		os.Getenv("GCP_PROJECT_ID"),
	); err != nil {
		log.Fatal(err)
	}

	texter := &watermeter.Texter{
		Account:              os.Getenv("TWILIO_ACCOUNT"),
		SID:                  os.Getenv("TWILIO_SID"),
		Secret:               os.Getenv("TWILIO_SECRET"),
		AccountPhoneNumber:   os.Getenv("TWILIO_ACCOUNT_PHONE_NUMBER"),
		RecipientPhoneNumber: os.Getenv("TWILIO_RECIPIENT_PHONE_NUMBER"),
	}

	watermeter.StartFlowMonitor(ctx, wg, db, texter, valve)

	wg.Wait()
}

func handlePrometheus() {
	http.Handle("/metrics", promhttp.HandlerFor(
		prometheus.DefaultGatherer,
		promhttp.HandlerOpts{
			// Opt into OpenMetrics to support exemplars.
			EnableOpenMetrics: true,
		},
	))
	if err := http.ListenAndServe(":8000", nil); err != nil {
		log.Fatal(err)
	}
}

func cancelContextOnInterrupt(ctx context.Context, cancel context.CancelFunc) {
	quit := make(chan os.Signal)
	signal.Notify(quit, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-quit:
		cancel()
	case <-ctx.Done():
		// noop
	}
}
