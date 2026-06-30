# AGENTS.md — watermeter

Guidance for AI coding agents working in this repo. Humans: see [README.md](README.md).

## What this is

A Raspberry Pi water-meter monitor with automatic + remote water shutoff. A
meter pulse = 0.1 gallon. The rpi service counts pulses, records them, closes a
valve on runaway flow, and accepts remote open/close commands via SMS → Lambda →
SQS. Read the README for the full architecture before making changes.

## Module structure — this is a multi-module monorepo

There are **three separate Go modules**; there is no single root `go.mod`. Run
Go commands from inside the relevant module directory:

| Directory            | Module                                | Go   |
|----------------------|---------------------------------------|------|
| `rpi/`               | `github.com/anthonywittig/watermeter` | 1.14 |
| `lambdas/`           | `lambdas`                             | 1.19 |
| `bin/deploy-lambda/` | `deploy-lambda`                       | 1.18 |

So `cd rpi && go build ./...`, `cd lambdas && go test ./...`, etc. A command run
at the repo root will not see any module.

## Build / test / run

- `make build` — builds `rpi/` to `bin/watermeter`; copies the `.env` from the
  sibling `watermeter-config` repo. **Requires `../watermeter-config` to exist.**
- `make run` — build, stop the systemd service, run the binary in the foreground.
- `make deploy-lambdas token=<MFA>` — vet/test/build/zip and deploy the lambda.
- Lambda CI-ish checks live in `bin/deploy-lambda/run.sh`: `go generate`,
  `go vet`, `go test` over `lambdas/`, then cross-compile `GOOS=linux GOARCH=amd64`.

There is no test suite of substance yet; prefer adding tests next to the code in
the module you touch.

## Conventions and constraints

- **Hardware is real.** `rpi/watermeter/hardware.go` and `iot/valve.go` use
  `go-rpio` and BCM pin numbers (meter=18, LED=17, valve open=19, close=26).
  This code only runs on the Pi; it can't be exercised on a laptop. Be careful
  changing pin assignments or relay polarity — the valve relays are **active-low**
  and held for 10 s per actuation.
- **Units:** one pulse = 0.1 gallon. Keep that constant consistent across
  `prometheus.go`, `gcpmonitor.go`, and `flowmonitor.go`.
- **Pulse listeners** implement the `PulseHandler` interface and are fanned out
  in `pulselisteners/pulselistener.go`. Add new sinks by implementing the
  interface and appending to the handler slice; handlers must not block.
- **Concurrency:** components are started with a shared `context.Context` and a
  `sync.WaitGroup`; respect cancellation (`<-ctx.Done()`) and call `wg.Done()`.
- **AWS:** the rpi side uses the shared-config profile `water-meter-rpi`; the
  SQS queue is `water-meter-rpi.fifo`. The SQS message type is `ValveChangeRequested{ Level int }`.

## Secrets — do NOT commit them here

Real config and secrets live in the sibling `watermeter-config` repo, not this
one. `.gitignore` excludes `*.env`, `*.gcp.credentials.json`, `bin/`, and
`.env.json`. Never add real Twilio/AWS/GCP credentials to this repo; update the
`.env.example*` files to document new config keys instead.

## When you change config shape

If you add or rename an environment variable, update **both**:
- the `*.env.example*` file in this repo, and
- the corresponding real file in `watermeter-config` (the user maintains that),
  and mention it so they can update the deployed config.
