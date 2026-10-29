#!/usr/bin/env python3
"""Digitral multi-number backend for a frontend dashboard.

Aggregates bima+ (Tri) and myIM3 (Indosat) numbers behind one HTTP API so a
frontend can list/poll all of them. Reuses scripts/bima_balance.py (provider
profiles + AES-GCM vault) without duplicating protocol logic.

Run:
    pip install fastapi uvicorn
    python3 server.py            # http://127.0.0.1:8765
    # or: uvicorn server:app --reload

Env:
    FRONTEND_HOME   base dir for registry + per-provider vaults (default ~/.bima-frontend)
    POLL_SEC        SSE poll interval (default 60)
    DEVICE_PROFILE  optional JSON file seeded into every new vault (real device identity)

Endpoints (JSON):
    GET  /health
    GET  /providers
    GET  /numbers
    POST /numbers            {provider, msisdn}
    POST /numbers/{id}/verify {otp}
    GET  /numbers/{id}/balance
    DELETE /numbers/{id}
    GET  /stream             (SSE) pushes {numbers:[...]} every POLL_SEC
"""
import json
import os
import sys
import time
import uuid
from pathlib import Path
from typing import Any, Dict, List, Optional

sys.path.insert(0, str(Path(__file__).resolve().parent / "scripts"))
import bima_balance as bm

HOME = Path(os.environ.get("FRONTEND_HOME", str(Path.home() / ".bima-frontend")))
POLL_SEC = float(os.environ.get("POLL_SEC", "60"))
REGISTRY_FILE = HOME / "registry.json"
VAULTS_DIR = HOME / "vaults"

# providers that are actually wired (constants verified). myXL/axisnet slots exist
# in bima_balance (PROVIDER_MYXL/AXISNET) but are None until decompile-verified.
PROVIDERS = {
    "bima": {
        "label": "bima+ (Tri)",
        "cfg": bm.PROVIDER_BIMA,
        "vault_dir": VAULTS_DIR / "bima",
    },
    "myim3": {
        "label": "myIM3 (Indosat)",
        "cfg": bm.PROVIDER_MYIM3,
        "vault_dir": VAULTS_DIR / "myim3",
    },
}

try:
    from fastapi import FastAPI, HTTPException
    from fastapi.middleware.cors import CORSMiddleware
    from fastapi.responses import StreamingResponse, HTMLResponse, FileResponse
    from pydantic import BaseModel
except ImportError as e:
    raise SystemExit("Missing dependency: pip install fastapi uvicorn") from e

app = FastAPI(title="Digitral multi-number backend")
app.add_middleware(
    CORSMiddleware, allow_origins=["*"], allow_methods=["*"], allow_headers=["*"]
)

# ---------- registry (which numbers this dashboard owns) ----------
def _load_registry() -> Dict[str, Any]:
    if not REGISTRY_FILE.exists():
        return {"numbers": []}
    try:
        return json.loads(REGISTRY_FILE.read_text())
    except Exception:
        return {"numbers": []}


def _save_registry(reg: Dict[str, Any]):
    HOME.mkdir(parents=True, exist_ok=True)
    REGISTRY_FILE.write_text(json.dumps(reg, indent=2))


def _find(num_id: str) -> Optional[Dict[str, Any]]:
    for n in _load_registry()["numbers"]:
        if n["id"] == num_id:
            return n
    return None


# ---------- device profile seeding ----------
def _seed_vault(provider: str):
    vault = bm.Vault(PROVIDERS[provider]["vault_dir"])
    prof_path = os.environ.get("DEVICE_PROFILE")
    if prof_path and Path(prof_path).exists():
        try:
            prof = json.loads(Path(prof_path).read_text())
            vault.put_device_profile(prof)
        except Exception:
            pass
    return vault


# ---------- per-number client factory ----------
_pending: Dict[str, str] = {}   # number_id -> transid awaiting OTP verify


def _client(provider: str, msisdn: str, vault: Optional[bm.Vault] = None):
    if provider not in PROVIDERS or PROVIDERS[provider]["cfg"] is None:
        raise HTTPException(400, f"provider not available: {provider}")
    p = PROVIDERS[provider]
    if vault is None:
        vault = bm.Vault(p["vault_dir"])
    http = bm.Http(p["cfg"]["base_default"], bm.DEFAULT_UA, 45.0)
    return bm.BimaClient(http, vault, msisdn, cfg=p["cfg"]), vault


def _fetch_balance(provider: str, msisdn: str) -> Dict[str, Any]:
    client, vault = _client(provider, msisdn)
    client.fresh = False
    # reuse session if present
    sess = vault.session(msisdn)
    if sess:
        client.token = sess.get("token") or sess.get("sessionId")
    try:
        result = client.get_balance()
    except SystemExit as e:
        return {"ok": False, "error": str(e)}
    except Exception as e:
        return {"ok": False, "error": f"{type(e).__name__}: {e}"}
    out = bm.parse_dashboard(result) if False else None
    # get_balance returns the parsed-ish result already? check: it returns parse_dashboard? 
    return {"ok": True, "data": result}


# ---------- models ----------
class AddNumber(BaseModel):
    provider: str
    msisdn: str


class VerifyOtp(BaseModel):
    otp: str


# ---------- endpoints ----------
@app.get("/health")
def health():
    return {"ok": True, "providers": list(PROVIDERS.keys())}


@app.get("/providers")
def providers():
    return [{"id": k, "label": v["label"], "available": v["cfg"] is not None}
            for k, v in PROVIDERS.items()]


@app.get("/numbers")
def list_numbers():
    reg = _load_registry()
    out = []
    for n in reg["numbers"]:
        out.append({
            "id": n["id"],
            "provider": n["provider"],
            "label": PROVIDERS.get(n["provider"], {}).get("label", n["provider"]),
            "msisdn": n["msisdn"],
            "added_at": n.get("added_at"),
        })
    return {"numbers": out}


@app.post("/numbers")
def add_number(body: AddNumber):
    if body.provider not in PROVIDERS or PROVIDERS[body.provider]["cfg"] is None:
        raise HTTPException(400, "unknown/unavailable provider")
    msisdn = bm.normalize_msisdn(body.msisdn)
    # avoid duplicates
    reg = _load_registry()
    for n in reg["numbers"]:
        if n["provider"] == body.provider and n["msisdn"] == msisdn:
            raise HTTPException(409, "number already registered")
    vault = _seed_vault(body.provider)
    client, _ = _client(body.provider, msisdn, vault)
    try:
        transid = client.send_otp()
    except SystemExit as e:
        raise HTTPException(502, str(e))
    except Exception as e:
        raise HTTPException(502, f"{type(e).__name__}: {e}")
    nid = uuid.uuid4().hex[:12]
    reg["numbers"].append({
        "id": nid, "provider": body.provider, "msisdn": msisdn,
        "added_at": time.time(),
    })
    _save_registry(reg)
    _pending[nid] = transid
    return {"id": nid, "msisdn": msisdn, "provider": body.provider,
            "transid": transid, "status": "otp_sent"}


@app.post("/numbers/{num_id}/verify")
def verify(num_id: str, body: VerifyOtp):
    info = _find(num_id)
    if not info:
        raise HTTPException(404, "number not found")
    transid = _pending.get(num_id)
    if not transid:
        raise HTTPException(400, "no pending OTP for this number (re-send)")
    vault = bm.Vault(PROVIDERS[info["provider"]]["vault_dir"])
    client, _ = _client(info["provider"], info["msisdn"], vault)
    try:
        token = client.validate_otp(transid, body.otp.strip())
    except SystemExit as e:
        raise HTTPException(400, str(e))
    except Exception as e:
        raise HTTPException(400, f"{type(e).__name__}: {e}")
    vault.put_session(info["msisdn"], {"token": token, "exp": bm._jwt_exp(token)})
    _pending.pop(num_id, None)
    return {"ok": True, "id": num_id}


@app.get("/numbers/{num_id}/balance")
def balance(num_id: str):
    info = _find(num_id)
    if not info:
        raise HTTPException(404, "number not found")
    vault = bm.Vault(PROVIDERS[info["provider"]]["vault_dir"])
    client, _ = _client(info["provider"], info["msisdn"], vault)
    sess = vault.session(info["msisdn"])
    if not sess:
        raise HTTPException(401, "no valid session; re-register")
    client.token = sess.get("token")
    try:
        raw = client.get_balance()
    except SystemExit as e:
        raise HTTPException(502, str(e))
    except Exception as e:
        raise HTTPException(502, f"{type(e).__name__}: {e}")
    return {"id": num_id, "provider": info["provider"], "msisdn": info["msisdn"],
            "balance": raw}


@app.delete("/numbers/{num_id}")
def delete_number(num_id: str):
    info = _find(num_id)
    if not info:
        raise HTTPException(404, "number not found")
    try:
        bm.Vault(PROVIDERS[info["provider"]]["vault_dir"]).drop_session(info["msisdn"])
    except Exception:
        pass
    reg = _load_registry()
    reg["numbers"] = [n for n in reg["numbers"] if n["id"] != num_id]
    _save_registry(reg)
    _pending.pop(num_id, None)
    return {"ok": True}


def _snapshot() -> List[Dict[str, Any]]:
    out = []
    for n in _load_registry()["numbers"]:
        item = {"id": n["id"], "provider": n["provider"],
                "label": PROVIDERS.get(n["provider"], {}).get("label", n["provider"]),
                "msisdn": n["msisdn"], "ok": False, "data": None, "error": None}
        try:
            vault = bm.Vault(PROVIDERS[n["provider"]]["vault_dir"])
            client, _ = _client(n["provider"], n["msisdn"], vault)
            sess = vault.session(n["msisdn"])
            if not sess:
                item["error"] = "no_session"
                out.append(item)
                continue
            client.token = sess.get("token")
            raw = client.get_balance()
            item["ok"] = True
            item["data"] = raw
        except SystemExit as e:
            item["error"] = str(e)
        except Exception as e:
            item["error"] = f"{type(e).__name__}: {e}"
        out.append(item)
    return out


@app.get("/stream")
def stream():
    def gen():
        while True:
            try:
                payload = {"numbers": _snapshot(), "ts": time.time()}
                yield f"data: {json.dumps(payload)}\n\n"
            except Exception as e:
                yield f"data: {json.dumps({'error': str(e)})}\n\n"
            time.sleep(POLL_SEC)
    return StreamingResponse(gen(), media_type="text/event-stream")


# ---------- minimal built-in dashboard (no build step) ----------
@app.get("/", response_class=HTMLResponse)
def index():
    html_path = Path(__file__).resolve().parent / "frontend" / "index.html"
    if html_path.exists():
        return HTMLResponse(html_path.read_text())
    return HTMLResponse("<h1>frontend/index.html missing</h1>", status_code=500)


if __name__ == "__main__":
    import uvicorn
    HOME.mkdir(parents=True, exist_ok=True)
    VAULTS_DIR.mkdir(parents=True, exist_ok=True)
    uvicorn.run(app, host=os.environ.get("HOST", "0.0.0.0"), port=int(os.environ.get("PORT", "8765")))