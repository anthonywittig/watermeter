package watermeter

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/stianeikeland/go-rpio"
)

func StartHardware(wg *sync.WaitGroup) (chan time.Time, error) {
	// Uses BCM addresses.
	led := rpio.Pin(17)
	meter := rpio.Pin(18)

	if err := rpio.Open(); err != nil {
		return nil, err
	}

	// Set led to output mode
	led.Output()

	meter.Input()
	meter.PullUp()
	//meter.Detect(rpio.FallEdge)     // enable falling edge event detection
	//defer meter.Detect(rpio.NoEdge) // disable edge event detection

	quit := make(chan os.Signal)
	signal.Notify(quit, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	wmTick := time.Tick(200 * time.Millisecond)
	pulse := make(chan time.Time, 50)
	wg.Add(1)

	go func() {
		defer wg.Done()

		lastState := rpio.Low
		for {
			select {
			case <-quit:
				fmt.Println("shutting down!")
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
