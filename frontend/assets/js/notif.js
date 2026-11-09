// Notifikasi Chrome Android PWA: masa aktif lewat -> bantu peringatan harian.
// Tidak Web Push (tanpa VAPID), jadi hanya muncul saat PWA aktif & menerima data.

import { allNumbers, subscribe } from "./store.js";

const ENABLED_KEY = "digitral:notif:enabled";

export function isEnabled() {
  try {
    return localStorage.getItem(ENABLED_KEY) === "1";
  } catch {
    return false;
  }
}

function setEnabled(v) {
  try {
    if (v) {
      localStorage.setItem(ENABLED_KEY, "1");
    } else {
      localStorage.removeItem(ENABLED_KEY);
    }
  } catch {
    // ignore
  }
}

function localMidnight(d = new Date()) {
  const x = new Date(d);
  x.setHours(0, 0, 0, 0);
  return x;
}

function parseDate(str) {
  if (!str) {
    return null;
  }
  const d = new Date(String(str).replace(/ /g, "T"));
  return Number.isNaN(d.getTime()) ? null : d;
}

function daysPast(activeUntil) {
  const d = parseDate(activeUntil);
  if (!d) {
    return null;
  }
  return Math.floor((localMidnight().getTime() - localMidnight(d).getTime()) / 86400000);
}

function notifiedKey(id, day) {
  return `digitral:notif:grace20:${id}:${day}`;
}

async function fire(item, days) {
  const beyond = days > 20;
  const title = beyond ? "Masa aktif lewat 20+ hari" : "Masa aktif lewat";
  const tail = String(item.msisdn ?? "").replace(/\D/g, "").slice(-4);
  const base = `${String(item.label ?? "Nomor")} •••• ${tail}: ${days} hari tenggang`;
  const body = beyond ? `${base} — harus segera diisi` : base;
  const tag = `grace-${item.id}`;
  let reg = null;
  try {
    reg = await navigator.serviceWorker.getRegistration();
  } catch {
    reg = null;
  }
  if (reg) {
    await reg.showNotification(title, {
      body,
      tag,
      renotify: false,
      requireInteraction: beyond,
      icon: "/assets/icon-192.png",
      badge: "/assets/icon-192.png",
    });
  } else {
    // eslint-disable-next-line no-new
    new Notification(title, { body, tag });
  }
}

export async function checkGraceNotifications(items) {
  if (!isEnabled() || !("Notification" in globalThis) || Notification.permission !== "granted") {
    return;
  }
  const today = localMidnight().toISOString().slice(0, 10);
  for (const item of (items || [])) {
    if (item.pending) {
      continue;
    }
    const data = item.data || {};
    const past = daysPast(data.active_until);
    if (past === null || past < 0) {
      continue;
    }
    const key = notifiedKey(item.id, today);
    let already = false;
    try {
      already = localStorage.getItem(key) === "1";
    } catch {
      already = false;
    }
    if (already) {
      continue;
    }
    try {
      await fire(item, past);
    } catch {
      // ignore permission/runtime errors
    }
    try {
      localStorage.setItem(key, "1");
    } catch {
      // ignore
    }
  }
}

export async function requestEnable() {
  if (!("Notification" in globalThis)) {
    return false;
  }
  const perm = await Notification.requestPermission();
  if (perm === "granted") {
    setEnabled(true);
    await checkGraceNotifications(allNumbers());
    return true;
  }
  setEnabled(false);
  return false;
}

function renderButton(btn) {
  if (!btn) {
    return;
  }
  const supported = "Notification" in globalThis;
  const granted = supported && Notification.permission === "granted";
  const enabled = granted && isEnabled();
  btn.disabled = !supported;
  btn.dataset.enabled = enabled ? "1" : "0";
  btn.textContent = enabled ? "🔔" : "🔕";
  btn.title = !supported
    ? "Notifikasi tidak didukung"
    : granted
      ? (enabled ? "Notifikasi aktif" : "Klik untuk aktifkan notifikasi")
      : "Permission notifikasi ditolak/belum diberikan";
}

export function initNotif() {
  const btn = document.getElementById("btn-notif");
  renderButton(btn);
  btn?.addEventListener("click", async () => {
    if (!("Notification" in globalThis)) {
      return;
    }
    if (Notification.permission === "denied") {
      // Tidak bisa re-prompt; biarkan user ubah di pengaturan browser.
      alert("Permission notifikasi ditolak. Aktifkan melalui pengaturan situs.");
      return;
    }
    if (Notification.permission !== "granted") {
      await requestEnable();
    } else {
      setEnabled(!isEnabled());
      if (isEnabled()) {
        await checkGraceNotifications(allNumbers());
      }
    }
    renderButton(btn);
  });
  subscribe(() => {
    checkGraceNotifications(allNumbers()).catch(() => {});
    renderButton(btn);
  });
}
