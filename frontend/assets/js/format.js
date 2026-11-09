// Fungsi murni untuk format tampilan. Tidak menyentuh DOM.

export const DASH = "—";
export const OTP_TTL_SECONDS = 300;

const KB_PER_MB = 1024;
const KB_PER_GB = 1024 * 1024;
const UI_ACCENT = "#38bdf8";
const INK_DARK = "#04101f";
const INK_LIGHT = "#ffffff";
const HEX_PATTERN = /^#(?:[0-9a-f]{3}|[0-9a-f]{6})$/i;

export function isBlank(value) {
  return value === null || value === undefined || value === "";
}

export function textOrDash(value) {
  return isBlank(value) ? DASH : String(value);
}

export function formatRupiah(value) {
  if (isBlank(value)) {
    return DASH;
  }
  const text = String(value).trim().replace(/\.0+$/, "");
  if (text === "") {
    return DASH;
  }
  const number = Number(text);
  return Number.isFinite(number) ? `Rp ${number.toLocaleString("id-ID")}` : `Rp ${text}`;
}

function formatKilobytes(quota) {
  if (quota >= KB_PER_GB) {
    return `${(quota / KB_PER_GB).toFixed(2)} GB`;
  }
  if (quota >= KB_PER_MB) {
    return `${Math.round(quota / KB_PER_MB)} MB`;
  }
  return `${quota} KB`;
}

export function formatQuota(quota, unit, unlimited) {
  if (unlimited === true) {
    return "UNLIMITED";
  }
  if (isBlank(quota)) {
    return DASH;
  }
  if (unit === "KB") {
    return formatKilobytes(Number(quota));
  }
  return isBlank(unit) ? String(quota) : `${quota} ${unit}`;
}

export function maskMsisdn(msisdn) {
  const digits = String(msisdn ?? "").replace(/\D/g, "");
  const tail = digits.slice(-4);
  return `•••• •••• ${tail}`.trimEnd();
}

export function normalizeMsisdn(input) {
  const digits = String(input ?? "").replace(/\D/g, "");
  if (digits.startsWith("62")) {
    return digits;
  }
  if (digits.startsWith("0")) {
    return `62${digits.slice(1)}`;
  }
  return digits.startsWith("8") ? `62${digits}` : digits;
}

export function isHexColor(value) {
  return typeof value === "string" && HEX_PATTERN.test(value.trim());
}

function expandHex(hex) {
  const body = hex.slice(1);
  if (body.length === 3) {
    return [...body].map((ch) => ch + ch).join("");
  }
  return body;
}

function channelToLinear(channel) {
  const value = channel / 255;
  return value <= 0.03928 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4;
}

export function relativeLuminance(hex) {
  const full = expandHex(hex.trim());
  const [r, g, b] = [0, 2, 4].map((i) => channelToLinear(Number.parseInt(full.slice(i, i + 2), 16)));
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

function contrast(lumA, lumB) {
  const hi = Math.max(lumA, lumB);
  const lo = Math.min(lumA, lumB);
  return (hi + 0.05) / (lo + 0.05);
}

export function inkFor(hex) {
  const lum = relativeLuminance(hex);
  const dark = contrast(lum, relativeLuminance(INK_DARK));
  const light = contrast(lum, relativeLuminance(INK_LIGHT));
  return dark >= light ? INK_DARK : INK_LIGHT;
}

export function cardColors(color) {
  const accent = isHexColor(color) ? color.trim() : UI_ACCENT;
  return { accent, ink: inkFor(accent) };
}

export function remainingSeconds(addedAtSeconds, nowMs) {
  const elapsed = nowMs / 1000 - Number(addedAtSeconds);
  if (!Number.isFinite(elapsed)) {
    return 0;
  }
  return Math.min(OTP_TTL_SECONDS, Math.max(0, Math.ceil(OTP_TTL_SECONDS - elapsed)));
}

export function formatCountdown(seconds) {
  const safe = Math.max(0, Math.floor(seconds));
  const secs = String(safe % 60).padStart(2, "0");
  return `${Math.floor(safe / 60)}:${secs}`;
}
