// Snapshot terakhir di localStorage. Isi snapshot tidak pernah dicatat ke log.

const KEY = "digitral:lastSnapshot";

export function saveSnapshot(snapshot) {
  try {
    localStorage.setItem(KEY, JSON.stringify(snapshot));
  } catch {
    // Penyimpanan penuh atau diblokir: aplikasi tetap jalan tanpa snapshot.
  }
}

export function loadSnapshot() {
  try {
    const raw = localStorage.getItem(KEY);
    if (raw === null) {
      return null;
    }
    const parsed = JSON.parse(raw);
    if (parsed && Array.isArray(parsed.providers) && Array.isArray(parsed.numbers)) {
      return parsed;
    }
  } catch {
    // Data rusak: anggap tidak ada snapshot.
  }
  return null;
}
