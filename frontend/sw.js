// Service worker Digitral. Naikkan CACHE_VERSION tiap rilis.
const CACHE_VERSION = "digitral-v6";
const SHELL = [
  "/",
  "/manifest.json",
  "/assets/css/app.css",
  "/assets/js/main.js",
  "/assets/js/api.js",
  "/assets/js/store.js",
  "/assets/js/order.js",
  "/assets/js/format.js",
  "/assets/js/cards.js",
  "/assets/js/sheet.js",
  "/assets/js/stream.js",
  "/assets/js/offline.js",
  "/assets/js/pwa.js",
  "/assets/js/busy.js",
  "/assets/js/notif.js",
  "/assets/icon.svg",
  "/assets/icon-192.png",
  "/assets/icon-512.png",
];
const API_PREFIXES = ["/health", "/providers", "/numbers"];
const STALE_HEADER = "x-from-cache";

self.addEventListener("install", (event) => {
  event.waitUntil(caches.open(CACHE_VERSION).then((cache) => cache.addAll(SHELL.map((url) => new Request(url, { cache: "reload" })))));
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((names) => Promise.all(names.filter((name) => name !== CACHE_VERSION).map((name) => caches.delete(name))))
      .then(() => self.clients.claim()),
  );
});

self.addEventListener("message", (event) => {
  if (event.data && event.data.type === "SKIP_WAITING") {
    self.skipWaiting();
  }
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  event.waitUntil(
    self.clients.matchAll({ type: "window", includeUncontrolled: true }).then((list) => {
      const client = list.find((c) => "url" in c && c.url.startsWith(self.location.origin)) ?? list[0];
      if (client) {
        return client.focus();
      }
      return self.clients.openWindow("/");
    }),
  );
});

function isApi(pathname) {
  return API_PREFIXES.some((prefix) => pathname === prefix || pathname.startsWith(`${prefix}/`));
}

async function cacheFirst(request) {
  const cached = await caches.match(request);
  if (cached) {
    return cached;
  }
  const response = await fetch(request);
  if (response.ok) {
    const cache = await caches.open(CACHE_VERSION);
    await cache.put(request, response.clone());
  }
  return response;
}

function markStale(response) {
  const headers = new Headers(response.headers);
  headers.set(STALE_HEADER, "1");
  return new Response(response.body, { status: response.status, statusText: response.statusText, headers });
}

async function networkFirst(request) {
  try {
    const response = await fetch(request);
    if (response.ok) {
      const cache = await caches.open(CACHE_VERSION);
      await cache.put(request, response.clone());
    }
    return response;
  } catch (error) {
    const cached = await caches.match(request);
    if (cached) {
      return markStale(cached);
    }
    throw error;
  }
}

self.addEventListener("fetch", (event) => {
  const { request } = event;
  const url = new URL(request.url);
  if (request.method !== "GET" || url.origin !== self.location.origin || url.pathname === "/stream") {
    return;
  }
  if (isApi(url.pathname)) {
    event.respondWith(networkFirst(request));
  } else if (url.pathname === "/" || url.pathname === "/manifest.json" || url.pathname.startsWith("/assets/")) {
    event.respondWith(cacheFirst(request));
  }
});
