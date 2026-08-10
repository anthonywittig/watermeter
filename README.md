# watermeter

A Raspberry Pi–based water meter monitor with automatic and remote water shutoff.

The house water meter has a magnet on its spinning dial; a passive two-wire reed
switch mounted next to the dial closes once per revolution, generating one pulse
per 0.1 gallon. A Go service on the Pi counts those pulses, records them to
several backends, and watches for runaway flow. If too much water flows in a
short window (e.g. a burst pipe), it automatically closes a motorized valve and
push-notifies the family. Anyone in the family can turn the water back on (or
off) from an installable web app (PWA).

```
                          ┌───────────────────────────────────────────┐
  water meter ──pulse──▶  │ rpi service (Go, runs on the Raspberry Pi)│
  (GPIO 18)               │                                           │
                          │  pulse fan-out ─▶ Postgres                │
                          │                ─▶ Prometheus (:8000)      │
                          │                ─▶ GCP custom metric       │
                          │                                           │
                          │  flow monitor ─▶ close valve + FCM push   │
                          │  (> 20 gal / 5 min)      │                │
                          │                          ▼                │
   family's PWA ◀──── FCM push ◀──────── pushTokens (Firestore)       │
   (Firebase Hosting,                                                 │
    Google sign-in) ──── valve/state (Firestore) ──▶ listener ──▶ valve
                          └───────────────────────────────────────────┘
```

Everything runs on one Firebase project: **Hosting** serves the PWA, **Auth**
(Google sign-in) + **Firestore security rules** enforce an email allowlist,
**Firestore** carries the desired valve state, and **Cloud Messaging (FCM)**
delivers shutoff alerts. The Pi makes only outbound connections.

Original meter pulse wiring comes from the
[Freenove Ultimate Starter Kit tutorial](https://github.com/Freenove/Freenove_Ultimate_Starter_Kit_for_Raspberry_Pi/blob/master/Tutorial.pdf),
chapter 2 (the button wiring).

## Repository layout

| Path        | What it is |
|-------------|------------|
| `rpi/`      | The Go module (`github.com/anthonywittig/watermeter`, Go ≥ 1.23) — the long-running service on the Pi. |
| `rpi/cmd/testpush/` | Sends a test push through the real notification path (no need to run 20 gallons through the meter). |
| `pwa/`      | The installable web app: valve control + shutoff alerts. Vanilla JS, Firebase SDK from the CDN, no build step. |
| `dev/`      | `build.sh`, `render-config.sh`, `deploy-rpi.sh`, and the `watermeter.service` systemd unit. |
| `rpi/monitoring/` | Prometheus + postgres_exporter Docker setup for the Pi. |

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
  push-notifies every registered device.
- **Remote control** (`watermeter/firestorecontrol.go`) — a realtime Firestore
  listener on the `valve/state` doc; `level <= 0` closes the valve, anything
  else opens it. Only actuates on state *changes* (restarts don't cycle the
  valve needlessly), and converges the valve to the stored state on startup.
- **Push** (`watermeter/push.go`) — fans a data-only FCM message out to every
  token in the `pushTokens` collection, pruning tokens FCM reports dead.
- **Usage publisher** (`watermeter/usagepublisher.go`) — every minute, rolls the
  Postgres pulse log up into `usage/minutely` (last ~6 h) and `usage/hourly`
  (last ~90 days) Firestore docs, keyed by bucket-start Unix seconds, plus
  `usage/daily` (last ~3 years, keyed by date in the configured timezone) every
  15 minutes; skips the write when nothing changed. The PWA charts from these,
  and mirrors the windows to decide how far back a bar can be zoomed into —
  widening a window means changing both sides.
- **Usage archives** (`watermeter/usagearchive.go`) — on the same 15-minute
  tick, writes one document per UTC day of minute buckets
  (`usage/minutely-YYYY-MM-DD`), plus `usage/minutely-index` listing which days
  exist and which are final, backfilling a few days per run up to ~3 years back.
  Nothing subscribes to these: the PWA fetches one on demand when you zoom into
  an hour the live doc no longer covers, so minute-level history costs nothing
  until someone looks at it.
- **Valve** (`watermeter/iot/valve.go`) — drives a two-relay motorized valve;
  relays are active-low and held for 10 s per actuation (mutex-guarded).
- Prometheus metrics are served at `:8000/metrics`.

## How the PWA works

The app lives in `pwa/` and is served from Firebase Hosting (installable on
Android via "Add to Home Screen"; iOS 16.4+ also works if added to the home
screen).

1. **Sign in with Google.** Authentication says who you are; *authorization* is
   the email allowlist enforced server-side by Firestore security rules — a
   signed-in stranger gets "not authorized", and their reads/writes are
   rejected by the rules regardless of what the UI shows.
2. **Valve control.** The ON/OFF buttons write `{level, requestedBy,
   requestedAt}` to the `valve/state` doc; a live snapshot listener shows the
   current state and who last changed it. The rpi's listener actuates the real
   valve.
3. **Shutoff alerts.** "Enable shutoff alerts" (per device) registers an FCM
   token under `pushTokens/{token}`. The app's own service worker receives the
   push and shows the notification; tapping it opens the app on the valve
   control. There's no separate `firebase-messaging-sw.js` — the one service
   worker handles the app shell *and* push.
4. **Usage chart.** A bar chart of water usage with hour / day / week / month /
   year ranges, live-updating from the `usage/*` docs. Minute bars for the hour
   view, hour bars for the day view; week/month aggregate the hourly buckets
   into calendar days in the viewer's own timezone, and the year view sums days
   into weeks. Clicking a bar zooms into the period it covers — a day in the
   week view opens the day view for that day, an hour in the day view opens
   that hour — and "← Back" returns to where you zoomed in from (picking a
   range button goes back to its live, trailing view). Day bars zoom while the
   hourly buckets behind them still exist (~90 days); hour bars zoom whenever
   the minute data exists — the live ~6 h window, or any UTC day with an
   archive — fetching that day's archive on demand and showing "Loading…" until
   it lands.

## Configuration

**No deployment-specific values live in this repo** — that's deliberate, so it
can be forked and deployed by anyone who supplies their own config. Everything
project-specific (secrets *and* otherwise-public identifiers like the Firebase
project ID) lives in the sibling
[`watermeter-config`](https://github.com/anthonywittig/watermeter-config) repo,
which must be cloned next to this one (`../watermeter-config`).

- `dev/build.sh` copies the rpi `.env` and the Firebase service-account key
  into `bin/`.
- `dev/render-config.sh` generates the Firebase deployment files —
  `.firebaserc`, `pwa/firebase-config.js`, and `firestore.rules` — from
  `watermeter-config/config/firebase/`. All three are **gitignored** (they're
  generated); run it before `firebase deploy`.

See the `*.example*` files in `watermeter-config` (and `rpi/.env.example`) for
the shape of the config.

## Firebase setup (one-time, manual)

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
   `watermeter-config` (the rpi uses it for Firestore + FCM), never in this
   repo.

Then populate `watermeter-config/config/firebase/` (see the `*.example.json`
files there), and:

```sh
firebase login                       # once, to authenticate the CLI
./dev/render-config.sh               # generate .firebaserc, pwa/firebase-config.js, firestore.rules
firebase deploy                      # hosting + firestore rules
```

## Database (poor man's migrations)

```sql
create table meter (
   id serial primary key,
   recorded_at timestamp not null
);

-- The usage publisher aggregates ~90 days of pulses every minute (and ~3 years
-- every 15 minutes, for the daily rollup); this index keeps those queries off a
-- full table scan.
create index if not exists meter_recorded_at_idx on meter (recorded_at);
```

## Building and running

**Deploying to the Pi:** the Pi's Go is too old to build this repo — cross-compile
on your workstation and copy the binary over. See
**[docs/rpi-deploy.md](docs/rpi-deploy.md)** for the full recipe, including
verification and rollback. Short version:

```sh
./dev/deploy-rpi.sh <pi-address>     # or: make deploy-rpi pi=<pi-address>
```

**Deploying the PWA / rules:**

```sh
./dev/render-config.sh && firebase deploy
```

On a machine with Go ≥ 1.23, the `make` targets work from the repo root:

```sh
make build           # builds rpi/ -> bin/watermeter, copies .env + service-account
make run             # build, stop the service, run bin/watermeter in foreground
make startService    # sudo systemctl restart watermeter
make stopService     # sudo systemctl stop watermeter
```

**Testing push notifications** (uses the real send path):

```sh
cd rpi && go run ./cmd/testpush \
  -project <firebase-project-id> \
  -credentials ../../watermeter-config/config/firebase/service-account.json
```

## Setup notes / gotchas

- The two repos must be cloned side by side for the build/render steps to find
  the config.
- **Gmail dots matter for the allowlist.** ID tokens carry the account's
  canonical address and the rules do an exact string match — `jane.doe@` won't
  match an account registered as `janedoe@`. If someone sees "not authorized",
  check their address character-for-character.
- Every Pi deploy restarts the service, which cycles the valve (close → open,
  ~20 s) — a brief water interruption.
- Push tokens are per-device: each phone/browser needs its own "Enable shutoff
  alerts" tap. Dead tokens are pruned automatically when a send fails.

## History

This project originally alerted via Twilio SMS and took commands by text
message through an AWS Lambda + SQS pipeline. That stack was retired in favor
of the Firebase PWA — see [docs/pwa-migration-plan.md](docs/pwa-migration-plan.md)
for the migration plan and rationale. The old implementation lives in git
history if you're curious.
