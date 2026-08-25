# AGENTS.md — watermeter

Guidance for AI coding agents working in this repo. Humans: see [README.md](README.md).

## What this is

A Raspberry Pi water-meter monitor with automatic + remote water shutoff. A
meter pulse = 0.1 gallon. The rpi service counts pulses, records them, closes a
valve on runaway flow, and push-notifies the family. Remote control and alerts
run through Firebase: an installable PWA (Google sign-in + Firestore + FCM).
Read the README for the full architecture before making changes.

## Layout

- `rpi/` — the **single Go module** (`github.com/anthonywittig/watermeter`,
  Go ≥ 1.23). Run Go commands from inside `rpi/`; the repo root has no `go.mod`.
  `rpi/cmd/testpush` sends a test push through the real notification path.
- `pwa/` — the web app: vanilla JS, Firebase SDK from the gstatic CDN, **no
  build step / no bundler**. One service worker handles both the app shell and
  FCM push (there is deliberately no separate `firebase-messaging-sw.js`).
- `dev/` — build/render/deploy scripts + the systemd unit.

## Build / test / deploy

- `cd rpi && go vet ./... && go build ./...` — basic checks. Cross-compile for
  the Pi with `GOOS=linux GOARCH=arm GOARM=7` (32-bit armv7l).
- `./dev/deploy-rpi.sh <pi-address>` (or `make deploy-rpi pi=<addr>`) — full Pi
  deploy: cross-compile, back up the running binary, scp binary+config, restart,
  verify. See docs/rpi-deploy.md. **Every deploy cycles the physical valve**
  (~20 s water interruption) — don't deploy to the Pi gratuitously.
- `./dev/render-config.sh && firebase deploy` — PWA hosting + Firestore rules.
  Render first; the deployment files are generated and gitignored.
- `./dev/rpi-health.sh <pi-address>` — read-only remote diagnostic: service,
  clock, pulse recency (journal/DB/prometheus), and GPIO pin state. Start here
  when datapoints go missing; safe to run any time.

There is no test suite of substance yet; prefer adding tests next to the code
you touch.

## Conventions and constraints

- **Hardware is real, and this controls house water.** `rpi/watermeter/hardware.go`
  and `iot/valve.go` use `go-rpio` and BCM pin numbers (meter=18, LED=17, valve
  open=19, close=26). This code only runs on the Pi. The valve relays are
  **active-low** and held for 10 s per actuation. Authorization must stay
  enforced in Firestore security rules — never only in the PWA's UI.
- **Units:** one pulse = 0.1 gallon. Keep that constant consistent across
  `prometheus.go`, `gcpmonitor.go`, and `flowmonitor.go`.
- **Firestore data model:** `valve/state` holds `{level, requestedBy,
  requestedAt}` (level <= 0 = closed); `pushTokens/{token}` holds `{email,
  updatedAt}` per registered device. The rules template
  (`firestore.rules.template`) enforces the email allowlist; the rendered
  `firestore.rules` is gitignored.
- **Pulse listeners** implement the `PulseHandler` interface and are fanned out
  in `pulselisteners/pulselistener.go`. Add new sinks by implementing the
  interface and appending to the handler slice; handlers must not block.
- **Concurrency:** components are started with a shared `context.Context` and a
  `sync.WaitGroup`; respect cancellation (`<-ctx.Done()`) and call `wg.Done()`.
- **Gmail dots:** the allowlist match is exact; ID tokens carry the account's
  canonical address (usually undotted). "Not authorized" for a family member is
  usually this.

## Keep this repo deployment-agnostic

**No deployment-specific values belong in this repo** — not secrets, and not
otherwise-public identifiers either (Firebase project ID, web API key, VAPID
key, allowed emails). The goal is that someone else could fork it and deploy
with only their own `watermeter-config`. All such values live in the sibling
`watermeter-config` repo and are pulled in at build/deploy time:

- `dev/build.sh` copies the rpi `.env` + service-account key from `watermeter-config`.
- `dev/render-config.sh` generates `.firebaserc`, `pwa/firebase-config.js`, and
  `firestore.rules` from `watermeter-config/config/firebase/`.

These generated files are **gitignored** (`.firebaserc`, `firestore.rules`,
`pwa/firebase-config.js`, `*.env`, `.env.json`, `*.gcp.credentials.json`). If you
find yourself typing a real project ID, key, or email into a tracked file, stop —
it belongs in `watermeter-config`, referenced through a template/render step.

## When you change config shape

If you add or rename a config value, update **all** of:
- the template/consumer in this repo (e.g. `firestore.rules.template`, the render
  script, or an `.env` reference), keeping only placeholders here;
- the `*.example*` file that documents the shape; and
- the real file in `watermeter-config` (the user maintains that) — mention it so
  they can update the deployed config.

## History

Twilio SMS + AWS (Lambda/SQS) were the original alert/command channels; they
were fully removed in the PWA migration (docs/pwa-migration-plan.md). Don't
reintroduce AWS/Twilio dependencies; the old code is in git history.
