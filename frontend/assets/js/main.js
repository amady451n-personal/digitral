// Titik masuk: menyambungkan store, kartu, sheet, stream, dan PWA.

import { ApiError, api, isNetworkError, messageOf, numberPath, onStale } from "./api.js";
import { withBusy } from "./busy.js";
import { bindCardActions, render, tick } from "./cards.js";
import { loadSnapshot, saveSnapshot } from "./offline.js";
import { initNotif } from "./notif.js";
import { initPwa } from "./pwa.js";
import { deleteNumber, confirmReauth, initSheet, openOtp, resendById } from "./sheet.js";
import {
  allNumbers, applyStream, claimRefresh, getNumber, mergeSnapshotData, releaseRefresh, restore, setBalance, setNumbers, setOffline, setProviders,
  setSessionLost, setVerifiedNumbers, setWarn, snapshotForStorage, subscribe,
} from "./store.js";
import { startStream } from "./stream.js";

const HTTP_UNAUTHORIZED = 401;
const LIVE_TEXT = { connecting: "Menyambung", live: "Live", offline: "Terputus" };

const byId = (id) => document.getElementById(id);
let stale = false;
let listGen = 0;
let loading = null;
let offlineMode = false;

function showPageError(text) {
  const box = byId("page-error");
  box.textContent = text ?? "";
  box.hidden = !text;
}

function setOfflineNote(on) {
  offlineMode = on;
  setOffline(on);
  byId("offline-note").hidden = !on;
}

function persist() {
  if (!offlineMode) {
    saveSnapshot(snapshotForStorage());
  }
}

function setLive(state) {
  byId("live-status").dataset.state = state;
  byId("live-text").textContent = LIVE_TEXT[state] ?? LIVE_TEXT.connecting;
}

function restoreOffline(error) {
  const snapshot = loadSnapshot();
  if (!isNetworkError(error) || snapshot === null) {
    showPageError(messageOf(error));
    return;
  }
  restore(snapshot);
  setOfflineNote(true);
}

// Respons /providers & /numbers dari cache SW: lengkapi data kartu dari snapshot lokal.
function useStaleSnapshot() {
  setOfflineNote(true);
  const snapshot = loadSnapshot();
  if (snapshot !== null) {
    mergeSnapshotData(snapshot.numbers);
  }
}

async function fetchNumbers(path, apply) {
  listGen += 1;
  const gen = listGen;
  const list = await api.get(path);
  if (gen === listGen) {
    apply(list.numbers ?? []);
  }
}

async function doLoad() {
  stale = false;
  try {
    setProviders(await api.get("/providers"));
    await fetchNumbers("/numbers?include_pending=1", setNumbers);
    showPageError("");
    if (stale) {
      useStaleSnapshot();
    } else {
      setOfflineNote(false);
      persist();
    }
  } catch (error) {
    restoreOffline(error);
  }
}

function load() {
  if (loading === null) {
    loading = doLoad().finally(() => {
      loading = null;
    });
  }
  return loading;
}

async function refreshOne(id) {
  if (!claimRefresh(id)) {
    return;
  }
  try {
    const res = await api.get(numberPath(id, "/balance"));
    setBalance(id, res.balance ?? null);
  } catch (error) {
    if (error instanceof ApiError && error.status === HTTP_UNAUTHORIZED) {
      setSessionLost(id);
    } else {
      setWarn(id, messageOf(error));
    }
  } finally {
    releaseRefresh(id);
  }
}

async function refreshAll() {
  showPageError("");
  try {
    await fetchNumbers("/numbers", setVerifiedNumbers);
  } catch (error) {
    showPageError(messageOf(error));
    return;
  }
  const ids = allNumbers().filter((n) => !n.pending).map((n) => n.id);
  await Promise.all(ids.map(refreshOne));
  persist();
}

async function reload() {
  await load();
  const missing = allNumbers().filter((n) => !n.pending && n.data === undefined);
  await Promise.all(missing.map((n) => refreshOne(n.id)));
}

async function onSnapshot(payload) {
  if (offlineMode) {
    setOfflineNote(false);
    await load();
  }
  applyStream(payload.numbers);
  persist();
}

function safe(task) {
  return async (id) => {
    try {
      await task(id);
    } catch (error) {
      showPageError(messageOf(error));
    }
  };
}

function cardHandlers() {
  return {
    refresh: safe(async (id) => {
      await refreshOne(id);
      persist();
    }),
    delete: safe(async (id) => {
      await deleteNumber(id);
      persist();
    }),
    cancel: safe((id) => deleteNumber(id)),
    continue: safe((id) => openOtp(getNumber(id))),
    resend: safe((id) => resendById(getNumber(id))),
    reauth: safe((id) => confirmReauth(getNumber(id))),
  };
}

function boot() {
  subscribe(render);
  bindCardActions(cardHandlers());
  initSheet({ reload, showError: showPageError });
  onStale(() => {
    stale = true;
  });
  byId("btn-refresh").addEventListener("click", () => {
    withBusy(byId("btn-refresh"), refreshAll).catch((error) => showPageError(messageOf(error)));
  });
  window.addEventListener("online", () => {
    load().catch(() => showPageError("Gagal memuat data."));
  });
  setInterval(tick, 1000);
  initPwa();
  initNotif();
  load()
    .catch((error) => showPageError(messageOf(error)))
    .finally(() => {
      startStream({
        onState: setLive,
        onSnapshot: (payload) => {
          onSnapshot(payload).catch((error) => showPageError(messageOf(error)));
        },
      });
    });
}

boot();
