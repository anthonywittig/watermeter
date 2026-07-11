// App-shell service worker. Network-first so an online user always gets the
// latest code (important — this app controls a water valve); the cache is only
// an offline fallback. Bump CACHE whenever you want to guarantee old entries
// are dropped.
//
// This worker also receives FCM push (the page passes this registration to
// getToken), so there's no separate firebase-messaging-sw.js. The rpi sends
// data-only messages ({data: {title, body}}) and the handlers below display
// them — keeping display logic here rather than split with the FCM SDK.

const CACHE = "watermeter-shell-v4";
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

self.addEventListener("push", (event) => {
  if (!event.data) return;

  let payload = {};
  try {
    payload = event.data.json();
  } catch {
    payload = {};
  }
  const data = payload.data || {};

  event.waitUntil(
    self.registration.showNotification(data.title || "Watermeter", {
      body: data.body || "",
      icon: "./icons/icon-192.png",
      badge: "./icons/icon-192.png",
      tag: "watermeter", // collapse repeats into one notification
    })
  );
});

// Tapping the notification focuses the app (or opens it) — landing the user
// on the valve control so they can turn the water back on.
self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  event.waitUntil(
    clients
      .matchAll({ type: "window", includeUncontrolled: true })
      .then((windows) => {
        for (const win of windows) {
          if ("focus" in win) return win.focus();
        }
        return clients.openWindow("./");
      })
  );
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
