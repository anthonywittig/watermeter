package pulselisteners

import (
	"database/sql"
	"log"
	"sync"
	"time"
)

type PulseHandler interface {
	HandlePulse(recordedAt time.Time) error
}

func HandlePulses(
	pulse chan time.Time,
	wg *sync.WaitGroup,
	db *sql.DB,
	gcpProjectID string,
) {
	handlers := []PulseHandler{
		//NewGcpMonitor(db, gcpProjectID),
		NewPulseRecorder(db),
		NewPrometheusRecorder(),
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
}
