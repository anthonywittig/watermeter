package watermeter

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
)

// StartFirestoreControl watches the `valve/state` document (the DESIRED state,
// written by the PWA — and by the flow monitor on auto-shutoff) and drives the
// valve to match it.
//
// The document's `level` field is the desired state: <= 0 means closed, > 0
// means open. Actuation is delegated to the ReportingValve, which compares
// against the hardware's ACTUAL state — so repeated snapshots, restarts, and
// echoes of writes made by other components (e.g. the flow monitor closing
// the valve itself) never re-actuate a valve that's already where it should be.
type firestoreControl struct {
	client *firestore.Client
	valve  *ReportingValve
}

func StartFirestoreControl(
	ctx context.Context,
	wg *sync.WaitGroup,
	client *firestore.Client,
	valve *ReportingValve,
) error {
	fc := &firestoreControl{
		client: client,
		valve:  valve,
	}

	wg.Add(1)
	go func() {
		defer wg.Done()

		for {
			if err := fc.listen(ctx); err != nil {
				if ctx.Err() != nil {
					fmt.Println("shutting down firestore control")
					return
				}
				log.Printf("firestore control listen error, retrying: %v", err)
			}

			// Back off before re-establishing the snapshot listener.
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
	}()

	return nil
}

func (fc *firestoreControl) listen(ctx context.Context) error {
	iter := fc.client.Collection("valve").Doc("state").Snapshots(ctx)
	defer iter.Stop()

	for {
		snap, err := iter.Next()
		if err != nil {
			return err
		}
		if !snap.Exists() {
			continue
		}
		fc.apply(ctx, snap)
	}
}

func (fc *firestoreControl) apply(ctx context.Context, snap *firestore.DocumentSnapshot) {
	levelRaw, err := snap.DataAt("level")
	if err != nil {
		log.Printf("valve doc has no level field: %v", err)
		return
	}

	level, ok := toInt64(levelRaw)
	if !ok {
		log.Printf("valve level is not a number: %v", levelRaw)
		return
	}

	if level > 0 {
		if !fc.valve.IsOpen() {
			fmt.Println("firestore control: opening valve")
		}
		if err := fc.valve.Open(ctx, "remote"); err != nil {
			log.Printf("error opening valve: %v", err)
		}
	} else {
		if fc.valve.IsOpen() {
			fmt.Println("firestore control: closing valve")
		}
		if err := fc.valve.Close(ctx, "remote"); err != nil {
			log.Printf("error closing valve: %v", err)
		}
	}
}

// toInt64 normalizes a Firestore numeric value. Integers written from the PWA
// come back as int64, but tolerate the other numeric shapes just in case.
func toInt64(v interface{}) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case float64:
		return int64(n), true
	default:
		return 0, false
	}
}
