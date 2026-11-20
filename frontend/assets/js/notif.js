// Notifikasi Chrome Android PWA: masa aktif lewat -> bantu peringatan harian.
// Tidak Web Push (tanpa VAPID), jadi hanya muncul saat PWA aktif & menerima data.

import { dateKey, daysPast, hasSeparateGrace, isGraceUrgent } from "./expiry.js";
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

function notifiedKey(id, day) {
  return `digitral:notif:grace20:${id}:${day}`;
}

async function fire(item, days, urgent, graceText) {
  const title = urgent ? "Masa tenggang hampir habis" : "Masa aktif lewat";
  const tail = String(item.msisdn ?? "").replace(/\D/g, "").slice(-4);
  const base = `${String(item.label ?? "Nomor")} •••• ${tail}: ${days} hari tenggang`;
  const grace = graceText ? ` (tenggang s/d ${graceText})` : "";
  const body = urgent ? `${base}${grace} — harus segera diisi` : base;
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
      requireInteraction: urgent,
      vibrate: urgent ? [0, 120, 90, 120] : [0, 80],
      lang: "id",
      dir: "ltr",
      timestamp: Date.now(),
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
  const today = dateKey();
  for (const item of (items || [])) {
    if (item.pending) {
      continue;
    }
    const data = item.data || {};
    const past = daysPast(data.active_until);
    if (past === null || past <= 0) {
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
      const urgent = isGraceUrgent(data.active_until, data.grace_until);
      const graceText = hasSeparateGrace(data.active_until, data.grace_until)
        ? String(data.grace_until ?? "").trim()
        : "";
      await fire(item, past, urgent, graceText);
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
  const denied = supported && Notification.permission === "denied";
  const enabled = granted && isEnabled();
  btn.disabled = !supported;
  btn.dataset.enabled = enabled ? "1" : "0";
  btn.setAttribute("aria-pressed", enabled ? "true" : "false");
  const label = !supported
    ? "Notifikasi tidak didukung"
    : enabled
      ? "Nonaktifkan notifikasi masa tenggang"
      : denied
        ? "Notifikasi diblokir — buka pengaturan situs"
        : "Aktifkan notifikasi masa tenggang";
  btn.setAttribute("aria-label", label);
  btn.title = label;
}

let noticeTimer = 0;

// Pesan inline menggantikan alert() agar tidak memblokir & sesuai tema.
function showNotice(message) {
  const host = document.querySelector(".notices");
  if (!host) {
    return;
  }
  let el = document.getElementById("notif-note");
  if (!el) {
    el = document.createElement("p");
    el.id = "notif-note";
    el.className = "notice notice-muted";
    el.setAttribute("role", "status");
    host.append(el);
  }
  el.textContent = message;
  el.hidden = false;
  clearTimeout(noticeTimer);
  noticeTimer = setTimeout(() => {
    el.hidden = true;
  }, 6000);
}

export function initNotif() {
  const btn = document.getElementById("btn-notif");
  renderButton(btn);
  btn?.addEventListener("click", async () => {
    if (!("Notification" in globalThis)) {
      return;
    }
    if (Notification.permission === "denied") {
      // Tidak bisa re-prompt; beri tahu lewat notice inline, bukan alert().
      showNotice("Notifikasi diblokir. Aktifkan izin notifikasi untuk situs ini di pengaturan browser.");
      return;
    }
    if (Notification.permission !== "granted") {
      const ok = await requestEnable();
      if (!ok && Notification.permission === "denied") {
        showNotice("Notifikasi diblokir. Aktifkan izin notifikasi untuk situs ini di pengaturan browser.");
      }
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
