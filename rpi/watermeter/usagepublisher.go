package watermeter

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
)

// StartUsagePublisher periodically rolls the Postgres pulse log up into two
// small Firestore documents the PWA charts from:
//
//	usage/minutely — gallons per minute for the last ~2 hours
//	usage/hourly   — gallons per hour for the last ~32 days
//
// Buckets are keyed by the bucket's start time in Unix seconds (UTC), so the
// client can render them in its own local timezone (and aggregate hourly
// buckets into local-timezone days for the week/month views). Documents are
// only written when their contents change, so idle periods cost no writes.
type usagePublisher struct {
	ctx    context.Context
	db     *sql.DB
	fs     *firestore.Client
	lastMu sync.Mutex
	last   map[string]string // doc name -> fingerprint of last written buckets
}

func StartUsagePublisher(
	ctx context.Context,
	wg *sync.WaitGroup,
	db *sql.DB,
	fs *firestore.Client,
) {
	up := &usagePublisher{
		ctx:  ctx,
		db:   db,
		fs:   fs,
		last: map[string]string{},
	}

	tick := time.NewTicker(time.Minute).C

	wg.Add(1)
	go func() {
		defer wg.Done()

		// Publish once at startup so a fresh install has data immediately.
		up.publishAll()

		for {
			select {
			case <-ctx.Done():
				fmt.Println("shutting down usage publisher")
				return
			case <-tick:
				up.publishAll()
			}
		}
	}()
}

func (up *usagePublisher) publishAll() {
	if err := up.publish("minutely", "minute", "120 minutes"); err != nil {
		log.Printf("usage publisher (minutely): %v", err)
	}
	if err := up.publish("hourly", "hour", "32 days"); err != nil {
		log.Printf("usage publisher (hourly): %v", err)
	}
}

func (up *usagePublisher) publish(docName string, trunc string, window string) error {
	// trunc and window are compile-time constants from publishAll, not user
	// input; they can't be bound as parameters in date_trunc/interval anyway.
	query := fmt.Sprintf(`
		select extract(epoch from date_trunc('%s', recorded_at))::bigint as bucket, count(*)
		from meter
		where recorded_at >= (now() at time zone 'UTC') - interval '%s'
		group by bucket
		order by bucket`, trunc, window)

	rows, err := up.db.QueryContext(up.ctx, query)
	if err != nil {
		return fmt.Errorf("querying %s buckets: %w", docName, err)
	}
	defer rows.Close()

	buckets := map[string]float64{}
	fingerprint := ""
	for rows.Next() {
		var bucket int64
		var count int
		if err := rows.Scan(&bucket, &count); err != nil {
			return fmt.Errorf("scanning %s bucket: %w", docName, err)
		}
		gallons := float64(count) * 0.1
		buckets[strconv.FormatInt(bucket, 10)] = gallons
		fingerprint += fmt.Sprintf("%d:%d;", bucket, count)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("reading %s buckets: %w", docName, err)
	}

	up.lastMu.Lock()
	unchanged := up.last[docName] == fingerprint
	up.lastMu.Unlock()
	if unchanged {
		return nil
	}

	if _, err := up.fs.Collection("usage").Doc(docName).Set(up.ctx, map[string]interface{}{
		"buckets":   buckets,
		"updatedAt": firestore.ServerTimestamp,
	}); err != nil {
		return fmt.Errorf("writing usage/%s: %w", docName, err)
	}

	up.lastMu.Lock()
	up.last[docName] = fingerprint
	up.lastMu.Unlock()

	return nil
}
