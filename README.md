# Digitral multi-number dashboard

Backend (FastAPI) + frontend (vanilla JS) yang menggabungkan nomor bima+ (Tri) dan
myIM3 (Indosat) di satu dashboard, dengan polling real-time (SSE).

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