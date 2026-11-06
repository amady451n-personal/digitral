# FRONTEND.md — Daftar permintaan frontend, status, dan sisa pekerjaan

Semua kemajuan frontend berhenti di commit `5452687` (lihat `git log`). Foto ini dibuat
3 Nov 2026; centang kotak saat item berubah status.

## A. Yang diminta dan sudah oke

| # | Permintaan | Status / bukti |
|---|------------|----------------|
| 1 | Multi-provider, multi-nomor (bima+, myIM3) | `server.py` `PROVIDERS`; `/providers`, `/numbers` jalan in-container |
| 2 | Nomor hanya tampil setelah OTP terverifikasi | status `pending` di memori, registry ditulis hanya di `verify()` sukses |
| 3 | Pending eksplisit + bisa dibatalkan | kartu "menunggu OTP" dengan tombol Batal (DELETE), TTL 300 s |
| 4 | Ganti provider di form → reset field + cancel pending | event `change` pada `#prov` memanggil DELETE pending |
| 5 | Tombol tidak bisa double-click saat proses | helper `withBusy(btn, fn)` di semua tombol |
| 6 | Resend/enter ulang OTP tanpa 409 | `add_number` idempoten utk (provider, msisdn) + `POST /numbers/{id}/resend` |
| 7 | Mobile-first | grid 1 fr → ≥640 px auto-fill; modal bottom-sheet; `env(safe-area-inset-*)`; input 16 px anti-zoom iOS; tap target ≥ 40 px |
| 8 | Logo APK sebagai watermark kartu (bukan gradasi warna) | `assets/bima.png`, `assets/myim3.png` (192×192, dari mipmap-xxxhdpi), `assets/axisnet.png` (180×180, apple-touch-icon axis.co.id — APK MuMu sedang mati), `::before` opacity 0.18 |
| 9 | Ikon PWA generik | `assets/icon.svg` (SVG gradasi biru, bukan logo provider) |
| 10 | localStorage utk snapshot offline | `localStorage["digitral:lastSnapshot"]` disimpan setiap load/SSE, dipulihkan saat offline |
| 11 | Mudah menerima update (service worker) | shell cache-first per nama cache `digitral-shell-v1`; activator menghapus cache lama → bump nama cache = update terpasang *(lihat B-3)* |
| 12 | Rincian kartu (pulsa, kuota, sisa masa aktif, grace, bonstri) | diperbaiki di `5452687`: ambil hasil `parse_dashboard` (`balance`/`quota`/`unit`/`active_until`), bukan shape mentah `prepaidinfo` |
| 13 | Tanpa teks sampah "EOF echo done" | dibersihkan di `5452687` (index.html & icon.svg); `grep -rn EOF frontend/` = 0 |
| 14 | PWA dasar: manifest + service worker + install prompt | `/manifest.json`, `/sw.js` (network-first API, cache-first shell), `beforeinstallprompt` → tombol Install |
| 15 | Frontend memakai HTTP backend, bukan shell CLI | semua panggilan via `fetch`; CLI `--json` hanya untuk debug/scripting |

## B. Yang diminta tetapi belum tuntas

| # | Item | Kekurangan | Rencana |
|---|------|-----------|---------|
| 1 | axisnet & myXL | slot `PROVIDER_MYXL`/`PROVIDER_AXISNET` masih `None`; kartu provider belum ada | setelah frontend stabil; perlu decompile & verifikasi konstanta per APK |
| 2 | PWA bukti jalan di perangkat | file 200, tetapi user perlu unduh via Caddy HTTPS (secure context wajib); instruksi iOS (Share → "Add to Home Screen") belum ada di UI | verifikasi manual dari HP; tambahkan baris instruksi iOS di modal Install |
| 3 | Update PWA mulus | shell "/" cache-first memakai cache statis `v1` — perubahan frontend tak terlihat sampai nama cache diubah | tambahkan versi otomatis (hash git → nama cache) atau `register('/sw.js').then(...updatefound)` + banner "versi baru, muat ulang" |
| 4 | Instruksi iOS pasang PWA | hanya tombol install Chromium event | teks "iOS: Share → Add to Home Screen" di dekat tombol Install |

## C. Batasan yang sudah disepakati

- **Jangan uji kirim OTP live** kecuali nomor milik user dan user minta. (Satu OTP salah kirim ke 6281234567890 di early shell test — tidak diulang.)
- Backend tanpa auth: pasang di bid 127.0.0.1 / Caddy basic-auth atau authproxy.
- Vault selalu dalam container (`/app/data/{provider}`), bukan di host.

## D. Cek fisik ringkas

```bash
docker compose up -d --build
curl -s localhost:8765/health
curl -s -o /dev/null -w "%{http_code}\n" localhost:8765/manifest.json localhost:8765/sw.js localhost:8765/assets/bima.png
# Buka https://<domain> dari HP → DevTools → Application → Manifest/SW
```
