package watermeter

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/stianeikeland/go-rpio"
)

func StartHardware(ctx context.Context, wg *sync.WaitGroup) (chan time.Time, error) {
	// Uses BCM addresses.
	led := rpio.Pin(17)
	meter := rpio.Pin(18)
	valveOpen := rpio.Pin(19)
	valveClose := rpio.Pin(26)

	if err := rpio.Open(); err != nil {
		return nil, err
	}

	// Set led to output mode
	led.Output()

	meter.Input()
	meter.PullUp()
	//meter.Detect(rpio.FallEdge)     // enable falling edge event detection
	//defer meter.Detect(rpio.NoEdge) // disable edge event detection

	fmt.Println("setting up valve")
	valveOpen.Output()
	valveClose.Output()
	fmt.Println("setting all valve inputs to high (default off)")
	valveOpen.High()
	valveClose.High()
	fmt.Println("setting open valve to low")
	valveOpen.Low()
	time.Sleep(10 * time.Second)
	fmt.Println("setting open valve to high")
	valveOpen.High()
	time.Sleep(10 * time.Second)

	/*
		fmt.Println("setting valve to low")
		valveOpen.Low()
		time.Sleep(10 * time.Second)
		fmt.Println("setting valve to high")
		valveOpen.High()
		time.Sleep(10 * time.Second)
		fmt.Println("setting valve to low")
		valveOpen.Low()
	*/

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

	return pulse, nil
}
