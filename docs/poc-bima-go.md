# POC: Porting Alur Bima ke Go

## Tujuan

Validasi bahwa Go bisa menggantikan `bima_balance.py` untuk alur lengkap:
`normalize_msisdn` → `Vault` → `Http` → `BimaClient` → `get_balance`

Bukan替换 seluruh backend. Jika gagal di salah satu tahap, hentikan porting
Python → Go dan pertimbangkan Rust sebagai gantinya.

---

## Scope: hanya alur BIMA+ (`bimaplus-api.ioh.co.id`)

```
Caller (Go)                  Python (dibiarkan utuh)
────────────────              ───────────────────────
get_balance(msisdn)           scripts/bima_balance.py
  ├─ normalize                scripts/bima_balance.py:310
  ├─ Vault.init()             scripts/bima_balance.py:324
  │    ├─ device_id()         scripts/bima_balance.py:358
  │    ├─ faaid()             scripts/bima_balance.py:377
  │    ├─ gaid()              scripts/bima_balance.py:383
  │    └─ session()           scripts/bima_balance.py:391
  ├─ Http.init()              scripts/bima_balance.py:420
  │    └─ curl_cffi fallback  scripts/bima_balance.py:431 (JIKA ada di Go)
  ├─ BimaClient.get_balance() scripts/bima_balance.py:718
  │    ├─ request_guest_token  scripts/bima_balance.py:607
  │    ├─ send_otp            scripts/bima_balance.py:618
  │    ├─ validate_otp         scripts/bima_balance.py:630
  │    ├─ dashboard           scripts/bima_balance.py:642
  │    └─ parse_dashboard     scripts/bima_balance.py:744 (di server.py)
  └─ parse_dashboard          server.py:184 (dead code, tapi perlu)
```

**TLS fingerprint adalah pengujian utama.** Ingress `/api/v2` menolak sebagian
stack TLS app-level dengan HTTP 200 + `{"code":"10001","message":"Authentication
failed"}`. Python stdlib kena; `curl_cffi impersonate=chrome124` lolos.

**Hasil uji (2026-11-21, `poc/bima/`): risiko ini gugur untuk Go.**
Go stdlib `crypto/tls` lolos ingress, baik HTTP/2 maupun HTTP/1.1 paksa:

| Stack | `/token/guest/v1` | `/dashboard/get/v7` (token guest) | Vonis |
|---|---|---|---|
| Python stdlib (`urllib`) | code 10001 | code 10001 | ditolak ingress |
| curl_cffi chrome124 | code 26100 + token | code 10003 `Mobile Number is not valid.` | lolos, app logic |
| Go stdlib (h2) | code 26100 + token | code 10003 `Mobile Number is not valid.` | **lolos, app logic** |
| Go stdlib (h1 paksa) | code 26100 + token | code 10003 `Mobile Number is not valid.` | **lolos, app logic** |
| Go + uTLS chrome120 (h2) | code 26100 + token | n/a | lolos |
| Go + uTLS chrome120 (ALPN h1) | code 26100 + token | n/a | lolos |

`10003` adalah error bisnis dari app, bukan gerbang ingress, jadi request Go
benar-benar sampai ke logika aplikasi. Artinya gate memblokir fingerprint Python,
bukan "non-browser" secara umum. **utls tidak wajib**; simpan sebagai fallback
opsional di balik interface `Doer`, jangan jadikan dependency default.

Catatan sisa risiko: `/otp/send/v1` dan `/otp/validate/v2` belum diuji karena
mengirim OTP tidak diizinkan. Keduanya bisa punya aturan bot tambahan. Desain
`http.go` harus tetap memungkinkan swap transport (stdlib ↔ utls) tanpa ubah
`bima.go`.

Biaya terukur: binary `-s -w` = 5.5 MB (stdlib) vs 7.9 MB (+utls/+x/net/http2);
peak RSS proses one-shot = 14.2 MB (utls).

**Hasil uji vault (2026-11-21):** `vault.go` membaca vault Python apa adanya
(AES-GCM `vault.key`/`vault.bin`) dan menulis balik yang bisa didekripsi Python.
Session bima di `~/.bima` masih valid (sisa ±37 hari), jadi alur
vault → session → `/dashboard/get/v7` → `parse_dashboard` **terbukti tanpa OTP**:

| Field | Python | Go |
|---|---|---|
| balance | `9400` | `9400` |
| quota / unit | `26992640` KB | `26992640` KB |
| used | `58990592` | `58990592` |
| active / grace | `16 Nov 2027` / `16 Dec 2027` | sama |
| package_quotas | 4 paket | 4 paket, nama + sisa identik |
| fmt_quota | `25.7 GB` | `25.7 GB` |

Catatan operasional: volume `digitral_data` di container **kosong**, jadi vault
nyata ada di host (`~/.bima` untuk CLI, `~/.bima-frontend/<provider>` untuk
frontend). POC harus menunjuk `POC_VAULT=<dir>`; uji dilakukan pada salinan di
`/tmp/opencode/`, vault asli tidak disentuh.

### Vault lintas provider (2026-11-21)

Format kontainer **identik** untuk semua provider: `vault.key` 32 byte +
`vault.bin` (iv 12 + ciphertext + tag 16, JSON). Yang berbeda hanya isi
`sessions`, jadi `vault.go` tetap generik dan semantik sesi dipindah ke client
masing-masing (`POC_VAULT_SCAN=<base>` memverifikasi ini, exit 0):

| Provider | Key sesi | Arti kedaluwarsa | Kebutuhan akses |
|---|---|---|---|
| bima | `action deviceId exp token` | `exp` = JWT login (detik), cek 60 s lebih awal | token langsung dipakai |
| myim3 | `action deviceId exp token` | sama dengan bima | token langsung dipakai |
| myxl | `access_token exp id_token refresh_token token token_exp` | `exp` = refresh_token, `token_exp` = access_token | saat `exp` lewat, **wajib** `RawSession` + grant `refresh_token`; `Session()` menyembunyikan entri itu |
| axisnet | `device issued provider scheme token` | **tidak ada `exp`**, hanya `issued` (string waktu) | token long-lived; jangan pakai aturan `exp` |

Konsekuensinya di Go:

- `Session(msisdn)` = aturan Python `Vault.session()` (exp detik, margin 60 s,
  `exp` kosong/0 berarti tanpa kedaluwarsa) → cocok untuk bima/myim3/axisnet.
- `RawSession(msisdn)` = tanpa cek exp → satu-satunya jalan untuk myXL refresh.
- `SessionMSISDNs()` untuk memilih nomor saat vault punya lebih dari satu.
- Identitas perangkat berbeda: hanya bima yang menyimpan `deviceProfile`
  (Xiaomi M2007J20CG), myim3/axisnet menyimpan `faaid`+`gaid` tanpa profil, myxl
  hanya `deviceId`. Python **tidak** memakai `deviceProfile` untuk header
  (default `generic/samsung/SM-A235F/14`); Go mengikuti itu secara default dan
  menyediakan `POC_DEVICE_PROFILE=1` untuk memakai profil asli.
- `Vault.DeviceID()` adalah `deviceId` top-level, bukan `deviceProfile.deviceId`
  (di vault bima keduanya berbeda: `2adbc8…` vs `4895f6…`), sama seperti Python.

---

## File yang akan dibuat (direktori baru: `poc/bima/`)

```
poc/bima/
├── go.mod
├── vault.go        # Vault: AES-GCM read/write vault.bin, device_id, faaid, gaid, session
├── crypto.go       # normalize, sha512_hex, rc4_hex, aes_gcm_encrypt, get_values
├── http.go         # Http: net/http stdlib (default) + utls fallback opsional
├── bima.go         # BimaClient: baseline headers, build_headers, _post,
│                   #   request_guest_token, send_otp, validate_otp, dashboard
├── parse.go         # parse_dashboard, fmt_quota (dari server.py:744-789)
├── config.go        # PROVIDER_BIMA / PROVIDER_MYIM3 constant
└── main.go         # CLI: go run . <msisdn> [--verbose]
```

---

## Catatan teknis per file

### vault.go

- `vault.key` (32 byte random, mode 0600)
- `vault.bin`: iv (12 byte) + ciphertext + tag (16 byte) — AES-GCM
- Gunakan `crypto/cipher` stdlib
- Device ID / faaid / gaid: random UUID di-generate sekali, disimpan di vault
- Session: `{token, exp, action, deviceId}` per msisdn

### crypto.go

- `normalize_msisdn` — persis sama (strip +, 0→62, 8-digit double-prepend)
- `sha512_hex` — stdlib `crypto/sha512`
- `rc4_hex` — RC4 dengan `crypto/rc4` stdlib (deprecated tapi tersedia; atau
  implementasi custom jika dihapus dari stdlib; cek dulu)
- `aes_gcm_encrypt` — `crypto/cipher` AES-GCM
- `get_values` — parsing string, replace `$` → `$`, hapus spasi

### http.go

**CRITICAL:** TLS fingerprint. Gunakan
`github.com/refraction-networking/utls` (port Go dari `curl_cffi`).

```go
import "github.com/refraction-networking/utls"
// uTLS Chrome 124 fingerprint
conn, _ := tls.Client(tcpConn, &utls.Config{
    ServerName: host,
    NextProto:  []string{"http/1.1"},
})
uConn := utls.MakeClientHello("chrome124")
// atau
client := &http.Client{
    Transport: &utls.UberRoundTripper{
        Inner: http.DefaultTransport,
    },
}
```

Pilih 1 dari 2 pola ini berdasarkan eksperimen.
**Hasil eksperimen: stdlib `net/http` sudah lolos** (lihat tabel di atas), jadi
default-nya stdlib. utls hanya fallback bila `/otp/send` ternyata lebih ketat.
Catatan: spesifikasi ClientHello uTLS Chrome membawa ALPN sendiri (`h2`), jadi
jalur utls butuh `golang.org/x/net/http2` (atau strip ekstensi ALPN untuk h1).

### bima.go

Salin persis semua header dari `baseline()` dan `build_headers()`.
Hash `sha512_hex(who + secret + ...)` dan `sha512_hex("REQBODY=" + body ...)` harus
identik dengan Python.

**OTP interaktif:** POC ini TIDAK kirim OTP ke jaringan. Input OTP dari CLI
(`fmt.Scanln`). Di backend nyata, OTP dari HTTP request.

**Session reuse:** Jika vault punya session valid, skip OTP → langsung dashboard.

### parse.go

Salin `parse_dashboard` dari `server.py:744` + `fmt_quota` dari `server.py:789`.
Ini yang menghasilkan output JSON balance.

---

## Verifikasi keberhasilan

POC dianggap berhasil jika CLI ini menghasilkan JSON balance identik
dengan `python3 scripts/bima_balance.py <msisdn>`:

```bash
cd poc/bima && go run . 628123456789 --verbose
```

### Checklist

- [x] `vault.go`: baca vault Python + tulis balik yang kompatibel (terverifikasi
  dua arah); device_id stabil diulang
- [x] `crypto.go`: sha512_hex/get_values dipakai di `build_headers` dan hasilnya
  diterima server (code 26100 / 26000)
- [x] `http.go`: request ke `/token/guest/v1` mengembalikan token (HTTP 200, ada
  `access_token` di JSON), BUKAN `{"code":"10001"}` — **lolos di 2026-11-21,
  stdlib maupun utls; lihat tabel hasil uji**
- [ ] `bima.go`: alur guest-token → send-otp → validate-otp → dashboard
  menghasilkan data balance tanpa error
- [x] `parse.go`: output balance sesuai format server.py (dibandingkan field per
  field dengan Python, cocok)

### Jika gagal

1. **TLS 10001** (sudah tidak terjadi untuk Go stdlib) → bila muncul lagi,
   coba utls `HelloChrome_Auto` / `HelloChrome_131`, lalu fingerprint lain
2. **Header mismatch** → bandingan header go vs python (`--verbose` di kedua)
   dan lihat mana yang ditolak
3. **RC4 wrong** → implementasi custom RC4 (Python RC4 stdlib sudah dihapus di
   Python 3.13; jika Go `crypto/rc4` tidak ada, gunakan implementasi custom)
4. **Hash wrong** → `get_values` perlu dicek ulang (string parsing berbeda)

---

## Waktu estimasi

| Tahap | Estimasi |
|-------|----------|
| vault.go + crypto.go | 1–2 jam |
| http.go (utls setup) | 2–4 jam (TLS fingerprint trial) |
| bima.go | 2–3 jam |
| parse.go + config.go | 1 jam |
| Test + debug | 2–4 jam |
| **Total** | **8–16 jam** |

---

## Hubungan dengan rencana porting keseluruhan

POC bima = langkah #1. Jika berhasil:

1. Port bima (`vault`, `crypto`, `http`, `bima`, `parse`, `config`) → folder
   `backend/` Go
2. Port myxl → `myxl.go` (reused crypto dari `crypto.go`)
3. Port axisnet → `axisnet.go` (reused crypto)
4. Rewrite `server.py` → `backend/main.go` (mapping semua route)
5. Dockerfile: multi-stage, build Go binary → distroless static

Jika POC gagal → hentikan dan pertimbangkan Rust (atau tetap Python).

---

## Toolchain yang perlu diinstall

```bash
go install github.com/refraction-networking/utls@latest
# atau
go get github.com/refraction-networking/utls
```

Tidak ada dependensi eksternal selain utls (TLS fingerprint).
Storage: stdlib (`crypto/aes`, `crypto/sha512`, `crypto/cipher`, `encoding/json`).

## Status akhir (2026-11-21)

POC berhasil dan sudah dipindah ke backend Go produksi:

- `poc/bima/` dihapus; CLI verifikasi hidup di `go/cmd/poc-bima`.
- `go/internal/cryptoutil`, `vault`, `provider/{imi,axisnet,myxl}`, `httpapi`,
  `push`, `notify` menggantikan `server.py` + modul Python.
- Semua test: `cd go && go test ./...` hijau.
- Image `digitral-backend:latest` (scratch, 8,44 MB) lolos gate RAM ≤30 MB.
- Rencana lengkap + hasil ukur: `docs/port-go-plan.md`.
