// State aplikasi: provider + nomor (terverifikasi dan pending).

const DATA_KEYS = ["balance", "quota", "unit", "used", "unlimited", "active_until", "grace_until", "bonstri"];
const listeners = [];
const numbers = new Map();
const inflight = new Set();
let providers = [];
let offline = false;

function notify() {
  listeners.forEach((listener) => listener());
}

function baseFields(item) {
  return {
    id: item.id,
    provider: item.provider,
    label: item.label,
    color: item.color,
    logo: item.logo ?? null,
    msisdn: item.msisdn,
    added_at: item.added_at,
    pending: item.pending === true,
  };
}

function hasKeys(data) {
  return data !== null && typeof data === "object";
}

export function dataSignature(data) {
  if (!hasKeys(data)) {
    return "";
  }
  return JSON.stringify(DATA_KEYS.map((key) => data[key] ?? null));
}

export function subscribe(listener) {
  listeners.push(listener);
}

export function getProviders() {
  return providers;
}

export function setProviders(list) {
  providers = Array.isArray(list) ? list : [];
  notify();
}

export function isOffline() {
  return offline;
}

export function setOffline(value) {
  offline = value;
}

export function allNumbers() {
  return [...numbers.values()];
}

export function getNumber(id) {
  return numbers.get(id);
}

export function findByMsisdn(msisdn) {
  return allNumbers().find((n) => n.msisdn === msisdn);
}

// Daftar dari /numbers (server otoritatif). Data kartu yang sudah ada dipertahankan.
export function setNumbers(list) {
  const seen = new Set();
  for (const item of list) {
    seen.add(item.id);
    numbers.set(item.id, { ...numbers.get(item.id), ...baseFields(item) });
  }
  for (const id of [...numbers.keys()]) {
    if (!seen.has(id)) {
      numbers.delete(id);
    }
  }
  notify();
}

// Daftar dari /numbers tanpa pending: pending lokal dipertahankan.
export function setVerifiedNumbers(list) {
  const seen = new Set();
  for (const item of list) {
    seen.add(item.id);
    numbers.set(item.id, { ...numbers.get(item.id), ...baseFields(item), pending: false });
  }
  for (const [id, entry] of [...numbers.entries()]) {
    if (!entry.pending && !seen.has(id)) {
      numbers.delete(id);
    }
  }
  notify();
}

function nextData(item, current) {
  if (item.ok === false) {
    return item.data ?? current?.data ?? null;
  }
  return item.data ?? null;
}

// Snapshot penuh dari SSE.
export function applyStream(list) {
  const seen = new Set();
  for (const item of list) {
    seen.add(item.id);
    const current = numbers.get(item.id);
    numbers.set(item.id, {
      ...current,
      ...baseFields(item),
      pending: false,
      ok: item.ok !== false,
      data: nextData(item, current),
      error: item.error ?? null,
      sessionLost: item.ok === false ? current?.sessionLost === true : false,
      warn: item.ok === false ? (current?.warn ?? null) : null,
    });
  }
  for (const [id, entry] of [...numbers.entries()]) {
    if (!entry.pending && !seen.has(id)) {
      numbers.delete(id);
    }
  }
  notify();
}

// Gabungkan data kartu dari snapshot lokal untuk nomor yang belum punya data.
export function mergeSnapshotData(list) {
  for (const saved of list) {
    const entry = numbers.get(saved.id);
    if (entry && !entry.pending && entry.data === undefined) {
      Object.assign(entry, { ok: saved.ok, data: saved.data ?? null, error: saved.error ?? null });
    }
  }
  notify();
}

// Cegah refresh ganda untuk id yang sama.
export function claimRefresh(id) {
  if (inflight.has(id)) {
    return false;
  }
  inflight.add(id);
  return true;
}

export function releaseRefresh(id) {
  inflight.delete(id);
}

export function upsertPending(item) {
  numbers.set(item.id, { ...numbers.get(item.id), ...baseFields({ ...item, pending: true }) });
  notify();
}

export function touchPending(id, addedAt) {
  const entry = numbers.get(id);
  if (entry) {
    entry.added_at = addedAt;
    notify();
  }
}

export function removeNumber(id) {
  if (numbers.delete(id)) {
    notify();
  }
}

export function setBalance(id, balance) {
  const entry = numbers.get(id);
  if (entry) {
    Object.assign(entry, { ok: true, data: balance, error: null, warn: null, sessionLost: false });
    notify();
  }
}

export function setSessionLost(id) {
  const entry = numbers.get(id);
  if (entry) {
    Object.assign(entry, { sessionLost: true, warn: null });
    notify();
  }
}

export function setWarn(id, text) {
  const entry = numbers.get(id);
  if (entry) {
    entry.warn = text;
    notify();
  }
}

export function snapshotForStorage() {
  const verified = allNumbers().filter((n) => !n.pending);
  return { providers, numbers: verified, savedAt: Date.now() };
}

export function restore(snapshot) {
  providers = snapshot.providers;
  numbers.clear();
  for (const item of snapshot.numbers) {
    numbers.set(item.id, item);
  }
  notify();
}
