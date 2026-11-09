// Service worker, banner pembaruan, tombol Install, dan petunjuk iOS.

const DISMISS_KEY = "digitral:iosHintDismissed";
const SW_URL = "/sw.js";

const byId = (id) => document.getElementById(id);
const UPDATE_CHECK_MS = 60 * 60 * 1000;
let waitingWorker = null;
let installEvent = null;
let reloadRequested = false;

function showUpdate(worker) {
  waitingWorker = worker;
  byId("update-banner").hidden = false;
}

function watchInstalling(registration) {
  const worker = registration.installing;
  worker?.addEventListener("statechange", () => {
    if (worker.state === "installed" && navigator.serviceWorker.controller) {
      showUpdate(worker);
    }
  });
}

async function registerWorker() {
  const registration = await navigator.serviceWorker.register(SW_URL);
  if (registration.waiting && navigator.serviceWorker.controller) {
    showUpdate(registration.waiting);
  }
  registration.addEventListener("updatefound", () => watchInstalling(registration));
  scheduleUpdateChecks(registration);
}

function checkForUpdate(registration) {
  registration.update().catch(() => {
    // Gagal memeriksa pembaruan (mis. offline): coba lagi nanti.
  });
}

function scheduleUpdateChecks(registration) {
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "visible") {
      checkForUpdate(registration);
    }
  });
  setInterval(() => checkForUpdate(registration), UPDATE_CHECK_MS);
}

function applyUpdate() {
  if (waitingWorker === null) {
    return;
  }
  reloadRequested = true;
  waitingWorker.postMessage({ type: "SKIP_WAITING" });
}

function initServiceWorker() {
  if (!globalThis.isSecureContext || !("serviceWorker" in navigator)) {
    return;
  }
  navigator.serviceWorker.addEventListener("controllerchange", () => {
    if (reloadRequested) {
      reloadRequested = false;
      globalThis.location.reload();
    }
  });
  byId("btn-update").addEventListener("click", applyUpdate);
  window.addEventListener("load", () => {
    registerWorker().catch(() => {
      // Pendaftaran gagal: aplikasi tetap berjalan tanpa cache offline.
    });
  });
}

async function promptInstall() {
  if (installEvent === null) {
    return;
  }
  const pending = installEvent;
  installEvent = null;
  await pending.prompt();
  await pending.userChoice;
  byId("btn-install").hidden = true;
}

function initInstall() {
  const button = byId("btn-install");
  window.addEventListener("beforeinstallprompt", (event) => {
    event.preventDefault();
    installEvent = event;
    button.hidden = false;
  });
  window.addEventListener("appinstalled", () => {
    installEvent = null;
    button.hidden = true;
  });
  button.addEventListener("click", () => {
    promptInstall().catch(() => {
      button.hidden = true;
    });
  });
}

function isIosSafari() {
  const ua = navigator.userAgent;
  const iosDevice = /iPhone|iPad|iPod/.test(ua) || (/Macintosh/.test(ua) && navigator.maxTouchPoints > 1);
  return iosDevice && !/CriOS|FxiOS|EdgiOS/.test(ua);
}

function isStandalone() {
  return navigator.standalone === true || globalThis.matchMedia("(display-mode: standalone)").matches;
}

function wasDismissed() {
  try {
    return localStorage.getItem(DISMISS_KEY) === "1";
  } catch {
    return false;
  }
}

function rememberDismiss() {
  try {
    localStorage.setItem(DISMISS_KEY, "1");
  } catch {
    // Tidak masalah bila tidak tersimpan.
  }
}

function initIosHint() {
  const hint = byId("ios-hint");
  if (!isIosSafari() || isStandalone() || wasDismissed()) {
    return;
  }
  hint.hidden = false;
  byId("btn-ios-dismiss").addEventListener("click", () => {
    hint.hidden = true;
    rememberDismiss();
  });
}

export function initPwa() {
  initServiceWorker();
  initInstall();
  initIosHint();
}
