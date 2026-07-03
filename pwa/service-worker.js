// Minimal app-shell service worker so the PWA is installable and the static
// shell works offline. Bump CACHE when the shell assets change.
//
// Note: firebase-config.js and the Firebase SDK (loaded from gstatic) are not
// pre-cached here — auth needs the network anyway, and we don't want to pin a
// stale config. Phase 3 adds a separate firebase-messaging-sw.js for push.

const CACHE = "watermeter-shell-v1";
const SHELL = [
  "./",
  "./index.html",
  "./styles.css",
  "./app.js",
  "./manifest.json",
  "./icons/icon-192.png",
];

self.addEventListener("install", (event) => {
  event.waitUntil(caches.open(CACHE).then((cache) => cache.addAll(SHELL)));
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) =>
        Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k)))
      )
  );
  self.clients.claim();
});

self.addEventListener("fetch", (event) => {
  const { request } = event;

  // Only handle same-origin GETs; let everything else (Firebase, Google) pass.
  if (request.method !== "GET" || new URL(request.url).origin !== self.location.origin) {
    return;
  }

  // Network-first for navigations so app updates show up; fall back to cache.
  if (request.mode === "navigate") {
    event.respondWith(
      fetch(request).catch(() => caches.match("./index.html"))
    );
    return;
  }

  // Cache-first for other same-origin shell assets.
  event.respondWith(
    caches.match(request).then((cached) => cached || fetch(request))
  );
});
