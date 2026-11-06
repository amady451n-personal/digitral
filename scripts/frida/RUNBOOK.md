# Runbook: sesi runtime axisnet (MuMu 12 + Frida) — pengumpulan terakhir

Status prasyarat statis SUDAH lengkap (docs/bima-protocol.md §15):
endpoint 261 URL + apiKey/keyTempest×5 + salt/secret/algorithmKey + 4 host.
Yang belum ada (masih terkurung di dex V-Key): **nama header** & **urutan/isi
hash signature**. Satu sesi runtime tanpa login/OTP menutup itu.

## Prasyarat (dari §13, sudah pernah jalan untuk bima)
- MuMu Player 12 di Windows, root aktif, adb port tersambung ke host analisis
  (`adb connect 127.0.0.1:7555` sesuai setup §13).
- `pip install frida-tools frida-dexdump` di host; frida-server diemulator
  (langkah sama seperti saat capture bima).

## Langkah A — crypto tap (utama; 10-15 menit)
1. MuMu: pasang APK axisnet versi ini (jangan login jauh-jauh).
2. Host:
   ```
   adb connect <mumu-adb>
   frida -U -f com.axis.net -l scripts/frida/axis_crypto_tap.js
   ```
3. Biarkan app naik sampai layar login DAN diamkan beberapa detik agar
   panggilan publik (banners/config/toggle) terlewat, lalu Ctrl-C.
   Kumpulkan semua baris `[TAP] HMAC/CIPHER/DIGEST/URL`.

Dari log itu ditentukan:
- pasangan endpoint → macam hash/HMAC (alg + key + pesan),
- endpoint mana yang body-nya terenkripsi (CIPHER AES-CBC?),
- salt/secret/algorithmKey mana yang ikut masuk pesan hash.

## Langkah B — dump dex V-Key (penguat; bukti kode persis)
```
frida-dexdump -U -f com.axis.net -o /tmp/axis-dexdump/
```
Hanya salah satu dex hasil dump berisi `com/axis/net/config/` asli
(`Signature`, `RequestInterceptor`) → dekompilasi dengan
`scripts/dex_static_fields.py` + jadx/Ghidra offline. Ini menutup nama
header yang mungkin tidak lewat javax.crypto (mis. di-set via StringBuilder).

## Mitigasi anti-tamper (preseden §13)
- attach-after-spawn; bila proses dibunuh ±30 s setelah attach: relaunch
  tanpa hook untuk fase berikutnya, ulangi hook pada relaunch berikutnya.
- Bila V-Key mendeteksi Frida dan app menutup sendiri: mode gagal aman
  (tidak ada OTP, tidak ada traffic ke server). Opsi lanjut: root device
  fisik sebagai pengganti MuMu.

## Setelah sesi
- Simpan log `[TAP]` + dump dex ke `/tmp/opencode/axis-runtime/` (JANGAN
  commit log berisi data device/user).
- Rekonstruksi header table → tambah §16 bima-protocol.md → baru tulis
  provider axisnet di Python dan uji hanya dengan nomor milik user dan
  atas persetujuan eksplisit.

## Setelah reboot VM MuMu (2026-11-05)
- adbd jadi `shell` (uid 2000) — inj tak bisa attach ke proses apa pun
  ("Unable to access process", bahkan `system_server`). Semua inj via
  `su -c "/data/local/tmp/inj …"` (root ada: `su -c id` = uid 0).
- Sequence attach: `su -c "am force-stop com.axis.net"; su -c "am start -n
  com.axis.net/.ui.splashLogin.SplashActivity"` lalu loop (step 0.2 s,
  max ~40 iterasi): ambil PID, `su -c "inj -p $P -s ppx4b.js --runtime=qjs
  -e"`; hentikan saat tak ada "Unable to access". Tanpa ppx4b app
  restart-loop (pairip). Setelah ppx ter-attach, baru attach script lain
  (tanpa `-e` supaya log console terbaca).
- IP VM kadang berganti: `adb devices` dulu; bila hilang, cek ulang.
- Accessor Kotlin object di inj: `Java.use('…Consta').INSTANCE.value` GAGAL
  (undefined); pakai reflection `Java.use('…').class.getDeclaredField('X')`
  + `setAccessible(true)`. Instance `Signature.a` dibaca via
  `getDeclaredFields()` + `Java.cast` (pola keysrc2.js).

## Rekam header request app (okhttp, aman — 2026-11-05)
- Hook `okhttp3.k$a.b()` (Request.Builder.build). Ini TIDAK memicu V-Key;
  app tetap jalan.
- Nama method runtime (jangan pakai nama jadx): `Request.a()` = method,
  `b()` = HttpUrl, `h()` = Headers, `c()` = RequestBody. `getMethod()` /
  `getUrl()` / `getHeaders()` TIDAK ada di runtime (itu nama metadata Kotlin
  hasil dekonpilasi). `Java.cast` juga tidak berguna di runtime qjs ini.
- Header: `req.h().toString()` sudah berisi semua header level-aplikasi
  (Host/Connection/User-Agent/Content-Length ditambah OkHttp setelah builder,
  jadi tak terlihat). Body: `var g = Java.use('Fl.g').$new();
  req.c().writeTo(g); g.a0()`.
- **JANGAN menekan request**: melempar exception dari hook (abort/no-OTP) atau
  memanggil method via reflection lalu melempar → app `System.exit(0)`
  (anti-tamper V-Key), log hilang. Cukup log, biarkan request lewat.
- Request hanya terjadi saat startup / aksi user; data home ter-cache
  (Room/RN), navigasi biasa tidak memicu HTTP. Attach hook langsung setelah
  ppx (sebelum window startup) untuk menangkap request awal.
- Hasil: request produk/profile app hanya membawa `Authorization` +
  `x-app-version`; header per-endpoint murni dari anotasi `@Header` Retrofit
  (tak ada injeksi global). `/v1/otp` = `Content-Type`, `Connection`,
  `X-API-KEY`, `x-app-version`, `x-signature` (AuthApiService.b: literal
  `"application/json"`, `"keep-alive"`).
- Cek cabang key: `Signature.x()` = false di device ini → `u() = q()`
  (`7L20...`), bukan key staging `t()` (`640cf313...`).
