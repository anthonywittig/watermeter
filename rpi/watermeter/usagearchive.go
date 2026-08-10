package watermeter

import (
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
)

// Minute-level history lives in one document per UTC day —
// usage/minutely-YYYY-MM-DD — alongside usage/minutely-index, which lists the
// days that exist and whether they're final. The live usage/minutely doc only
// reaches back a few hours; these archives let the PWA zoom into any past hour
// by fetching exactly one more document. A closed day never changes again, so
// nothing subscribes to them and they cost nothing until someone looks — which
// is the whole point: retention here doesn't grow what every client downloads.
//
// They're partitioned by UTC day rather than by USAGE_TIMEZONE because the
// buckets inside are epoch-keyed: this is a storage partition, not a calendar
// anyone sees. An epoch-aligned hour never straddles two UTC days, so zooming
// into an hour is always a single fetch.
//
// They also need no security-rules change: `match /usage/{doc}` already covers
// every document in the collection, which is why they're named with a prefix
// instead of living in a subcollection.
const (
	archiveIndexDoc  = "minutely-index"
	archiveDocPrefix = "minutely-"
	archiveDayFormat = "2006-01-02"
	// How far back to backfill, matching the daily rollup's window so every bar
	// the year view can draw is eventually zoomable down to the minute.
	archiveDays = 1100
	// Closed days built per run: a fresh install backfills over a few hours
	// rather than running 1100 aggregates back to back.
	archiveDaysPerRun = 10
)

// publishArchives rewrites today's (still filling) archive and rebuilds a few
// missing closed days. Runs on the slow tick, alongside the daily rollup.
func (up *usagePublisher) publishArchives() error {
	if up.archives == nil {
		if err := up.loadArchiveIndex(); err != nil {
			return fmt.Errorf("loading archive index: %w", err)
		}
	}

	// Today is indexed as incomplete: the PWA may read it but knows not to hold
	// onto it. Tomorrow it's just another closed day for the backfill to
	// rebuild once, in full — which is what fills in the minutes written after
	// this run.
	today := time.Now().UTC()
	changed, err := up.publishArchiveDay(today, false)
	if err != nil {
		return err
	}

	built := 0
	for i := 1; i <= archiveDays && built < archiveDaysPerRun; i++ {
		day := today.AddDate(0, 0, -i)
		if up.archives[day.Format(archiveDayFormat)] {
			continue
		}
		if _, err := up.publishArchiveDay(day, true); err != nil {
			return err
		}
		changed = true
		built++
	}

	if !changed {
		return nil
	}
	return up.writeArchiveIndex()
}

// publishArchiveDay writes one day's archive, reporting whether the index needs
// rewriting as a result.
func (up *usagePublisher) publishArchiveDay(day time.Time, complete bool) (bool, error) {
	key := day.Format(archiveDayFormat)

	// recorded_at holds UTC (hardware.go stamps every pulse with time.Now().UTC()),
	// so comparing against a bare date bounds the query to that UTC day.
	rows, err := up.db.QueryContext(up.ctx, `
		select extract(epoch from date_trunc('minute', recorded_at))::bigint as bucket, count(*)
		from meter
		where recorded_at >= $1::date and recorded_at < $1::date + interval '1 day'
		group by bucket
		order by bucket`, key)
	if err != nil {
		return false, fmt.Errorf("querying %s buckets: %w", key, err)
	}
	defer rows.Close()

	buckets, fingerprint, err := scanBuckets(rows)
	if err != nil {
		return false, fmt.Errorf("reading %s buckets: %w", key, err)
	}

	if err := up.write(archiveDocPrefix+key, buckets, fingerprint); err != nil {
		return false, err
	}
	if complete {
		// Nothing will rebuild this day, so its fingerprint is dead weight.
		up.forget(archiveDocPrefix + key)
	}

	if was, ok := up.archives[key]; ok && was == complete {
		return false, nil
	}
	up.archives[key] = complete
	return true, nil
}

func (up *usagePublisher) loadArchiveIndex() error {
	snap, err := up.fs.Collection("usage").Doc(archiveIndexDoc).Get(up.ctx)
	// A missing document comes back as a non-nil snapshot that doesn't exist
	// (plus a NotFound error) — that's just the first run, not a failure.
	if snap != nil && !snap.Exists() {
		up.archives = map[string]bool{}
		return nil
	}
	if err != nil {
		return err
	}

	days, _ := snap.Data()["days"].(map[string]interface{})
	up.archives = make(map[string]bool, len(days))
	for day, complete := range days {
		done, _ := complete.(bool)
		up.archives[day] = done
	}
	return nil
}

func (up *usagePublisher) writeArchiveIndex() error {
	if _, err := up.fs.Collection("usage").Doc(archiveIndexDoc).Set(up.ctx, map[string]interface{}{
		"days":      up.archives,
		"updatedAt": firestore.ServerTimestamp,
	}); err != nil {
		return fmt.Errorf("writing usage/%s: %w", archiveIndexDoc, err)
	}
	return nil
}
