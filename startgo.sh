#!/usr/bin/env bash
# Deploy backend Go (Digitral) ke STB ARM64 RAM rendah tanpa build di perangkat.
#   opsi 1: di mesin build  -> cross-build image arm64 + tar, lalu tampilkan scp
#   opsi 2: di STB          -> load image + jalankan compose (tanpa build)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

IMAGE="digitral-backend:arm64"
ARTIFACT="$SCRIPT_DIR/dist/digitral-arm64.tar.gz"
DEVICE_COMPOSE="docker-compose.device.yml"

need_docker() {
  command -v docker >/dev/null 2>&1 || { echo "ERROR: docker tidak ditemukan" >&2; exit 1; }
}

ensure_device_compose() {
  [[ -f "$DEVICE_COMPOSE" ]] && return
  cat > "$DEVICE_COMPOSE" <<'YAML'
services:
  digitral:
    image: digitral-backend:arm64
    pull_policy: never
    container_name: digitral-dashboard
    restart: unless-stopped
    ports:
      - "${HOST_PORT:-8765}:8765"
    environment:
      FRONTEND_HOME: /app/data
      POLL_SEC: "60"
      TZ: ${TZ:-Asia/Jakarta}
      GOMEMLIMIT: 32MiB
      GOGC: "50"
      VAPID_PRIVATE_KEY: ${VAPID_PRIVATE_KEY:-}
      VAPID_PUBLIC_KEY: ${VAPID_PUBLIC_KEY:-}
      VAPID_MAILTO: ${VAPID_MAILTO:-mailto:kamaya021@gmail.com}
    volumes:
      - digitral_data:/app/data
    healthcheck:
      test: ["CMD", "/digitrald", "-healthcheck"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 10s

volumes:
  digitral_data:
    name: digitral_data
YAML
  echo "Dibuat $DEVICE_COMPOSE (volume digitral_data = data lama dipakai ulang)"
}

build_here() {
  need_docker
  docker buildx version >/dev/null 2>&1 || { echo "ERROR: docker buildx tidak tersedia" >&2; exit 1; }
  mkdir -p "$SCRIPT_DIR/dist"

  echo ">> cross-build linux/arm64 di mesin ini (tanpa QEMU di STB)"
  docker buildx build --platform linux/arm64 -f go/Dockerfile -t "$IMAGE" --load .
  docker image inspect "$IMAGE" --format '   image: {{.Os}}/{{.Architecture}} {{.Size}} bytes'

  echo ">> simpan artifact"
  docker save "$IMAGE" | gzip -1 > "$ARTIFACT"
  ls -lh "$ARTIFACT"

  cat <<EOF

>> Salin ke STB (ganti user@stb):

   scp "$SCRIPT_DIR/startgo.sh" "$SCRIPT_DIR/$DEVICE_COMPOSE" "$ARTIFACT" root@192.168.8.200:/tmp/

>> Di STB:

   bash /tmp/startgo.sh
   # pilih 2

Artifact : $ARTIFACT
Image    : $IMAGE
EOF
}

deploy_device() {
  need_docker
  ensure_device_compose

  if [[ -f "$ARTIFACT" ]]; then
    echo ">> load image dari $ARTIFACT"
    docker load -i "$ARTIFACT"
  elif docker image inspect "$IMAGE" >/dev/null 2>&1; then
    echo ">> image $IMAGE sudah ada, skip load"
  else
    echo "ERROR: tidak ada $ARTIFACT dan image $IMAGE tidak ditemukan" >&2
    exit 1
  fi

  local arch
  arch="$(docker image inspect "$IMAGE" --format '{{.Architecture}}')"
  echo ">> arsitektur image: $arch"
  [[ "$arch" == "arm64" ]] || echo "WARNING: image bukan arm64, periksa lagi artifact yang disalin"

  echo ">> jalankan compose (pull_policy: never, tidak ada build)"
  docker compose -f "$DEVICE_COMPOSE" up -d

  sleep 2
  echo ">> status"
  docker ps --format '{{.Names}} {{.Image}} {{.Status}}' | grep digitral || true
  docker stats --no-stream --format '   RAM: {{.MemUsage}} CPU: {{.CPUPerc}}' digitral-dashboard || true
  echo ">> health"
  curl -fsS --max-time 5 http://127.0.0.1:${HOST_PORT:-8765}/health || echo "WARNING: /health belum merespons"
}

echo "Digitral Go deploy"
echo "  1) Build image arm64 di mesin ini + info scp"
echo "  2) Deploy di STB (load image + compose up, tanpa build)"
echo "  3) Keluar"

choice="${1:-}"
if [[ -z "$choice" ]]; then
  read -r -p "Pilih [1/2/3]: " choice
fi

case "$choice" in
  1) build_here ;;
  2) deploy_device ;;
  3|"") exit 0 ;;
  *) echo "Pilihan tidak valid: $choice" >&2; exit 1 ;;
esac
