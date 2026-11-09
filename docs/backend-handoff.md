# Handoff — cara pakai 3 backend (bima+, myIM3, axisnet)

Dokumen ini untuk siapa pun (orang atau agen) yang akan memakai/menambah provider.
Ketiganya ada di `scripts/`, tetapi **axisnet bukan keluarga Digitral** — cara
pakainya beda; jangan asumsikan satu pola untuk semua.

## 1. Ringkasan cepat

| | bima+ (Tri) | myIM3 (Indosat) | axisnet (AXIS) |
|---|---|---|---|
| Keluarga protokol | Digitral (`com.linkit.bimatri`) | Digitral (sama, konstanta beda) | Retrofit + kunci HMAC dari native lib (JNI) |
| Entry point | `scripts/bima_balance.py` | `scripts/myim3_balance.py` (wrapper tipis) | `scripts/axisnet_balance.py` |
| Vault (default) | `~/.bima` | `~/.myim3` | `~/.axisnet` |
| Override vault | `BIMA_HOME` | `BIMA_HOME` | `BIMA_HOME` |
| Base host | `bimaplus-api.ioh.co.id/api/v2` | `myim3api1.ioh.co.id/api/v2` | `otp.api.axis.co.id` + `balance.api.axis.co.id` |
| Identitas app | header `X-IMI-*` + `X-FAIID`/`X-IMI-ADID` | idem, `X-IMI-CHANNEL=V2MYIM3` | `X-API-KEY` + `x-signature` (HMAC) |
| OTP | `/otp/send/v1` → `/otp/validate/v2` | idem | `/v1/otp` → `/otplogin/v2` |
| Body OTP | plaintext JSON | plaintext JSON | **terenkripsi DESede** (`encryptSSO`) |
| Balasan | XML `<data>{JSON}</data>` | idem | JSON `{data:"<b64url>"}` terenkripsi |
| Crypto | AES-256-GCM (PINPUK, IV statis) | idem | DESede/ECB + `md5("xl"+md5(0-form)+"axis")` |
| Transport | TLS biasa | TLS biasa | **Cloudflare**: stack python kena 403 di otplogin/getbalance → fallback `curl` |
| Selftest offline | `--selftest` | `--selftest` | `--selftest` |

## 2. bima+ — `scripts/bima_balance.py`

Alur: `POST /token/guest/v1` (JWT guest) → `POST /otp/send/v1`
`{"msisdn":"62...","action":"register"}` → OTP SMS → `POST /otp/validate/v2`
(body `AES-256-GCM`, Base64) → token → `POST /dashboard/get/v7` (envelope XML).

```bash
python3 scripts/bima_balance.py 081234567890 --action register   # OTP diminta interaktif
python3 scripts/bima_balance.py 081234567890                     # pakai sesi vault
python3 scripts/bima_balance.py 081234567890 --fresh              # paksa login ulang
python3 scripts/bima_balance.py 081234567890 --watch 60           # polling tiap 60 s
python3 scripts/bima_balance.py 081234567890 --json               # output mesin
python3 scripts/bima_balance.py --selftest                        # offline, tanpa nomor
```

Penting:
- `--action register` wajib untuk OTP pertama (risk gate server mengharapkannya).
- Identitas perangkat: `--deviceid --devicename --oem --model --osver`. Flag yang
  diberikan **disimpan** ke profil di `vault.bin` dan dipakai run berikutnya.
- OTP di prompt = `input()` interaktif; jangan dipanggil dari skrip non-interaktif.
- Vault menyimpan juga `dashboard_{msisdn}.json` (debug, **data pribadi**, jangan di-commit).

## 3. myIM3 — `scripts/myim3_balance.py`

Protokol/CLI/perilaku **identik bima+**; hanya konstanta yang diganti
(`SERVICEKEY`, `CHANNEL=V2MYIM3`, `APP_VERSION=82.17.0`, host). Jadi semua contoh
perintah bima+ berlaku apa adanya, ditambah:

```bash
python3 scripts/myim3_balance.py 085612345678 --action register
```

Kalau menambah provider Digitral lain (myXL), pola ini yang dipakai: entry tipis
yang set `bm.PROVIDER_CFG = ...` lalu panggil `bm.main()`. Slot `PROVIDER_MYXL`
di `bima_balance.py` masih `None` (belum diverifikasi) — **jangan** disamakan
dengan axisnet.

## 4. axisnet — `scripts/axisnet_balance.py` (beda total)

Alur: `POST /v1/otp` (`{msisdn: encryptSSO, device_type:"android"}`) → OTP SMS →
`POST /otplogin/v2` (body `rc.c`: `msisdn, otp_code, fcm_token, fb_id, model,
brand, device_os, device_type, imei`) → token → `GET /getbalance`
(`Authorization: <token>` **mentah, tanpa `Bearer`**). Semua balasan `data`
didekripsi DESede/ECB (kunci `sso_key` NUL-pad 24).

```bash
python3 scripts/axisnet_balance.py --selftest                                  # offline
python3 scripts/axisnet_balance.py 083185770311 --dry-run                      # tampilkan request OTP
python3 scripts/axisnet_balance.py 083185770311 --send-otp                     # KIRIM OTP nyata (nomor sendiri)
python3 scripts/axisnet_balance.py 083185770311 --otp-code 123456 --balance    # login + balance + simpan vault
python3 scripts/axisnet_balance.py 083185770311 --json                         # balance dari sesi vault
python3 scripts/axisnet_balance.py --token <tok>                               # token manual
python3 scripts/axisnet_balance.py --decrypt <b64url>                          # dekripsi offline
```

Penting (perbedaan yang sering bikin bingung):
- **Tidak ada prompt interaktif.** Kode OTP diberikan lewat `--otp-code`, jadi
  alur bisa skrip. (Keputusan sadar: hindari OTP nyata terkirim dari skrip otomatis.)
- Nama field body = nama getter `rc.c`; key JSON `/v1/otp` harus `device_type`
  (bukan `deviceType`) — kalau salah → `401 Invalid signature`.
- `device_type` **huruf kecil** `"android"`; `"Android"` → 401.
- msisdn dinormalkan ke bentuk `62...` sebelum `encryptSSO`; `fb_id` pakai bentuk
  `0...` (`msisdnIDFormat`).
- Balasan **bukan** plain JSON: `data` = Base64url terenkripsi; gunakan `--decrypt`.
- Cloudflare: `otplogin/v2` dan `getbalance` menantang stack python (403
  "Just a moment"); skrip otomatis mengulang lewat CLI `curl`. Kalau menjalankan
  HTTP sendiri, siapkan fallback serupa.
- Kode OTP cepat kedaluwarsa (percobaan dengan kode basi → `404 "Otp not found or
  expired"`; fakta server menjawab 404/200 sudah membuktikan header+signature valid).

## 5. Backend HTTP (frontend) — `server.py`

- Yang sudah tersambung: **bima** dan **myim3** (`PROVIDERS`). Endpoint:
  `GET /providers`, `POST /numbers {provider,msisdn}`, `POST /numbers/{id}/verify {otp}`,
  `GET /numbers/{id}/balance`, `POST /numbers/{id}/resend`, `DELETE /numbers/{id}`,
  `/events` (SSE), `/health`.
- **axisnet belum dilayani server.** Slot `PROVIDER_AXISNET` sengaja tidak dipakai
  di `PROVIDERS` karena:
  1. Alurnya bukan alur OTP generik `BimaClient` (verify OTP + kripto berbeda);
     butuh adapter: `/numbers` harus mengirim `/v1/otp`, `verify` memanggil
     `/otplogin/v2` + menyimpan token, `balance` memanggil `/getbalance` + dekripsi.
  2. Perlu penyimpanan token (UUID) di vault, bukan JWT sesi Digitral.
  3. Perlu transport fallback `curl` (Cloudflare).
- Vault per provider di container: `/app/data/{provider}` (§frontend-architecture).
  Untuk axisnet nanti: `/app/data/axisnet` + `BIMA_HOME` saat membuat `bm.Vault`.
- Batasan tetap: **jangan uji OTP nyata** kecuali nomor milik user dan user minta.

## 6. Cheat-sheet gotcha

| Gejala | Penyebab | Aksi |
|---|---|---|
| `401 Invalid signature` (axisnet) | `device_type` salah huruf besar / sig salah bentuk | pakai `"android"`, msisdn `62...` |
| `404 Otp not found or expired` (axisnet) | kode OTP kedaluwarsa | `--send-otp` lagi, kode baru |
| `403` "Just a moment" (axisnet) | Cloudflare menantang TLS python | biarkan fallback `curl` skrip menangani |
| Prompt menggantung (bima/myIM3) | OTP `input()` menunggu | jalankan interaktif, atau pakai mode sesi vault |
| Balasan `data` tampak acak | field `data` terenkripsi | `--decrypt` (axisnet) / endpoint sudah men-parse (Digitral) |
| Token hilang setelah restart | vault beda `BIMA_HOME` | pastikan `BIMA_HOME` sesuai provider |

## 7. Verifikasi offline (tanpa jaringan)

```bash
python3 scripts/bima_balance.py --selftest
python3 scripts/myim3_balance.py --selftest
python3 scripts/axisnet_balance.py --selftest
```
