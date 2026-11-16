// Web Push (VAPID): subscribe pushManager, kirim subscription ke backend.
// Notifikasi dikirim oleh thread harian server (push_notify.py).

const PUSH_KEY_URL = "/push/public-key";
const SUB_URL = "/push/subscribe";
const FLAG = "digitral:push:subscribed";

function urlB64ToUint8Array(b64) {
  const pad = "=".repeat((4 - (b64.length % 4)) % 4);
  const raw = atob((b64 + pad).replace(/-/g, "+").replace(/_/g, "/"));
  return Uint8Array.from([...raw].map((c) => c.charCodeAt(0)));
}

function subscribed() {
  try {
    return localStorage.getItem(FLAG) === "1";
  } catch {
    return false;
  }
}

async function subscribeOnce() {
  const reg = await navigator.serviceWorker.getRegistration();
  if (!reg) {
    return false;
  }
  const existing = await reg.pushManager.getSubscription();
  const sub = existing ?? await reg.pushManager.subscribe({
    userVisibleOnly: true,
    applicationServerKey: urlB64ToUint8Array(
      (await (await fetch(PUSH_KEY_URL)).json()).public_key,
    ),
  });
  const res = await fetch(SUB_URL, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(sub.toJSON()),
  });
  if (!res.ok) {
    return false;
  }
  try {
    localStorage.setItem(FLAG, "1");
  } catch {
    // ignore
  }
  return true;
}

export async function initPush() {
  if (!("serviceWorker" in navigator) || !("PushManager" in window) ||
      !("Notification" in globalThis) || Notification.permission !== "granted" ||
      subscribed()) {
    return;
  }
  try {
    await subscribeOnce();
  } catch {
    // silent; dicoba lagi saat app dibuka berikutnya
  }
}

// SW meminta re-subscribe setelah pushsubscriptionchange.
export function watchResubscribe() {
  navigator.serviceWorker?.addEventListener("message", (event) => {
    if (event.data?.type !== "push-resubscribe" || subscribed()) {
      return;
    }
    try {
      localStorage.removeItem(FLAG);
    } catch {
      // ignore
    }
    initPush().catch(() => {});
  });
}
