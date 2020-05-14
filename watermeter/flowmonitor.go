package watermeter

import (
	"context"
	"database/sql"
	"log"
	"sync"
	"time"
)

type flowMonitor struct {
	ctx context.Context
	db  *sql.DB
}

func StartFlowMonitor(ctx context.Context, wg *sync.WaitGroup, db *sql.DB) {
	fm := flowMonitor{
		ctx: ctx,
		db:  db,
	}

	tick := time.Tick(30 * time.Second)

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick:
				if err := fm.monitorAndAlarm(); err != nil {
					log.Print(err)
				}
			}
		}
	}()
}

func (fm *flowMonitor) monitorAndAlarm() error {
	// Do some queries and alarm!
	row := fm.db.QueryRowContext(fm.ctx, `select count(*) from meter where meter.recordedAt >= now() - interval '5' minute`)

	var metricCount int
	if err := row.Scan(&metricCount); err != nil {
		return err
	}

	gallons := float64(metricCount) * 0.1
	log.Printf("--- query for alarm: %g\n", gallons)

	return nil
}

/*
func (d *DatabaseRecorder) HandlePulse(recordedAt time.Time) error {
	if _, err := d.db.Exec("insert into meter (recorded_at) values ($1)", recordedAt); err != nil {
		log.Printf("error inserting into db, continuing. %s\n", err.Error())
		return err
	}
	return nil
}
*/
