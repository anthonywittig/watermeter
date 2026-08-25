package watermeter

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"
)

// StallMonitor alerts when the meter goes silent. Historically "no pulses"
// has been indistinguishable from "no usage" — a broken sensor, wiring fault,
// or wedged pipeline just looked like a quiet house until someone noticed the
// chart was empty. A healthy household reads at least a few pulses within any
// stallThreshold window, so a silent meter is worth a push notification.
//
// It implements pulselisteners.PulseHandler; register it via HandlePulses.
type StallMonitor struct {
	notifier *PushNotifier

	mu          sync.Mutex
	lastPulse   time.Time
	lastAlerted time.Time
}

const (
	defaultStallThreshold = 12 * time.Hour
	stallRealertEvery     = 24 * time.Hour
	stallCheckEvery       = 10 * time.Minute
)

func StartStallMonitor(
	ctx context.Context,
	wg *sync.WaitGroup,
	db *sql.DB,
	notifier *PushNotifier,
	threshold time.Duration,
) *StallMonitor {
	if threshold <= 0 {
		threshold = defaultStallThreshold
	}

	m := &StallMonitor{
		notifier:  notifier,
		lastPulse: time.Now().UTC(),
	}

	// Seed from the DB so a service restart doesn't reset the clock on a
	// stall that's already in progress.
	var last sql.NullTime
	if err := db.QueryRowContext(ctx, "select max(recorded_at) from meter").Scan(&last); err != nil {
		log.Printf("stall monitor: could not read last pulse from db, starting from now: %v", err)
	} else if last.Valid {
		m.lastPulse = last.Time.UTC()
	}
	log.Printf("stall monitor: starting; last pulse %s, alert threshold %s", m.lastPulse.Format(time.RFC3339), threshold)

	wg.Add(1)
	go func() {
		defer wg.Done()
		tick := time.NewTicker(stallCheckEvery)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				m.checkAndAlert(ctx, threshold)
			}
		}
	}()

	return m
}

// HandlePulse implements pulselisteners.PulseHandler.
func (m *StallMonitor) HandlePulse(recordedAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastPulse = recordedAt.UTC()
	m.lastAlerted = time.Time{}
	return nil
}

func (m *StallMonitor) checkAndAlert(ctx context.Context, threshold time.Duration) {
	m.mu.Lock()
	lastPulse := m.lastPulse
	lastAlerted := m.lastAlerted
	m.mu.Unlock()

	now := time.Now().UTC()
	if !shouldAlertForStall(lastPulse, lastAlerted, now, threshold, stallRealertEvery) {
		return
	}

	silentFor := now.Sub(lastPulse).Round(time.Minute)
	log.Printf("stall monitor: no pulses since %s (%s)", lastPulse.Format(time.RFC3339), silentFor)
	body := fmt.Sprintf(
		"No water usage recorded since %s (%s ago). If water has been used, the meter sensor may be faulty.",
		lastPulse.Format(time.RFC3339), silentFor,
	)
	if err := m.notifier.NotifyAll(ctx, "Water meter silent", body); err != nil {
		// Leave lastAlerted unset so the next check retries the notification.
		log.Printf("stall monitor: error sending alert: %v", err)
		return
	}

	m.mu.Lock()
	m.lastAlerted = now
	m.mu.Unlock()
}

// shouldAlertForStall is the pure decision: alert once the silence exceeds
// threshold, then again every realertEvery while it continues.
func shouldAlertForStall(lastPulse, lastAlerted, now time.Time, threshold, realertEvery time.Duration) bool {
	if now.Sub(lastPulse) < threshold {
		return false
	}
	if !lastAlerted.IsZero() && now.Sub(lastAlerted) < realertEvery {
		return false
	}
	return true
}
