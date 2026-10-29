#!/bin/bash
# Mulai perekaman mitmproxy untuk tangkapan bima+.
# Jalankan dari WSL di folder emulator-capture ini.
# - Bila mitmproxy terpasang di Windows: skrip ini memakai mitmdump WSL bila ada;
#   bila tidak ada, jalankan manual di PowerShell Windows:
#       mitmweb --listen-port 8080
#   lalu emulator tetap diarahkan ke 127.0.0.1:8080 (flag -http-proxy).
# - Semua flow tersimpan ke flows.mitm di folder ini untuk dibandingkan Jalu.
set -euo pipefail
cd "$(dirname "$0")"

if command -v mitmdump >/dev/null 2>&1; then
  echo "mitmdump (WSL) di port 8080, menyimpan ke flows.mitm — Ctrl-C untuk berhenti"
  exec mitmdump --listen-host 0.0.0.0 --listen-port 8080 -w flows.mitm
else
  echo "mitmdump tidak ditemukan di WSL."
  echo "Jalankan mitmproxy di Windows (PowerShell):  mitmweb --listen-port 8080"
  echo "atau pasang di WSL:  pip3 install --user mitmproxy"
  exit 1
fi
