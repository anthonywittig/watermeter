# PWA migration plan: replacing Twilio with a Firebase PWA

> Status: **complete** (Phase 5 landed; Phase 4 onboarding continues as family
> members add devices). Decisions locked along the way: **all-in on Firebase/GCP**
> (Twilio *and* AWS retired) on a **dedicated new project** (`watermeter-501022`,
> *not* the monitoring project — monitoring downtime during the transition was
> acceptable), with a **parallel rollout** of the Twilio/valve path until the
> family was onboarded. This doc is kept as the historical record of the plan.

## Goal

Replace the Twilio SMS interface with an installable Progressive Web App so the
family can:

- "Install" a simple app on their Android devices,
- receive **push notifications** when the water is shut off (today's SMS alert),
- **turn the water back on** from the app (today's inbound text), and
- sign in with their **Google accounts**, with allowed emails hardcoded in the
  [`watermeter-config`](https://github.com/anthonywittig/watermeter-config) repo.

## Why Firebase

Google sign-in, push, and the command channel can all live in Firebase, so we can
drop **both** Twilio and AWS (SQS + the `inbound-text` Lambda) and run the
family-facing system on a single cloud. We're using a **dedicated new project**
(`watermeter-501022`) rather than the existing monitoring project, so the rpi will
need a **new service-account credential** scoped to it (the current
`GOOGLE_APPLICATION_CREDENTIALS` is for the monitoring project). Monitoring may go
dark during the transition — an accepted tradeoff.

Properties that make this a good fit:

- **No inbound ports on the rpi.** A Firestore listener is an *outbound*
  connection, like the current SQS poll — the home network/NAT is untouched.
- **The rpi can send push directly.** With a service-account credential for the
  project, it can call FCM itself on shutoff; no extra backend hop.
- **Possibly zero backend servers.** The PWA writes commands straight to
  Firestore, with **security rules** enforcing the email allowlist.

## What maps to what

| Today (Twilio + AWS)                        | PWA world (Firebase)                                   |
|---------------------------------------------|--------------------------------------------------------|
| Family texts a number → `inbound-text` Lambda | Family taps a button in the installed PWA            |
| Lambda validates allowed phone numbers      | Firebase Auth (Google sign-in) + email allowlist       |
| Lambda → SQS FIFO queue                      | PWA → Firestore doc                                    |
| `remotecontrol.go` polls SQS every 10s      | rpi opens a Firestore realtime listener (push, lower latency) |
| `texter.go` sends Twilio SMS on shutoff     | rpi sends a push notification (FCM) to registered devices |
| Allowed phone numbers in config             | Allowed Google emails in config (`watermeter-config`)  |

## Caveats

- **iOS web push** needs iOS 16.4+ and "Add to Home Screen." Android (the target)
  is fine — flagged only for any iPhone family members.
- **This button controls a physical valve on the house water supply.** Auth must
  be enforced in Firestore security rules (verify the Google ID token + allowlist
  on every command), never just hidden in the UI.
- **Push tokens rotate/expire.** The PWA should refresh its token on each launch
  and the rpi should prune tokens FCM reports as invalid.

## Prerequisite — bump the rpi Go version

`rpi/go.mod` is on **Go 1.14**; current Firestore/Firebase Go SDKs need ~1.21+.
This is the first step since the whole Pi side depends on it. (The lambdas are
already on 1.19.)

## Phases

Sequenced so the family-facing path (auth → control → push) works end-to-end
before Twilio is removed, with the Pi running both command channels in parallel
during the transition.

### Phase 0 — Firebase foundations ✅
- Dedicated Firebase project **`watermeter-501022`** with Google Authentication
  (consent screen published to Production), Firestore, and Cloud Messaging enabled.
- **This repo stays deployment-agnostic:** all deployment-specific values (the
  Firebase project ID, web config, VAPID key, and allowed emails — even the
  publicly-embedded ones) live in `watermeter-config/config/firebase/`.
  `dev/render-config.sh` generates the actual deployment files —`.firebaserc`,
  `pwa/firebase-config.js`, and `firestore.rules` — which are all **gitignored**.
- The **email allowlist** (`config/firebase/allowed-emails.json`) is injected
  into `firestore.rules.template`'s `isAllowed()` gate.
- A **service-account key** for the rpi was generated; it'll be stored in
  `watermeter-config` and wired up in Phase 2/3.

### Phase 1 — PWA shell + Google auth
- New `pwa/` (or `web/`) directory in this repo: `index.html`, `manifest.json`
  (`display: standalone` so Android offers "Add to Home Screen"), service worker.
- Firebase JS SDK Google sign-in; gate the UI on *signed-in **and** email in the
  allowlist*.
- Deploy to Firebase Hosting (free HTTPS + `*.web.app` domain; required for PWAs
  and push).
- **Milestone:** install on an Android phone, sign in with Google.

### Phase 2 — Command path (turn water on/off)
- Firestore model: a `valve` doc with desired state + `requestedBy` + timestamp
  (maps to the existing `ValveChangeRequested{ Level }`; `level <= 0` = close).
- **Security rules** enforce: only allowlisted emails can write, and the level is
  in range. This is the load-bearing auth check.
- rpi: add a Firestore listener in a new package translating writes into
  `valve.Open()` / `valve.Close()`. Run it **alongside** the existing
  `remotecontrol.go` SQS poller during the parallel phase — don't remove SQS yet.
- **Milestone:** tap the button in the PWA, the valve actuates.

### Phase 3 — Push notifications (shutoff alerts)
- PWA: request notification permission, get an FCM token, store it in Firestore
  (e.g. `pushTokens/{token}` with the owner's email). Refresh on each launch.
- `firebase-messaging-sw.js` handles background push and shows the notification;
  tapping it deep-links to the "turn water on" screen.
- rpi: in `flowmonitor.go`, when it auto-closes the valve on high flow, also send
  FCM to all registered tokens (reusing the Pi's GCP service account). Prune
  invalid tokens. Keep the Twilio text firing too, for now.
- **Milestone:** trigger a shutoff, every installed device gets a push.

### Phase 4 — Family onboarding (parallel run)
- Install on each family member's device; confirm push *and* control work for
  everyone. Twilio stays fully live as the safety net.

### Phase 5 — Retire Twilio + AWS ✅
- Removed the Twilio path (`texter.go`), the SQS poll (`remotecontrol.go` +
  `watermeter/sqs/`), the whole `lambdas/` module, and `bin/deploy-lambda/`.
  `flowmonitor` alerts via push only; `go.mod` has no AWS deps.
- Stripped Twilio/AWS config from `watermeter-config` and the `.example` files;
  READMEs / AGENTS.md rewritten for the Firebase-only architecture.
- Cloud-side cleanup (manual, in consoles): delete the `inbound-text` Lambda +
  its IAM role, the `water-meter-rpi.fifo` SQS queue, and close/release the
  Twilio number.

## New rpi dependencies (when we start)

- `cloud.google.com/go/firestore` — the command listener.
- `firebase.google.com/go/v4/messaging` — sending push (FCM).
