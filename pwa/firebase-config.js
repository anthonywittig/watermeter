// Firebase web config for the watermeter PWA.
//
// These values are PUBLIC by design — they're client-side identifiers, not
// secrets. Access control is enforced server-side by Firestore security rules
// (the email allowlist), not by hiding these.

export const firebaseConfig = {
  apiKey: "AIzaSyBMllFBa942n9BstfTKxZyFUJFSK88SyLM",
  authDomain: "watermeter-501022.firebaseapp.com",
  projectId: "watermeter-501022",
  storageBucket: "watermeter-501022.firebasestorage.app",
  messagingSenderId: "150937904299",
  appId: "1:150937904299:web:78c125d71e5425b83743ee",
};

// Public VAPID key for web push (FCM). Used by the PWA when requesting an FCM
// registration token.
export const vapidKey =
  "BJNA3QPf4rKwgJHlgSFxTCpDmX14Eqgba3KotBDG_MqiDrjzH01ed8JemevW_ehEvFpeanLBumDXtCrV-_mRX_I";
