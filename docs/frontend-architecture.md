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

## Known limits of the sample
- Pending OTPs are in-memory: server restart mid-verify requires re-send.
- No auth on the API: bind to 127.0.0.1 or put behind your own auth proxy.
- Provider constants for myXL/axisnet are not yet verified (slots are None).
- Device profile defaults to a generic stand-in; seed a real profile via
  `DEVICE_PROFILE=/path/to/profile.json` for better risk-engine parity.
EOF
echo done