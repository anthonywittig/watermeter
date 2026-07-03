// App-shell service worker. Network-first so an online user always gets the
// latest code (important — this app controls a water valve); the cache is only
// an offline fallback. Bump CACHE whenever you want to guarantee old entries
// are dropped. Phase 3 adds a separate firebase-messaging-sw.js for push.

const CACHE = "watermeter-shell-v2";
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

  // Network-first: fetch fresh, cache a copy for offline, fall back to cache
  // (and to the cached shell for navigations) when the network is unavailable.
  event.respondWith(
    fetch(request)
      .then((response) => {
        const copy = response.clone();
        caches.open(CACHE).then((cache) => cache.put(request, copy));
        return response;
      })
      .catch(() =>
        caches.match(request).then(
          (cached) =>
            cached ||
            (request.mode === "navigate" ? caches.match("./index.html") : undefined)
        )
      )
  );
});
