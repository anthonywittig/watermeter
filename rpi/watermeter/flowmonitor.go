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
	ctx      context.Context
	db       *sql.DB
	texter   *Texter
	notifier *PushNotifier
	valve    *iot.Valve
}

func StartFlowMonitor(
	ctx context.Context,
	wg *sync.WaitGroup,
	db *sql.DB,
	texter *Texter,
	notifier *PushNotifier,
	valve *iot.Valve,
) {
	fm := flowMonitor{
		ctx:      ctx,
		db:       db,
		texter:   texter,
		notifier: notifier,
		valve:    valve,
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
			// We probably still want to try to alert... we'll just ignore any errors it has.
			fm.sendHighWaterAlerts(gallons)
			return fmt.Errorf("error closing valve: %w", err)
		}
		if err := fm.sendHighWaterAlerts(gallons); err != nil {
			return fmt.Errorf("error sending high water alerts: %w", err)
		}
	}

	return nil
}

// sendHighWaterAlerts notifies over both channels — Twilio SMS and FCM push —
// during the Twilio -> PWA transition. A failure on one channel doesn't stop
// the other.
func (fm *flowMonitor) sendHighWaterAlerts(gallons float64) error {
	log.Printf("--- sendHighWaterAlerts --- %.2f\n", gallons)
	message := fmt.Sprintf("The water is running full blast! %.2f gallons in 5 minutes.", gallons)

	var pushErr error
	if fm.notifier != nil {
		pushErr = fm.notifier.NotifyAll(fm.ctx, "Water shut off", message)
		if pushErr != nil {
			log.Printf("error sending push: %v", pushErr)
		}
	}

	if err := fm.texter.SendMessage(message); err != nil {
		return fmt.Errorf("error sending text: %w", err)
	}
	return pushErr
}
