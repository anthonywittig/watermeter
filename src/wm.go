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

		defer fmt.Println("this is a defer!")

		for {
			select {
			case <-quit:
				fmt.Println("\r- Ctrl+C pressed in Terminal")
				return
			case <-wmTick:
				fmt.Println("tick!")
				if meter.EdgeDetected() { // check if event occured
					fmt.Println("wm on!")
					led.Toggle()
				}
			}
		}

	}()

	wg.Wait()
}
