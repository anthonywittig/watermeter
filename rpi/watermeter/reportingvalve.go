package watermeter

import (
	"context"
	"fmt"
	"log"
	"sync"

	"cloud.google.com/go/firestore"
	"github.com/anthonywittig/watermeter/watermeter/iot"
)

// ReportingValve wraps the physical valve with two things every valve writer
// (the Firestore listener, the flow monitor) needs to share:
//
//   - the current ACTUAL state, in memory, so writers can skip actuations that
//     wouldn't change anything (and so a flow-monitor close doesn't get
//     re-actuated when its own Firestore write echoes back), and
//   - best-effort reporting of that actual state to the `valve/actual` doc,
//     so the PWA can show what the hardware really is (including during the
//     ~10 s an actuation takes, and after an auto-shutoff).
//
// Hardware always comes first: the physical actuation happens (and the
// in-memory state updates) whether or not the Firestore report succeeds.
type ReportingValve struct {
	fs    *firestore.Client
	valve *iot.Valve

	mu   sync.Mutex
	open bool
}

// NewReportingValve wraps the valve. initialOpen must match the hardware's
// state at wrap time (StartHardware leaves the valve open), and is reported
// immediately so a fresh install has an actual doc.
func NewReportingValve(
	ctx context.Context,
	fs *firestore.Client,
	valve *iot.Valve,
	initialOpen bool,
	source string,
) *ReportingValve {
	rv := &ReportingValve{
		fs:    fs,
		valve: valve,
		open:  initialOpen,
	}
	rv.report(ctx, source)
	return rv
}

func (rv *ReportingValve) IsOpen() bool {
	rv.mu.Lock()
	defer rv.mu.Unlock()
	return rv.open
}

// Open actuates the valve open (a no-op if already open) and reports the
// resulting state. source names who asked ("remote", "flow-monitor").
func (rv *ReportingValve) Open(ctx context.Context, source string) error {
	return rv.set(ctx, true, source)
}

// Close actuates the valve closed (a no-op if already closed) and reports the
// resulting state.
func (rv *ReportingValve) Close(ctx context.Context, source string) error {
	return rv.set(ctx, false, source)
}

func (rv *ReportingValve) set(ctx context.Context, open bool, source string) error {
	rv.mu.Lock()
	if rv.open == open {
		rv.mu.Unlock()
		return nil
	}
	rv.mu.Unlock()

	var err error
	if open {
		err = rv.valve.Open()
	} else {
		err = rv.valve.Close()
	}
	if err != nil {
		// Actuation failed; the hardware state is unknown, so don't claim one.
		return err
	}

	rv.mu.Lock()
	rv.open = open
	rv.mu.Unlock()

	rv.report(ctx, source)
	return nil
}

// report publishes the in-memory actual state; failures are logged, never
// fatal — the physical valve is already where it should be.
func (rv *ReportingValve) report(ctx context.Context, source string) {
	rv.mu.Lock()
	open := rv.open
	rv.mu.Unlock()

	if _, err := rv.fs.Collection("valve").Doc("actual").Set(ctx, map[string]interface{}{
		"open":      open,
		"source":    source,
		"updatedAt": firestore.ServerTimestamp,
	}); err != nil {
		log.Printf("error reporting actual valve state: %v", err)
	} else {
		fmt.Printf("reported actual valve state: open=%t (%s)\n", open, source)
	}
}
