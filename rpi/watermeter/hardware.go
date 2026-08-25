package watermeter

import (
	"context"
	"fmt"
	"log"
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

	// GPIO config has been observed to get partially reset while the process
	// runs (pin modes back to input, pull-ups cleared) — cause unknown, likely
	// an electrical/firmware upset. Re-asserting our config is a cheap,
	// idempotent register write, so do it periodically to self-heal — but log
	// any drift found first, so upsets leave a trail instead of being
	// silently repaired.
	const reassertEvery = 60 * time.Second
	expectedModes := map[uint8]string{
		uint8(led): "OUT", uint8(meter): "IN", uint8(valveOpen): "OUT", uint8(valveClose): "OUT",
	}
	regs, err := openGPIORegs()
	if err != nil {
		log.Printf("gpio drift detection unavailable (reassert still active): %v", err)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		tick := time.NewTicker(reassertEvery)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if regs != nil {
					if drift := gpioModeDrift(regs, expectedModes); drift != "" {
						log.Printf("gpio config drift detected (repairing): %s", drift)
					}
				}
				meter.Input()
				meter.PullUp()
				led.Output()
				// Valve pins are re-asserted under the valve's lock so we
				// can't fight an in-progress actuation.
				valve.Reassert()
			}
		}
	}()

	go func() {
		defer wg.Done()

		lastState := rpio.Low
		droppedPulses := 0
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
					// Never block the detector on a slow consumer: dropping a
					// pulse loses 0.1 gal of history; blocking here would stop
					// pulse detection entirely.
					select {
					case pulse <- now:
					default:
						droppedPulses++
						fmt.Printf("pulse channel full, dropped pulse (%d dropped total)\n", droppedPulses)
					}
					led.Toggle()
				}
				lastState = state
			}
		}
	}()

	return pulse, valve, nil
}
