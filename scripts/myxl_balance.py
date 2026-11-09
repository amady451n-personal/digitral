#!/usr/bin/env python3
"""myXL (XL Axiata) balance client — ForgeRock CIAM OTP login + Digitral-like API.

Reversed from myxl-9.6.0.apk (runtime-decrypted CIAM dex d0 + core d3/d4).
See /tmp/opencode/myxl/FINDINGS.md for the full evidence trail.

Protocol (verified endpoints; see FINDINGS.md):
  1) request OTP   GET  {CIAM}realms/xl-ciam/auth/otp?contact=&contactType=&alternateContact=
  2) validate OTP  POST {CIAM}realms/xl-ciam/protocol/openid-connect/token
                        form grant_type=password&contact&contactType&code&scope=openid
                        headers ax-request-at + ax-api-signature (HMAC-SHA256/hmacKey, base64)
  3) balance       POST {API}packages/balance-and-credit  (Authorization: Bearer <id_token>)
                        response {xdata,xtime} -> AES-256-CBC decrypt (see _decrypt)

Session reuse + OTP are identical shape to bima_balance so server.py can plug it in.
Usage:
    python3 scripts/myxl_balance.py 628xxxx --action register   # sends OTP, prompts
    python3 scripts/myxl_balance.py 628xxxx                     # reuse stored session
"""
import base64
import hashlib
import hmac
import json
import os
import sys
import time
import uuid
from datetime import datetime, timedelta
from pathlib import Path

_HOME = os.environ.setdefault(
    "MYXL_HOME", os.path.join(os.path.expanduser("~"), ".myxl"))
sys.path.insert(0, str(Path(__file__).resolve().parent))
import bima_balance as bm  # noqa: E402  (Vault, Http, normalize_msisdn, _jwt_exp)

# --- app constants (env-overridable; embedded app values, same pattern as bima) ---
CIAM_BASE = os.environ.get("MYXL_CIAM_BASE", "https://gede.ciam.xlaxiata.co.id/")
API_BASE = os.environ.get("MYXL_API_BASE", "https://api.myxl.xlaxiata.co.id/api/v8/")
CLIENT_ID = os.environ.get("MYXL_CLIENT_ID", "db80b743-bcb9-4c2c-b55e-5609743f0ce4")
CLIENT_SECRET = os.environ.get("MYXL_CLIENT_SECRET", "3dUJ8XUhX4Sj0Dp449bN1fu82tLzUgOf")
HMAC_KEY = os.environ.get("MYXL_HMAC_KEY", "18b4d589826af50241177961590e6693")
API_KEY = os.environ.get("MYXL_API_KEY", "vT8tINqHaOxXbGE7eOWAhA==")
ENC_KEY = os.environ.get("MYXL_ENC_KEY", "880d8e7e9b4b787aa50a3917b09fc0ec")
HMAC512_KEY = os.environ.get("MYXL_HMAC512_KEY",
    "3c855bf981ac7bc595925d1158a8c590d04a19520d503eea309fe81d59b9f1efca05a45f753a570653a481c3eb9a9d8f82c23aa302e01ca4095c5519f44b6438")

APP_UA = "myXL/9.6.0 (Android 13)"   # required by myXL API gateway (UA must start with "myXL")
CIAM_UA = "okhttp/4.12.0"             # CIAM token endpoint binds refresh_token to this UA
PROVIDER_MYXL = {
    "base_default": API_BASE,
    "ciam_base": CIAM_BASE,
    "client_id": CLIENT_ID,
    "client_secret": CLIENT_SECRET,
    "hmac_key": HMAC_KEY,
    "api_key": API_KEY,
    "enc_key": ENC_KEY,
    "hmac512_key": HMAC512_KEY,
}


# --- crypto helpers -----------------------------------------------------------
def _hashkey(s: str) -> str:
    return hashlib.sha256(s.encode()).hexdigest()


def _ts(dt: datetime) -> str:
    """Java SimpleDateFormat "yyyy-MM-dd'T'HH:mm:ss.SSSZ"."""
    return dt.strftime("%Y-%m-%dT%H:%M:%S.") + f"{dt.microsecond // 1000:03d}" + dt.strftime("%z")


def _signature(grant_type: str, contact_type: str, contact: str, code: str,
               scope: str = "openid", skew: int = 0):
    """(ax-request-at, ax-api-signature) exactly as d5/m.a + x4/m.c."""
    now = datetime.now().astimezone() + timedelta(seconds=skew)
    req_at = _ts(now)
    expiry = _ts(now + timedelta(seconds=300))
    msg = expiry + grant_type + contact_type + contact + code + scope
    sig = base64.b64encode(
        hmac.new(HMAC_KEY.encode(), msg.encode(), hashlib.sha256).digest()).decode()
    return req_at, sig


def _fingerprint(msisdn: str) -> str:
    """x4/n.b + x4/m.e: device string, AES-256-CBC (key=hmacKey, IV=0), Base64."""
    from cryptography.hazmat.primitives.ciphers import Cipher, algorithms, modes
    from cryptography.hazmat.primitives import padding
    fp = "|".join([
        "Xiaomi", "M2101K7AG", "en", "1080x2400", "WIB", "0.0.0.0",
        "1.0", "Android 13", msisdn or "6287800000000",
    ])
    key = HMAC_KEY.encode()[:32]
    padder = padding.PKCS7(128).padder()
    data = padder.update(fp.encode()) + padder.finalize()
    enc = Cipher(algorithms.AES(key), modes.CBC(b"\x00" * 16)).encryptor()
    ct = enc.update(data) + enc.finalize()
    return base64.b64encode(ct).decode()


def _decrypt(text: str):
    """Decrypt {xdata, xtime} -> dict; passthrough if already plain."""
    doc = json.loads(text)
    if not isinstance(doc, dict) or "xdata" not in doc:
        return doc
    from cryptography.hazmat.primitives.ciphers import Cipher, algorithms, modes
    xdata, xtime = doc["xdata"], doc["xtime"]
    ct = base64.urlsafe_b64decode(xdata + "=" * (-len(xdata) % 4))
    key = _hashkey(ENC_KEY).encode()[:32]
    iv = _hashkey(str(xtime)).encode()[:16]
    dec = Cipher(algorithms.AES(key), modes.CBC(iv)).decryptor()
    pt = dec.update(ct) + dec.finalize()
    pt = pt[:-pt[-1]]                      # PKCS5 unpad
    return json.loads(pt.decode())


def _encrypt(obj) -> str:
    """RequestBodyEncryptionInterceptor: AES-256-CBC, base64url -> {"xdata","xtime"} (ms)."""
    from cryptography.hazmat.primitives.ciphers import Cipher, algorithms, modes
    from cryptography.hazmat.primitives import padding
    ts = int(time.time() * 1000)
    key = _hashkey(ENC_KEY).encode()[:32]
    iv = _hashkey(str(ts)).encode()[:16]
    padder = padding.PKCS7(128).padder()
    pt = json.dumps(obj, separators=(",", ":")).encode()
    data = padder.update(pt) + padder.finalize()
    enc = Cipher(algorithms.AES(key), modes.CBC(iv)).encryptor()
    ct = enc.update(data) + enc.finalize()
    return json.dumps({"xdata": base64.urlsafe_b64encode(ct).decode(), "xtime": ts},
                      separators=(",", ":"))


# --- client -------------------------------------------------------------------
class MyXLClient:
    """Duck-types bima_balance.BimaClient for server.py."""

    def __init__(self, vault, msisdn: str, action: str = "login", cfg=None):
        self.vault = vault
        self.msisdn = bm.normalize_msisdn(msisdn)
        self.token = None
        self.fresh = False
        self.cfg = cfg or {}
        self.hmac512 = self.cfg.get("hmac512_key", HMAC512_KEY)
        self.http = bm.Http(API_BASE, APP_UA, 45.0)
        self.ciam_http = bm.Http(CIAM_BASE, CIAM_UA, 45.0)

    def _api_headers(self, method: str, path: str, extra: dict = None) -> dict:
        from urllib.parse import urlparse
        p = urlparse(path).path.lstrip("/") if "://" in path else path.lstrip("/")
        ts = str(int(time.time()))
        key = ";".join([self.hmac512, self.token, method.upper(), p, ts])
        msg = self.token + ";" + ts + ";"
        sig = hmac.new(key.encode(), msg.encode(), hashlib.sha512).hexdigest()
        h = {
            "Authorization": "Bearer " + self.token,
            "x-api-key": API_KEY,
            "Accept": "application/json",
            "X-REQUEST-ID": str(uuid.uuid4()),
            "X-REQUEST-AT": _ts(datetime.now().astimezone()),
            "X-VERSION-APP": "9.6.0",
            "x-hv": "v3",
            "x-signature-time": ts,
            "x-signature": sig,
            "User-Agent": APP_UA,
            "Content-Type": "application/json",
        }
        if extra:
            h.update(extra)
        return h

    # headers shared by the CIAM client (c5/a Basic + c5/b device headers)
    def _basic(self) -> str:
        raw = f"{CLIENT_ID}:{CLIENT_SECRET}".encode()
        return "Basic " + base64.b64encode(raw).decode()

    def _device_headers(self) -> dict:
        dev = self.vault.device_id()
        return {
            "ax-device-id": dev,
            "ax-request-id": str(uuid.uuid4()),
            "ax-request-device": "Xiaomi",
            "ax-request-device-model": "M2101K7AG",
            "ax-fingerprint": _fingerprint(self.msisdn),
        }

    def request_guest_token(self) -> str:
        return ""   # CIAM has no guest token

    def send_otp(self) -> str:
        contact_type = "EMAIL" if "@" in self.msisdn else "SMS"
        alt = "true" if contact_type == "EMAIL" else "false"
        url = (f"{CIAM_BASE}realms/xl-ciam/auth/otp"
               f"?contact={self.msisdn}&contactType={contact_type}&alternateContact={alt}")
        headers = {"Authorization": self._basic(),
                   "Accept": "application/json",
                   "Content-Type": "application/json",
                   "ax-request-at": _ts(datetime.now().astimezone()),
                   **self._device_headers()}
        st, txt = self.ciam_http.get(url, headers)
        if st not in (200, 201):
            raise RuntimeError(f"request-otp {st}: {txt[:200]}")
        return ""   # CIAM validate is stateless (contact+code); no transid

    def validate_otp(self, transid: str, otp: str) -> str:
        contact_type = "EMAIL" if "@" in self.msisdn else "SMS"
        req_at, sig = _signature("password", contact_type, self.msisdn, otp.strip())
        headers = {"Authorization": self._basic(),
                   "Accept": "application/json",
                   "Content-Type": "application/x-www-form-urlencoded",
                   "ax-request-at": req_at, "ax-api-signature": sig,
                   **self._device_headers()}
        body = ("grant_type=password&contact=" + self.msisdn +
                "&contactType=" + contact_type +
                "&code=" + otp.strip() + "&scope=openid")
        st, txt = self.ciam_http.post(CIAM_BASE + "realms/xl-ciam/protocol/openid-connect/token",
                                 body, headers)
        if st != 200:
            raise RuntimeError(f"token {st}: {txt[:200]}")
        tok = json.loads(txt)
        # app uses the id_token as the API bearer (CiamSession.e -> entity.d -> idToken)
        bearer = tok.get("id_token") or tok.get("access_token")
        self.token = bearer
        self.vault.put_session(self.msisdn, {
            "token": bearer,
            "id_token": tok.get("id_token"),
            "access_token": tok.get("access_token"),
            "refresh_token": tok.get("refresh_token"),
            "exp": bm._jwt_exp(tok.get("access_token") or bearer),
        })
        return bearer

    def refresh(self) -> str:
        """grant_type=refresh_token -> new id_token (no OTP needed)."""
        sess = self.vault.session(self.msisdn) or {}
        rt = sess.get("refresh_token")
        if not rt:
            raise RuntimeError("no refresh_token")
        headers = {"Authorization": self._basic(),
                   "Accept": "application/json",
                   "Content-Type": "application/x-www-form-urlencoded",
                   "ax-request-at": _ts(datetime.now().astimezone()),
                   **self._device_headers()}
        body = "grant_type=refresh_token&refresh_token=" + rt
        st, txt = self.ciam_http.post(
            CIAM_BASE + "realms/xl-ciam/protocol/openid-connect/token", body, headers)
        if st != 200:
            raise RuntimeError(f"refresh {st}: {txt[:200]}")
        tok = json.loads(txt)
        bearer = tok.get("id_token") or tok.get("access_token")
        self.token = bearer
        self.vault.put_session(self.msisdn, {
            "token": bearer,
            "id_token": tok.get("id_token"),
            "access_token": tok.get("access_token"),
            "refresh_token": tok.get("refresh_token") or rt,
            "exp": bm._jwt_exp(tok.get("access_token") or bearer),
        })
        return bearer

    def get_balance(self) -> dict:
        # auto-refresh if token missing/expired
        if not self.token:
            sess = self.vault.session(self.msisdn) or {}
            self.token = sess.get("token")
        sess = self.vault.session(self.msisdn) or {}
        exp = sess.get("exp") or 0
        if self.token and exp and time.time() > int(exp) - 60:
            try:
                self.refresh()
            except Exception:
                pass
        if not self.token:
            raise RuntimeError("no token")
        # 1) balance + credit
        url = API_BASE + "packages/balance-and-credit"
        h = self._api_headers("POST", "api/v8/packages/balance-and-credit")
        st, txt = self.http.post(url, _encrypt({"lang": "id", "is_enterprise": False}), h)
        if st != 200:
            raise RuntimeError(f"balance {st}: {txt[:200]}")
        bal = _decrypt(txt)
        # 2) quota summary
        url2 = API_BASE + "packages/quota-summary"
        h2 = self._api_headers("POST", "api/v8/packages/quota-summary")
        st2, txt2 = self.http.post(url2, "", h2)
        q = _decrypt(txt2) if st2 == 200 else {}
        # 3) quota details (remaining quotas)
        url3 = API_BASE + "packages/quota-details"
        h3 = self._api_headers("POST", "api/v8/packages/quota-details")
        st3, txt3 = self.http.post(url3, "", h3)
        qd = _decrypt(txt3) if st3 == 200 else {}
        # normalize to bima-ish shape
        baldata = (bal.get("data") or {}) if isinstance(bal, dict) else {}
        b = baldata.get("balance") or {}
        qdata = (q.get("data") or {}) if isinstance(q, dict) else {}
        quota = qdata.get("quota", {}).get("data", {}) if isinstance(qdata.get("quota"), dict) else {}
        qd_data = (qd.get("data") or {}) if isinstance(qd, dict) else {}
        def _fmt_ts(ts):
            try:
                return datetime.fromtimestamp(int(ts)).strftime("%Y-%m-%d")
            except Exception:
                return None
        total_b = int(quota.get("total") or 0)
        rem_b = int(quota.get("remaining") or 0)
        used_b = total_b - rem_b if total_b >= rem_b else 0
        unlimited = bool(quota.get("is_unlimited"))
        return {
            "balance": b.get("remaining"),
            "quota": total_b // 1024,
            "unit": "KB",
            "used": used_b // 1024,
            "unlimited": unlimited,
            "active_until": _fmt_ts(b.get("expired_at")),
            "grace_until": _fmt_ts(baldata.get("grace_end_date")),
            "bonstri": None,
            "subscription_status": baldata.get("subscription_status"),
        }


# --- self-check (crypto round-trip + timestamp format) ------------------------
def _selftest():
    import re
    now = datetime.now().astimezone()
    assert re.fullmatch(r"\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{3}[+-]\d{4}", _ts(now)), _ts(now)
    from cryptography.hazmat.primitives.ciphers import Cipher, algorithms, modes
    payload = {"hello": "world", "n": 1}
    xtime = 1762650000
    key = _hashkey(ENC_KEY).encode()[:32]
    iv = _hashkey(str(xtime)).encode()[:16]
    raw = json.dumps(payload).encode()
    pad = 16 - len(raw) % 16
    raw += bytes([pad]) * pad
    enc = Cipher(algorithms.AES(key), modes.CBC(iv)).encryptor()
    ct = enc.update(raw) + enc.finalize()
    blob = {"xdata": base64.urlsafe_b64encode(ct).decode().rstrip("="), "xtime": xtime}
    assert _decrypt(json.dumps(blob)) == payload
    req_at, sig = _signature("password", "SMS", "628123", "123456")
    assert req_at and sig
    print("selftest ok")


def main(argv=None):
    argv = list(sys.argv[1:] if argv is None else argv)
    if argv and argv[0] == "--selftest":
        _selftest()
        return 0
    action = "login"
    watch = None
    otp_arg = None
    send_only = False
    if "--action" in argv:
        i = argv.index("--action")
        action = argv[i + 1]
        del argv[i:i + 2]
    if "--otp" in argv:
        i = argv.index("--otp")
        otp_arg = argv[i + 1]
        del argv[i:i + 2]
    if "--send-only" in argv:
        send_only = True
        argv.remove("--send-only")
    if "--watch" in argv:
        i = argv.index("--watch")
        watch = float(argv[i + 1])
        del argv[i:i + 2]
    if not argv:
        print(__doc__)
        return 2
    msisdn = argv[0]
    vault = bm.Vault(_HOME)
    client = MyXLClient(vault, msisdn)
    sess = vault.session(client.msisdn)
    if send_only:
        client.request_guest_token()
        client.send_otp()
        print("otp sent to", client.msisdn)
        return 0
    if action == "register" or not sess:
        if otp_arg is None:                      # no --otp => send, then prompt
            client.request_guest_token()
            client.send_otp()
            otp = input("OTP: ").strip()
        else:                                    # --otp given => OTP already sent
            otp = otp_arg
        token = client.validate_otp("", otp)
        print("login ok, session stored")
    else:
        token = sess.get("token")
        client.token = token
        print("reusing stored session", file=sys.stderr)
    result = client.get_balance()
    print(json.dumps(result, indent=2, ensure_ascii=False))
    if watch:
        while True:
            time.sleep(watch)
            print(json.dumps(client.get_balance(), indent=2, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    sys.exit(main() or 0)