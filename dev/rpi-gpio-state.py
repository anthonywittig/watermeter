"""Print the watermeter GPIO pins' modes and levels (read-only).

Run on the Pi (deployed there by dev/rpi-health.sh). Reads the BCM283x GPIO
registers via /dev/gpiomem. Registers must be accessed as aligned 32-bit
words — byte-wise reads (e.g. python mmap slicing) return garbage — hence the
ctypes view.

Expected healthy state: 17=OUT (LED), 18=IN (meter, pulled up, HIGH when no
water is flowing), 19=OUT HIGH and 26=OUT HIGH (valve relays, active-low,
idle off).
"""
import ctypes
import mmap
import os

PINS = {17: "led", 18: "meter", 19: "valve-open", 26: "valve-close"}
MODES = {0: "IN", 1: "OUT", 4: "ALT0", 5: "ALT1", 6: "ALT2", 7: "ALT3", 3: "ALT4", 2: "ALT5"}

# O_RDWR only because ctypes.from_buffer requires a writable mapping; this
# script never writes.
f = os.open("/dev/gpiomem", os.O_RDWR)
m = mmap.mmap(f, 4096)
regs = (ctypes.c_uint32 * 1024).from_buffer(m)

levels = regs[0x34 // 4]  # GPLEV0
for pin, name in sorted(PINS.items()):
    fsel = regs[pin // 10]  # GPFSEL0..
    mode = MODES.get((fsel >> ((pin % 10) * 3)) & 7, "?")
    level = "HIGH" if (levels >> pin) & 1 else "LOW"
    print(f"pin {pin:2d} ({name:11s}): mode={mode:4s} level={level}")
