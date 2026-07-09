package watermeter

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/anthonywittig/watermeter/watermeter/iot"
)

// StartFirestoreControl watches the `valve/state` document written by the PWA
// and drives the valve to match it. It runs ALONGSIDE the SQS-based remote
// control (remotecontrol.go) during the Twilio -> PWA transition.
//
// The document's `level` field is the desired state: <= 0 means closed, > 0
// means open (mirrors the SQS ValveChangeRequested semantics). We only actuate
// when the desired open/closed state actually changes, so restarts and repeated
// snapshots don't needlessly cycle the valve. The first snapshot after startup
// does actuate, converging the valve to whatever state Firestore holds.
type firestoreControl struct {
	client   *firestore.Client
	valve    *iot.Valve
	lastOpen *bool
}

func StartFirestoreControl(
	ctx context.Context,
	wg *sync.WaitGroup,
	client *firestore.Client,
	valve *iot.Valve,
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
		fc.apply(snap)
	}
}

func (fc *firestoreControl) apply(snap *firestore.DocumentSnapshot) {
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

	open := level > 0
	if fc.lastOpen != nil && *fc.lastOpen == open {
		return
	}

	if open {
		fmt.Println("firestore control: opening valve")
		if err := fc.valve.Open(); err != nil {
			log.Printf("error opening valve: %v", err)
			return
		}
	} else {
		fmt.Println("firestore control: closing valve")
		if err := fc.valve.Close(); err != nil {
			log.Printf("error closing valve: %v", err)
			return
		}
	}

	fc.lastOpen = &open
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
