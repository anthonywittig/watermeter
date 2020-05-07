package pulselisteners

import (
	"context"
	"database/sql"
	"log"
	"sync"
	"time"
)

type PulseHandler interface {
	HandlePulse(recordedAt time.Time) error
}

func HandlePulses(
	ctx context.Context,
	pulse chan time.Time,
	wg *sync.WaitGroup,
	db *sql.DB,
	gcpProjectID string,
) error {
	handlers := []PulseHandler{
		NewDatabaseRecorder(db),
		NewPrometheusRecorder(),
	}

	if g, err := NewGcpMonitor(ctx, db, gcpProjectID); err != nil {
		return err
	} else {
		handlers = append(handlers, g)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for recordedAt := range pulse {
			for _, ph := range handlers {
				if err := ph.HandlePulse(recordedAt); err != nil {
					log.Print(err)
				}
			}
		}
	}()

	return nil
}
