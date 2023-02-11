package iot

import (
	"fmt"
	"sync"
	"time"

	"github.com/stianeikeland/go-rpio"
)

type Valve struct {
	openRelay  rpio.Pin
	closeRelay rpio.Pin
	lock       sync.Mutex
}

func NewValve(openRelay rpio.Pin, closeRelay rpio.Pin) (*Valve, error) {
	fmt.Println("setting up valve")
	openRelay.Output()
	closeRelay.Output()
	fmt.Println("setting all valve inputs to high (default off)")
	openRelay.High()
	closeRelay.High()

	v := &Valve{
		closeRelay: closeRelay,
		openRelay:  openRelay,
		lock:       sync.Mutex{},
	}

	if err := v.Close(); err != nil {
		return nil, fmt.Errorf("error closing valve: %w", err)
	}
	if err := v.Open(); err != nil {
		return nil, fmt.Errorf("error opening valve: %w", err)
	}

	return v, nil
}

func (v *Valve) Close() error {
	v.lock.Lock()
	defer v.lock.Unlock()

	fmt.Println("setting close valve to low (on)")
	v.closeRelay.Low()
	time.Sleep(10 * time.Second)
	fmt.Println("setting close valve to high (off)")
	v.closeRelay.High()

	return nil
}

func (v *Valve) Open() error {
	v.lock.Lock()
	defer v.lock.Unlock()

	fmt.Println("setting open valve to low (on)")
	v.openRelay.Low()
	time.Sleep(10 * time.Second)
	fmt.Println("setting open valve to high (off)")
	v.openRelay.High()

	return nil
}
