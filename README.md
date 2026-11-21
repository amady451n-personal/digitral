# Digitral multi-number dashboard

Backend (Go, menggantikan FastAPI) + frontend (vanilla JS) yang menggabungkan
nomor bima+, myIM3, AXISNET, dan myXL di satu dashboard, dengan polling real-time
(SSE) dan notifikasi Web Push.

## Backend Go

```bash
cd go && go test ./...                 # semua test golden + kontrak + grace
cd go && go build ./cmd/digitrald      # binary tunggal
# image: docker build -f go/Dockerfile -t digitral-backend:latest .
```

Status port: selesai untuk keempat provider, vault/registry kompatibel Python,
gate RAM lolos (2,5–6,1 MiB; detail di `docs/port-go-plan.md`).

Rollback: image Python lama masih tersimpan sebagai `digitral-python:latest` dan
`Dockerfile` root masih utuh. Untuk kembali, kembalikan `build: .` /
`image: digitral-dashboard:latest` di `docker-compose.yml` lalu
`docker compose up -d --build`. Format `vault.*` dan `registry.json` tidak
berubah, jadi data tetap terbaca kedua backend.

Perintah manual untuk verifikasi tanpa mengirim OTP:

```bash
cd go
POC_VAULT_SCAN=/tmp/opencode/vault-frontend-copy go run ./cmd/poc-bima
POC_VAULT=/tmp/opencode/vault-bima-copy go run ./cmd/poc-bima
```

## Jalankan (Docker Compose)

```bash
docker compose up -d --build
# buka http://localhost:8765
docker compose down          # hentikan
docker compose down -v       # hentikan + hapus data/vault
```

Data (vault terenkripsi + registry nomor) tersimpan di volume `digitral_data`,
jadi survive restart container. Untuk profil perangkat asli:

```bash
# buat profile.json (contoh), lalu uncomment mount di docker-compose.yml
echo '{"deviceId":"...","deviceName":"...","oem":"...","model":"...","osver":"..."}' > profile.json
```

## Jalankan tanpa Docker

```bash
pip install -r requirements.txt
python3 server.py            # http://127.0.0.1:8765
```

## CLI (tanpa frontend)

```bash
python3 scripts/bima_balance.py  <62xxxxxxxxxx> --action register
python3 scripts/bima_balance.py  <62xxxxxxxxxx> --watch 60
python3 scripts/myim3_balance.py <62xxxxxxxxxx> --action register
```

## Struktur

- `scripts/bima_balance.py` — klien Digitral (header-set, crypto, Vault AES-GCM, parse v7)
- `scripts/myim3_balance.py` — wrapper brand-tukar (konstanta dari decompile)
- `server.py` — FastAPI (provider registry, vault per provider, SSE)
- `frontend/index.html` — dashboard
- `docs/bima-protocol.md` — protokol (§13 tangkapan MuMu, §14 konstanta myIM3)
- `docs/frontend-architecture.md` — arsitektur multi-nomor

## Catatan keamanan

- Vault key & sesi hanya di backend/host; frontend tidak pernah melihat token.
- Backend tidak bisa membuktikan kepemilikan nomor — alur OTP adalah buktinya.
  Frontend hanya boleh mengirim nomor yang user login-nya miliki.
- CORS terbuka di sample; batasi ke origin frontend di produksi.
- Konstanta myXL/axisnet belum diverifikasi (slot `None` di `PROVIDERS`).
## Deploy ke perangkat ARM64 RAM rendah (STB 680HG)

Jangan build di perangkat. Build di mesin lain yang punya RAM cukup, lalu
pindahkan image yang sudah jadi:

```bash
# di mesin build (x86_64)
docker buildx build --platform linux/arm64 -f go/Dockerfile \
  -t digitral-backend:arm64 --load .
docker save digitral-backend:arm64 -o digitral-arm64.tar
scp digitral-arm64.tar user@stb:/tmp/

# di STB (tidak ada compiler Go, tidak ada build)
docker load -i /tmp/digitral-arm64.tar
docker compose -f docker-compose.device.yml up -d
docker stats --no-stream digitral-dashboard
```

`docker-compose.device.yml` memakai `pull_policy: never` (tidak pull/build) dan
membatasi heap Go lewat `GOMEMLIMIT=32MiB` + `GOGC=50`. Volume `digitral_data`
dan env tetap sama seperti compose utama, jadi data tidak perlu dipindah.
