#!/bin/bash
#
# One-shot health/diagnostic report for the rpi watermeter service.
#
#   ./dev/rpi-health.sh <pi-address>
#
# Read-only: safe to run any time. Checks the service, pulse recency (journal,
# DB, and prometheus), GPIO pin state, clock sync, and recent errors — the
# questions you'd otherwise answer by hand over ssh when datapoints go missing.

set -e

cd "$(dirname "$0")/.."

PI_ADDRESS="$1"
if [[ -z "$PI_ADDRESS" ]]; then
    echo "Usage: $0 <pi-address>" >&2
    exit 1
fi
PI="pi@${PI_ADDRESS}"

section() {
    echo
    echo "=== $1 ==="
}

section "service"
ssh "$PI" "systemctl is-active watermeter; systemctl show watermeter -p ActiveEnterTimestamp --value; uptime"

section "clock (a skewed clock corrupts every timestamp downstream)"
ssh "$PI" "timedatectl | grep -E 'Local time|synchronized|NTP'"
echo "local (this machine): $(date -u '+%a %Y-%m-%d %H:%M:%S UTC')"

section "power / disk"
ssh "$PI" "vcgencmd get_throttled; df -h / | tail -1"

section "last pulses seen by the process (journal)"
ssh "$PI" "sudo journalctl -u watermeter --no-pager | grep 'wm pulse' | tail -3 || echo 'no pulses in journal'"

section "pulses recorded in postgres"
# repeated -c: psql only prints the last result when statements share one -c.
# recorded_at is naive UTC but the postgres server timezone is not UTC, so
# always compare against now() at time zone 'utc' (bare now() is skewed).
ssh "$PI" "sudo -u postgres psql -d water -At \
    -c \"select 'last row:            ' || max(recorded_at) from meter\" \
    -c \"select 'rows last 1h:        ' || count(*) from meter where recorded_at > (now() at time zone 'utc') - interval '1 hour'\" \
    -c \"select 'rows last 24h:       ' || count(*) from meter where recorded_at > (now() at time zone 'utc') - interval '24 hours'\" \
    -c \"select 'rows previous 7d avg: ' || (count(*) / 7) || '/day' from meter where recorded_at between (now() at time zone 'utc') - interval '8 days' and (now() at time zone 'utc') - interval '1 day'\""

section "prometheus metrics (:8000)"
ssh "$PI" "curl -s --max-time 5 localhost:8000/metrics | grep -E '^(gallons|last_pulse_timestamp_seconds)' || echo 'metrics endpoint not answering'"

section "gpio pin state"
scp -q dev/rpi-gpio-state.py "$PI:/tmp/rpi-gpio-state.py"
ssh "$PI" "python3 /tmp/rpi-gpio-state.py"

section "recent non-pulse log lines (errors, valve actions)"
ssh "$PI" "sudo journalctl -u watermeter --since '-48 hours' --no-pager | grep -v 'wm pulse' | tail -15"

echo
echo "Healthy looks like: service active, clock synchronized, rows in the last"
echo "24h in the same ballpark as the 7d average, pin 18 mode=IN (HIGH unless"
echo "water is flowing right now), pins 19/26 mode=OUT level=HIGH."
