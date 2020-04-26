package main

import (
	"fmt"
	"github.com/stianeikeland/go-rpio"
	"os"
	"os/signal"
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
	// Open and map memory to access gpio, check for errors
	if err := rpio.Open(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	// Unmap gpio memory when done
	defer rpio.Close()

	// Set led to output mode
	led.Output()

	/*
	// Toggle led 20 times
	for x := 0; x < 20; x++ {
		led.Toggle()
		time.Sleep(time.Second)
	}
	*/


	meter.Input()
	meter.PullUp()
	meter.Detect(rpio.FallEdge) // enable falling edge event detection
	defer meter.Detect(rpio.NoEdge) // disable edge event detection

	defer fmt.Println("this is a defer!")


	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		fmt.Println("\r- Ctrl+C pressed in Terminal")

		// Clean up
		rpio.Close()
		meter.Detect(rpio.NoEdge)

		os.Exit(0)
	}()

	fmt.Println("press a button")

	for i := 0; i < (5 * 20); {
		if meter.EdgeDetected() { // check if event occured
			fmt.Println("button pressed")
			led.Toggle()
			i++
		}
		time.Sleep(time.Second / 5)
	}
}
