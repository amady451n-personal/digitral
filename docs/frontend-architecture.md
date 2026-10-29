# Frontend multi-number architecture (bima+ & myIM3)

## Goal
Display N owned numbers across providers (e.g. 3 bima+ Tri + 2 myIM3 Indosat) in one
dashboard, with live polling, add-number via OTP, and delete.

## Why a backend proxy (not direct from browser)
The Digitral protocol cannot run in a browser:
- TLS fingerprint (curl_cffi chrome) and empty `User-Agent` header cannot be set from JS
- Header set (X-FAIID, X-IMI-ADID, …) + AES-GCM body + RC4 child line
- Encrypted vault keys must never reach the client
- CORS / mixed-content: provider hosts are http(s) with their own origins
So the browser talks to *our* backend, which runs `scripts/bima_balance.py`.

## Components
```
frontend/index.html  (vanilla JS, SSE consumer)
        │
        ▼
server.py (FastAPI)  ── imports scripts/bima_balance.py
        │
        ├── Vault per provider: ~/.bima-frontend/vaults/{bima,myim3}/vault.bin
        ├── Registry:        ~/.bima-frontend/registry.json  (id, provider, msisdn, added_at)
        ├── Pending OTP:     in-memory {id: transid}  (lost on restart → re-send)
        └── Providers:       bima_balance.PROVIDERS (cfg per provider)
        │
        ▼
Digitral backends:  bimaplus-api.ioh.co.id  /  myim3api1.ioh.co.id  (separate accounts)
```

## Provider model
A provider = one operator app. Constants differ (SERVICEKEY, CHANNEL, version, base host)
but protocol/crypto/header shape are shared → `make_provider_cfg()` in bima_balance.py.
Adding a new provider (myXL, axisnet) = verify its constants from its APK, then add an
entry to `PROVIDERS` in server.py. No protocol code changes needed.

## Number lifecycle (API)
| step | endpoint | notes |
|---|---|---|
| list | `GET /numbers` | registry only (fast) |
| add  | `POST /numbers {provider, msisdn}` | sends real OTP SMS → returns transid; stores pending |
| verify | `POST /numbers/{id}/verify {otp}` | AES-GCM validate; on success stores session in vault |
| read  | `GET /numbers/{id}/balance` | reuses stored session; 401 if expired → re-add |
| poll  | `GET /stream` (SSE) | snapshots all numbers every POLL_SEC |
| remove | `DELETE /numbers/{id}` | drops session + registry entry |

## Data flow for "5 numbers, 2 providers"
1. Frontend calls `/numbers` → gets 5 ids (3 bima, 2 myim3).
2. SSE `/stream` triggers `_snapshot()` → for each number: open its provider vault,
   reuse session, `get_balance()` → parsed dashboard (balance, quota, active_until,
   grace, bonstri).
3. Frontend renders cards; quota normalised KB→MB/GB in JS (same as fmt_quota).

## Security boundaries
- Vault keys and sessions live only on the backend host; frontend never sees tokens.
- `X-FAIID`/`X-IMI-ADID` are per-vault (generated once, stable) — consistent fingerprint.
- Ownership is a *trust* boundary: backend cannot prove a user owns a number; the OTP
  flow is the proof. Frontend must only submit numbers the logged-in user owns.
  Never run OTP sends against numbers you do not own.
- CORS is open (`*`) in the sample — restrict to your frontend origin in production.
- Single-user sample: registry is a flat JSON file. Multi-user needs auth + per-user
  registry table; vaults can stay shared per provider (sessions keyed by msisdn).

## Real-time
SSE with server-side polling (POLL_SEC, default 60s). Each snapshot does N HTTP calls to
provider backends sequentially — fine for ≤~20 numbers. For larger sets, parallelise
with a thread pool or move to per-number refresh on demand.

## Reuse
- Parsing: `parse_dashboard`, `fmt_quota` in bima_balance.py (already lowercase keys).
- Crypto: `aes_gcm_encrypt`, `rc4_hex`, `sha512_hex` (identical across providers).
- Device identity: `Vault.faaid/gaid/device_id` (stable per vault).

## Deployment (Docker Compose)
Lihat `Dockerfile`, `docker-compose.yml`, `requirements.txt`, `.dockerignore`, `README.md`.

```bash
docker compose up -d --build   # http://localhost:8765
docker compose down -v         # hentikan + hapus data/vault
```

- Image berbasis `python:3.11-slim`, ~ (pip install curl_cffi + cryptography + fastapi).
- Volume `digitral_data` → `/app/data` (vaults + registry) persisten.
- `HOST` default `0.0.0.0` di container (server.py baca env `HOST`/`PORT`).
- Healthcheck via `urllib` ke `/health`.
- `DEVICE_PROFILE=/app/profile.json` (mount read-only) untuk seed profil perangkat asli.
## JSON mode (CLI)
`scripts/bima_balance.py --json` (dan `myim3_balance.py`) mencetak satu objek JSON ke stdout,
log/progress ke stderr, exit 0/1. Schema balance:
`{provider, msisdn, status, balance, quota, unit, used, unlimited, active_until,
grace_until, bonstri, session_reused}`. Error: `{status:"error", code, message}`.
Send: `{status:"otp_sent", transid}`. Berguna untuk scripting/debug/host — frontend
memakai HTTP API, bukan shell CLI.

## pending lifecycle
- `PENDING_TTL_SEC` (default 300, env). Entri pending kadaluarsa auto-drop.
- `add_number` idempoten untuk `(provider, msisdn)` yang sama → re-send OTP, update transid.
- `POST /numbers/{id}/resend`, `GET /numbers?include_pending=1`.
- Frontend menampilkan kartu "menunggu OTP" + tombol kirim ulang/batal.

## PWA & mobile
- Mobile-first CSS, input types `tel`/`numeric`, safe-area insets, bottom sheet modal.
- `manifest.json`, `sw.js` (cache-first shell, network-first API, offline fallback ke
  localStorage snapshot), `assets/icon.svg` + `assets/{bima,myim3}.png`.
- Install prompt (`beforeinstallprompt`) + instruksi iOS.
- Wajib HTTPS di produksi (secure context).

## All-in-container
- Vault + registry di volume `digitral_data` → `/app/data`. `BIMA_HOME=/app/data/bima`
  di Dockerfile agar CLI berbagi sesi dengan backend.
- Default bridge (tanpa `network_mode: host`). Jika 502: cek egress/DNS/proxy container.
