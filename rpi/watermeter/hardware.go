package watermeter

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/anthonywittig/watermeter/watermeter/iot"
	"github.com/stianeikeland/go-rpio"
)

func StartHardware(ctx context.Context, wg *sync.WaitGroup) (chan time.Time, *iot.Valve, error) {
	// Uses BCM addresses.
	led := rpio.Pin(17)
	meter := rpio.Pin(18)
	valveOpen := rpio.Pin(19)
	valveClose := rpio.Pin(26)

	if err := rpio.Open(); err != nil {
		return nil, nil, fmt.Errorf("error opening rpio: %w", err)
	}

	led.Output()

	meter.Input()
	meter.PullUp()

	valve, err := iot.NewValve(valveOpen, valveClose)
	if err != nil {
		return nil, nil, fmt.Errorf("error setting up valve: %w", err)
	}

	fmt.Println("after initial valve settings")

	wmTick := time.NewTicker(200 * time.Millisecond).C
	pulse := make(chan time.Time, 50)
	wg.Add(1)

	go func() {
		defer wg.Done()

		lastState := rpio.Low
		for {
			select {
			case <-ctx.Done():
				fmt.Println("shutting down hardware")
				close(pulse)
				return
			case <-wmTick:
				// look at https://github.com/stianeikeland/go-rpio/issues/46#issuecomment-524267649
				state := meter.Read()
				if state == rpio.Low && state != lastState {
					now := time.Now().UTC()
					fmt.Printf("wm pulse @ %s\n", now.Format(time.RFC3339))
					pulse <- now
					led.Toggle()
				}
				lastState = state
			}
		}
	}()

	return pulse, valve, nil
}
