#!/usr/bin/env python3
"""Web Push (VAPID) untuk notifikasi masa aktif — walau PWA tertutup.

Komponen:
- VAPID keypair: dari env VAPID_PRIVATE_KEY/VAPID_PUBLIC_KEY (docker-compose)
  atau file ~/.bima-frontend/vapid.json sebagai fallback lokal.
- subscriptions.json: daftar endpoint push per browser (disimpan server).
- check_and_notify(): dipanggil thread harian di server (jam 08:00 TZ container).
    * active_until == hari ini      -> "Hari ini, masa aktif <provider> <nomor> berakhir"
    * tenggang & grace terpisah     -> "harus segera diisi" mulai 20 hari sebelum grace
    * tenggang tanpa grace terpisah -> "harus segera diisi" setelah lewat > 20 hari

CLI:
    python3 scripts/push_notify.py --genkey   # selalu generate keypair baru + print
    python3 scripts/push_notify.py --test     # kirim push test ke semua subscription
"""
import base64
import json
import os
import sys
from datetime import datetime
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import bima_balance as bm  # noqa: E402
import myxl_balance as mx  # noqa: E402
import axisnet_balance as ax  # noqa: E402
from pywebpush import webpush, WebPushException  # noqa: E402

HOME = Path(os.environ.get("FRONTEND_HOME", str(Path.home() / ".bima-frontend")))
SUBS_FILE = HOME / "subscriptions.json"
VAPID_FILE = HOME / "vapid.json"
CLAIM_EMAIL = os.environ.get("VAPID_MAILTO", "mailto:admin@example.com")
GRACE_WARN_DAYS = 20

PROVIDER_LABELS = {"bima": "bima+", "myim3": "myIM3", "axisnet": "AXISNET", "myxl": "myXL"}


def _b64url(raw: bytes) -> str:
    return base64.urlsafe_b64encode(raw).decode().rstrip("=")


def generate_vapid() -> "tuple[str, str]":
    """Generate keypair P-256 fresh. Tidak membaca env/file, tidak menulis file."""
    from cryptography.hazmat.primitives.asymmetric import ec
    from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat
    key = ec.generate_private_key(ec.SECP256R1())
    priv_num = key.private_numbers().private_value
    priv_b64 = _b64url(priv_num.to_bytes(32, "big"))
    pub_b64 = _b64url(key.public_key().public_bytes(Encoding.X962, PublicFormat.UncompressedPoint))
    return priv_b64, pub_b64


def ensure_vapid() -> "tuple[str, str]":
    """Return (private_b64url, public_b64url). Env > file > generate+save file."""
    priv = os.environ.get("VAPID_PRIVATE_KEY")
    pub = os.environ.get("VAPID_PUBLIC_KEY")
    if priv and pub:
        return priv, pub
    if VAPID_FILE.exists():
        d = json.loads(VAPID_FILE.read_text())
        return d["private_key"], d["public_key"]
    priv_b64, pub_b64 = generate_vapid()
    HOME.mkdir(parents=True, exist_ok=True)
    VAPID_FILE.write_text(json.dumps({"private_key": priv_b64, "public_key": pub_b64}))
    VAPID_FILE.chmod(0o600)
    return priv_b64, pub_b64


def load_subs() -> "list[dict]":
    if not SUBS_FILE.exists():
        return []
    try:
        return json.loads(SUBS_FILE.read_text())
    except Exception:
        return []


def save_subs(subs: "list[dict]") -> None:
    HOME.mkdir(parents=True, exist_ok=True)
    SUBS_FILE.write_text(json.dumps(subs, indent=1))


def add_sub(sub: dict) -> "tuple[bool, int]":
    """Insert/replace by endpoint. Returns (new, total)."""
    subs = [s for s in load_subs() if s.get("endpoint") != sub.get("endpoint")]
    new = not any(s.get("endpoint") == sub.get("endpoint") for s in load_subs())
    subs.append(sub)
    save_subs(subs)
    return new, len(subs)


def send_all(payload: dict) -> "tuple[int, int]":
    """Push payload to every subscription; drop dead ones. Returns (sent, dropped)."""
    priv, _ = ensure_vapid()
    claims = {"sub": CLAIM_EMAIL}
    sent = dropped = 0
    keep = []
    for sub in load_subs():
        try:
            webpush(sub, json.dumps(payload), vapid_private_key=priv, vapid_claims=claims)
            sent += 1
            keep.append(sub)
        except WebPushException as e:
            status = getattr(getattr(e, "response", None), "status_code", None)
            if status in (404, 410):
                dropped += 1  # subscription expired/revoked -> drop
            else:
                print(f"push fail ({status}): {e}", file=sys.stderr)
                keep.append(sub)
    if dropped:
        save_subs(keep)
    return sent, dropped


def _mask(msisdn: str) -> str:
    digits = "".join(ch for ch in str(msisdn or "") if ch.isdigit())
    return f"•••• •••• {digits[-4:]}" if len(digits) >= 4 else str(msisdn or "-")


def _parse_active(value) -> "datetime | None":
    """Loose date parser: '01 Sep 2027', '2027-09-01', '29/10/2027', 'Exp on 29 Dec 2026'."""
    raw = str(value or "").strip()
    if not raw or raw == "-":
        return None
    for fmt in ("%d %b %Y", "%Y-%m-%d", "%d/%m/%Y"):
        try:
            return datetime.strptime(raw, fmt)
        except ValueError:
            pass
    m = raw.replace("Exp on ", "").strip()
    for fmt in ("%d %b %Y", "%d-%m-%Y"):
        try:
            return datetime.strptime(m, fmt)
        except ValueError:
            pass
    return None


def _grace_urgent(active: datetime, grace: "datetime | None", today) -> bool:
    """Eskalasi 'harus segera diisi'.

    Grace terpisah dari masa aktif -> 20 hari sebelum tanggal grace.
    Grace kosong/sama dengan masa aktif -> aturan lama (> 20 hari lewat).
    """
    if grace is not None and grace.date() != active.date():
        return (grace.date() - today).days <= GRACE_WARN_DAYS
    return (today - active.date()).days > GRACE_WARN_DAYS


def _fetch_balance(provider: str, msisdn: str) -> "dict | None":
    vault_dir = HOME / provider
    vault = bm.Vault(vault_dir)
    if provider == "myxl":
        client = mx.MyXLClient(vault, msisdn)
        sess = client._saved()
        client.token = sess.get("token")
        tok_exp = sess.get("token_exp") or 0
        if client.token and tok_exp and datetime.now().timestamp() > int(tok_exp) - 120:
            try:
                client.refresh()
            except Exception:
                pass
        return client.get_balance() if client.token else None
    if provider == "axisnet":
        client = ax.AxisnetClient(vault, msisdn)
        client.token = (vault.session(msisdn) or {}).get("token")
        return client.get_balance() if client.token else None
    cfg = {"bima": bm.PROVIDER_BIMA, "myim3": bm.PROVIDER_MYIM3}.get(provider)
    if cfg is None:
        return None
    http = bm.Http(cfg["base_default"], bm.DEFAULT_UA, 45.0)
    client = bm.BimaClient(http, vault, msisdn, action="balance", cfg=cfg)
    return client.get_balance()


def check_and_notify() -> "list[dict]":
    """Cek semua nomor terdaftar; push untuk hari-H & tenggang >20 hari."""
    reg_file = HOME / "registry.json"
    registry = json.loads(reg_file.read_text()) if reg_file.exists() else {"numbers": []}
    today = datetime.now().date()
    notified = []
    for n in registry.get("numbers") or []:
        provider = n.get("provider")
        msisdn = n.get("msisdn")
        try:
            data = _fetch_balance(provider, msisdn)
        except Exception as e:
            print(f"{provider} {msisdn}: balance gagal: {e}", file=sys.stderr)
            continue
        if not data:
            continue
        active = _parse_active(data.get("active_until"))
        if active is None:
            continue
        grace = _parse_active(data.get("grace_until"))
        label = PROVIDER_LABELS.get(provider, provider)
        days = (active.date() - today).days
        if days == 0:
            body = f"Hari ini, masa aktif {label} {_mask(msisdn)} berakhir"
            tag = f"end-{n.get('id')}"
        elif days < 0 and _grace_urgent(active, grace, today):
            grace_note = ""
            if grace is not None and grace.date() != active.date():
                grace_note = f" s/d {grace.strftime('%d %b %Y')}"
            body = (f"{label} {_mask(msisdn)}: masa aktif lewat {-days} hari"
                    f" (tenggang{grace_note}) — harus segera diisi")
            tag = f"grace-{n.get('id')}"
        else:
            continue
        sent, _ = send_all({"title": "Digitral", "body": body, "tag": tag})
        if sent:
            notified.append({"provider": provider, "msisdn": msisdn, "body": body})
    return notified


def daily_loop(run_hour: int = 8) -> None:
    """Thread harian: tidur sampai jam run_hour, lalu check_and_notify() tiap hari."""
    import time as _time
    while True:
        now = datetime.now()
        target = now.replace(hour=run_hour, minute=0, second=0, microsecond=0)
        if now >= target:
            target += _time.timedelta(days=1)
        wait = max(0.0, (target - now).total_seconds())
        print(f"[push] next daily check at {target}", flush=True)
        _time.sleep(wait)
        try:
            for row in check_and_notify():
                print(f"[push] notified: {row['provider']} {row['msisdn']} -> {row['body']}", flush=True)
        except Exception as e:
            print(f"[push] daily check gagal: {e}", file=sys.stderr, flush=True)


def main(argv=None) -> int:
    argv = list(sys.argv[1:] if argv is None else argv)
    if "--genkey" in argv:
        priv, pub = generate_vapid()
        print(f"VAPID_PRIVATE_KEY={priv}")
        print(f"VAPID_PUBLIC_KEY={pub}")
        return 0
    if "--test" in argv:
        sent, dropped = send_all({"title": "Digitral", "body": "Push test", "tag": "test"})
        print(f"sent={sent} dropped={dropped}")
        return 0
    print(__doc__)
    return 2


if __name__ == "__main__":
    sys.exit(main() or 0)
