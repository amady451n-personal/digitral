# FRONTEND.md — Handoff: Dashboard Digitral (PWA)

Dokumen ini **berdiri sendiri**. Kamu tidak perlu tahu isi repo ini selain yang
tertulis di sini. Tugasmu: **membuat frontend (satu PWA) yang siap dikoneksikan
ke backend yang sudah jadi di repo ini.** Backend tidak perlu kamu ubah.

---

## 0. Ringkasan tugas

Bangun PWA satu halaman, mobile-first, untuk memantau **beberapa nomor prabayar
dari beberapa operator** (sekarang: bima+ Tri dan myIM3 Indosat) dalam satu
dashboard. Alur pengguna:

1. Lihat semua nomor yang sudah terdaftar sebagai kartu (pulsa, kuota, masa
   aktif, grace, poin bonus).
2. Tambah nomor baru: pilih operator → isi nomor → minta OTP → masukkan OTP →
   nomor muncul di daftar.
3. Batalkan OTP yang menunggu, minta ulang OTP, hapus nomor.
4. Data ter-update otomatis (SSE, tiap 60 detik), dan tetap terlihat saat
   offline (snapshot terakhir).

Semua data diambil dari backend HTTP di repo ini. Frontend **tidak boleh**
menghubungi server operator mana pun secara langsung.

**Definition of done** ada di §10. Kontrak API ada di §3. Checklist perilaku UI
ada di §5.

---

## 1. Konteks & batasan

- **Backend sudah selesai untuk provider `bima` dan `myim3`.** Kamu hanya
  mengerjakan frontend.
- Browser **tidak bisa** bicara langsung ke operator: butuh TLS fingerprint
  khusus, header perangkat, body terenkripsi, dan kunci vault rahasia. Karena
  itu ada backend proxy.
- Backend **tanpa autentikasi** dan CORS `allow_origins=["*"]`, dan **hanya
  boleh diakses lewat localhost / reverse proxy dengan basic-auth**. Jangan
  mengasumsikan ada login di sisi frontend.
- Backend menyimpan token sesi di vault terenkripsi di sisi server. Frontend
  **tidak pernah** melihat token, transid OTP, atau device profile.
- Jangan pernah mengirim OTP ke nomor yang bukan milik pemasang aplikasi.
  Kirim OTP hanya saat pengguna menekan tombol "Kirim OTP" untuk nomornya
  sendiri. Jangan menulis skrip uji yang menembak endpoint OTP dengan nomor
  contoh (server benar-benar mengirim SMS).
- Nomor tampil di daftar **hanya setelah** OTP terverifikasi.

---

## 2. Cara menjalankan backend

```bash
# Opsi A — Docker (disarankan)
docker compose up -d --build
# buka http://localhost:8765

# Opsi B — tanpa Docker
pip install -r requirements.txt
python3 server.py            # http://127.0.0.1:8765
```

- Port: `8765` (di Docker: `HOST_PORT` env, default `8765`).
- Konfigurasi env yang relevan:
  | Env | Default | Arti |
  |---|---|---|
  | `FRONTEND_HOME` | `~/.bima-frontend` (Docker: `/app/data`) | lokasi registry + vault |
  | `POLL_SEC` | `60` | interval push SSE (detik) |
  | `PENDING_TTL_SEC` | `300` | umur OTP menunggu sebelum kedaluwarsa |
  | `HOST_PORT` (compose) | `8765` | port di host |
- Cek hidup: `curl -s localhost:8765/health`.

---

## 3. Kontrak API

Base URL: origin yang sama dengan frontend (di produksi biasanya di belakang
reverse proxy). Semua request/response **JSON**, kecuali `/stream` (SSE) dan
`/assets/*` (file).

Error: FastAPI mengembalikan `{"detail": "<pesan>"}` dengan status `400` /
`401` / `404` / `409` / `502`. Frontend harus menampilkan `detail` apa adanya.

| Method | Path | Guna |
|---|---|---|
| GET | `/health` | cek hidup |
| GET | `/providers` | daftar operator yang tersedia |
| GET | `/numbers` | daftar nomor terdaftar |
| POST | `/numbers` | mulai tambah nomor (kirim OTP) |
| POST | `/numbers/{id}/resend` | kirim ulang OTP untuk nomor pending |
| POST | `/numbers/{id}/verify` | verifikasi OTP → nomor resmi terdaftar |
| GET | `/numbers/{id}/balance` | satu kali tarik data nomor |
| DELETE | `/numbers/{id}` | hapus nomor / batalkan pending |
| GET | `/stream` | SSE: snapshot semua nomor tiap `POLL_SEC` |

### 3.1 `GET /health`

```json
{ "ok": true, "providers": ["bima", "myim3"] }
```

### 3.2 `GET /providers`

```json
[
  { "id": "bima",   "label": "bima+ (Tri)",   "available": true },
  { "id": "myim3",  "label": "myIM3 (Indosat)","available": true }
]
```

Render hanya entri `available: true`. `id` inilah nilai `provider` yang dipakai
di endpoint lain.

### 3.3 `GET /numbers`

```json
{
  "numbers": [
    { "id": "a1b2c3d4e5f6", "provider": "bima", "label": "bima+ (Tri)",
      "msisdn": "6281234567890", "added_at": 1762345678.9, "pending": false }
  ]
}
```

- `?include_pending=1` menambahkan nomor yang masih menunggu OTP
  (`"pending": true`). Tanpa parameter ini, nomor pending tidak muncul.
- `msisdn` selalu format `62…` tanpa `+`.

### 3.4 `POST /numbers`

Request: `{ "provider": "bima", "msisdn": "081234567890" }`
(msisdn boleh format lokal `08…`, `+62…`, atau `62…`; server menormalkan).

Respons `200` — salah satu dari tiga bentuk:

```json
{ "id": "a1b2c3d4e5f6", "msisdn": "6281234567890", "provider": "bima",
  "transid": "…", "status": "otp_sent", "otp_required": true }
```
```json
{ "…": "…", "status": "otp_resent", "otp_required": true }
```
```json
{ "…": "…", "status": "session_reused", "otp_required": false }
```

- `otp_sent` / `otp_resent` → tampilkan kartu "menunggu OTP" + input OTP.
  `id` dipakai untuk verify/resend/cancel.
- `session_reused` → **tanpa OTP**; nomor sudah langsung terdaftar. Muat ulang
  daftar dan tampilkan kartu normal.
- `status` lain `otp_resent` juga terjadi kalau nomor yang sama ditambahkan lagi
  saat masih pending (bukan error 409).
- Error: `400` provider tak dikenal, `409` nomor sudah terdaftar (anggap sukses:
  cukup refresh daftar), `502` gagal kirim OTP di sisi operator.

### 3.5 `POST /numbers/{id}/resend`

Tanpa body. `200`:
```json
{ "id": "…", "msisdn": "…", "provider": "…", "transid": "…",
  "status": "otp_resent", "otp_required": true }
```
`400` bila tidak ada OTP pending untuk id itu (sudah kedaluwarsa / sudah
diverifikasi) → arahkan pengguna menambah nomor ulang. Timer TTL di-reset.

### 3.6 `POST /numbers/{id}/verify`

Request: `{ "otp": "123456" }`. `200`:
```json
{ "ok": true, "id": "a1b2c3d4e5f6", "status": "verified" }
```
`400` bila OTP salah atau pending sudah hilang (isi `detail` = pesan dari
operator; tampilkan ke pengguna dan biarkan ia coba lagi).

### 3.7 `GET /numbers/{id}/balance`

`200`:
```json
{ "id": "a1b2c3d4e5f6", "provider": "bima", "msisdn": "6281234567890",
  "balance": { "balance": "12450", "quota": 387072, "unit": "KB", "used": 1024,
               "unlimited": false, "active_until": "2026-12-29 23:59:59",
               "grace_until": null, "bonstri": 12 } }
```
`401` = tidak ada sesi valid → minta tambah nomor ulang.
`502` = operator menolak/gagal; tampilkan `detail`.

### 3.8 `DELETE /numbers/{id}`

Bekerja untuk nomor terdaftar **dan** untuk pending. `200`: `{"ok": true}`.
`404` bila id tidak dikenal.

### 3.9 `GET /stream` (SSE)

`Content-Type: text/event-stream`. Setiap `POLL_SEC` detik:

```
data: {"numbers":[{"id":"…","provider":"bima","label":"bima+ (Tri)",
        "msisdn":"6281234567890","ok":true,
        "data":{ "balance":"12450","quota":387072,"unit":"KB","used":1024,
                 "unlimited":false,"active_until":"2026-12-29 23:59:59",
                 "grace_until":null,"bonstri":12 },
        "error":null}],"ts":1762345678.9}
```

- `data` berisi objek yang sama dengan field `balance` di §3.7.
- `ok:false` + `error` terisi (mis. `"no_session"`) → kartu dalam keadaan error,
  tetap tampilkan nomor, beri tombol tambah ulang.
- SSE **hanya** memuat nomor terdaftar; nomor pending dikelola lewat respons
  `POST /numbers` (server tidak mengirimnya di stream).
- Sambungkan saat halaman dimuat; saat error/terputus, tampilkan status dan
  andalkan `EventSource` retry bawaan.
- **Jangan** cache respons `/stream` di service worker.

---

## 4. Model data & aturan format

Field per nomor (dari `data` / `balance`):

| Field | Tipe | Arti | Tampilan |
|---|---|---|---|
| `balance` | string/number/null | pulsa rupiah | `Rp <angka>`; buang akhiran `.0`; `-` bila null |
| `quota` | number/null | sisa kuota utama, satuan = `unit` | lihat aturan di bawah |
| `unit` | string | satuan `quota`, umumnya `"KB"` | — |
| `used` | number/null | kuota terpakai (satuan sama) | opsional, tampil kecil |
| `unlimited` | bool | kuota tak terbatas | bila `true` → `UNLIMITED` |
| `active_until` | string/null | masa aktif berakhir | tampil apa adanya |
| `grace_until` | string/null | masa tenggang berakhir | tampil bila ada |
| `bonstri` | number/null | poin BonsTri | tampil bila ada |

Aturan format kuota (samakan dengan backend: `fmt_quota`):
- `unlimited === true` → `"UNLIMITED"`.
- `unit === "KB"`: `>= 1024*1024` → `XX.XX GB`; `>= 1024` → `XX MB`; selain itu
  `N KB`.
- `unit` lain (`MB`, `GB`, …) → `"<quota> <unit>"`.
- `quota` null → `-`.

Nilai tanggal datang sebagai string apa adanya dari operator; jangan parsing
ketat — tampilkan langsung.

---

## 5. Perilaku UI wajib (checklist)

Kerjakan semuanya; inilah kriteria "frontend jadi".

**Daftar & kartu**
1. Satu kartu per nomor: nama operator (`label`), nomor, pulsa, kuota, masa
   aktif, grace (bila ada), poin bonus (bila ada).
2. Kartu bisa dibedakan per operator (lihat §7) dan punya tombol hapus.
3. Kartu dalam status error (§3.9) tetap tampil dengan pesan + aksi pulih.
4. Tombol "Kirim ulang OTP" / "Batal" pada kartu pending.
5. Kosong = empty state yang menjelaskan cara menambah nomor.

**Tambah nomor**
6. Form tambah nomor di bottom-sheet/modal: pilih operator (dari
   `GET /providers`) + input nomor. Input `inputmode="numeric"`.
7. Mengganti operator saat ada pending **membatalkan** pending itu
   (`DELETE /numbers/{id}`) dan mereset field.
8. Setelah `POST /numbers` dengan `otp_required:true`: tampilkan input OTP +
   tombol Verifikasi, Kirim ulang, Batal; hitung mundur TTL 300 detik dari waktu
   respons terakhir (munculkan "kedaluwarsa" saat habis).
9. Verifikasi sukses → pending hilang, muat ulang daftar, kartu normal muncul.
10. Semua tombol mustahil double-submit: nonaktifkan selama request berjalan
    (pola `withBusy(btn, fn)`), tampilkan status progres.
11. Pesan error `detail` dari server ditampilkan apa adanya.
12. 409 "number already registered" diperlakukan sebagai sukses (refresh daftar).

**Update data**
13. `EventSource("/stream")` saat load; update kartu tanpa reload halaman.
14. Indikator status koneksi: live / terputus (retry).
15. Fallback manual: tombol refresh yang memanggil `GET /numbers` lalu
    `GET /numbers/{id}/balance` per nomor.
16. Snapshot terakhir disimpan ke `localStorage["digitral:lastSnapshot"]` setiap
    load maupun event SSE, dan dipulihkan saat offline (tampilkan data lama +
    tanda "data offline").

**PWA & perangkat**
17. `manifest.json` + service worker terdaftar (§6).
18. Tombol "Install" muncul dari event `beforeinstallprompt` (Chromium) dan
    instruksi iOS: "Share → Add to Home Screen" bila perangkat iOS.
19. Banner "versi baru tersedia, muat ulang" saat service worker baru mengambil
    alih.
20. Layout mobile-first: grid 1 kolom → ≥640 px auto-fill; hormati
    `env(safe-area-inset-*)`; target sentuh ≥40 px; font input ≥16 px
    (anti-zoom iOS).
21. Tidak ada teks debug/placeholder nyasar di UI atau aset.

---

## 6. Persyaratan PWA

- Service worker di `/sw.js` (rute server sudah menyediakannya — §8).
  - Contoh kebijakan: **cache-first untuk shell** (`/`, `/manifest.json`,
    `/assets/*`) dan **network-first untuk API** (`/health`, `/providers`,
    `/numbers*`).
  - **Jangan** cache `/stream`.
  - Ganti nama cache setiap rilis (mis. `digitral-shell-v2`) dan hapus cache
    lama di event `activate` supaya update langsung terlihat.
- `manifest.json`: `name`, `short_name`, `start_url:"/"`, `display:"standalone"`,
  `theme_color`, `background_color`, `icons` (min. satu 192×192 dan satu
  maskable 512×512).
- Service worker & install prompt hanya aktif di **secure context**:
  `localhost` atau HTTPS. Kalau diakses dari HP lewat IP/LAN, taruh di belakang
  reverse proxy HTTPS (mis. Caddy) agar bisa di-install.
- Sediakan `apple-touch-icon` dan meta
  `apple-mobile-web-app-capable` / `-status-bar-style` / `-title`.

---

## 7. Aset & tema

Sudah tersedia di `frontend/assets/` (jangan hapus, kamu boleh menambah):

| File | Isi |
|---|---|
| `bima.png` | logo bima+ (Tri), 192×192, RGBA |
| `myim3.png` | logo myIM3 (Indosat), 192×192, RGBA |
| `axisnet.png` | logo AXIS (untuk kartu operator yang menyusul §11), 180×180 |
| `icon.svg` | ikon PWA generik (gradasi biru) — **bukan** logo operator |

Aturan visual yang harus dipertahankan:
- Logo operator dipakai sebagai **watermark** kartu, bukan gradasi warna:
  `background-image` pada pseudo-element (`::before`) dengan
  `opacity: 0.18`, kartu `overflow:hidden`, logo tidak menutupi teks.
- Kartu operator mengenali kelas `provider-bima`, `provider-myim3`,
  `provider-axisnet` (kelas = `"provider-" + provider`).
- Tema gelap netral (contoh palet yang sudah dipakai: bg `#0f172a`, card
  `#1e293b`, teks `#e2e8f0`, aksen `#38bdf8`, danger `#f87171`, ok `#4ade80`).
  Bebas menyempurnakan, tetapi pertahankan kontras dan nuansa gelap.

---

## 8. Tata letak file & cara server menyajikannya

Server menyajikan frontend dari folder `frontend/` dengan aturan tetap:

| Rute | Sumber |
|---|---|
| `GET /` | `frontend/index.html` (dibaca sebagai HTML) |
| `GET /assets/*` | isi `frontend/assets/` (static mount) |
| `GET /manifest.json` | `frontend/manifest.json` |
| `GET /sw.js` | `frontend/sw.js` |

Konsekuensi yang wajib kamu patuhi:

- `index.html` **harus** di `frontend/index.html` dan memakai path absolut
  (`/assets/...`, `/manifest.json`, `/sw.js`) atau relatif yang tetap resolve
  dari `/`.
- Tidak ada build step yang diperlukan: HTML/CSS/JS vanilla sudah cukup. Bila
  kamu memakai bundler (Vite/webpack/React dsb.), hasil build **harus** mendarat
  di `frontend/` dengan aturan tabel di atas (asset ke `frontend/assets/`,
  service worker tetap `frontend/sw.js`) — server.py tidak akan diubah.
- **Jangan** mengubah `server.py` atau `scripts/`.

---

## 9. Alur implementasi yang disarankan

1. Jalankan backend (§2), pastikan `curl -s localhost:8765/health` balas `ok`.
2. Bangun layout + kartu dengan data statis (mock sesuai §4), sempurnakan
   format pulsa/kuota/tanggal.
3. Sambungkan `GET /providers` dan `GET /numbers`; render kartu asli.
4. Implementasi alur tambah nomor (§3.4 → §3.6) termasuk pending, resend,
   cancel, TTL, dan anti double-submit.
5. Sambungkan `GET /stream` + snapshot offline.
6. Pasang manifest + service worker + install prompt.
7. Jalankan checklist §5 lalu verifikasi §10.

Saat menambah nomor sungguhan untuk uji, pakai **nomor milikmu sendiri** dan
hanya ketika kamu memang bisa membaca SMS-nya.

---

## 10. Verifikasi & definition of done

Perintah cepat (ganti `<id>` dengan id dari `GET /numbers`):

```bash
docker compose up -d --build
curl -s localhost:8765/health
curl -s localhost:8765/providers
curl -s localhost:8765/numbers
curl -s localhost:8765/numbers/<id>/balance
curl -s -o /dev/null -w "%{http_code}\n" localhost:8765/ localhost:8765/manifest.json \
  localhost:8765/sw.js localhost:8765/assets/bima.png
curl -sN localhost:8765/stream | head -c 400      # cek format SSE
```

Lalu dari browser (idealnya dari HP):

- DevTools → Application: Manifest valid, service worker `activated`, cache
  terisi; Console bersih dari error.
- Network: `/stream` berstatus `200` dan terus mengalir.
- Lighthouse → kategori PWA/installability lolos.

**Definition of done** — semua benar:

1. Semua item §5 terpenuhi.
2. Data kartu berasal dari API (§3), bukan data mock.
3. Alur tambah nomor berfungsi penuh: OTP terkirim → verifikasi → kartu muncul;
   resend, batal, hapus bekerja.
4. Update otomatis via SSE terlihat tanpa reload, dan snapshot offline muncul
   saat jaringan mati.
5. PWA bisa di-install (manifest + SW + install prompt/instruksi iOS).
6. `curl` di atas semua `200`; tidak ada error di Console.

---

## 11. Di luar cakupan / jangan diasumsikan

- **Provider `axisnet` (AXIS) dan `myXL` belum dilayani backend.** Backend baru
  punya `bima` dan `myim3`. `axisnet.png` disediakan untuk kartu yang menyusul —
  jangan menampilkannya sampai `GET /providers` memuat `axisnet`.
- **Tidak ada autentikasi pengguna** di backend. Jangan bangun layar login;
  keamanan ditangani reverse proxy.
- **Tidak ada notifikasi push**, tidak ada WebSocket — hanya SSE + polling.
- **Tidak ada endpoint riwayat/transaksi.** Hanya snapshot terbaru.
- Detail kuota per paket (rincian beberapa paket sekaligus) **belum** ada di
  API; yang tersedia hanya satu `quota` ringkasan per nomor.
- Format `detail` error operator bisa berubah; jangan mencocokkan string untuk
  logika — pakai status HTTP.
- Bila kamu merasa butuh field backend baru, **jangan mengarang**; berhenti dan
  minta penambahan endpoint ke pemilik repo.

---

## 12. Aturan aman (tidak boleh dilanggar)

- Kirim OTP hanya atas aksi eksplisit pengguna untuk nomor miliknya sendiri.
  Jangan pernah menjalankan uji otomatis ke endpoint yang mengirim OTP.
- Jangan mencatat (log/kirim/commit) nomor telepon nyata, token, transid, atau
  isi snapshot pengguna.
- Jangan menanam kredensial/kunci apa pun di frontend; semua rahasia ada di
  server.
- Jangan menghubungi host operator langsung dari browser.
**END OF DOCUMENT**
