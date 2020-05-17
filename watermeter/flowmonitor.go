package watermeter

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"
)

type flowMonitor struct {
	ctx    context.Context
	db     *sql.DB
	texter *Texter
}

func StartFlowMonitor(
	ctx context.Context,
	wg *sync.WaitGroup,
	db *sql.DB,
	texter *Texter,
) {
	fm := flowMonitor{
		ctx:    ctx,
		db:     db,
		texter: texter,
	}

	tick := time.Tick(5 * time.Minute)

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
	row := fm.db.QueryRowContext(fm.ctx, `select count(*) from meter where recorded_at >= (select now() at time zone 'UTC') - interval '5' minute`)

	var metricCount int
	if err := row.Scan(&metricCount); err != nil {
		return err
	}

	gallons := float64(metricCount) * 0.1
	if gallons > 13 {
		return fm.sendHighWaterText(gallons)
	}

	return nil
}

func (fm *flowMonitor) sendHighWaterText(gallons float64) error {
	log.Printf("--- sendHighWaterText --- %.2f\n", gallons)
	return fm.texter.SendMessage(fmt.Sprintf("The water is running full blast! %.2f gallons in 5 minutes.", gallons))
}
