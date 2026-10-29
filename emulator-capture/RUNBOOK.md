# Runbook: Tangkapan Request Asli bima+ dari Emulator

> **STATUS 2026-10-29: TUNTAS & OPSIONAL.** Tangkapan sudah dilakukan (bukan jalur mitm/CA —
> Frida hook OkHttp di MuMu 12; lihat `docs/bima-protocol.md` §13), akar `10011` ditemukan =
> header (`X-FAIID`/`X-IMI-ADID`/casing/lANGUAGE/UA kosong/action register). Klien Python kini
> mandiri. Runbook disimpan untuk kasus server mengubah provokasi di masa depan.

Tujuan: merekam SATU request kirim OTP asli dari aplikasi bima+ (host
`bimaplus-api.ioh.co.id`, endpoint `/otp/send/v1`) untuk dibandingkan baris demi
baris dengan request hasil rekayasa Jalu yang selalu ditolak server dengan
kode `10011`. Perbandingan itu yang akan menunjukkan potongan yang hilang
(urutan panggilan startup, cookie, header persis, atau sidik jari OkHttp).

Kenapa emulator + CA sistem: config keamanan bima+ hanya percaya CA bawaan
sistem (bukan CA yang dipasang pengguna), jadi sertifikat mitmproxy harus
masuk ke penyimpanan CA sistem — hanya bisa di emulator dengan system image
Google APIs (bukan Google Play) yang mengizinkan `adb root`.

Prasyarat terverifikasi (2026-10-20, lihat env-inventory.txt):
- `bima+5.17.0.apk+` di folder ini = build yang sama persis dengan yang
  dianalisis (SHA256 cocok). Isinya split bundle: base.apk + 2 split.
- Belum ada Android SDK/emulator/adb/mitmproxy di laptop — Langkah 1-2
  memasangnya. Semua unduhan dari sumber resmi Google / mitmproxy.org.

---

## Langkah 1 — Pasang Android SDK + Emulator (Windows)

1. Unduh "Command line tools only" dari https://developer.android.com/studio
   (bagian Command-line tools), ekstrak ke `C:\Android\cmdline-tools\latest\`
   (struktur akhir: `C:\Android\cmdline-tools\latest\bin\sdkmanager.bat`).
   Alternatif: pasang Android Studio, SDK Manager-nya melakukan hal yang sama.
2. Dari PowerShell:
   ```powershell
   cd C:\Android\cmdline-tools\latest\bin
   .\sdkmanager.bat --sdk_root=C:\Android "platform-tools" "emulator" "system-images;android-33;google_apis;x86_64"
   .\sdkmanager.bat --sdk_root=C:\Android --licenses   # terima semua
   .\avdmanager.bat --sdk_root=C:\Android create avd -n bima-cap -k "system-images;android-33;google_apis;x86_64" -d pixel_5
   ```
   Wajib `google_apis` (BUKAN `google_apis_playstore`) — image Play Store
   tidak bisa `adb root`, CA sistem tidak bisa dipasang.
3. Cek akselerasi: `C:\Android\emulator\emulator.exe -accel-check`
   (harus WHPX/Hyper-V aktif; kalau tidak, aktifkan "Windows Hypervisor
   Platform" di Windows Features lalu restart).

## Langkah 2 — Pasang mitmproxy (Windows)

Unduh installer Windows dari https://mitmproxy.org dan pasang, ATAU dari WSL:
`pip3 install --user mitmproxy` (lalu jalankan dengan mode jaringan WSL
mirrored; jalur Windows lebih sederhana untuk proxy emulator, jadi disarankan
installer Windows). Jalankan sekali supaya sertifikat CA dibuat:
`mitmweb` lalu tutup — CA ada di `%USERPROFILE%\.mitmproxy\mitmproxy-ca-cert.cer`.

## Langkah 3 — Pasang CA mitmproxy ke penyimpanan sistem emulator

Jalankan skrip `install-ca.sh` dari folder ini lewat WSL (skrip memanggil
adb.exe/emulator.exe Windows). Ringkasnya skrip melakukan:
1. `emulator -avd bima-cap -writable-system -no-snapshot` (boot pertama)
2. `adb root`, `adb remount`
3. Hitung nama hash sertifikat: `openssl x509 -inform PEM -subject_hash_old`
   → berkas `<hash>.0`
4. `adb push <hash>.0 /system/etc/security/cacerts/`, `chmod 644`, reboot
Verifikasi: di emulator, Settings → Security → Encryption & credentials →
Trusted credentials → tab System harus memuat "mitmproxy".

## Langkah 4 — Pasang bima+ ke emulator

Dari WSL, di folder reamobile:
```bash
mkdir -p /tmp/bima-bundle && cd /tmp/bima-bundle
unzip -o "/home/elixerwsl/yasin/reamobile/bima+5.17.0.apk+"
ADB=/mnt/c/Android/platform-tools/adb.exe
$ADB install-multiple base.apk split_config.arm64_v8a.apk split_config.xxhdpi.apk
```

## Langkah 5 — Tangkapan (bagian sensitif, lakukan SEKALI)

1. Mulai perekaman: jalankan `run-mitm.sh` (mitmdump di port 8080, menyimpan
   semua flow ke `flows.mitm` di folder ini) — atau `mitmweb` bila ingin
   melihat langsung.
2. Mulai emulator dengan proxy:
   `emulator -avd bima-cap -writable-system -http-proxy 127.0.0.1:8080`
   (dari sisi emulator, host Windows terlihat sebagai 127.0.0.1 lewat flag ini;
   bila memakai setelan proxy manual di Android, alamat host = 10.0.2.2:8080.)
3. Buka bima+, tunggu layar login tampil penuh (biarkan semua panggilan
   startup selesai — justru urutannya yang kita butuh).
4. Ketik nomor Tri, tekan tombol kirim OTP **satu kali**. Ini mengirim OTP
   sungguhan ke nomor itu — total kirim OTP jalur aplikasi untuk nomor Tri
   akan menjadi 4 (3 percobaan Jalu 2026-10-19 + 1 ini).
5. BERHENTI di situ. Jangan masukkan kode OTP, jangan tekan validate —
   perbandingan harus selesai dulu sebelum langkah lanjutan diputuskan.
6. Matikan emulator, kirim `flows.mitm` ke Jalu (taruh di folder ini, Jalu
   ambil lewat SSH, atau ekspor dari mitmweb sebagai HAR).

## Langkah 6 — Yang dibandingkan Jalu dari tangkapan

- Urutan lengkap panggilan dari aplikasi start sampai /otp/send/v1
  (panggilan pemanasan yang mungkin belum ditiru: token tamu/aplikasi,
  konfigurasi, cek versi, dll.)
- Header persis request sendOTP: nama, urutan, nilai (terutama
  X-IMI-TOKENID, X-DEVICEID, X-ESIM-SUPPORT, User-Agent OkHttp)
- Cookie yang dikirim/diterima di sepanjang sesi
- Isi body JSON persis (field action, format nomor)
- Apakah ada host lain yang dipanggil sebelum sendOTP yang belum dikenal

Hasil diff menentukan perbaikan runner `bima_live.py`; uji ulang sesudah itu
hanya atas instruksi Amad, dengan hitungan OTP per nomor dilaporkan terpisah
seperti biasa.

## Jangan

- Jangan pakai image Google Play / perangkat fisik untuk ini (CA sistem
  terkunci; di perangkat fisik config aplikasi menolak CA pengguna).
- Jangan ulangi tekan kirim OTP "untuk memastikan" — satu tangkapan cukup.
- Jangan simpan nomor/OTP di berkas mana pun di folder ini.
