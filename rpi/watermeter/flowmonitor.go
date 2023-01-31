package watermeter

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/anthonywittig/watermeter/watermeter/iot"
)

type flowMonitor struct {
	ctx    context.Context
	db     *sql.DB
	texter *Texter
	valve  *iot.Valve
}

func StartFlowMonitor(
	ctx context.Context,
	wg *sync.WaitGroup,
	db *sql.DB,
	texter *Texter,
	valve *iot.Valve,
) {
	fm := flowMonitor{
		ctx:    ctx,
		db:     db,
		texter: texter,
		valve:  valve,
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
	if gallons > 20 {
		if err := fm.valve.Close(); err != nil {
			// We probably still want to try to send the text... we'll just ignore any errors it has.
			fm.sendHighWaterText(gallons)
			return fmt.Errorf("error closing valve: %w", err)
		}
		if err := fm.sendHighWaterText(gallons); err != nil {
			return fmt.Errorf("error sending high water text: %w", err)
		}
	}

	return nil
}

func (fm *flowMonitor) sendHighWaterText(gallons float64) error {
	log.Printf("--- sendHighWaterText --- %.2f\n", gallons)
	return fm.texter.SendMessage(fmt.Sprintf("The water is running full blast! %.2f gallons in 5 minutes.", gallons))
}
