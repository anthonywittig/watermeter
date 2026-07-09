# Deploying to the Raspberry Pi

How the rpi service gets built and shipped. The short version: **cross-compile
on your workstation, `scp` into the Pi's `bin/`, restart the systemd service.**

## Why not build on the Pi?

The original flow (`make build` on the Pi) no longer works: `rpi/go.mod`
requires Go ≥ 1.23 (for the Firestore SDK), the Pi's Go is older, and it's slow
to build on anyway. Deploying by copying files sidesteps that.

The Pi is an **armv7l (32-bit)** Raspberry Pi, so cross-compile with
`GOOS=linux GOARCH=arm GOARM=7`. Go produces a statically-linked binary — no
runtime deps needed on the Pi.

## Prerequisites (one-time)

- Both repos cloned side by side on your workstation (`watermeter` +
  `watermeter-config`), with `watermeter-config` populated (rpi `.env`,
  `config/firebase/service-account.json`).
- SSH access to the Pi as the `pi` user.
- The `pi` user can `sudo systemctl` without a password (default on Raspbian).

## Deploy steps

From the `watermeter` repo root on your workstation (replace `<pi-address>`
with your Pi's hostname or IP):

```sh
# 0. Be on the code you mean to ship
git checkout master && git pull

# 1. Cross-compile for the Pi (armv7l, 32-bit)
(cd rpi && GOOS=linux GOARCH=arm GOARM=7 go build -o /tmp/watermeter main.go)

# 2. Stop the service and back up the running binary (rollback point)
ssh pi@<pi-address> \
  'sudo systemctl stop watermeter && cp ~/projects/watermeter/bin/watermeter ~/projects/watermeter/bin/watermeter.bak'

# 3. Copy the binary + config into the Pi's bin/
scp /tmp/watermeter                                            pi@<pi-address>:projects/watermeter/bin/watermeter
scp ../watermeter-config/config/rpi/.env                       pi@<pi-address>:projects/watermeter/bin/.env
scp ../watermeter-config/config/firebase/service-account.json  pi@<pi-address>:projects/watermeter/bin/service-account.json

# 4. Restart and verify
ssh pi@<pi-address> \
  'chmod +x ~/projects/watermeter/bin/watermeter && sudo systemctl start watermeter && sleep 25 && systemctl is-active watermeter'
```

Config-only changes (a `.env` edit, a new service-account key) are steps 2–4
without the compile: the service reads config at startup, so copy the file and
restart.

## Verify

```sh
ssh pi@<pi-address> 'sudo journalctl -u watermeter -n 50 --no-pager'
```

Healthy startup looks like:

- `starting up`, then the valve init sequence (`setting up valve` … `after
  initial valve settings` — this takes ~20 s; the valve relays cycle on boot),
- `remote control tick` every ~10 s (the SQS poller),
- `wm pulse @ …` lines whenever water is flowing,
- **no** `Fatal`/`panic` lines. A fatal during the first seconds usually means a
  config problem (bad `.env`, missing/invalid `service-account.json`, no DB).

The Firestore valve listener is silent until it acts: toggling the valve in the
PWA should print `firestore control: opening valve` / `closing valve`.

## Rollback

```sh
ssh pi@<pi-address> \
  'sudo systemctl stop watermeter && cp ~/projects/watermeter/bin/watermeter.bak ~/projects/watermeter/bin/watermeter && sudo systemctl start watermeter'
```

(If the bad deploy included config changes, restore the matching `.env` /
service-account file too — config lives in `watermeter-config` git history.)

## Notes

- `dev/build.sh` / `make build` still work **on a machine with Go ≥ 1.23** and
  a sibling `watermeter-config` checkout; they build into `bin/` and copy the
  config. The cross-compile flow above is the supported path to the actual Pi.
- The systemd unit is `dev/watermeter.service`
  (`ExecStart=/home/pi/projects/watermeter/bin/watermeter`, runs as `pi`,
  auto-restarts). If it changes: copy it to
  `/etc/systemd/system/watermeter.service` and `sudo systemctl daemon-reload`.
- The service loads `.env` from the directory next to the binary, so keeping
  binary + `.env` + `service-account.json` together in `bin/` is required.
