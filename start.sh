#!/usr/bin/env bash
# Start digitral-dashboard: cek VAPID env (generate otomatis jika belum), lalu compose up.
set -euo pipefail
cd "$(dirname "$0")"

ENV_FILE=.env

gen_vapid() {
  # Generate keypair fresh via container (host tidak perlu pywebpush).
  # Image dibangun dulu tanpa env: ENV_FILE dipindah sementara agar ARG default kosong.
  docker compose build --quiet digitral
  env -u VAPID_PRIVATE_KEY -u VAPID_PUBLIC_KEY \
    docker compose run --rm --entrypoint "" digitral \
    python /app/scripts/push_notify.py --genkey
}

need_gen=false
if [ ! -f "$ENV_FILE" ]; then
  need_gen=true
elif ! grep -qE "^VAPID_PRIVATE_KEY=.+" "$ENV_FILE" || ! grep -qE "^VAPID_PUBLIC_KEY=.+" "$ENV_FILE"; then
  need_gen=true
fi

if $need_gen; then
  echo "[start] VAPID keys belum terisi -> generate..."
  keys=$(gen_vapid)
  grep -vE "^VAPID_(PRIVATE|PUBLIC)_KEY=" "$ENV_FILE" 2>/dev/null > "$ENV_FILE.tmp" || true
  mv "$ENV_FILE.tmp" "$ENV_FILE"
  printf '%s\n' "$keys" >> "$ENV_FILE"
  echo "[start] VAPID keys ditulis ke $ENV_FILE (jangan di-commit)"
fi

docker compose up --build -d
echo "[start] digitral-dashboard jalan di port ${HOST_PORT:-8765}"
