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

// Single doc holding the desired valve state. level <= 0 = OFF, > 0 = ON.
// The rpi watches this doc (Phase 2b) and actuates the valve.
const valveRef = doc(db, "valve", "state");

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
  usageChart: document.getElementById("usage-chart"),
  usageTooltip: document.getElementById("usage-tooltip"),
  notifications: document.getElementById("notifications"),
  notificationsEnable: document.getElementById("notifications-enable"),
  notificationsStatus: document.getElementById("notifications-status"),
  unauthorized: document.getElementById("unauthorized"),
  error: document.getElementById("error"),
};

let currentUser = null;
let unsubscribeValve = null;

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

function renderValve(snap) {
  clearError();
  els.unauthorized.hidden = true;
  els.valve.hidden = false;

  // A successful valve read means this user is on the allowlist, so they can
  // also see usage and register for push (those rules use the same gate).
  subscribeUsage();
  offerNotifications();

  const data = snap.exists() ? snap.data() : null;
  const level = data?.level ?? 0;
  const on = level > 0;

  els.valveState.textContent = on ? "ON" : "OFF";
  els.valveState.classList.toggle("valve__state--on", on);
  els.valveState.classList.toggle("valve__state--off", !on);

  if (data?.requestedBy) {
    const when = data.requestedAt?.toDate ? data.requestedAt.toDate() : null;
    els.valveMeta.textContent =
      `Last set by ${data.requestedBy}` + (when ? ` · ${when.toLocaleString()}` : "");
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
    await setDoc(valveRef, {
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
// The rpi rolls the pulse log up into two Firestore docs (usage/minutely and
// usage/hourly) whose buckets are keyed by bucket-start Unix seconds (UTC).
// Everything time-zone-ish happens here in the browser's local zone: the day
// bars for week/month are grouped by *local* calendar day.

const SVG_NS = "http://www.w3.org/2000/svg";
const usageDocs = { minutely: null, hourly: null }; // raw buckets keyed by epoch-sec string
let usageRange = "day";
let usageUnsubs = [];

const USAGE_RANGES = {
  hour: { label: "last hour" },
  day: { label: "last 24 hours" },
  week: { label: "last 7 days" },
  month: { label: "last 30 days" },
};

// Build the bar list for the selected range: [{start: Date, gallons, label}].
function usageBars(range) {
  const now = Date.now();
  const bars = [];

  if (range === "hour") {
    const src = usageDocs.minutely || {};
    const curMinute = Math.floor(now / 60000) * 60;
    for (let i = 59; i >= 0; i--) {
      const sec = curMinute - i * 60;
      bars.push({ start: new Date(sec * 1000), gallons: src[String(sec)] || 0 });
    }
  } else if (range === "day") {
    const src = usageDocs.hourly || {};
    const curHour = Math.floor(now / 3600000) * 3600;
    for (let i = 23; i >= 0; i--) {
      const sec = curHour - i * 3600;
      bars.push({ start: new Date(sec * 1000), gallons: src[String(sec)] || 0 });
    }
  } else {
    // week/month: sum hourly buckets into local calendar days.
    const days = range === "week" ? 7 : 30;
    const src = usageDocs.hourly || {};
    const byDay = new Map();
    for (const [secStr, gal] of Object.entries(src)) {
      const d = new Date(Number(secStr) * 1000);
      const key = `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`;
      byDay.set(key, (byDay.get(key) || 0) + gal);
    }
    for (let i = days - 1; i >= 0; i--) {
      const d = new Date(now - i * 86400000);
      const key = `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`;
      bars.push({
        start: new Date(d.getFullYear(), d.getMonth(), d.getDate()),
        gallons: byDay.get(key) || 0,
      });
    }
  }
  return bars;
}

function usageBarLabel(range, start) {
  if (range === "hour" || range === "day") {
    return start.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
  }
  return start.toLocaleDateString([], { weekday: "short", month: "short", day: "numeric" });
}

// Which bar indexes get an x-axis tick.
function usageTickIndexes(range, bars) {
  const every = { hour: 15, day: 6, week: 1, month: 7 }[range];
  const ticks = [];
  bars.forEach((b, i) => {
    if (range === "hour" || range === "day") {
      // tick on round local times (:00/:15/… for hour, midnight/6/12/18 for day)
      const unit = range === "hour" ? b.start.getMinutes() : b.start.getHours();
      if (unit % every === 0) ticks.push(i);
    } else if ((bars.length - 1 - i) % every === 0) {
      ticks.push(i);
    }
  });
  return ticks;
}

function usageTickLabel(range, start) {
  if (range === "hour") return start.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
  if (range === "day") return start.toLocaleTimeString([], { hour: "numeric" });
  if (range === "week") return start.toLocaleDateString([], { weekday: "narrow" });
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

  const bars = usageBars(usageRange);
  const total = bars.reduce((s, b) => s + b.gallons, 0);
  els.usageTotal.textContent =
    `${total >= 100 ? Math.round(total) : total.toFixed(1)} gal in the ${USAGE_RANGES[usageRange].label}`;

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
    const hit = svgEl("rect", {
      x: pad.left + (plotW / bars.length) * i, y: pad.top,
      width: plotW / bars.length, height: plotH, class: "usage__hit",
    });
    hit.addEventListener("pointerenter", () => showUsageTooltip(b, hit));
    hit.addEventListener("pointerleave", () => { els.usageTooltip.hidden = true; });
    svg.appendChild(hit);
  });
}

function showUsageTooltip(bar, hitEl) {
  const tip = els.usageTooltip;
  tip.textContent = "";
  const val = document.createElement("strong");
  val.textContent = `${bar.gallons.toFixed(1)} gal`;
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

els.usageRanges.addEventListener("click", (e) => {
  const btn = e.target.closest("button[data-range]");
  if (!btn) return;
  usageRange = btn.dataset.range;
  for (const b of els.usageRanges.querySelectorAll("button")) {
    b.setAttribute("aria-pressed", String(b === btn));
  }
  renderUsage();
});

function subscribeUsage() {
  if (usageUnsubs.length) return;
  els.usage.hidden = false;
  for (const name of ["minutely", "hourly"]) {
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
  els.usage.hidden = true;
}

// Local-dev hook: lets a localhost session inject usage buckets and render the
// chart without Firestore (there's no auth/emulator in the static preview).
if (location.hostname === "localhost" || location.hostname === "127.0.0.1") {
  window.__usageDebug = {
    setDocs(minutely, hourly) {
      usageDocs.minutely = minutely;
      usageDocs.hourly = hourly;
      els.usage.hidden = false;
      renderUsage();
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
    // snapshot listener reveals either the valve control (+ notifications)
    // or the "not authorized" note.
    els.valve.hidden = true;
    els.notifications.hidden = true;
    els.unauthorized.hidden = true;
    clearError();
    unsubscribeValve = onSnapshot(valveRef, renderValve, onValveError);
  } else {
    if (unsubscribeValve) {
      unsubscribeValve();
      unsubscribeValve = null;
    }
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
