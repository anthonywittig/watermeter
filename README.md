# watermeter

A Raspberry Pi–based water meter monitor with automatic and remote water shutoff.

The house water meter has a magnet on its spinning dial; a passive two-wire reed
switch mounted next to the dial closes once per revolution, generating one pulse
per 0.1 gallon. A Go service on the Pi counts those pulses, records them to
several backends, and watches for runaway flow. If too much water flows in a short
window (e.g. a burst pipe), it automatically closes a motorized valve and texts
you. You can also open/close the valve remotely by sending a text message.

```
                          ┌──────────────────────────────────────────┐
  water meter ──pulse──▶  │ rpi service (Go, runs on the Raspberry Pi)│
  (GPIO 18)               │                                           │
                          │  pulse fan-out ─▶ Postgres                │
                          │                ─▶ Prometheus (:8000)      │
                          │                ─▶ GCP custom metric       │
                          │                                           │
                          │  flow monitor ─▶ close valve + Twilio SMS │
                          │                  (> 20 gal / 5 min)       │
                          │                                           │
   text "0".."10" ──▶ Twilio ─▶ inbound-text Lambda ─▶ SQS ─▶ remote  │
                          │     (Function URL)        (FIFO) control ─▶ valve
                          └──────────────────────────────────────────┘
```

Original meter pulse wiring comes from the
[Freenove Ultimate Starter Kit tutorial](https://github.com/Freenove/Freenove_Ultimate_Starter_Kit_for_Raspberry_Pi/blob/master/Tutorial.pdf),
chapter 2 (the button wiring).

## Repository layout

This is a monorepo containing **three independent Go modules**:

| Path                 | Module                                | Go   | What it is |
|----------------------|---------------------------------------|------|------------|
| `rpi/`               | `github.com/anthonywittig/watermeter` | 1.14 | The long-running service that runs on the Raspberry Pi. |
| `lambdas/`           | `lambdas`                             | 1.19 | AWS Lambda(s). Currently `inbound-text`, the Twilio inbound-SMS webhook. |
| `bin/deploy-lambda/` | `deploy-lambda`                       | 1.18 | A Go tool that builds and deploys a lambda (function, IAM role, SQS queue). |

Other directories:

- `dev/` — `build.sh`, and the `watermeter.service` systemd unit (runs as user `pi`).
- `rpi/monitoring/` — Prometheus + postgres_exporter Docker setup for the Pi.

## How the rpi service works

`rpi/main.go` wires everything together and blocks until interrupted. The pieces:

- **Hardware** (`watermeter/hardware.go`) — uses BCM GPIO pins: meter input on
  18 (pulled up), status LED on 17, valve relays on 19 (open) / 26 (close).
  Polls the meter every 200 ms and emits a `time.Time` on the `pulse` channel
  on each falling edge.
- **Pulse listeners** (`watermeter/pulselisteners/`) — every pulse is fanned out
  to all handlers; one slow/failing handler doesn't block the others:
  - `database.go` — inserts a row into the Postgres `meter` table.
  - `prometheus.go` — increments a `gallons` counter (0.1 per pulse).
  - `gcpmonitor.go` — writes a GCP custom metric, batched (~every 30 s, since
    GCP caps reporting at one point per 10 s).
- **Flow monitor** (`watermeter/flowmonitor.go`) — every 5 minutes, counts
  pulses in the last 5 minutes. If > 20 gallons, it **closes the valve** and
  sends a Twilio SMS alert.
- **Remote control** (`watermeter/remotecontrol.go`) — polls the SQS FIFO queue
  `water-meter-rpi.fifo` every 10 s (AWS profile `water-meter-rpi`). A message
  with `level <= 0` closes the valve; anything else opens it.
- **Valve** (`watermeter/iot/valve.go`) — drives a two-relay motorized valve;
  relays are active-low and held for 10 s per actuation (mutex-guarded).
- **Texter** (`watermeter/texter.go`) — Twilio SMS client.
- Prometheus metrics are served at `:8000/metrics`.

## How the inbound text flow works

1. You text a number `0`–`10` to the Twilio number.
2. Twilio calls the `inbound-text` Lambda's Function URL.
3. The Lambda validates the Twilio signature, checks the sender against an
   allow-list, and pushes the number as a valve "level" onto the SQS FIFO queue.
4. The rpi remote-control loop picks it up and opens/closes the valve.
5. The Lambda texts back a confirmation.

## Configuration

**No deployment-specific values live in this repo** — that's deliberate, so it
can be forked and deployed by anyone who supplies their own config. Everything
project-specific (secrets *and* otherwise-public identifiers like the Firebase
project ID) lives in the sibling
[`watermeter-config`](https://github.com/anthonywittig/watermeter-config) repo,
which must be cloned next to this one (`../watermeter-config`).

- `dev/build.sh` copies `../watermeter-config/config/rpi/.env` into `bin/`.
- The lambda's `.env.json` is supplied by `watermeter-config`'s deploy step.
- `dev/render-config.sh` generates the Firebase deployment files —
  `.firebaserc`, `pwa/firebase-config.js`, and `firestore.rules` — from
  `watermeter-config/config/firebase/`. All three are **gitignored** (they're
  generated); run it before `firebase deploy`.

See the `*.example*` files in `watermeter-config` (and `rpi/.env.example` /
`lambdas/cmd/inbound-text/.env.example.json`) for the shape of the config.

## Firebase setup (one-time, manual)

The PWA runs on Firebase (see [docs/pwa-migration-plan.md](docs/pwa-migration-plan.md)).
Standing up a deployment requires a few clicks in the Firebase / Google Cloud
consoles that can't be scripted — these create the project and its credentials.
Do them once, then feed the resulting values into `watermeter-config`.

1. **Create a Firebase project** at <https://console.firebase.google.com> →
   *Add project*. (A Firebase project *is* a GCP project; you can also enable
   Firebase on an existing GCP project by picking it from the dropdown.)
2. **Register a Web app** (the `</>` icon) and copy the `firebaseConfig` object
   into `watermeter-config/config/firebase/web-config.json` (`web` + `projectId`).
   These are public client-side identifiers, not secrets.
3. **Authentication** → enable the **Google** sign-in provider (set a support
   email). Then, in the Google Cloud console → *APIs & Services → OAuth consent
   screen* (a.k.a. "Google Auth Platform"), set the audience to **External** and
   **Publish** the app to Production. Publishing avoids the Testing-mode gotcha
   where users get logged out every ~7 days; access is still restricted by the
   Firestore allowlist, not the consent screen. (Non-Workspace accounts only have
   the External option, which is what we want anyway.)
4. **Firestore Database** → *Create database* in **Production mode**; pick a
   region near the deployment (permanent).
5. **Cloud Messaging** → *Project Settings → Cloud Messaging → Web Push
   certificates* → *Generate key pair*. Copy the public key into `vapidKey` in
   `web-config.json`.
6. **Service account for the rpi** → *Project Settings → Service accounts →
   Generate new private key*. This JSON **is a secret** — store it in
   `watermeter-config` (wired up when the rpi talks to Firestore/FCM), never in
   this repo.

Then populate `watermeter-config/config/firebase/` (see the `*.example.json`
files there), and:

```sh
firebase login                       # once, to authenticate the CLI
./dev/render-config.sh               # generate .firebaserc, pwa/firebase-config.js, firestore.rules
firebase deploy --only firestore:rules
```

## Database (poor man's migrations)

```sql
create table meter (
   id serial primary key,
   recorded_at timestamp not null
);
```

## Building and running

From the repo root (`make` targets):

```sh
make build           # builds rpi/ -> bin/watermeter and copies the .env
make run             # build, stop the service, run bin/watermeter in foreground
make startService    # sudo systemctl restart watermeter
make stopService     # sudo systemctl stop watermeter
make deploy-lambdas token=<MFA>   # build + deploy the inbound-text lambda
```

Deploying a lambda runs `bin/deploy-lambda/run.sh <profile> <name> <mfa-token>`,
which vets/tests, cross-compiles for `linux/amd64`, zips, and creates/updates
the function, IAM role, and SQS queue.

## Setup notes / gotchas

- **Twilio** — you must configure the webhook to point at the Lambda Function URL.
- **Lambda** — the `Function URL` must be enabled (the deploy tool doesn't do
  this yet — see `bin/deploy-lambda/main.go`).
- The two config repos must be cloned side by side for the build to find the
  `.env` files.
