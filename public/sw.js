// Service worker for the Фикусин storefront.
//
// Caching here is deliberately timid. The site deploys on every merge, and a
// stale bundle served from cache is far worse than a slow one: a customer
// would be shopping against yesterday's prices. So:
//
//   * /assets/* carry a content hash in the file name, so they can never go
//     stale — those are cached forever and served from cache first.
//   * the page shell goes over the network, falling back to cache only when
//     the phone is offline.
//   * /api/** is never cached. Prices, stock and the session must be live.

const VERSION = "v1";
const SHELL = `ficusin-shell-${VERSION}`;
const ASSETS = `ficusin-assets-${VERSION}`;
const OFFLINE_URL = "/offline.html";

self.addEventListener("install", (event) => {
  event.waitUntil((async () => {
    const cache = await caches.open(SHELL);
    await cache.addAll([OFFLINE_URL, "/icon-192.png"]);
    // Take over as soon as the new worker is ready rather than waiting for
    // every tab to close.
    await self.skipWaiting();
  })());
});

self.addEventListener("activate", (event) => {
  event.waitUntil((async () => {
    const keep = new Set([SHELL, ASSETS]);
    for (const name of await caches.keys()) {
      if (!keep.has(name)) await caches.delete(name);
    }
    await self.clients.claim();
  })());
});

function isAsset(url) {
  return url.pathname.startsWith("/assets/") ||
    /\.(png|svg|webp|jpg|jpeg|woff2?)$/.test(url.pathname);
}

self.addEventListener("fetch", (event) => {
  const { request } = event;
  if (request.method !== "GET") return;

  const url = new URL(request.url);
  if (url.origin !== self.location.origin) return;
  if (url.pathname.startsWith("/api/")) return;

  if (isAsset(url)) {
    event.respondWith((async () => {
      const cached = await caches.match(request);
      if (cached) return cached;
      const response = await fetch(request);
      if (response.ok) (await caches.open(ASSETS)).put(request, response.clone());
      return response;
    })());
    return;
  }

  // Page navigations: network first, cache only as a lifeline.
  if (request.mode === "navigate") {
    event.respondWith((async () => {
      try {
        return await fetch(request);
      } catch {
        return (await caches.match(OFFLINE_URL)) ||
          new Response("Нет соединения", { status: 503, headers: { "Content-Type": "text/plain; charset=utf-8" } });
      }
    })());
  }
});

// A push arrives as JSON: { title, body, url }. Anything missing falls back
// to wording that still makes sense on a lock screen.
self.addEventListener("push", (event) => {
  let payload = {};
  try {
    payload = event.data ? event.data.json() : {};
  } catch {
    payload = { body: event.data ? event.data.text() : "" };
  }
  const title = payload.title || "Фикусин";
  event.waitUntil(self.registration.showNotification(title, {
    body: payload.body || "",
    icon: "/icon-192.png",
    badge: "/icon-192.png",
    lang: "ru",
    data: { url: payload.url || "/" },
    tag: payload.tag || "ficusin",
  }));
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const target = new URL(event.notification.data?.url || "/", self.location.origin).href;
  event.waitUntil((async () => {
    // Reuse an open tab if the shop is already on screen.
    const windows = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
    for (const client of windows) {
      if (client.url === target && "focus" in client) return client.focus();
    }
    if (self.clients.openWindow) return self.clients.openWindow(target);
  })());
});
