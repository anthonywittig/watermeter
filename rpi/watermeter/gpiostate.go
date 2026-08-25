package watermeter

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"syscall"
	"unsafe"
)

// Read-only view of the BCM283x GPIO registers, used to detect config drift
// (pin modes changing underneath the running process) before the periodic
// reassert repairs it. go-rpio doesn't expose mode reads, so this maps
// /dev/gpiomem itself. Registers must be accessed as aligned 32-bit words —
// byte-wise access returns garbage — hence the []uint32 view.
type gpioRegs []uint32

func openGPIORegs() (gpioRegs, error) {
	f, err := os.Open("/dev/gpiomem")
	if err != nil {
		return nil, fmt.Errorf("opening /dev/gpiomem: %w", err)
	}
	defer f.Close()

	b, err := syscall.Mmap(int(f.Fd()), 0, 4096, syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return nil, fmt.Errorf("mmapping /dev/gpiomem: %w", err)
	}
	return unsafe.Slice((*uint32)(unsafe.Pointer(&b[0])), len(b)/4), nil
}

var gpioModeNames = map[uint32]string{
	0: "IN", 1: "OUT", 4: "ALT0", 5: "ALT1", 6: "ALT2", 7: "ALT3", 3: "ALT4", 2: "ALT5",
}

// mode decodes a pin's function-select bits (GPFSEL0.. hold 10 pins each,
// 3 bits per pin).
func (r gpioRegs) mode(pin uint8) string {
	fsel := r[pin/10]
	return gpioModeNames[(fsel>>((pin%10)*3))&7]
}

// gpioModeDrift compares each pin's current mode against the expected one and
// describes any mismatches ("" = no drift). Modes are the clean invariant to
// check: they never legitimately change at runtime, unlike levels (flow, LED
// toggles, valve actuations) — and pull state isn't readable on this chip.
func gpioModeDrift(r gpioRegs, expected map[uint8]string) string {
	var drifts []string
	for pin, want := range expected {
		if got := r.mode(pin); got != want {
			drifts = append(drifts, fmt.Sprintf("pin %d: mode=%s want %s", pin, got, want))
		}
	}
	// Map order is random; sort for stable log lines.
	sort.Strings(drifts)
	return strings.Join(drifts, ", ")
}
