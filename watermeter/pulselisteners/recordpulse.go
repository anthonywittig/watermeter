package pulselisteners

import (
	"database/sql"
	"log"
	"time"
)

type PulseRecorder struct {
	db *sql.DB
}

func NewPulseRecorder(db *sql.DB) *PulseRecorder {
	return &PulseRecorder{
		db: db,
	}
}

func (pr *PulseRecorder) HandlePulse(recordedAt time.Time) error {
	if _, err := pr.db.Exec("insert into meter (recorded_at) values ($1)", recordedAt); err != nil {
		log.Printf("error inserting into db, continuing. %s\n", err.Error())
		return err
	}
	return nil
}
