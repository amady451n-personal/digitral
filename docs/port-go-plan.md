# Go Port (Digitral Backend) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ganti backend Python (`server.py` + uvicorn) dengan satu binary Go yang mempertahankan semua perilaku yang sudah terverifikasi (4 provider, vault AES-GCM, grace notification, web push) dengan RAM ≤ 30 MB.

**Architecture:** Satu binary `digitrald` yang menyajikan frontend statis + API JSON. Package `internal/` dipisah per tanggung jawab: `vault`, `cryptoutil`, `provider` (interface + imi/axisnet/myxl), `registry`, `httpapi`, `push`, `notify`. Format `vault.*` dan `registry.json` **tidak diubah**, sehingga Python dan Go bisa hidup berdampingan (jalur rollback). `scripts/` tidak disentuh.

**Tech Stack:** Go 1.25, `net/http` stdlib (transport default, terbukti lolos ingress), `golang.org/x/net/http2` + `refraction-networking/utls` hanya di balik build tag `utls` sebagai fallback, `SherClockHolmes/webpush-go` untuk push, `embed` untuk frontend.

**Spec:**
- `docs/poc-bima-go.md` (hasil uji TLS, vault, parsing)
- `server.py` (kontrak API + registry, 469 baris)
- `scripts/bima_balance.py`, `scripts/myxl_balance.py`, `scripts/axisnet_balance.py`, `scripts/myim3_balance.py`, `scripts/push_notify.py` (sumber kebenaran perilaku)
- `docs/bima-protocol.md`

## Global Constraints

- **RAM:** ≤ 30 MB pada beban nyata (5 nomor, polling 60 s, ≤ 20 klien SSE). Ini gate Task 11, bukan aspirasi.
- **OTP:** tidak pernah dikirim tanpa perintah eksplisit; live OTP hanya ke `6287777313358`. `send_otp` hanya dipanggil dari endpoint `resend`/alur verify.
- **`scripts/` tidak diubah.** Porting hanya untuk backend container.
- **Frontend tidak berubah:** file statis disalin apa adanya, `embed` atau bind-mount; tampilan/output shape identik.
- **Provider-blind:** JSON `/providers` dan `/numbers` harus punya shape yang sama untuk keempat provider.
- **Vault/registry kompatibel Python:** `vault.key` 32 byte, `vault.bin` = iv(12)+ct+tag(16), mode 0600, penulisan atomik; `registry.json` = `{"numbers":[{"id","provider","msisdn","added_at"}]}`.
- **Tanpa secret di repo:** dump runtime hanya di `/tmp`; token/JWT/HASH tidak masuk log (pakai redaksi seperti `scripts/bima_balance.py:redact`).
- **Rollback:** image Python lama tetap tersedia (`docker tag`), `compose` hanya menukar image/port.

## Review Focus

1. **Kesetaraan output lintas provider** — satu provider mengembalikan field yang provider lain tidak punya; test golden per provider wajib membandingkan shape, bukan nilai.
2. **Grace/eskalasi** — `grace_until` beda dari `active_until` → peringatan 20 hari sebelum `grace_until`; pin test kasus `cf989ce`.
3. **Format kuota `N MB` seragam** — axisnet `remaining_text`, myxl byte, imi KB; test membandingkan string akhir, bukan angka mentah.
4. **Tulis atomik & integritas vault** — proses mati di tengah tulis tidak boleh merusak `vault.bin`.
5. **Tanpa kebocoran rahasia** — log error memuat body respons? Jangan. Test: payload token tidak muncul di output stderr.

---

## File Structure

```
go/
├── go.mod                       # module digitral/backend
├── Dockerfile                   # multi-stage: golang:1.25-alpine -> scratch
├── cmd/digitrald/main.go        # wiring: registry, clients, api, push, scheduler
├── internal/cryptoutil/         # sha512hex, get_values, rc4, aes-gcm, jwt_exp
├── internal/vault/              # Vault: read/write vault.bin, Session/RawSession
├── internal/provider/
│   ├── provider.go              # interface Client + Balance/Quota types
│   ├── imi/                     # bima+ dan myim3 (bedanya hanya konstanta)
│   ├── axisnet/                 # header ax-*, otplogin, dashboard
│   └── myxl/                    # CIAM basic auth, signature, refresh grant
├── internal/registry/           # registry.json atomik, pending map, snapshot
├── internal/httpapi/            # handler: routes, CORS, SSE, static, /health
├── internal/push/               # VAPID, subscribe store, webpush
└── internal/notify/             # daily loop + _grace_urgent

poc/bima/                        # spike, dipindah ke internal/ pada Task 1
docs/port-go-plan.md             # rencana ini
```

Setiap package punya `*_test.go` di sebelahnya. Data uji: `internal/*/testdata/*.json` (tangkapan Python, bukan data live).

---

## Task 1: Fondasi — module, cryptoutil, vault, pindah spike

**Files:**
- Create: `go/go.mod`, `go/internal/cryptoutil/{cryptoutil.go,cryptoutil_test.go}`
- Create: `go/internal/vault/{vault.go,vault_test.go}`
- Move: `poc/bima/{vault.go,parse.go,vault_test.go}` → `go/internal/...` (poc dipertahankan sebagai CLI tipis atau dihapus pada Task 13)

**Interfaces:**
- Consumes: `scripts/bima_balance.py` (`sha512_hex`, `get_values`, `aes_gcm_encrypt`, `_jwt_exp`, `Vault`).
- Produces: `cryptoutil.SHA512Hex(s string) string`, `cryptoutil.GetValues(s string) string`, `cryptoutil.AESGCMEncrypt(key, iv, plain []byte) []byte`, `cryptoutil.JWTExp(jwt string) int64`, `vault.Open(dir string) (*Vault, error)`, `(*Vault).Session(msisdn string) map[string]any`, `(*Vault).RawSession(msisdn string) map[string]any`, `(*Vault).Save() error`.

- [ ] **Step 1: Tulis vector golden dari Python.** Jalankan `python3 - <<'PY'` yang mencetak `testdata/crypto_vectors.json`: 8 kasus `get_values` (termasuk `""`, panjang ganjil, string ms-epoch), 8 `sha512_hex`, 4 `aes_gcm_encrypt` (key/iv statis `A9$gT...`/`Rx4Cm9Pn2Vf8Kt6B`, plaintext JSON), 4 `jwt_exp` (termasuk JWT tanpa `exp` → 0). Simpan output, bukan hardcode.
- [ ] **Step 2: Tulis `cryptoutil_test.go` yang membandingkan semua vector.** Test `TestCryptoVectors` gagal karena `cryptoutil` belum ada.
- [ ] **Step 3: Implement `internal/cryptoutil/cryptoutil.go`.** RC4: `crypto/rc4` masih ada di Go 1.25; jika terhapus, RC4 40 baris sendiri (documentasikan pilihan di komentar).
- [ ] **Step 4: Jalankan `go test ./internal/cryptoutil/ -run TestCryptoVectors -v` → PASS.**
- [ ] **Step 5: Port `poc/bima/vault.go` ke `go/internal/vault/vault.go`, tambah `RawSession`, `SessionMSISDNs`, `num`.** Jangan ubah format: IV 12 byte, `aead.Seal(iv, iv, ...)`, `Save` atomik + `chmod 0600`.
- [ ] **Step 6: Salin `poc/bima/vault_test.go`, sesuaikan nama package, tambah `TestVaultPythonLayout`: buka salinan vault nyata (dibuat `t.TempDir` dari `testdata/vault_py/`), bandingkan `deviceId`, key sesi, dan `Session`/`RawSession`.**
- [ ] **Step 7: Commit** `feat(go): vault + crypto kompatibel Python (vector golden)`.

**Acceptance:** `go test ./internal/...` hijau; `vault_test` membaca vault buatan Python; `vault.bin` yang ditulis Go bisa didekripsi `python3 -c` dengan key sama (langkah manual Task 0, ulangi di sini sebagai test `exec` opsional).

---

## Task 2: Interface provider + parser bersama

**Files:**
- Create: `go/internal/provider/provider.go`, `go/internal/provider/parse.go`, `go/internal/provider/parse_test.go`
- Create: `go/internal/provider/testdata/dashboards/{bima,myim3,axisnet,myxl}.json` (tangkapan Python, bukan live)

**Interfaces:**
- Consumes: `Balance` dari `poc/bima/parse.go`, `parse_dashboard` di `scripts/bima_balance.py:744`.
- Produces: `type Client interface { Provider() string; Balance(ctx, msisdn string) (Balance, error) }`, `type Balance struct { Balance any; Quota any; Unit string; Used any; Unlimited bool; ActiveUntil, GraceUntil any; PackageQuotas []PackageQuota; Bonstri any }`, `func ParseDashboard(provider string, raw []byte) (Balance, error)`, `func FmtQuota(Balance) string`.

- [ ] **Step 1: Tangkap dashboard mentah.** Untuk tiap provider, jalankan Python (`bima_balance`/`myim3`/`axisnet`/`myxl`) **pakai sesi tersimpan** agar tanpa OTP; simpan body mentah ke `testdata/dashboards/*.json`; simpan juga output `parse_dashboard` + `fmt_quota` sebagai `testdata/dashboards/*.expected.json`. Jika sesi myxl/axisnet kedaluwarsa: jangan kirim OTP; cukup tangkap dari dump cache yang ada (`~/.bima/dashboard_*.json`) dan catat kekurangannya.
- [ ] **Step 2: Tulis `parse_test.go`: `TestParseParity` membandingkan `ParseDashboard` vs `.expected.json` field-per-field (bukan string JSON).**
- [ ] **Step 3: Implement `ParseDashboard`.** Envelope: JSON `{status,code,data:{...}}` → turun ke `data`; XML `<Response><data>…` → unescape entitas; gzip dibaca di HTTP layer. Hanya kuota `quotaUnit == "MB"`; lewati paket `EndDate < today`; `used < 0` → `null`.
- [ ] **Step 4: Implement `FmtQuota`** (KB→`N MB`/`N GB`, unlimited → `UNLIMITED`).
- [ ] **Step 5: Commit** `feat(go): provider interface + parser dashboard`.

**Acceptance:** 4 fixture provider lolos; `go test ./internal/provider/` hijau.

---

## Task 3: Client IMI (bima+ dan myim3)

**Files:**
- Create: `go/internal/provider/imi/{imi.go,headers.go,imi_test.go}`
- Create: `go/internal/provider/imi/testdata/{headers_bima.json,headers_myim3.json}`

**Interfaces:**
- Consumes: `cryptoutil`, `vault.Vault`.
- Produces: `type Client struct{...}`, `New(base, brand, serviceKey, channel, appVersion string, v *vault.Vault, device *vault.Device, transport http.RoundTripper)`, `(c *Client) GuestToken(ctx) (string, error)`, `(c *Client) SendOTP(ctx, msisdn, action string) (string, error)`, `(c *Client) ValidateOTP(ctx, transid, otp string) (string, error)`, `(c *Client) Dashboard(ctx) (map[string]any, error)`, `(c *Client) Balance(ctx, msisdn string) (provider.Balance, error)`.

- [ ] **Step 1: Tulis `TestHeadersParity`:** bangun header untuk path `/token/guest/v1`, body `{}`, `req_time` tetap, token kosong → bandingkan dengan `testdata/headers_bima.json` yang dihasilkan Python (`--verbose` / `build_headers`). Ulangi dengan `token` JWT pendek → `testdata/headers_myim3.json` berisi `serviceKey=i4WxFMMLvWqnrvuAyg58`, `channel=V2MYIM3`, `appVersion=82.17.0`.
- [ ] **Step 2: Implement `headers.go`:** salin `baseline()` + `build_headers()`; `User-Agent` kosong; `X-IMI-UID` = ms epoch; `X-IMI-TOKENID` = token atau ms epoch; `X-IMI-HASH`/`x-imi-oauth` via `cryptoutil`; `X-ESIM-SUPPORT=false` untuk `/token/guest/` dan `/token/app/`.
- [ ] **Step 3: Implement `imi.go`:** transport stdlib default; `Connection: close` di h1; dekompres gzip manual; simpan sesi lewat `vault` (`exp = JWTExp`).
- [ ] **Step 4: `TestGuestTokenLive` (di-skip kecuali `POC_LIVE=1`):** POST `/token/guest/v1` → `code == 26100`, token ≥ 100 char. Ini bukan test default; dipakai saat verifikasi manual.
- [ ] **Step 5: Commit** `feat(go): client imi (bima+, myIM3)`.

**Acceptance:** header vector identik byte-per-byte; sesi tersimpan terbaca; OTP **tidak** dikirim oleh test.

---

## Task 4: Client axisnet

**Files:** `go/internal/provider/axisnet/{axisnet.go,headers.go,axisnet_test.go}`; `testdata/axisnet_headers.json`.

- [ ] **Step 1: Baca `scripts/axisnet_balance.py` (otplogin, dashboard, `remaining_text`, `(UNLIMITED)`).** Tulis test header/signature vector dari Python.
- [ ] **Step 2: Implement header `ax-*` + `otplogin/v2` + dashboard + parse ke `provider.Balance`.** Sesi: `{provider, token, issued, scheme, device}` — **tanpa `exp`**, jangan pakai aturan `exp`.
- [ ] **Step 3: Test `TestAxisnetUnlimitedSuffix`: input `remaining_text="500 MB", unlimited=true` → output `"500 MB (UNLIMITED)"`.
- [ ] **Step 4: Commit** `feat(go): client axisnet`.

---

## Task 5: Client myXL (CIAM)

**Files:** `go/internal/provider/myxl/{myxl.go,ciam.go,myxl_test.go}`; `testdata/myxl_token.json` (tangkapan body token, disensor).

- [ ] **Step 1: Port alur CIAM:** `auth/otp` (GET, `contactType` SMS/EMAIL), `protocol/openid-connect/token` (form, `grant_type=password`), `refresh_token` grant. Signature `ax-api-signature` + `ax-request-at` dari Python; jangan hardcode secret, baca dari `cfg` yang sama dengan `PROVIDER_MYXL`.
- [ ] **Step 2: Sesi:** tulis `{token, id_token, access_token, refresh_token, exp, token_exp}`; baca pakai `RawSession` saat `Session()` kosong → coba refresh dulu. Ini perbaikan yang ditemukan Task 0.
- [ ] **Step 3: Test `TestMyXLRefreshPath`: vault dengan `exp` lewat tapi `refresh_token` ada → `Balance` tidak mengembalikan error OTP; gunakan `httptest.Server` yang meniru CIAM (jangan panggil network).**
- [ ] **Step 4: Commit** `feat(go): client myXL + refresh grant`.

---

## Task 6: Registry + API parity

**Files:** `go/internal/registry/{registry.go,registry_test.go}`; `go/internal/httpapi/{api.go,static.go,api_test.go}`.

- [ ] **Step 1: Test registry:** baca `testdata/registry.json` (salinan `~/.bima-frontend/registry.json`), tulis ulang, bandingkan JSON semantik; tambah/hapus nomor; `id` = `<provider>-<msisdn>`. Tulis atomik (tmp + rename).
- [ ] **Step 2: Implement handler** untuk route yang ada di `server.py:97-455`: `/health`, `/providers`, `/numbers` (GET/POST), `/numbers/{id}/resend`, `/numbers/{id}/verify`, `/numbers/{id}/balance`, `/numbers/{id}` DELETE, `/stream`, `/push/public-key`, `/push/subscribe`, `/`, `/manifest.json`, `/sw.js`, `/assets/*`. Status code + shape body harus sama.
- [ ] **Step 3: Test kontrak:** `httptest` + fixture tangkapan Python (`testdata/api_*.json`) untuk `/providers` dan `/numbers`; bandingkan JSON, bukan string.
- [ ] **Step 4: SSE:** `/stream` mengirim `data: {numbers, ts}\n\n` tiap `POLL_SEC`; test: baca 2 event, tutup klien → goroutine berhenti (deteksi leak).
- [ ] **Step 5: Commit** `feat(go): registry + API setara FastAPI`.

---

## Task 7: Push (VAPID + webpush)

**Files:** `go/internal/push/{vapid.go,store.go,webpush.go,push_test.go}`.

- [ ] **Step 1: Port `ensure_vapid`/`add_sub`/`send` dari `scripts/push_notify.py`:** `vapid.json` kompatibel (baca/tulis kunci yang sama), store subscription per `FRONTEND_HOME`.
- [ ] **Step 2: Test `TestVAPIDKeyReuse`: `ensure_vapid` dua kali → kunci identik; `TestSubscriptionStore`: tambah duplikat → `new=false`, total tidak naik.
- [ ] **Step 3: Kirim:** `webpush-go` dengan subject dari `VAPID_MAILTO`. Test pengiriman nyata **hanya** saat verifikasi manual (`POC_LIVE=1`), bukan test default.
- [ ] **Step 4: Commit** `feat(go): web push VAPID`.

---

## Task 8: Notifikasi + scheduler

**Files:** `go/internal/notify/{notify.go,grace_test.go}`.

- [ ] **Step 1: Port `_grace_urgent` + `daily_loop`** (`scripts/push_notify.py:141,218`), `GRACE_WARN_DAYS = 20`; jangan ubah `timedelta` fix `107caf2`.
- [ ] **Step 2: Test grace (wajib, kasus `cf989ce`):**
  - `grace_until` ada dan **beda** dari `active_until` → urgent bila sisa ≤ 20 hari dari `grace_until`.
  - `grace_until` sama dengan `active_until` → perilaku lama (>20 hari lewat `active_until`).
  - `grace_until` absen → perilaku lama.
- [ ] **Step 3: Commit** `feat(go): grace notification + daily loop`.

---

## Task 9: Docker + cutover

**Files:** `go/Dockerfile`, `docker-compose.yml` (ubah image + tambah `POC_PORT` opsional), `.dockerignore`.

- [ ] **Step 1: Dockerfile multi-stage:** `golang:1.25-alpine` build `-ldflags="-s -w"`, target `scratch` + CA certs; copy `frontend/`, `server.py` **tidak**; env `FRONTEND_HOME=/app/data`, `BIMA_HOME` per provider (sama seperti image Python), `POLL_SEC`, `TZ`, VAPID.
- [ ] **Step 2: Compose:** `image: digitral-backend:latest` (build `./go`), volume `digitral_data`, healthcheck `/health` sama.
- [ ] **Step 3: Cutover berdampingan:** `docker run` image Go di port 8766 dengan **volume salinan** `/tmp/opencode/data-copy`; bandingkan `/providers`, `/numbers`, `/health`; baru tukar service.
- [ ] **Step 4: Commit** `feat(deploy): image Go + cutover`.

---

## Task 10: Gate ukur (RAM ≤ 30 MB)

**Files:** `go/Makefile` target `measure`, `docs/port-go-plan.md` (hasil).

- [ ] **Step 1: Beban:** 5 nomor (4 provider), `POLL_SEC=60`, 20 klien SSE, 60 menit atau 10 menit bila stabil.
- [ ] **Step 2: Ukur:** `docker stats --no-stream digitral-dashboard` (RSS), binary size, `go tool nm`/`-ldflags` report.
- [ ] **Step 3: Keputusan tertulis:** ≤ 30 MB → lanjut; 30–40 MB → optimasi (matikan http2/utls, `GOGC`, matikan debug); > 40 MB → **stop, lapor, pertimbangkan Rust** (keputusan pemegang anggaran).
- [ ] **Step 4: Commit** `docs: hasil gate RAM`.

---

## Task 11: Bersih-bersih

- [ ] Hapus/arsipkan `poc/bima/` (spike sudah pindah), atau jadikan `cmd/poc-bima` bila masih dipakai manual.
- [ ] Perbarui `README`/`docs/poc-bima-go.md`: status port, cara menjalankan `go test ./...`, cara rollback.
- [ ] Commit.

---

## Milestone & Urutan

| Fase | Task | Hasil yang bisa dipakai |
|---|---|---|
| M1 Fondasi | 1–2 | vault + crypto + parser teruji golden |
| M2 Provider | 3–5 | keempat provider membaca balance (myxl bisa butuh refresh) |
| M3 Service | 6–8 | API + push + notif berjalan, rollback masih mungkin |
| M4 Rilis | 9–10 | image Go + gate RAM |
| M5 Bersih | 11 | repo rapi |

**Gate wajib sebelum lanjut:** Task 1 dan Task 2 hijau (mereka adalah fondasi yang dipakai semua). Task 10 adalah gate uang/anggaran, bukan kosmetik.

**Risiko tertinggi:** (a) `/otp/send` menolak Go stdlib — baru terbukti saat OTP live diminta; mitigasi: build tag `utls` + vector `poc/bima` sudah siap; (b) myXL/axisnet memerlukan jalur akses yang belum terlihat di vault scan — Task 4/5 mengungkapnya dengan `httptest`, bukan network; (c) RAM gate gagal — Task 10 mengukur dini (bisa dijalankan setelah M3, jangan tunggu M5).

**Estimasi kasar:** M1 4–6 jam, M2 8–12 jam, M3 6–10 jam, M4 3–5 jam, M5 1 jam.

## Keputusan yang Sudah Terkunci (dari pengujian)

- Transport default stdlib; utls opsional (TLS 10001 tidak terjadi di Go; `docs/poc-bima-go.md`).
- `Session()` = aturan Python; `RawSession()` wajib untuk myXL.
- `deviceFromVault` default generik (paritas Python), profil nyata opsional.
- Envelope JSON `{...,"data":{...}}` dan XML `<data>` dinormalisasi di parser.
- `POC_VAULT_SCAN` (exit 0) membuktikan vault generik; test sintetis per bentuk sesi ada di `vault_test.go`.

## Self-Review

1. **Spec coverage:** semua route `server.py` → Task 6; vault → 1; crypto → 1; 4 provider → 3–5; push → 7; grace → 8; RAM → 10; rollback → 9. Tidak ada yang hilang.
2. **Step scan:** setiap step punya perintah/verdict; tidak ada "TBD". Algoritma yang ditentukan (AES-GCM, hash) hanya disebut sebagai referensi Python, bukan ditranskrip.
3. **Type consistency:** `provider.Balance`/`PackageQuota`/`Client` didefinisikan di Task 2 dan dipakai 3–6; `vault.Session/RawSession` di Task 1 dipakai 3–5.
4. **Review focus:** 5 item punya test (Task 2, 3, 4, 1/6, 6).
5. **Proportion:** rencana ~2× spec, bukan transkrip; body kode hanya pada tanda tangan dan nilai yang dikunci.
