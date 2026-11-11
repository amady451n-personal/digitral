// Parser tanggal longgar untuk field masa aktif provider. Tidak menyentuh DOM.

const MONTHS = new Map([
  ["jan", 0], ["feb", 1], ["mar", 2], ["apr", 3],
  ["may", 4], ["mei", 4], ["jun", 5], ["jul", 6],
  ["aug", 7], ["agu", 7], ["agt", 7], ["sep", 8],
  ["oct", 9], ["okt", 9], ["nov", 10], ["dec", 11], ["des", 11],
]);

function startOfDay(date) {
  const d = new Date(date);
  d.setHours(0, 0, 0, 0);
  return d;
}

function validDate(year, month, day) {
  const d = new Date(year, month, day);
  if (d.getFullYear() !== year || d.getMonth() !== month || d.getDate() !== day) {
    return null;
  }
  return startOfDay(d);
}

export function parseActiveDate(value) {
  if (value === null || value === undefined) {
    return null;
  }
  const raw = String(value).trim();
  if (raw === "" || raw === "-") {
    return null;
  }

  // Epoch detik/milidetik, kalau ada provider yang mengirim angka.
  if (/^\d{10,13}$/.test(raw)) {
    const n = Number(raw);
    return startOfDay(new Date(n < 1e12 ? n * 1000 : n));
  }

  // ISO: 2026-10-29 / 2026-10-29 23:59:59 / 2026-10-29T...
  let m = raw.match(/^(\d{4})[-/](\d{1,2})[-/](\d{1,2})/);
  if (m) {
    return validDate(Number(m[1]), Number(m[2]) - 1, Number(m[3]));
  }

  // "Exp on 29 Dec 2026" / "29 Oct 2026" / "29-Okt-2026"
  m = raw.match(/(\d{1,2})[\s/-]+([A-Za-z]{3,9})\.?[\s/-]+(\d{4})/);
  if (m) {
    const month = MONTHS.get(m[2].slice(0, 3).toLowerCase());
    if (month !== undefined) {
      return validDate(Number(m[3]), month, Number(m[1]));
    }
  }

  // Day-first: 29/10/2026 / 29-10-2026
  m = raw.match(/^(\d{1,2})[-/](\d{1,2})[-/](\d{4})/);
  if (m) {
    return validDate(Number(m[3]), Number(m[2]) - 1, Number(m[1]));
  }

  const fallback = new Date(raw);
  return Number.isNaN(fallback.getTime()) ? null : startOfDay(fallback);
}

export function daysPast(value, now = new Date()) {
  const date = parseActiveDate(value);
  if (date === null) {
    return null;
  }
  return Math.round((startOfDay(now).getTime() - date.getTime()) / 86400000);
}

export function isExpired(value, now = new Date()) {
  const past = daysPast(value, now);
  return past !== null && past > 0;
}

// Kunci tanggal lokal YYYY-MM-DD (bukan UTC, agar dedup harian tidak geser).
export function dateKey(now = new Date()) {
  const d = startOfDay(now);
  return [
    d.getFullYear(),
    String(d.getMonth() + 1).padStart(2, "0"),
    String(d.getDate()).padStart(2, "0"),
  ].join("-");
}
