// Watermeter PWA — Phase 1: Google sign-in.
//
// Authorization (the email allowlist) is enforced server-side by Firestore
// security rules, which arrive in Phase 2. This file only proves that Google
// sign-in works and reflects the auth state in the UI.

import { initializeApp } from "https://www.gstatic.com/firebasejs/10.12.2/firebase-app.js";
import {
  getAuth,
  GoogleAuthProvider,
  signInWithPopup,
  signOut,
  onAuthStateChanged,
} from "https://www.gstatic.com/firebasejs/10.12.2/firebase-auth.js";

import { firebaseConfig } from "./firebase-config.js";

const app = initializeApp(firebaseConfig);
const auth = getAuth(app);
const provider = new GoogleAuthProvider();

const els = {
  loading: document.getElementById("loading"),
  signedOut: document.getElementById("signed-out"),
  signedIn: document.getElementById("signed-in"),
  userEmail: document.getElementById("user-email"),
  signIn: document.getElementById("sign-in"),
  signOut: document.getElementById("sign-out"),
  error: document.getElementById("error"),
};

function showError(message) {
  els.error.textContent = message;
  els.error.hidden = false;
}

function clearError() {
  els.error.hidden = true;
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

onAuthStateChanged(auth, (user) => {
  els.loading.hidden = true;
  if (user) {
    els.userEmail.textContent = user.email ?? "";
    els.signedIn.hidden = false;
    els.signedOut.hidden = true;
  } else {
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
