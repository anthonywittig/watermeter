package pulselisteners

import (
	"context"
	"database/sql"
	"log"
	"time"
)

// dbWriteTimeout bounds each insert; all handlers share one goroutine, so a
// hung write would otherwise stall every pulse listener indefinitely.
const dbWriteTimeout = 10 * time.Second

type DatabaseRecorder struct {
	ctx context.Context
	db  *sql.DB
}

func NewDatabaseRecorder(ctx context.Context, db *sql.DB) *DatabaseRecorder {
	return &DatabaseRecorder{
		ctx: ctx,
		db:  db,
	}
}

func (d *DatabaseRecorder) HandlePulse(recordedAt time.Time) error {
	ctx, cancel := context.WithTimeout(d.ctx, dbWriteTimeout)
	defer cancel()
	if _, err := d.db.ExecContext(ctx, "insert into meter (recorded_at) values ($1)", recordedAt); err != nil {
		log.Printf("error inserting into db, continuing. %s\n", err.Error())
		return err
	}
	return nil
}
