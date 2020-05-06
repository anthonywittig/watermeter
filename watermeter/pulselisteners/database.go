package pulselisteners

import (
	"database/sql"
	"log"
	"time"
)

type DatabaseRecorder struct {
	db *sql.DB
}

func NewDatabaseRecorder(db *sql.DB) *DatabaseRecorder {
	return &DatabaseRecorder{
		db: db,
	}
}

func (d *DatabaseRecorder) HandlePulse(recordedAt time.Time) error {
	if _, err := d.db.Exec("insert into meter (recorded_at) values ($1)", recordedAt); err != nil {
		log.Printf("error inserting into db, continuing. %s\n", err.Error())
		return err
	}
	return nil
}
