#!/bin/bash
# Pasang CA mitmproxy ke penyimpanan CA SISTEM emulator Android.
# Jalankan dari WSL. Emulator harus memakai system image google_apis
# (bukan Play Store) dan diboot dengan -writable-system.
#
# Pakai:  ./install-ca.sh [path-ke-mitmproxy-ca-cert.cer]
# Env:    SDK_ROOT_WIN (default C:\Android), AVD_NAME (default bima-cap)
set -euo pipefail

SDK_WIN="${SDK_ROOT_WIN:-C:\\Android}"
SDK_WSL="/mnt/c/Android"
ADB="$SDK_WSL/platform-tools/adb.exe"
EMU="$SDK_WSL/emulator/emulator.exe"
AVD="${AVD_NAME:-bima-cap}"
CERT="${1:-/mnt/c/Users/elixer/.mitmproxy/mitmproxy-ca-cert.cer}"

[ -x "$ADB" ] || { echo "adb.exe tidak ditemukan di $ADB — jalankan RUNBOOK Langkah 1 dulu"; exit 1; }
[ -f "$CERT" ] || { echo "Sertifikat CA tidak ada: $CERT — jalankan mitmproxy sekali (RUNBOOK Langkah 2)"; exit 1; }

echo "== Hitung nama hash sertifikat (subject_hash_old) =="
HASH=$(openssl x509 -inform PEM -subject_hash_old -in "$CERT" -noout | head -1)
OUT="/tmp/${HASH}.0"
openssl x509 -inform PEM -in "$CERT" -out "$OUT"
echo "Sertifikat -> $OUT"

echo "== Boot emulator (writable system) bila belum jalan =="
if ! "$ADB" devices | grep -q "emulator-.*device$"; then
  echo "Menjalankan emulator $AVD ... (tutup jendela emulator setelah selesai reboot nanti)"
  "$EMU" -avd "$AVD" -writable-system -no-snapshot &
  "$ADB" wait-for-device
fi
# Tunggu boot selesai
until [ "$("$ADB" shell getprop sys.boot_completed 2>/dev/null | tr -d '\r')" = "1" ]; do sleep 3; done
echo "Boot selesai."

echo "== Root + remount =="
"$ADB" root
"$ADB" wait-for-device
"$ADB" remount

echo "== Push CA ke /system/etc/security/cacerts =="
"$ADB" push "$OUT" "/system/etc/security/cacerts/${HASH}.0"
"$ADB" shell chmod 644 "/system/etc/security/cacerts/${HASH}.0"

echo "== Reboot =="
"$ADB" reboot
"$ADB" wait-for-device
until [ "$("$ADB" shell getprop sys.boot_completed 2>/dev/null | tr -d '\r')" = "1" ]; do sleep 3; done

echo "Selesai. Verifikasi di emulator: Settings > Security > Encryption & credentials"
echo "> Trusted credentials > System — harus ada entri 'mitmproxy'."
echo "Jangan lupa: boot berikutnya untuk tangkapan tetap pakai -writable-system"
echo "agar partisi sistem hasil modifikasi ini yang dipakai (RUNBOOK Langkah 5)."
