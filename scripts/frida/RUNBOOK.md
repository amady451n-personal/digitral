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
