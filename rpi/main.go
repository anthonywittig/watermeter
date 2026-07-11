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

	"cloud.google.com/go/firestore"
	"github.com/anthonywittig/watermeter/watermeter"
	"github.com/anthonywittig/watermeter/watermeter/pulselisteners"
	_ "github.com/jackc/pgx/v4/stdlib"
	"github.com/joho/godotenv"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/api/option"
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

	fsClient, err := firestore.NewClient(
		ctx,
		os.Getenv("FIREBASE_PROJECT_ID"),
		option.WithCredentialsFile(os.Getenv("FIREBASE_CREDENTIALS")),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer fsClient.Close()

	pulse, valve, err := watermeter.StartHardware(ctx, wg)
	if err := db.Ping(); err != nil {
		log.Fatal(err)
	}

	// StartHardware's valve init ends with the valve open; the wrapper tracks
	// (and reports) the ACTUAL hardware state for everyone who drives it.
	reportingValve := watermeter.NewReportingValve(ctx, fsClient, valve, true, "startup")

	// Firestore-based remote control (the PWA).
	if err := watermeter.StartFirestoreControl(ctx, wg, fsClient, reportingValve); err != nil {
		log.Fatal(err)
	}

	watermeter.StartUsagePublisher(ctx, wg, db, fsClient, os.Getenv("USAGE_TIMEZONE"))

	if err := pulselisteners.HandlePulses(
		ctx,
		pulse,
		wg,
		db,
		os.Getenv("FIREBASE_PROJECT_ID"),
		os.Getenv("FIREBASE_CREDENTIALS"),
	); err != nil {
		log.Fatal(err)
	}

	notifier, err := watermeter.NewPushNotifier(
		ctx,
		fsClient,
		os.Getenv("FIREBASE_PROJECT_ID"),
		os.Getenv("FIREBASE_CREDENTIALS"),
	)
	if err != nil {
		log.Fatal(err)
	}

	watermeter.StartFlowMonitor(ctx, wg, db, fsClient, notifier, reportingValve)

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
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-quit:
		cancel()
	case <-ctx.Done():
		// noop
	}
}
