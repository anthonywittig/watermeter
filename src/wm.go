package main

import (
	"fmt"
	"github.com/stianeikeland/go-rpio"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

var (
	// Uses BCM address.
	// 11 is 17
	led = rpio.Pin(17)
	// 12 is 18
	meter = rpio.Pin(18)
)

func main() {
	var wg sync.WaitGroup
	wg.Add(1)

	quit := make(chan os.Signal)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	wmTick := time.Tick(100 * time.Millisecond)

	go func() {
		defer wg.Done()

		// Open and map memory to access gpio, check for errors
		if err := rpio.Open(); err != nil {
			fmt.Println(err)
			return
		}

		// Set led to output mode
		led.Output()

		meter.Input()
		meter.PullUp()
		meter.Detect(rpio.FallEdge) // enable falling edge event detection
		defer meter.Detect(rpio.NoEdge) // disable edge event detection

		currentState := meter.EdgeDetected()
		for {
			select {
			case <-quit:
				fmt.Println("\r- Ctrl+C pressed in Terminal")
				return
			case <-wmTick:
				if meter.EdgeDetected() != currentState {
					currentState = !currentState
					if currentState {
						fmt.Printf("wm pulse @ %s\n", time.Now().Format("2006-01-02 15:04:05"))
						led.Toggle()
					}
				}
			}
		}

	}()

	wg.Wait()
}
