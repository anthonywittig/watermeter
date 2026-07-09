// Watermeter PWA — Phase 2: Google sign-in + valve control.
//
// Authorization (the email allowlist) is enforced server-side by Firestore
// security rules. A non-allowlisted user can still *sign in*, but reads/writes
// to the valve doc are rejected with permission-denied — we surface that as a
// "not authorized" message rather than a broken UI.

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

import { firebaseConfig } from "./firebase-config.js";

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
  els.unauthorized.hidden = false;
}

function renderValve(snap) {
  clearError();
  els.unauthorized.hidden = true;
  els.valve.hidden = false;

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

    // Reset the control to a neutral state, then start listening. The snapshot
    // listener reveals either the valve control or the "not authorized" note.
    els.valve.hidden = true;
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
  }
});

// Register the service worker (required for install / offline shell).
if ("serviceWorker" in navigator) {
  window.addEventListener("load", () => {
    navigator.serviceWorker
      .register("./service-worker.js")
      .catch((err) => console.error("service worker registration failed", err));
  });
}
