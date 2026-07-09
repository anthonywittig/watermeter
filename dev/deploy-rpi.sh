#!/bin/bash
#
# Deploy the rpi service to the Raspberry Pi.
#
#   ./dev/deploy-rpi.sh <pi-address>
#
# Cross-compiles for the Pi (armv7l), backs up the running binary, copies the
# binary + config from the sibling watermeter-config repo, and restarts the
# systemd service. See docs/rpi-deploy.md for details and rollback.

set -e

# start in the repo root (this script lives in dev/)
cd "$(dirname "$0")/.."

PI_ADDRESS="$1"
if [[ -z "$PI_ADDRESS" ]]; then
    echo "Usage: $0 <pi-address>" >&2
    exit 1
fi
PI="pi@${PI_ADDRESS}"
PI_BIN="projects/watermeter/bin"

CONFIG_DIR="../watermeter-config/config"
for f in "$CONFIG_DIR/rpi/.env" "$CONFIG_DIR/firebase/service-account.json"; do
    if [[ ! -f "$f" ]]; then
        echo "Missing $f -- is watermeter-config cloned next to this repo?" >&2
        exit 1
    fi
done

echo "==> Cross-compiling for the Pi (linux/arm, armv7)"
BUILD_OUT=$(mktemp -d)
trap 'rm -rf "$BUILD_OUT"' EXIT
(cd rpi && GOOS=linux GOARCH=arm GOARM=7 go build -o "$BUILD_OUT/watermeter" main.go)

echo "==> Stopping the service and backing up the current binary"
ssh "$PI" "sudo systemctl stop watermeter && { [ -f ~/$PI_BIN/watermeter ] && cp ~/$PI_BIN/watermeter ~/$PI_BIN/watermeter.bak || true; }"

echo "==> Copying binary + config"
scp -q "$BUILD_OUT/watermeter"                        "$PI:$PI_BIN/watermeter"
scp -q "$CONFIG_DIR/rpi/.env"                         "$PI:$PI_BIN/.env"
scp -q "$CONFIG_DIR/firebase/service-account.json"    "$PI:$PI_BIN/service-account.json"

echo "==> Restarting the service"
ssh "$PI" "chmod +x ~/$PI_BIN/watermeter && sudo systemctl start watermeter"

echo "==> Waiting for startup (valve init takes ~20s)"
sleep 25

echo "==> Verifying"
STATUS=$(ssh "$PI" "systemctl is-active watermeter" || true)
echo "service status: $STATUS"
if [[ "$STATUS" != "active" ]]; then
    echo "Service is not active! Recent logs:" >&2
    ssh "$PI" "sudo journalctl -u watermeter -n 30 --no-pager" >&2
    echo "Rollback: see docs/rpi-deploy.md (bin/watermeter.bak)" >&2
    exit 1
fi

if ssh "$PI" "sudo journalctl -u watermeter --since '1 minute ago' --no-pager" | grep -iE "fatal|panic"; then
    echo "Service is active but logged fatal/panic above — investigate." >&2
    exit 1
fi

echo "==> Deploy complete. Recent logs:"
ssh "$PI" "sudo journalctl -u watermeter -n 12 --no-pager"
