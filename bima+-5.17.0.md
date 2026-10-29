# bima+ 5.17.0 — Analisis Statis (REA + JADX + penambangan DEX)

Tanggal analisis: 2026-10-19 (Jalu, atas penugasan Amad — tahap yang sama dengan myIM3, tanpa uji langsung)

## Identitas

- Aplikasi: bima+ (Tri / Indosat Ooredoo Hutchison)
- Package: `com.linkit.bimatri`
- Versi: 5.17.0 (versionCode 560003792), minSdk 23, targetSdk 36
- File sumber: STB `/media/devmon/redext4/target/bima+-5.17.0.apk+` (56.965.152 byte)
- SHA256 (identik di STB dan salinan lokal): `bf5c2587082bd6ce7b00212680aa012c34536cc4f5e5bd4eec5061977cf407fb`
- Isi bundle .apk+: base.apk (105.722.274 byte) + split_config.arm64_v8a + split_config.xxhdpi
- base.apk: aplikasi **native Kotlin/Java** (bukan Flutter/RN): 4.785 entri, 12 dex + assets/audience_network.dex, tanpa native lib di base
- REA inspect package: 44.934 class, 43 permission (bukti: `output/bima-inspect-package.json`)

## Temuan utama: platform yang SAMA dengan myIM3

bima+ dibangun di atas platform **Digitral** yang sama persis dengan myIM3 (kode `com.digitral.*`; package aplikasi `com.linkit.bimatri` — kelas-kelas LinkIt yang juga ditemukan di dalam myIM3). versionCode-nya pun berurutan (myIM3 560003790, bima+ 560003792). Seluruh skema di bawah ini terverifikasi satu per satu untuk build ini, bukan sekadar diasumsikan sama:

- Lapisan jaringan: `com.digitral.network.ApiService` (pembungkus OkHttp sendiri, pola nama-request → URL via AppSettings) — penanda string penandatanganan lengkap ditemukan di DEX: `REQBODY=`, `&SALT=`, `x-imi-oauth`, `X-IMI-HASH`, `X-IMI-TOKENID`, `X-IMI-CHILD-LINENO`.
- Skema tanda tangan identik dengan myIM3:
  - `x-imi-oauth` = SHA512(`"REQBODY=" + body + "&SALT=" + getValues(token)`)
  - `X-IMI-HASH` = SHA512(`who + "$" + AppOS + "$" + versi + "$" + token + "&SALT=" + getValues(reqTime)`)
  - getValues = karakter posisi ganjil (1-based); encryption = SHA-512 hex.

## Yang BERBEDA dari myIM3 (nilai spesifik build ini)

- **Base API: `http://bimaplus.ioh.co.id/api/v2`** (varian https: `https://bimaplus-api.ioh.co.id/api/v2`)
- Header statis `Authorization`: `642d1cc69d90666962726e` — **sama** dengan myIM3 (konstanta platform, terbukti dari decompile `ProjectHeaders.getHeaders`, bukti `output/bima-projectheaders.json`)
- `X-IMI-SERVICEKEY`: **`FPi7ZP3Jy8Uv3KBd4QeG`** (BuildConfig `com.digitral.common`, FLAVOR `bimaproduction`)
- `X-IMI-CHANNEL`: **`BIMA`**
- `X-IMI-VERSION` / `VERSIONCODE`: `5.17.0` / `560003792`

## Login OTP (terverifikasi dari decompile)

- Kirim OTP → `/otp/send/v1`, body JSON polos (dibangun `LoginViewModel.requestSendOTP`, bukti `output/bima-vm-sendotp.json`):
  `{"msisdn":"<nomor format 62…>","action":"<action>"}` — msisdn via `Utils.getMSISDNCountryAppended`; respons berisi `transId`.
  Nilai `action` mengikuti pola platform yang sama (string tujuan alur; untuk login kandidatnya `"login"` — inferensi, sama seperti myIM3).
- Validasi OTP → `/otp/validate/v2`, body terenkripsi AES-256-GCM — **perbedaan lapisan**: di bima+ enkripsi dilakukan di `LoginRepository.requestValidateOTP` (bukti `output/bima-repo-validateotp.json`):
  `new ODPAESEncryption(BuildConfig.PINPUK_KEY).encryptNew("{\"transid\":\"…\",\"otp\":\"…\"}")`
  - Kunci `PINPUK_KEY` dari `com.linkit.bimatri.BuildConfig` = **`A9$gT#2vXp!qW7LmZ@e4Ky8Nb3Hj5Uq9`** — sama persis dengan kunci hardcoded myIM3.
  - IV statis `ODPAESEncryption.AES_IV` = `Rx4Cm9Pn2Vf8Kt6B` — juga sama. Keluaran Base64 NO_WRAP (ciphertext+tag).
- Token sesi hasil validasi dikirim sebagai header `X-IMI-TOKENID`. Alur token tamu/app juga ada: `/token/guest/v1`, `/token/app/v1`, `/token/app/stk`.

## Cek kuota

Endpoint yang sama dengan myIM3 (dari tabel string DEX, 292 path bersih):
- **`/dashboard/get/v7`** — data dashboard utama
- **`/dashboard/activepack/v2`** — paket aktif / sisa kuota
- `/user/quota`, `/dashboard/details`, `/dashboard/widget`, `/customerprofile/get`

Tidak ada parameter MSISDN pada request normal — server mengenali pemilik dari token sesi.

## Multi-nomor

Mekanisme sama seperti myIM3 (by design platform): nomor tambahan ("lines") ditautkan ke akun utama lewat `/primary/lines`, `/primary/addline`, `/primary/removeline`, `/profile/switch`; untuk nomor child, request yang sama ditambah header `X-IMI-CHILD-LINENO` (MSISDN child terenkripsi ODPRC4). Berlaku juga kesimpulan yang sama: dua nomor parent independen tetap butuh dua sesi terpisah.

## PoC

`~/workspace/rea-lab/poc/bima_client.py` — dry-run terverifikasi: merakit header lengkap + kedua tanda tangan + body kirim/validasi OTP (termasuk enkripsi AES-GCM; ciphertext dummy identik dengan keluaran myIM3 untuk masukan yang sama, sesuai dugaan karena kunci/IV sama). Tidak mengirim apa pun.

## Lanjutan uji langsung (2026-10-19, atas penugasan Amad)

**Jalur API aplikasi (Digitral): DITOLAK server di langkah kirim OTP.**
- Rantai token berhasil penuh setelah satu koreksi penting: `/token/guest/v1` hanya mau jalan bila header `X-DEVICEID` disertakan (tanpa itu error 10019); JWT tamu yang terbit terikat perangkat (klaim `mob` = device id). Kesimpulan lama "X-DEVICEID membuat /token/* hang" ternyata spesifik host myim3-he1, bukan umum.
- Kirim OTP ke nomor Tri Amad: 3 percobaan (rantai token aplikasi; rantai token tamu terikat perangkat + X-DEVICEID; rantai penuh diulang dengan sidik jari TLS Chrome-Android via curl_cffi) — semuanya dijawab `10011 "Sorry we can not complete your verification process now"`, TIDAK ada SMS terkirim. Pola sama persis dengan myIM3 (4 percobaan di sana, termasuk dari IP rumah via STB): bukan cooldown per-nomor (nomor Tri perawan saat percobaan pertama), bukan IP, bukan action ("" sesuai bytecode), bukan sidik jari TLS Chrome. Tersangka tersisa hanya bisa dilihat dari tangkapan request aplikasi asli (emulator) — ditunda atas keputusan Amad 2026-10-19.
- **TUNTAS 2026-10-29:** tangkapan asli berhasil via Frida hook OkHttp di MuMu 12 (bukan jalur mitm). Akar `10011` = header yang hilang (`X-FAIID`, `X-IMI-ADID`+`ISLIMITED`), casing `X-IMI-NETWORK: WiFi`, `X-IMI-LANGUAGE: EN`, `User-Agent` kosong, dan body `action:"register"`. Alur penuh guest→OTP→validate→dashboard kini hijau murni dari Python (nomor Amad, pulsa Rp 9.400 pasti sesuai app). Detail: `docs/bima-protocol.md` §13.

**Jalur WEB selfcare (dari catatan Amad sendiri di STB): LOGIN BERHASIL, DATA BASI.**
- API web lama `https://www.bima.tri.co.id/apibima`: `POST /login/otp-request {"imei":"WebSelfcare","msisdn":...}` → SMS OTP; `POST /login/login-with-otp {imei, msisdn, otp}` → `secretKey` + `accessToken`; lalu `/homescreen/profile` dan `/profile/account`. Tanpa tanda tangan/atestasi. Wajib dari IP rumah (dari VM koneksi diputus, HTTP 000).
- TAPI data backend web menyimpang dari aplikasi untuk nomor yang sama: web melaporkan pulsa Rp 5.000, kuota 0 MB, masa aktif s.d. 12/05/2027; aplikasi bima+ menunjukkan pulsa Rp 9.400, kuota 1,2 GB, s.d. 1 Sep 2027. Backend web = bayangan lama, TIDAK layak jadi sumber kebenaran. (Endpoint `/mobile` di layanan honeygain-api STB Amad memakai API ini dan saat ini menjawab "Developer Inactive" untuk kredensial tersimpannya; pembaruan compose dibatalkan karena data tidak akurat.)

## Bukti (~/workspace/rea-lab/output/)

- `bima-inspect-package.json` — inspeksi paket REA
- `bima-projectheaders.json` — decompile ProjectHeaders.getHeaders
- `bima-vm-sendotp.json`, `bima-vm-validateotp.json`, `bima-vm-init.json` — decompile LoginViewModel
- `bima-repo-validateotp.json` — decompile LoginRepository.requestValidateOTP
- `bima-class-vm.json` — daftar method LoginViewModel
- Nilai statis BuildConfig dibaca dengan parser DEX lokal (kedua BuildConfig: com.digitral.common & com.linkit.bimatri)
