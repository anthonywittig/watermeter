package watermeter

import "testing"

// buildRegs assembles a synthetic GPFSEL register bank from pin->mode-bits.
func buildRegs(modes map[uint8]uint32) gpioRegs {
	r := make(gpioRegs, 1024)
	for pin, bits := range modes {
		r[pin/10] |= bits << ((pin % 10) * 3)
	}
	return r
}

func TestGpioModeDrift(t *testing.T) {
	expected := map[uint8]string{17: "OUT", 18: "IN", 19: "OUT", 26: "OUT"}

	t.Run("healthy config reports no drift", func(t *testing.T) {
		regs := buildRegs(map[uint8]uint32{17: 1, 18: 0, 19: 1, 26: 1})
		if drift := gpioModeDrift(regs, expected); drift != "" {
			t.Errorf("drift = %q, want empty", drift)
		}
	})

	t.Run("reset output pin is reported", func(t *testing.T) {
		// The field observation: pin 17 back to IN while 19/26 stayed OUT.
		regs := buildRegs(map[uint8]uint32{17: 0, 18: 0, 19: 1, 26: 1})
		drift := gpioModeDrift(regs, expected)
		if drift != "pin 17: mode=IN want OUT" {
			t.Errorf("drift = %q, want pin 17 IN/OUT mismatch", drift)
		}
	})

	t.Run("multiple drifts are all reported, sorted", func(t *testing.T) {
		regs := buildRegs(map[uint8]uint32{17: 0, 18: 4, 19: 1, 26: 1}) // 18 -> ALT0
		drift := gpioModeDrift(regs, expected)
		want := "pin 17: mode=IN want OUT, pin 18: mode=ALT0 want IN"
		if drift != want {
			t.Errorf("drift = %q, want %q", drift, want)
		}
	})
}
