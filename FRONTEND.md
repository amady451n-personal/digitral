# FRONTEND.md — Bangun frontend dashboard (kontrak API)

Dokumen ini berdiri sendiri. Kamu tidak perlu membaca apa pun di luar folder ini.
Tugasmu: **membuat frontend di dalam folder ini** yang mengikuti kontrak API di
bawah. Backend sudah selesai dan tidak boleh kamu ubah.

Folder ini sudah berisi `assets/` (logo operator + ikon PWA). Kamu menambah
`index.html`, `manifest.json`, `sw.js`, dan aset tambahan bila perlu.

---

## 1. Yang dibangun

Satu halaman PWA, mobile-first, tema gelap, untuk memantau nomor prabayar dari
beberapa operator dalam satu dashboard berbentuk kartu.

- Kondisi awal: layar hampir kosong — hanya satu tombol **"Tambah nomor"**.
- Tambah nomor: form berisi input nomor + daftar operator yang dikirim backend.
- Setelah submit, frontend **mengikuti jawaban backend**: minta kode,
  langsung tersimpan, atau gagal.
- Setelah kode benar, nomor tersimpan dan muncul sebagai **kartu**.
- Kartu bertambah mengikuti nomor yang ditambahkan.

Frontend **generik**: tidak ada nama operator, warna operator, logo operator,
atau aturan khusus operator yang ditulis di kode. Semua itu datang dari API.
Kalau sebuah informasi tidak ada di API, jangan dikarang dan jangan ditampilkan.

---

## 2. Aturan penyajian oleh backend

Folder ini disajikan apa adanya oleh backend dengan pemetaan tetap:

| Rute | Berkas |
|---|---|
| `GET /` | `index.html` |
| `GET /assets/*` | isi folder `assets/` |
| `GET /manifest.json` | `manifest.json` |
| `GET /sw.js` | `sw.js` |

Konsekuensi:

- `index.html` wajib ada di akar folder ini; `manifest.json` dan `sw.js` juga.
- Pakai path absolut (`/assets/...`, `/manifest.json`, `/sw.js`) agar tidak
  pecah saat rute apa pun dibuka.
- **Tanpa build step.** HTML/CSS/JS langsung jalan. Kalau memakai bundler,
  hasil build harus tetap mendarat dengan nama dan lokasi di tabel di atas.
- API dan halaman berada di origin yang sama. Tidak perlu mengatur CORS.

---

## 3. Kontrak API

Semua JSON. Error selalu berbentuk `{"detail": "<pesan>"}` dengan status
`400` / `401` / `404` / `409` / `502`; tampilkan `detail` apa adanya.

**Jaminan bentuk:** untuk operator mana pun, bentuk respons identik. Field yang
tidak ada bernilai `null` atau string kosong — tidak pernah dihilangkan, tidak
pernah berbeda nama. Frontend tidak boleh bercabang per operator.

**Jaminan identitas:** setiap objek yang memuat `provider` juga memuat
`label`, `color`, dan `logo` — jadi satu objek cukup untuk menggambar satu kartu
tanpa menggabungkan data dari endpoint lain.

- `label` — nama tampilan operator (string).
- `color` — warna aksen operator, hex CSS (mis. `"#e01b2e"`).
- `logo` — path gambar yang bisa langsung dipakai `<img src>` (mis.
  `"/assets/bima.png"`), atau `null` bila tidak ada.

### 3.1 `GET /health`

```json
{ "ok": true, "providers": ["bima", "myim3"] }
```

### 3.2 `GET /providers`

Urutan array adalah **urutan tampil resmi** untuk pengelompokan kartu.

```json
[
  { "id": "bima",    "label": "bima+ (Tri)",    "available": true,
    "color": "#e01b2e", "logo": "/assets/bima.png" },
  { "id": "myim3",   "label": "myIM3 (Indosat)", "available": true,
    "color": "#ffd200", "logo": "/assets/myim3.png" }
]
```

- `available: false` = operator belum dilayani. Jangan tampilkan di form, jangan
  dipakai untuk grouping.
- Jangan menulis daftar operator di kode; selalu ambil dari sini.

### 3.3 `GET /numbers`

```json
{
  "numbers": [
    { "id": "a1b2c3d4e5f6", "provider": "bima", "label": "bima+ (Tri)",
      "msisdn": "6281234567890", "added_at": 1762345678.9, "pending": false,
      "color": "#e01b2e", "logo": "/assets/bima.png" }
  ]
}
```

`?include_pending=1` menambahkan nomor yang masih menunggu OTP
(`"pending": true`). Pakai ini saat halaman dimuat ulang agar status menunggu
tidak hilang. `msisdn` selalu `62…` tanpa `+`. Untuk nomor pending, `added_at`
adalah waktu kode diminta (dipakai untuk hitung mundur 300 detik).

### 3.4 `POST /numbers`

Request: `{"provider": "bima", "msisdn": "081234567890"}` — nomor boleh format
lokal/`+62`/`62`, backend menormalkan.

Respons `200`, salah satu dari tiga:

```json
{ "id": "…", "msisdn": "628…", "provider": "bima", "label": "bima+ (Tri)",
  "color": "#e01b2e", "logo": "/assets/bima.png",
  "transid": "…", "status": "otp_sent", "otp_required": true }
```

```json
{ "…": "…", "status": "otp_resent", "otp_required": true }
```

```json
{ "…": "…", "status": "session_reused", "otp_required": false }
```

- `otp_sent` / `otp_resent` → tampilkan input kode untuk `id` tersebut.
- `session_reused` → tidak perlu kode; muat ulang daftar, kartu langsung muncul.
- `409 "number already registered"` → nomor sudah terdaftar dan sesinya masih
  hidup; perlakukan sebagai sukses: muat ulang daftar. (Kalau kartunya sedang
  error `no_session`, ikuti §5.6.)

### 3.5 `POST /numbers/{id}/resend`

Tanpa body. `200` = bentuk yang sama dengan `otp_resent` di atas.
`400` = tidak ada kode yang menunggu (kedaluwarsa/sudah terverifikasi) →
kembali ke form tambah nomor.

### 3.6 `POST /numbers/{id}/verify`

Request: `{"otp": "123456"}`.

```json
{ "ok": true, "id": "a1b2c3d4e5f6", "status": "verified" }
```

`400` = kode salah atau sudah kedaluwarsa. Tampilkan `detail`, biarkan pengguna
mencoba lagi atau meminta kode baru.

### 3.7 `GET /numbers/{id}/balance`

```json
{ "id": "…", "provider": "bima", "msisdn": "628…", "label": "bima+ (Tri)",
  "color": "#e01b2e", "logo": "/assets/bima.png",
  "balance": { "balance": "12450", "quota": 387072, "unit": "KB", "used": 1024,
               "unlimited": false, "active_until": "2026-12-29 23:59:59",
               "grace_until": null, "bonstri": 12 } }
```

`401` = sesi tidak valid → arahkan pengguna menambah nomor ulang.
`502` = operator gagal; tampilkan `detail`.

### 3.8 `DELETE /numbers/{id}`

Bekerja untuk nomor terdaftar maupun yang masih menunggu kode. `200`:
`{"ok": true}`. `404` = id tidak dikenal.

### 3.9 `GET /stream` (SSE)

`Content-Type: text/event-stream`, satu kejadian tiap ±60 detik. Kejadian
pertama dikirim **segera** setelah koneksi, tidak menunggu interval.

Setiap kejadian adalah **snapshot penuh** (semua nomor terdaftar), bukan delta:

```
data: {"numbers":[{"id":"…","provider":"bima","label":"bima+ (Tri)",
        "msisdn":"628…","added_at":1762345678.9,"ok":true,
        "data":{ "balance":"12450","quota":387072,"unit":"KB","used":1024,
                 "unlimited":false,"active_until":"2026-12-29 23:59:59",
                 "grace_until":null,"bonstri":12 },
        "error":null,"color":"#e01b2e","logo":"/assets/bima.png"}],"ts":1762345678.9}
```

- `data` = objek yang sama dengan field `balance` di §3.7.
- `ok:false` + `error` terisi (mis. `"no_session"`) → kartu tetap tampil, beri
  penanda error dan aksi tambah ulang.
- Karena snapshot penuh: nomor yang muncul ditambahkan, yang tidak muncul
  dihapus dari daftar. Urutan tetap dihitung dari `/numbers` + `/providers`.
- Stream hanya memuat nomor terdaftar; nomor yang menunggu kode dikelola dari
  respons §3.4/§3.5 dan `?include_pending=1`.

---

## 4. Isi kartu & aturan format

Objek `data` (dari `/stream`) atau `balance` (dari §3.7):

| Field | Tipe | Tampilan |
|---|---|---|
| `balance` | string/number/null | `Rp <angka>`; buang akhiran `.0`; null → `—` |
| `quota` | number/null | diformat menurut `unit` (lihat bawah); null → `—` |
| `unit` | string | satuan `quota`, umumnya `"KB"` |
| `used` | number/null | opsional, tampil kecil |
| `unlimited` | bool | `true` → tulis `UNLIMITED` |
| `active_until` | string/null | tampil apa adanya |
| `grace_until` | string/null | tampil bila ada |
| `bonstri` | number/null | tampil bila ada |

Format kuota:

- `unlimited` → `UNLIMITED`
- `unit === "KB"`: `≥ 1024×1024` → `XX.XX GB`; `≥ 1024` → `XX MB`; selain itu `N KB`
- unit lain → `"<quota> <unit>"`
- null → `—`

Tanggal adalah string dari operator; tampilkan langsung, jangan parsing ketat.
Angka `0` adalah nilai sah, bukan "kosong".

---

## 5. Layar & perilaku

### 5.1 Kosong

Hanya tombol **"Tambah nomor"**. Tidak ada teks sambutan panjang, tidak ada
kartu placeholder. Tombol membuka form.

### 5.2 Form tambah nomor

Bottom sheet: input nomor (`type="tel"`, `inputmode="numeric"`, font ≥16 px) +
daftar operator dari §3.2 (hanya `available:true`; tampilkan `label` + `logo`).

Mengganti operator saat ada form kode yang menunggu **membatalkan** yang menunggu
itu (`DELETE`) dan mereset field.

### 5.3 Setelah submit

Ikuti `status` (§3.4) apa adanya:

- `otp_sent`/`otp_resent` → tampilkan input kode + tombol Verifikasi, Kirim
  ulang, Batal; hitung mundur dari 300 detik dan tandai "kedaluwarsa" saat habis.
- `session_reused` → tutup form, muat ulang daftar.
- `409` → tutup form, muat ulang daftar.
- error lain → tampilkan `detail` di dalam form, form tetap terbuka.

Verifikasi sukses → tutup form, muat ulang daftar, kartu muncul.

### 5.4 Daftar kartu

- Satu kartu per nomor; kartu bertambah sesuai nomor yang ditambahkan.
- **Pengelompokan:** per operator. Urutan grup = urutan array `/providers`.
  Di dalam grup = urutan `added_at` menaik (urutan input). Bukan urutan input
  global.
- Nomor yang operatornya tidak ada di `/providers` (mis. operator dinonaktifkan
  setelah nomor terdaftar) **jangan disembunyikan**: taruh di grup paling akhir
  memakai `label`/`color`/`logo` milik nomor itu sendiri, urut `label`.
- Tiap grup punya header kecil: `label` grup + jumlah nomor terverifikasi
  (nomor yang masih menunggu kode tidak dihitung).
- Aksi per kartu: hapus (`DELETE`), dan segarkan data kartu
  (`GET /numbers/{id}/balance`).

### 5.5 Nomor yang menunggu kode setelah halaman dimuat ulang

- `GET /numbers?include_pending=1` mengembalikan nomor pending beserta
  `added_at` (= waktu kode diminta).
- Tampilkan sebagai kartu di grup operatornya, ditandai "menunggu kode", dengan
  aksi **Lanjutkan** (buka input kode) dan **Batal**.
- Hitung mundur 300 detik dihitung dari `added_at`; kalau sudah lewat, tampilkan
  "kedaluwarsa" dan arahkan ke **Kirim ulang**.
- **Batal** menghapus nomor pending (`DELETE`). Menutup sheet tanpa menekan
  Batal **tidak** menghapus — statusnya tetap bisa dilanjutkan.

### 5.6 Sesi tidak valid (`401` / `no_session`)

Tidak ada endpoint re-auth. Pemulihannya: hapus nomor itu (`DELETE`) lalu buka
form tambah nomor dengan nomor yang sudah terisi, setelah pengguna mengonfirmasi
(mis. "Sesi berakhir. Tambah ulang nomor ini?"). Jangan menghapus diam-diam.

### 5.7 Pembaruan data

- Saat halaman dimuat: `GET /providers`, lalu `GET /numbers?include_pending=1`,
  lalu buka `EventSource("/stream")` (kejadian pertama datang segera, jadi tidak
  perlu menarik `/balance` satu per satu saat awal).
- Setiap event stream adalah snapshot penuh: perbarui kartu, tambah nomor baru,
  buang nomor yang hilang. Nomor pending tetap dipertahankan dari state lokal.
- Indikator status: live / terputus (EventSource retry bawaan).
- Tombol segarkan manual: `GET /numbers` lalu `GET /numbers/{id}/balance`.
- Simpan snapshot terakhir (`localStorage["digitral:lastSnapshot"]`) setiap load
  dan setiap event; pulihkan saat offline dengan penanda "data offline".
- Semua tombol tidak boleh bisa ditekan dua kali saat request berjalan.

---

## 6. Desain kartu (konsep acuan)

Kartu bergaya **kartu debit/kredit**, tema gelap. Ini arah yang disepakati;
detail akhir bebas kamu tentukan selama ciri berikut dipertahankan:

- Sudut membulat, rasio mendekati kartu (sekitar 1.6 : 1), warna operator
  menutupi permukaan kartu (gradien dari `color` ke gelap).
- `logo` operator sebagai **watermark** di sudut kanan bawah, `object-fit:
  contain`, opacity ±0.16, tidak menutupi teks.
- Baris atas: aksen "chip" kecil + tombol aksi (`⋯`).
- Tengah: nomor termask gaya kartu (`•••• •••• 7890`) + pulsa sebagai angka
  besar.
- Bawah: baris label kecil — `KUOTA`, `AKTIF`, `GRACE`, `BONSTRI` — nilai di
  bawah label, kosong jadi `—`.

Titik awal CSS (silakan disempurnakan):

```css
.card {
  position: relative; overflow: hidden; border-radius: 16px;
  aspect-ratio: 1.62 / 1; padding: 14px 15px 12px; color: #e2e8f0;
  display: flex; flex-direction: column; justify-content: space-between;
  background: linear-gradient(140deg, var(--accent) 0%, #101a2e 58%, #0b1220 100%);
}
.card .logo {
  position: absolute; right: -10px; bottom: -8px;
  width: 118px; height: 118px; object-fit: contain; opacity: .16;
}
.card .amount { font-size: 21px; font-weight: 700; }
.card .foot { display: flex; gap: 12px; position: relative; z-index: 2; }
.card .foot span { display: block; font-size: 10px; color: #94a3b8;
  text-transform: uppercase; letter-spacing: .08em; }
.card .foot b { font-size: 11px; color: #e2e8f0; }
```

Catatan kontras: sebagian `color` terang (mis. kuning). Pastikan teks tetap
terbaca — cara termudah: pertahankan bagian gelap gradien dan teks terang, atau
pilih warna teks berdasarkan luminance `color`.

Palet dasar tema gelap: bg `#0b1220`, permukaan `#1e293b`, teks `#e2e8f0`,
redup `#94a3b8`, aksen UI `#38bdf8`, danger `#f87171`, ok `#4ade80`.

---

## 7. PWA & mobile

- `manifest.json`: `name`, `short_name`, `start_url:"/"`,
  `display:"standalone"`, `theme_color`, `background_color`, ikon 192×192 dan
  satu `purpose:"maskable"` 512×512.
- `sw.js`: cache-first untuk shell (`/`, `/manifest.json`, `/assets/*`),
  network-first untuk API (`/health`, `/providers`, `/numbers*`).
  **Jangan cache `/stream`.** Ganti nama cache tiap rilis dan hapus cache lama di
  `activate`; tampilkan banner "versi baru tersedia — muat ulang".
- Tombol Install dari event `beforeinstallprompt`; di iOS tampilkan instruksi
  "Share → Add to Home Screen".
- Service worker hanya aktif di `localhost` atau HTTPS.
- Mobile-first: 1 kolom, `≥640px` boleh grid; hormati `env(safe-area-inset-*)`;
  target sentuh ≥40 px; input ≥16 px.
- `<meta name="viewport" content="width=device-width, initial-scale=1,
  viewport-fit=cover">`, `theme-color`, `apple-mobile-web-app-*`,
  `apple-touch-icon`.

---

## 8. Aset yang sudah tersedia

```
assets/bima.png     192×192
assets/myim3.png    192×192
assets/axisnet.png  187×187
assets/myxl.png     158×158
assets/icon.svg     ikon PWA generik (bukan logo operator)
```

Jangan menulis nama berkas logo di kode: pakai `logo` dari API. `assets/` boleh
ditambah (mis. ikon 512×512 untuk manifest) tetapi jangan dihapus.

---

## 9. Batasan

- Jangan mengubah backend, API, atau bentuk respons. Kalau butuh field baru,
  berhenti dan minta.
- Jangan menghubungi server operator dari browser; satu-satunya sumber data
  adalah API di §3.
- Jangan menaruh rahasia/kunci apa pun di frontend.
- Jangan mengirim kode OTP atas inisiatif sendiri (mis. saat load halaman atau
  saat pengujian otomatis). Hanya setelah pengguna menekan tombol.
- Jangan mencatat nomor telepon nyata, kode, atau isi snapshot ke log/commit.
- Backend tanpa autentikasi; jangan bangun layar login.

---

## 10. Verifikasi & definition of done

```bash
curl -s localhost:8765/health
curl -s localhost:8765/providers
curl -s localhost:8765/numbers
curl -s -o /dev/null -w "%{http_code}\n" localhost:8765/ \
  localhost:8765/manifest.json localhost:8765/sw.js localhost:8765/assets/bima.png
curl -sN localhost:8765/stream | head -c 400
```

Selesai bila:

1. Kondisi awal hanya menampilkan tombol "Tambah nomor".
2. Form memuat daftar operator dari API dan berhasil menambah nomor lewat kode.
3. Status dari backend diikuti apa adanya: minta kode, langsung tersimpan, atau
   pesan error.
4. Kartu muncul setelah verifikasi, bertambah per nomor, dikelompokkan per
   operator sesuai §5.4, dan memakai `color` + `logo` dari API.
5. Data kartu ter-update lewat SSE tanpa reload; snapshot offline bekerja.
6. PWA bisa di-install; manifest & service worker valid.
7. Tidak ada nama/warna/logo operator yang ditulis di kode (nilai pada contoh di
   dokumen ini hanya ilustrasi, bukan konstanta) — semuanya dari API.
8. `curl` di atas semua `200`; Console browser bersih.
