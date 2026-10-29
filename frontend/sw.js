const CACHE = "digitral-shell-v1";
const SHELL = ["/", "/manifest.json", "/assets/icon.svg", "/assets/bima.png", "/assets/myim3.png"];

self.addEventListener("install", (e) => {
  e.waitUntil(caches.open(CACHE).then((c) => c.addAll(SHELL)).then(() => self.skipWaiting()));
});

self.addEventListener("activate", (e) => {
  e.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k)))
    ).then(() => self.clients.claim())
  );
});

// network-first for API, cache-first for shell
self.addEventListener("fetch", (e) => {
  const url = new URL(e.request.url);
  if (url.pathname.startsWith("/numbers") || url.pathname.startsWith("/providers") ||
      url.pathname.startsWith("/stream") || url.pathname.startsWith("/health")) {
    e.respondWith(
      fetch(e.request).then((r) => {
        // cache a clone of successful GETs for offline fallback
        if (e.request.method === "GET" && r.ok) {
          const clone = r.clone();
          caches.open(CACHE).then((c) => c.put(e.request, clone));
        }
        return r;
      }).catch(() => caches.match(e.request))
    );
    return;
  }
  e.respondWith(
    caches.match(e.request).then((cached) => cached || fetch(e.request).then((r) => {
      if (r.ok) {
        const clone = r.clone();
        caches.open(CACHE).then((c) => c.put(e.request, clone));
      }
      return r;
    }))
  );
});