// Watermeter PWA — Google sign-in, valve control, and shutoff push alerts.
//
// Authorization (the email allowlist) is enforced server-side by Firestore
// security rules. A non-allowlisted user can still *sign in*, but reads/writes
// to the valve doc are rejected with permission-denied — we surface that as a
// "not authorized" message rather than a broken UI.
//
// Push: we reuse our own service worker for FCM (passed to getToken via
// serviceWorkerRegistration) and handle raw `push` events there, so there's no
// separate firebase-messaging-sw.js and no config duplicated into a worker.

import { initializeApp } from "https://www.gstatic.com/firebasejs/10.12.2/firebase-app.js";
import {
  getAuth,
  GoogleAuthProvider,
  signInWithPopup,
  signOut,
  onAuthStateChanged,
} from "https://www.gstatic.com/firebasejs/10.12.2/firebase-auth.js";
import {
  getFirestore,
  doc,
  onSnapshot,
  setDoc,
  serverTimestamp,
} from "https://www.gstatic.com/firebasejs/10.12.2/firebase-firestore.js";
import {
  getMessaging,
  getToken,
  isSupported as messagingIsSupported,
} from "https://www.gstatic.com/firebasejs/10.12.2/firebase-messaging.js";

import { firebaseConfig, vapidKey } from "./firebase-config.js";

const app = initializeApp(firebaseConfig);
const auth = getAuth(app);
const db = getFirestore(app);
const provider = new GoogleAuthProvider();

// valve/state is the DESIRED state (level <= 0 = OFF, > 0 = ON) written by
// this app (and by the rpi's flow monitor on auto-shutoff). valve/actual is
// what the hardware really is, reported by the rpi — the UI shows actual, and
// "Turning on/off…" while desired and actual disagree (an actuation takes ~10s).
const valveStateRef = doc(db, "valve", "state");
const valveActualRef = doc(db, "valve", "actual");

const els = {
  loading: document.getElementById("loading"),
  signedOut: document.getElementById("signed-out"),
  signedIn: document.getElementById("signed-in"),
  userEmail: document.getElementById("user-email"),
  signIn: document.getElementById("sign-in"),
  signOut: document.getElementById("sign-out"),
  valve: document.getElementById("valve"),
  valveState: document.getElementById("valve-state"),
  valveOn: document.getElementById("valve-on"),
  valveOff: document.getElementById("valve-off"),
  valveMeta: document.getElementById("valve-meta"),
  usage: document.getElementById("usage"),
  usageRanges: document.getElementById("usage-ranges"),
  usageTotal: document.getElementById("usage-total"),
  usageBack: document.getElementById("usage-back"),
  usageChart: document.getElementById("usage-chart"),
  usageTooltip: document.getElementById("usage-tooltip"),
  notifications: document.getElementById("notifications"),
  notificationsEnable: document.getElementById("notifications-enable"),
  notificationsStatus: document.getElementById("notifications-status"),
  unauthorized: document.getElementById("unauthorized"),
  error: document.getElementById("error"),
};

let currentUser = null;
let valveUnsubs = [];
const valveDocs = { state: null, actual: null }; // null = not loaded yet

function showError(message) {
  els.error.textContent = message;
  els.error.hidden = false;
}

function clearError() {
  els.error.hidden = true;
}

function showUnauthorized() {
  els.valve.hidden = true;
  els.notifications.hidden = true;
  unsubscribeUsage();
  els.unauthorized.hidden = false;
}

function renderValve() {
  els.valve.hidden = false;

  const state = valveDocs.state;
  const actual = valveDocs.actual;
  const desiredOn = (state?.level ?? 0) > 0;

  let text, mode;
  if (state && actual && typeof actual.open === "boolean" && actual.open !== desiredOn) {
    // The rpi hasn't caught up (an actuation takes ~10 s) — show progress.
    text = desiredOn ? "turning ON…" : "turning OFF…";
    mode = "pending";
  } else if (actual && typeof actual.open === "boolean") {
    text = actual.open ? "ON" : "OFF";
    mode = actual.open ? "on" : "off";
  } else {
    // No actual report yet (fresh install / rpi offline): show desired.
    text = desiredOn ? "ON" : "OFF";
    mode = desiredOn ? "on" : "off";
  }

  els.valveState.textContent = text;
  els.valveState.classList.toggle("valve__state--on", mode === "on");
  els.valveState.classList.toggle("valve__state--off", mode === "off");
  els.valveState.classList.toggle("valve__state--pending", mode === "pending");

  if (state?.requestedBy) {
    const when = state.requestedAt?.toDate ? state.requestedAt.toDate() : null;
    const who = state.requestedBy === "flow-monitor"
      ? "auto-shutoff (high water flow)"
      : state.requestedBy;
    els.valveMeta.textContent =
      `Last set by ${who}` + (when ? ` · ${when.toLocaleString()}` : "");
    els.valveMeta.hidden = false;
  } else {
    els.valveMeta.hidden = true;
  }
}

function onValveError(err) {
  if (err?.code === "permission-denied") {
    showUnauthorized();
  } else {
    showError(`Couldn't read the valve state: ${err?.message ?? err}`);
  }
}

async function setLevel(level) {
  if (!currentUser) return;
  clearError();
  els.valveOn.disabled = true;
  els.valveOff.disabled = true;
  try {
    await setDoc(valveStateRef, {
      level,
      requestedBy: currentUser.email,
      requestedAt: serverTimestamp(),
    });
  } catch (err) {
    if (err?.code === "permission-denied") {
      showUnauthorized();
    } else {
      showError(`Couldn't update the valve: ${err?.message ?? err}`);
    }
  } finally {
    els.valveOn.disabled = false;
    els.valveOff.disabled = false;
  }
}

// ---- Usage chart -----------------------------------------------------------
//
// The rpi rolls the pulse log up into three Firestore docs (usage/minutely,
// usage/hourly, usage/daily). Everything time-zone-ish happens here in the
// browser's local zone: the day bars for week/month are grouped by *local*
// calendar day.
//
// A view is a range (how wide a bar is, and how many) plus the window's end.
// Live views end now; clicking a bar zooms into the period that bar covers by
// pinning the end to the bar's end (usageAnchor) and switching to the next
// finer range — a day bar in the week view opens the day view for that day.
// Back pops the stack of views we zoomed in from.

const SVG_NS = "http://www.w3.org/2000/svg";
const MINUTE_MS = 60000, HOUR_MS = 60 * MINUTE_MS, DAY_MS = 24 * HOUR_MS;
// The rpi keeps ~32 days of hourly buckets — stay a little inside that.
const HOURLY_DAYS = 30;

// minutely/hourly buckets are keyed by epoch-sec string; daily by "YYYY-MM-DD"
// in the deployment's configured timezone (see USAGE_TIMEZONE on the rpi).
const usageDocs = { minutely: null, hourly: null, daily: null };
let usageRange = "day";
let usageAnchor = null; // exclusive end of the plotted window; null = live
let usageZoomStack = []; // views we zoomed in from: [{range, anchor}]
let usageUnsubs = [];

const USAGE_RANGES = {
  hour: { name: "Hour", label: "last hour", unit: "minute", count: 60 },
  day: { name: "Day", label: "last 24 hours", unit: "hour", count: 24 },
  week: { name: "Week", label: "last 7 days", unit: "day", count: 7 },
  month: { name: "Month", label: "last 30 days", unit: "day", count: 30 },
  year: { name: "Year", label: "last year", unit: "week", count: 52 },
};

// Clicking a bar opens the range that shows that bar's period in finer bars.
// The rpi only keeps ~2 h of minutely and ~32 days of hourly buckets (see
// usagepublisher.go), so bars older than their target's data don't zoom.
const USAGE_ZOOM = {
  year: { range: "week", maxAge: 370 * DAY_MS },
  month: { range: "day", maxAge: HOURLY_DAYS * DAY_MS },
  week: { range: "day", maxAge: HOURLY_DAYS * DAY_MS },
  day: { range: "hour", maxAge: 2 * HOUR_MS },
};

function localDateKey(d) {
  const pad = (n) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

// Bucket math in the browser's zone: minute/hour buckets align to the epoch
// (that's how the rpi keys them), day/week buckets to local midnight.
function bucketStart(d, unit) {
  if (unit === "minute") return new Date(Math.floor(d.getTime() / MINUTE_MS) * MINUTE_MS);
  if (unit === "hour") return new Date(Math.floor(d.getTime() / HOUR_MS) * HOUR_MS);
  return new Date(d.getFullYear(), d.getMonth(), d.getDate());
}

function addBuckets(d, unit, n) {
  if (unit === "minute") return new Date(d.getTime() + n * MINUTE_MS);
  if (unit === "hour") return new Date(d.getTime() + n * HOUR_MS);
  const days = unit === "week" ? 7 * n : n;
  return new Date(d.getFullYear(), d.getMonth(), d.getDate() + days);
}

// The exclusive end of the plotted window: the anchor when zoomed in, else the
// end of the in-progress bucket, so the current partial bar is included. Weekly
// bars aren't aligned to a calendar week — the most recent one ends today.
function usageWindowEnd(range) {
  if (usageAnchor) return usageAnchor;
  const unit = USAGE_RANGES[range].unit;
  const step = unit === "week" ? "day" : unit;
  return addBuckets(bucketStart(new Date(), step), step, 1);
}

function hourlyByLocalDay() {
  const byDay = new Map();
  for (const [secStr, gal] of Object.entries(usageDocs.hourly || {})) {
    const key = localDateKey(new Date(Number(secStr) * 1000));
    byDay.set(key, (byDay.get(key) || 0) + gal);
  }
  return byDay;
}

// Day totals come from the hourly buckets (summed into *local* days) while the
// rpi still keeps them; older days fall back to the daily doc, which is
// pre-bucketed in the deployment's timezone rather than the viewer's.
function dayGallons(day, byDay, now) {
  const key = localDateKey(day);
  if (now - day.getTime() < HOURLY_DAYS * DAY_MS) return byDay.get(key) || 0;
  return (usageDocs.daily || {})[key] || 0;
}

// Build the bar list for the current view: [{start: Date, gallons}].
function usageBars(range) {
  const { unit, count } = USAGE_RANGES[range];
  const end = usageWindowEnd(range);
  const now = Date.now();
  const byDay = unit === "day" || unit === "week" ? hourlyByLocalDay() : null;
  const bars = [];

  for (let i = count; i >= 1; i--) {
    const start = addBuckets(end, unit, -i);
    let gallons;
    if (unit === "minute" || unit === "hour") {
      const src = (unit === "minute" ? usageDocs.minutely : usageDocs.hourly) || {};
      gallons = src[String(start.getTime() / 1000)] || 0;
    } else if (unit === "day") {
      gallons = dayGallons(start, byDay, now);
    } else {
      gallons = 0;
      for (let d = 0; d < 7; d++) {
        gallons += dayGallons(addBuckets(start, "day", d), byDay, now);
      }
    }
    bars.push({ start, gallons });
  }
  return bars;
}

// The range a click on this bar opens, or null if it doesn't zoom: the finest
// range doesn't, nor do bars older than their target's data or not yet started
// (a zoomed-in view of today plots the rest of the day as empty bars).
function usageZoomTarget(bar) {
  const zoom = USAGE_ZOOM[usageRange];
  if (!zoom) return null;
  const age = Date.now() - bar.start.getTime();
  if (age < 0 || age > zoom.maxAge) return null;
  return zoom.range;
}

function zoomIntoBar(bar) {
  const range = usageZoomTarget(bar);
  if (!range) return;
  // End the zoomed view at this bar's end, snapped to the finer range's own
  // buckets: in a half-hour-offset zone (IST, say) local midnight isn't on an
  // epoch hour, and unsnapped we'd look up hourly keys that can't exist.
  const end = addBuckets(bar.start, USAGE_RANGES[usageRange].unit, 1);
  const anchor = bucketStart(end, USAGE_RANGES[range].unit);
  usageZoomStack.push({ range: usageRange, anchor: usageAnchor });
  usageRange = range;
  usageAnchor = anchor;
  renderUsage();
}

function zoomOut() {
  const prev = usageZoomStack.pop();
  if (!prev) return;
  usageRange = prev.range;
  usageAnchor = prev.anchor;
  renderUsage();
}

// Describes the plotted window for the total line.
function usageWindowLabel(bars) {
  if (!usageAnchor) return `in the ${USAGE_RANGES[usageRange].label}`;

  const first = bars[0].start;
  const last = bars[bars.length - 1].start;
  // Spell out the year on older windows — "Aug 12" alone is ambiguous there.
  const year = first.getFullYear() === new Date().getFullYear() ? undefined : "numeric";
  const day = (d) =>
    d.toLocaleDateString([], { weekday: "short", month: "short", day: "numeric", year });
  if (usageRange === "hour") {
    // Buckets are epoch hours, so in a half-hour-offset zone they start at :30.
    const hour = (d) => d.toLocaleTimeString([], d.getMinutes()
      ? { hour: "numeric", minute: "2-digit" } : { hour: "numeric" });
    return `from ${hour(first)} to ${hour(new Date(first.getTime() + HOUR_MS))} on ${day(first)}`;
  }
  // Name the day from the middle of the window: its edges can sit in the
  // neighbouring day once the anchor is snapped to an epoch hour.
  if (usageRange === "day") return `on ${day(bars[Math.floor(bars.length / 2)].start)}`;
  const short = (d, y) => d.toLocaleDateString([], { month: "short", day: "numeric", year: y });
  const firstYear = first.getFullYear() === last.getFullYear() ? undefined : year;
  return `from ${short(first, firstYear)} to ${short(last, year)}`;
}

function usageBarLabel(range, start) {
  if (range === "hour" || range === "day") {
    return start.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
  }
  if (range === "year") {
    return `Week of ${start.toLocaleDateString([], { month: "short", day: "numeric" })}`;
  }
  return start.toLocaleDateString([], { weekday: "short", month: "short", day: "numeric" });
}

// Which bar indexes get an x-axis tick.
function usageTickIndexes(range, bars) {
  const ticks = [];
  bars.forEach((b, i) => {
    if (range === "hour" || range === "day") {
      // tick on round local times (:00/:15/… for hour, midnight/6/12/18 for day)
      const every = range === "hour" ? 15 : 6;
      const unit = range === "hour" ? b.start.getMinutes() : b.start.getHours();
      if (unit % every === 0) ticks.push(i);
    } else if (range === "year") {
      // tick on the first week bar of each month
      if (i > 0 && b.start.getMonth() !== bars[i - 1].start.getMonth()) ticks.push(i);
    } else {
      const every = range === "week" ? 1 : 7;
      if ((bars.length - 1 - i) % every === 0) ticks.push(i);
    }
  });
  return ticks;
}

function usageTickLabel(range, start) {
  if (range === "hour") return start.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
  if (range === "day") return start.toLocaleTimeString([], { hour: "numeric" });
  if (range === "week") return start.toLocaleDateString([], { weekday: "narrow" });
  if (range === "year") return start.toLocaleDateString([], { month: "short" });
  return start.toLocaleDateString([], { day: "numeric" });
}

function niceMax(v) {
  if (v <= 0) return 1;
  const pow = Math.pow(10, Math.floor(Math.log10(v)));
  for (const m of [1, 2, 5, 10]) {
    if (v <= m * pow) return m * pow;
  }
  return 10 * pow;
}

function svgEl(name, attrs) {
  const el = document.createElementNS(SVG_NS, name);
  for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, v);
  return el;
}

// A bar path with rounded top corners only (data end), square at the baseline.
function barPath(x, y, w, h, r) {
  r = Math.min(r, w / 2, h);
  const bottom = y + h;
  return `M${x},${bottom} V${y + r} Q${x},${y} ${x + r},${y} H${x + w - r} Q${x + w},${y} ${x + w},${y + r} V${bottom} Z`;
}

function renderUsage() {
  const svg = els.usageChart;
  svg.textContent = "";
  els.usageTooltip.hidden = true;

  for (const b of els.usageRanges.querySelectorAll("button")) {
    b.setAttribute("aria-pressed", String(b.dataset.range === usageRange));
  }
  const zoomedFrom = usageZoomStack[usageZoomStack.length - 1];
  els.usageBack.hidden = !zoomedFrom;
  if (zoomedFrom) els.usageBack.textContent = `← ${USAGE_RANGES[zoomedFrom.range].name}`;

  const bars = usageBars(usageRange);
  const total = bars.reduce((s, b) => s + b.gallons, 0);
  const totalText = total >= 100 ? Math.round(total).toLocaleString() : total.toFixed(1);
  els.usageTotal.textContent = `${totalText} gal ${usageWindowLabel(bars)}`;

  const W = 360, H = 160;
  const pad = { left: 30, right: 4, top: 8, bottom: 18 };
  const plotW = W - pad.left - pad.right;
  const plotH = H - pad.top - pad.bottom;
  const yMax = niceMax(Math.max(...bars.map((b) => b.gallons)));
  const gap = 2;
  const barW = Math.max(1, plotW / bars.length - gap);

  // y gridlines + labels (recessive)
  for (const frac of [0.5, 1]) {
    const val = yMax * frac;
    const y = pad.top + plotH * (1 - frac);
    svg.appendChild(svgEl("line", {
      x1: pad.left, y1: y, x2: W - pad.right, y2: y, class: "usage__grid",
    }));
    const lbl = svgEl("text", { x: pad.left - 4, y: y + 3, class: "usage__tick", "text-anchor": "end" });
    lbl.textContent = val >= 10 ? String(Math.round(val)) : String(val);
    svg.appendChild(lbl);
  }
  // baseline
  svg.appendChild(svgEl("line", {
    x1: pad.left, y1: pad.top + plotH, x2: W - pad.right, y2: pad.top + plotH,
    class: "usage__baseline",
  }));

  const ticks = new Set(usageTickIndexes(usageRange, bars));
  let maxIdx = -1;
  bars.forEach((b, i) => { if (b.gallons > 0 && (maxIdx < 0 || b.gallons > bars[maxIdx].gallons)) maxIdx = i; });

  bars.forEach((b, i) => {
    const x = pad.left + (plotW / bars.length) * i + gap / 2;
    const h = yMax > 0 ? (b.gallons / yMax) * plotH : 0;
    const y = pad.top + plotH - h;

    if (h > 0) {
      svg.appendChild(svgEl("path", { d: barPath(x, y, barW, h, 2), class: "usage__bar" }));
    }

    // selective direct label: the max bar only
    if (i === maxIdx && h > 10) {
      const lbl = svgEl("text", {
        x: x + barW / 2, y: Math.max(pad.top + 8, y - 3),
        class: "usage__tick usage__tick--label", "text-anchor": "middle",
      });
      lbl.textContent = b.gallons >= 10 ? Math.round(b.gallons) : b.gallons.toFixed(1);
      svg.appendChild(lbl);
    }

    if (ticks.has(i)) {
      const lbl = svgEl("text", {
        x: x + barW / 2, y: H - 5, class: "usage__tick", "text-anchor": "middle",
      });
      lbl.textContent = usageTickLabel(usageRange, b.start);
      svg.appendChild(lbl);
    }

    // full-height transparent hit target, bigger than the mark
    const zoomTo = usageZoomTarget(b);
    const hit = svgEl("rect", {
      x: pad.left + (plotW / bars.length) * i, y: pad.top,
      width: plotW / bars.length, height: plotH,
      class: zoomTo ? "usage__hit usage__hit--zoom" : "usage__hit",
    });
    hit.addEventListener("pointerenter", () => showUsageTooltip(b, hit));
    hit.addEventListener("pointerleave", () => { els.usageTooltip.hidden = true; });
    if (zoomTo) hit.addEventListener("click", () => zoomIntoBar(b));
    svg.appendChild(hit);
  });
}

function showUsageTooltip(bar, hitEl) {
  const tip = els.usageTooltip;
  tip.textContent = "";
  const val = document.createElement("strong");
  val.textContent = `${
    bar.gallons >= 100 ? Math.round(bar.gallons).toLocaleString() : bar.gallons.toFixed(1)
  } gal`;
  const when = document.createElement("span");
  when.textContent = usageBarLabel(usageRange, bar.start);
  tip.append(val, when);
  tip.hidden = false;

  const wrap = tip.parentElement.getBoundingClientRect();
  const r = hitEl.getBoundingClientRect();
  // Clamp to the wrap, applying the >= 0 bound last so a tooltip wider than
  // the wrap pins to the left edge instead of going negative.
  let left = r.left - wrap.left + r.width / 2 - tip.offsetWidth / 2;
  left = Math.min(left, wrap.width - tip.offsetWidth);
  left = Math.max(left, 0);
  tip.style.left = `${left}px`;
}

// Picking a range always goes back to the live (trailing) view of it.
els.usageRanges.addEventListener("click", (e) => {
  const btn = e.target.closest("button[data-range]");
  if (!btn) return;
  usageRange = btn.dataset.range;
  usageAnchor = null;
  usageZoomStack = [];
  renderUsage();
});

els.usageBack.addEventListener("click", zoomOut);

function subscribeUsage() {
  if (usageUnsubs.length) return;
  els.usage.hidden = false;
  for (const name of ["minutely", "hourly", "daily"]) {
    usageUnsubs.push(
      onSnapshot(doc(db, "usage", name), (snap) => {
        usageDocs[name] = snap.exists() ? snap.data().buckets || {} : {};
        renderUsage();
      }, (err) => console.error(`usage/${name} listener`, err))
    );
  }
}

function unsubscribeUsage() {
  for (const u of usageUnsubs) u();
  usageUnsubs = [];
  usageAnchor = null;
  usageZoomStack = [];
  els.usage.hidden = true;
}

// Local-dev hook: lets a localhost session inject usage buckets and render the
// chart without Firestore (there's no auth/emulator in the static preview).
if (location.hostname === "localhost" || location.hostname === "127.0.0.1") {
  window.__usageDebug = {
    setDocs(minutely, hourly, daily) {
      usageDocs.minutely = minutely;
      usageDocs.hourly = hourly;
      usageDocs.daily = daily ?? usageDocs.daily;
      els.usage.hidden = false;
      renderUsage();
    },
    setValve(state, actual) {
      valveDocs.state = state;
      valveDocs.actual = actual;
      renderValve();
    },
  };
}

// ---- Push notifications (shutoff alerts) ----------------------------------

function notificationsStatus(text) {
  els.notificationsStatus.textContent = text;
  els.notificationsStatus.hidden = !text;
}

// Fetch an FCM token for this device and record it in Firestore so the rpi
// can notify us. Safe to call repeatedly — the doc id is the token itself, so
// refreshes just overwrite.
async function registerPushToken() {
  const supported = await messagingIsSupported();
  if (!supported) {
    notificationsStatus("Push isn't supported in this browser.");
    return;
  }

  const registration = await swRegistration;
  if (!registration) {
    notificationsStatus("Service worker unavailable; alerts disabled.");
    return;
  }

  const messaging = getMessaging(app);
  const token = await getToken(messaging, {
    vapidKey,
    serviceWorkerRegistration: registration,
  });

  await setDoc(doc(db, "pushTokens", token), {
    email: currentUser.email,
    updatedAt: serverTimestamp(),
  });

  els.notificationsEnable.hidden = true;
  notificationsStatus("Shutoff alerts are on for this device.");
}

// Decide what the notifications UI should show for an allowlisted user, and
// silently refresh the token when permission was already granted.
async function offerNotifications() {
  els.notifications.hidden = false;

  if (!("Notification" in window)) {
    notificationsStatus("Notifications aren't supported in this browser.");
    return;
  }

  if (Notification.permission === "granted") {
    els.notificationsEnable.hidden = true;
    try {
      await registerPushToken();
    } catch (err) {
      console.error("push token refresh failed", err);
      notificationsStatus("Couldn't refresh shutoff alerts on this device.");
    }
  } else if (Notification.permission === "denied") {
    els.notificationsEnable.hidden = true;
    notificationsStatus(
      "Notifications are blocked for this app — enable them in your browser settings to get shutoff alerts."
    );
  } else {
    els.notificationsEnable.hidden = false;
    notificationsStatus("");
  }
}

els.notificationsEnable.addEventListener("click", async () => {
  clearError();
  els.notificationsEnable.disabled = true;
  try {
    const permission = await Notification.requestPermission();
    if (permission !== "granted") {
      await offerNotifications();
      return;
    }
    await registerPushToken();
  } catch (err) {
    console.error("enabling notifications failed", err);
    notificationsStatus(`Couldn't enable alerts: ${err?.message ?? err}`);
  } finally {
    els.notificationsEnable.disabled = false;
  }
});

// ---- Auth ------------------------------------------------------------------

els.signIn.addEventListener("click", async () => {
  clearError();
  try {
    await signInWithPopup(auth, provider);
  } catch (err) {
    // auth/popup-closed-by-user is a normal cancel; don't shout about it.
    if (err?.code !== "auth/popup-closed-by-user") {
      showError(`Sign-in failed: ${err?.message ?? err}`);
    }
  }
});

els.signOut.addEventListener("click", () => signOut(auth));
els.valveOn.addEventListener("click", () => setLevel(10));
els.valveOff.addEventListener("click", () => setLevel(0));

onAuthStateChanged(auth, (user) => {
  els.loading.hidden = true;
  currentUser = user;

  if (user) {
    els.userEmail.textContent = user.email ?? "";
    els.signedIn.hidden = false;
    els.signedOut.hidden = true;

    // Reset the controls to a neutral state, then start listening. The
    // snapshot listeners reveal either the valve control (+ notifications)
    // or the "not authorized" note.
    els.valve.hidden = true;
    els.notifications.hidden = true;
    els.unauthorized.hidden = true;
    clearError();
    valveDocs.state = null;
    valveDocs.actual = null;
    valveUnsubs = [
      onSnapshot(valveStateRef, (snap) => {
        valveDocs.state = snap.exists() ? snap.data() : {};
        clearError();
        els.unauthorized.hidden = true;
        // A successful valve read means this user is on the allowlist, so
        // they can also see usage and register for push (same rules gate).
        subscribeUsage();
        offerNotifications();
        renderValve();
      }, onValveError),
      onSnapshot(valveActualRef, (snap) => {
        valveDocs.actual = snap.exists() ? snap.data() : {};
        renderValve();
      }, (err) => {
        // The state listener surfaces permission problems; just log here.
        if (err?.code !== "permission-denied") console.error("valve/actual listener", err);
      }),
    ];
  } else {
    for (const u of valveUnsubs) u();
    valveUnsubs = [];
    els.signedIn.hidden = true;
    els.signedOut.hidden = false;
    els.notifications.hidden = true;
    unsubscribeUsage();
  }
});

// Register the service worker (required for install / offline shell, and the
// registration FCM delivers push through). Registered immediately — not on
// window load — so push setup never races it.
const swRegistration = ("serviceWorker" in navigator)
  ? navigator.serviceWorker.register("./service-worker.js").catch((err) => {
      console.error("service worker registration failed", err);
      return null;
    })
  : Promise.resolve(null);
