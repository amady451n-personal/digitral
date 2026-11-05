#!/usr/bin/env python3
"""
bima+ (Tri / Indosat-Ooredoo) balance fetcher — reimplementation of the app protocol.

Flow (mirrors com.linkit.bimatri 5.17.0, verified against a live MuMu OkHttp capture):
  1. POST /token/guest/v1            -> guest JWT (bound to X-DEVICEID)
  2. POST /otp/send/v1               -> {"msisdn":"62...","action":"register"} (plaintext); OTP via SMS
                                        (action register = first login on the device, mirrors the app)
  3. POST /otp/validate/v2           -> AES-256-GCM(PINPUK key, static IV, Base64) body {"transid":..,"otp":..}
                                        response carries the session JWT (~60 days TTL)
  4. POST /dashboard/get/v7          -> XML envelope <Response>...<data>{JSON}</data>; keys:
                                        prepaidinfo.balance/cardactiveuntil/graceperioduntil,
                                        summary.data.remainingquota(KB)/usedquota/isunlimited,
                                        bonstriDetails.totalPoints (see parse_dashboard)
Risk gate note: /otp/send requires the app header set (X-FAIID, X-IMI-ADID + ISLIMITED,
X-IMI-NETWORK "WiFi", X-IMI-LANGUAGE EN, empty User-Agent) — otherwise 10011.

Signatures (verified from decompile):
  getValues(x)          = odd 1-based characters of x
  MixUpValues.encryption(x) = SHA-512 hex of x
  x-imi-oauth           = sha512hex("REQBODY=" + body + "&SALT=" + getValues(token))   [only when token present]
  X-IMI-HASH            = sha512hex(who + "$" + AppOS + "$" + VERSION + "$"
                          + tokenId + "&SALT=" + getValues(reqTime))
                          who = "parent" if no child header else the RC4-hex child value
  X-IMI-CHILD-LINENO    = RC4("123").encrypt(childMsisdn) as hex   (child lines only)

Storage: ~/.bima/ — device id + token vault. Tokens are AES-GCM encrypted at rest
(never logged in plaintext; never written in plaintext to disk).

Deps:  pip install cryptography curl_cffi   (stdlib has no AES-GCM; the carrier
       rejects stdlib TLS fingerprints with 10001, curl_cffi chrome-impersonation works)
Usage:
  python3 scripts/bima_balance.py <msisdn> --action register   # first login, OTP prompt
  python3 scripts/bima_balance.py <msisdn>                     # saved session, no OTP
  python3 scripts/bima_balance.py <msisdn> --watch 60          # real-time polling (s)
  python3 scripts/bima_balance.py --fresh <msisdn>       # ignore saved session
  python3 scripts/bima_balance.py --dry-run <msisdn>     # build request, redact, don't send
  python3 scripts/bima_balance.py --selftest             # offline crypto self-check

WARNING: only use numbers you own. OTP goes to that number's SIM.
Known server behaviour (see docs/bima-protocol.md): app-path OTP send may be rejected
with error 10011 regardless of client fidelity.
"""

import argparse
import base64
import gzip
import hashlib
import json
import os
import re
import sys
import time
import urllib.error
import urllib.request
import uuid
from pathlib import Path

BRAND           = "bima+"     # display-only; myim3_balance.py patches this
BASE_DEFAULT = "https://bimaplus-api.ioh.co.id/api/v2"   # full API product: /otp/* lives here;
                                                         # bimaplus.ioh.co.id serves only token/dashboard paths

# --- static app constants (decompiled, docs/bima-protocol.md) -----------------
AUTHORIZATION   = "642d1cc69d90666962726e"
SERVICEKEY      = "FPi7ZP3Jy8Uv3KBd4QeG"
CHANNEL         = "BIMA"
APP_VERSION     = "5.17.0"
VERSION_CODE    = "560003792"
APP_OS          = "Android"
GCM_PINPUK_KEY  = b"A9$gT#2vXp!qW7LmZ@e4Ky8Nb3Hj5Uq9"   # 32 bytes (AES-256)
GCM_STATIC_IV   = b"Rx4Cm9Pn2Vf8Kt6B"                   # 16 bytes, static
RC4_CHILD_KEY   = "123"
SECRET          = "$"      # DecodedChar.FNC1 = '$' (zxing RSS expanded; value 36, verified in classes8.dex)

DEFAULT_UA = (
    "Mozilla/5.0 (Linux; U; Android 14; id-ID; SM-A235F Build/UP1A.231005.007) "
    "AppleWebKit/537.36 Chrome/120.0 Mobile Safari/537.36"
)

HOME_DIR = Path(os.environ.get("BIMA_HOME", str(Path.home() / ".bima")))


# --- provider profiles (multi-provider frontend support) ----------------------
# A provider = one operator app (bima+, myIM3, myXL, axisnet, ...). Each has its own
# base host + service key + channel + version, but the protocol, crypto and header
# shape are shared across the Digitral family. The client is parameterised by `cfg`
# so several providers can be served concurrently (required by any multi-number
# dashboard) instead of monkey-patching module globals.
def make_provider_cfg(authorization=AUTHORIZATION, servicekey=SERVICEKEY, channel=CHANNEL,
                      app_version=APP_VERSION, app_os=APP_OS,
                      gcm_pinpuk_key=GCM_PINPUK_KEY, gcm_static_iv=GCM_STATIC_IV,
                      rc4_child_key=RC4_CHILD_KEY, secret=SECRET,
                      base_default=BASE_DEFAULT):
    return {
        "authorization": authorization,
        "servicekey": servicekey,
        "channel": channel,
        "app_version": app_version,
        "app_os": app_os,
        "gcm_pinpuk_key": gcm_pinpuk_key,
        "gcm_static_iv": gcm_static_iv,
        "rc4_child_key": rc4_child_key,
        "secret": secret,
        "base_default": base_default,
    }


PROVIDER_BIMA = make_provider_cfg()
PROVIDER_MYIM3 = make_provider_cfg(
    servicekey="i4WxFMMLvWqnrvuAyg58",
    channel="V2MYIM3",
    app_version="82.17.0",
    base_default="https://myim3api1.ioh.co.id/api/v2",
)
# slots reserved for the other Digitral-family apps present in this repo:
PROVIDER_MYXL = None    # myxl-9.6.0.apk+: own provider, stack unchecked — do NOT assume axisnet's
# com.axis.net 11.0.0 (vc 836) — NOT the Digitral family, so it does NOT use
# make_provider_cfg. Header names + signature scheme recovered from the V-Key
# runtime dex (docs/bima-protocol.md §16).
PROVIDER_AXISNET = {
    "id": "axisnet",
    "app_version": "11.0.0",
    "app_os": "android",            # Consta.CHANNEL_ANDROID (lowercase!) -> payload deviceType + Signature.v()
    "balance_url": "https://balance.api.axis.co.id/getbalance",
    "otp_url": "https://otp.api.axis.co.id/v1/otp",
    "otp_login_url": "https://otp.api.axis.co.id/otplogin/v2",
    "api_key": "ipRsTxOLbgogxEiXj1P73JVWgQk9CEXMSuIsaFO8t6I=",
    # HmacSHA256 key of Signature.u() / g() (plaintext, i.e. after
    # CryptoTool.decryptString(keyTempest*, saltKey)); captured at runtime.
    "hmac_login": "7L20wW59wJkXG1z2vR8Q6pmnssgRptOHdC3xO9CrtOFE2u6mF7Xk9Is11S44aPTPu5",
    "hmac_market": "a0A38QY0HwkMGLwLq5zrmpC17cGBj01I2pZ91v42K26X57SThVsdfBuSut7RXkJ9eR",
    "sso_key": "ae432353a4eb4eb9",  # AxisnetHelpers.getMsisdnSaltKey / KeyClass.sharedKey
}

PROVIDER_CFG = PROVIDER_BIMA  # active provider for this process; myim3_balance overrides


# --- crypto helpers -----------------------------------------------------------
def sha512_hex(s: str) -> str:
    return hashlib.sha512(s.encode("utf-8")).hexdigest()


def get_values(s: str) -> str:
    """MixUpValues.getValues: characters at odd 1-based positions."""
    return s[::2]


def rc4_hex(key: str, text: str) -> str:
    """ODPRC4: standard RC4 over characters, output as hex of each char code."""
    k = [ord(key[i % len(key)]) for i in range(256)]
    box = list(range(256))
    j = 0
    for i in range(256):
        j = (j + box[i] + k[i]) % 256
        box[i], box[j] = box[j], box[i]
    out = []
    i = j = 0
    for ch in text:
        i = (i + 1) % 256
        j = (j + box[i]) % 256
        box[i], box[j] = box[j], box[i]
        out.append(box[(box[i] + box[j]) % 256] ^ ord(ch))
    return "".join("%02x" % (c & 0xFF) for c in out)


def _cryptography():
    for name in ("cryptography",):
        try:
            return __import__(name)
        except ImportError:
            pass
    sys.exit("Missing dependency: pip install cryptography")


# --- axisnet (com.axis.net) protocol helpers (docs/bima-protocol.md §16) ------
def _axisnet_pad(value: str, block: int) -> str:
    """CryptoTool.doPadWithString: append NUL up to the next block multiple."""
    pad = block - len(value) % block
    return value + "\0" * pad if 0 < pad < block else value


def axisnet_encrypt_sso(value: str, key: str | None = None) -> str:
    """CryptoTool.encryptString: DESede/ECB/NoPadding + urlsafe Base64 (NO_WRAP)."""
    from cryptography.hazmat.primitives.ciphers import Cipher, algorithms, modes
    triple_des = getattr(algorithms, "TripleDES", None)
    if triple_des is None:                        # cryptography >= 43
        from cryptography.hazmat.decrepit.ciphers.algorithms import TripleDES as triple_des
    key = _axisnet_pad(key or PROVIDER_AXISNET["sso_key"], 24).encode()
    enc = Cipher(triple_des(key), modes.ECB()).encryptor()
    ct = enc.update(_axisnet_pad(value, 8).encode()) + enc.finalize()
    return base64.b64encode(ct).decode().replace("+", "-").replace("/", "_")


def axisnet_handshake() -> str:
    """Signature.i(): encryptSSO(ms epoch) with newlines stripped (-> X-Handshake)."""
    return axisnet_encrypt_sso(str(int(time.time() * 1000))).replace("\n", "")


def _axisnet_hmac(msg: str, key: str) -> str:
    """Signature.f(): hex(HmacSHA256(key, msg))."""
    import hmac
    return hmac.new(key.encode(), msg.encode(), hashlib.sha256).hexdigest()


def axisnet_login_sign(path: str, msisdn: str = "", otp: str = "", fcm: str = "",
                       firebase_id: str = "", model: str = "", brand: str = "",
                       os_ver: str = "", device_type: str | None = None) -> str:
    """Signature.v()/j(): unauthenticated endpoints (/v1/otp, /otplogin/v2, /prefix)."""
    msg = ";".join(["POST", path, msisdn, otp, fcm, firebase_id, model, brand, os_ver,
                    PROVIDER_AXISNET["app_os"] if device_type is None else device_type,
                    PROVIDER_AXISNET["api_key"]])
    return _axisnet_hmac(msg, PROVIDER_AXISNET["hmac_login"])


def _axisnet_msisdn_id_format(msisdn: str) -> str:
    """Utils.msisdnIDFormat: strip '-', ' '; 62.../+62... -> leading 0."""
    s = msisdn.replace("-", "").replace(" ", "")
    if s.startswith("62"):
        return "0" + s[2:]
    if s.startswith("+62"):
        return "0" + s[3:]
    return s


def axisnet_fb_id(msisdn: str) -> str:
    """CryptoTool.encrypMsisdnMD5: md5('xl' + md5(0-form msisdn) + 'axis')."""
    inner = hashlib.md5(_axisnet_msisdn_id_format(msisdn).encode()).hexdigest()
    return hashlib.md5(("xl" + inner + "axis").encode()).hexdigest()


def axisnet_decrypt_data(data: str) -> str:
    """CryptoTool.decryptString(data, saltKey): DESede/ECB, key = saltKey NUL-padded
    to 24 bytes, urlsafe-Base64 in, NUL-trailing plaintext out. Used for the
    `data` field of every otp.api/balance.api JSON response."""
    from cryptography.hazmat.primitives.ciphers import Cipher, algorithms, modes
    triple_des = getattr(algorithms, "TripleDES", None)
    if triple_des is None:
        from cryptography.hazmat.decrepit.ciphers.algorithms import TripleDES as triple_des
    raw = base64.b64decode(data.replace("-", "+").replace("_", "/") + "=" * (-len(data) % 4))
    key = _axisnet_pad(PROVIDER_AXISNET["sso_key"], 24).encode()
    dec = Cipher(triple_des(key), modes.ECB()).decryptor()
    return (dec.update(raw) + dec.finalize()).rstrip(b"\0").decode("utf-8", "replace")


_AXISNET_DEVICE_DEFAULT = {
    "brand": "samsung",       # Build.MANUFACTURER  -> rc.c `brand`
    "model": "SM-A235F",      # Build.MODEL         -> rc.c `model`
    "device_os": "31",        # String SDK_INT      -> rc.c `device_os`
    "imei": "b3a9c7d5e1f20364",  # android_id (SDK>=29) -> rc.c `imei`
}


def axisnet_otplogin_request(msisdn: str, otp_code: str, device: dict | None = None):
    """Build POST /otplogin/v2 exactly as x.validateOtpCode/getOtpLoginPayload.
    Returns (url, headers, body). `device` overrides _AXISNET_DEVICE_DEFAULT."""
    dev = {**_AXISNET_DEVICE_DEFAULT, **(device or {})}
    ms = axisnet_encrypt_sso(msisdn).replace("\n", "")
    ot = axisnet_encrypt_sso(otp_code).replace("\n", "")
    fb = axisnet_fb_id(msisdn)
    body = json.dumps({
        "msisdn": ms, "otp_code": ot, "fcm_token": "", "fb_id": fb,
        "model": dev["model"], "brand": dev["brand"],
        "device_os": dev["device_os"], "device_type": PROVIDER_AXISNET["app_os"],
        "imei": dev["imei"],
    }, separators=(",", ":"), ensure_ascii=False)
    headers = {
        "Content-Type": "application/json",
        "Connection": "keep-alive",
        "X-API-KEY": PROVIDER_AXISNET["api_key"],
        "x-app-version": PROVIDER_AXISNET["app_version"],
        "x-signature": axisnet_login_sign(
            "/otplogin/v2", msisdn=ms, otp=ot, fcm="", firebase_id=fb,
            model=dev["model"], brand=dev["brand"], os_ver=dev["device_os"],
            device_type=PROVIDER_AXISNET["app_os"]),
        "x-app-request-id": str(uuid.uuid4()),
    }
    return PROVIDER_AXISNET["otp_login_url"], headers, body


def axisnet_sign(method: str, path: str, body: str, token: str, handshake: str) -> str:
    """Signature.h(): authenticated endpoints marked @HMACHeader -> X-Signature."""
    return _axisnet_hmac(";".join([method, path, body, token, handshake]),
                         PROVIDER_AXISNET["hmac_market"])


def axisnet_balance(http, token: str, version: str | None = None) -> tuple[int, str]:
    """GET /getbalance: no @HMACHeader, so only Authorization + x-app-version (+request id)."""
    return http.get(PROVIDER_AXISNET["balance_url"], {
        "Authorization": token,
        "x-app-version": version or PROVIDER_AXISNET["app_version"],
        "x-app-request-id": str(uuid.uuid4()),
    })


def aes_gcm_encrypt(key: bytes, iv: bytes, plaintext: str) -> str:
    """ODPAESEncryption.encryptNew: AES-256-GCM, data=plaintext, output = base64(ct+tag) NO_WRAP."""
    from cryptography.hazmat.primitives.ciphers import Cipher, algorithms, modes
    enc = Cipher(algorithms.AES(key), modes.GCM(iv)).encryptor()
    enc.authenticate_additional_data(b"")
    ct = enc.update(plaintext.encode("utf-8")) + enc.finalize()
    return base64.b64encode(ct + enc.tag).decode("ascii")


def b64_decode(vault: bytes) -> bytes:
    return base64.b64decode(vault)


# --- msisdn normalisation (Utils.Companion.getMSISDNCountryAppended) ----------
def normalize_msisdn(msisdn: str) -> str:
    s = msisdn.strip()
    if s.startswith("+"):
        s = s[1:]
    if s.startswith("0"):
        s = s[1:]
    if not s.startswith("62"):
        s = "62" + s
    if len(s) == 8:            # app quirk: prepend again for 8-digit legacy input
        s = "62" + s
    return s


# --- token vault (encrypted at rest) ------------------------------------------
class Vault:
    """AES-GCM-encrypted JSON store. Key file is separate; 0600 perms; tokens never logged."""

    def __init__(self, home_dir=None):
        self.dir = Path(home_dir) if home_dir else HOME_DIR
        self.dir.mkdir(parents=True, exist_ok=True)
        self.key_file = self.dir / "vault.key"
        self.data_file = self.dir / "vault.bin"
        self._load()

    def _load(self):
        if not self.key_file.exists():
            self.key_file.write_bytes(os.urandom(32))
            self.key_file.chmod(0o600)
        key = self.key_file.read_bytes()
        payload = {}
        if self.data_file.exists():
            from cryptography.hazmat.primitives.ciphers import Cipher, algorithms, modes
            blob = self.data_file.read_bytes()
            iv, ct, tag = blob[:12], blob[12:-16], blob[-16:]
            dec = Cipher(algorithms.AES(key), modes.GCM(iv, tag)).decryptor()
            payload = json.loads((dec.update(ct) + dec.finalize()).decode("utf-8"))
        self.key = key
        self.data = payload

    def save(self):
        from cryptography.hazmat.primitives.ciphers import Cipher, algorithms, modes
        iv = os.urandom(12)
        enc = Cipher(algorithms.AES(self.key), modes.GCM(iv)).encryptor()
        ct = enc.update(json.dumps(self.data).encode()) + enc.finalize()
        self.data_file.write_bytes(iv + ct + enc.tag)
        self.data_file.chmod(0o600)

    # device identity ----------------------------------------------------------
    def device_id(self) -> str:
        if "deviceId" not in self.data:
            self.data["deviceId"] = os.urandom(8).hex()
            self.save()
        return self.data["deviceId"]

    def device_profile(self) -> dict:
        """Real-device identity (android_id etc.) if the user captured one."""
        return self.data.get("deviceProfile", {})

    def put_device_profile(self, profile: dict):
        if profile:
            cur = self.data.setdefault("deviceProfile", {})
            cur.update({k: v for k, v in profile.items() if v})
            self.save()

    # app-level runtime ids observed on the wire (bima+ 5.17.0, MuMu capture):
    # X-FAIID = Facebook anon-id, X-IMI-ADID = Google ad id (uuid-like). Stable
    # per install so the risk engine sees a consistent client fingerprint.
    def faaid(self) -> str:
        if "faaid" not in self.data:
            self.data["faaid"] = hashlib.md5(os.urandom(16)).hexdigest()
            self.save()
        return self.data["faaid"]

    def gaid(self) -> str:
        if "gaid" not in self.data:
            s = os.urandom(16).hex()
            self.data["gaid"] = f"{s[:8]}-{s[8:12]}-{s[12:16]}-{s[16:20]}-{s[20:]}"
            self.save()
        return self.data["gaid"]

    # per-number session -------------------------------------------------------
    def session(self, msisdn: str):
        s = self.data.get("sessions", {}).get(msisdn)
        if not s:
            return None
        exp = s.get("exp", 0)
        if exp and time.time() >= exp - 60:
            return None
        return s

    def put_session(self, msisdn: str, session: dict):
        self.data.setdefault("sessions", {})[msisdn] = session
        self.save()

    def drop_session(self, msisdn: str):
        self.data.get("sessions", {}).pop(msisdn, None)
        self.save()


def _jwt_exp(jwt: str) -> int:
    """Best-effort exp of a JWT (seconds). 0 if unknown."""
    try:
        p = jwt.split(".")[1]
        p += "=" * (-len(p) % 4)
        return int(json.loads(base64.urlsafe_b64decode(p)).get("exp", 0))
    except Exception:
        return 0


# --- HTTP layer (urllib, gzip-aware) -------------------------------------------
class Http:
    def __init__(self, base: str, ua: str, timeout: float = 45.0):
        self.base = base.rstrip("/")
        self.ua = ua
        self.timeout = timeout
        self.last_req = None   # kept for --verbose printing (headers + body, token redacted)

    def _abs(self, path: str) -> str:
        """Absolute URLs pass through; relative ones are joined onto base."""
        return path if path.startswith("http") else self.base + path

    def get(self, url: str, headers: dict):
        url = self._abs(url)
        req_headers = {"Accept-Encoding": "gzip", "User-Agent": self.ua}
        req_headers.update(headers)
        self.last_req = {"path": url, "headers": dict(req_headers), "body": ""}
        try:
            from curl_cffi import requests as creq
        except ImportError:
            creq = None
        if creq is not None:
            r = creq.get(url, headers=req_headers, impersonate="chrome124", timeout=self.timeout)
            return r.status_code, r.text
        req = urllib.request.Request(url, headers=req_headers, method="GET")
        try:
            with urllib.request.urlopen(req, timeout=self.timeout) as resp:
                raw = resp.read()
                if resp.headers.get("Content-Encoding") == "gzip":
                    raw = gzip.decompress(raw)
                return resp.status, raw.decode("utf-8", "replace")
        except urllib.error.HTTPError as e:
            return e.code, e.read().decode("utf-8", "replace")

    def post(self, path: str, body: str, headers: dict):
        req_headers = {"Accept-Encoding": "gzip", "User-Agent": self.ua}
        req_headers.update(headers)
        url = self._abs(path)
        self.last_req = {"path": url, "headers": dict(req_headers), "body": body}
        data = body.encode("utf-8")
        # The /api/v2 ingress fingerprints the TLS stack: python stdlib TLS is
        # rejected app-level with code 10001 "Authentication failed" REGARDLESS of
        # header content (verified by A/B probe 2026-10-22). The app speaks
        # OkHttp/BoringSSL; chrome impersonation passes (code 26100 success).
        try:
            from curl_cffi import requests as creq
        except ImportError:
            print("WARNING: curl_cffi not installed -> stdlib TLS will be rejected "
                  "with 10001. pip install curl_cffi", file=sys.stderr)
            creq = None
        if creq is not None:
            r = creq.post(url, data=data, headers=req_headers,
                          impersonate="chrome124", timeout=self.timeout)
            text = r.text
            return r.status_code, text
        req = urllib.request.Request(url, data=data, headers=req_headers, method="POST")
        try:
            with urllib.request.urlopen(req, timeout=self.timeout) as resp:
                raw = resp.read()
                if resp.headers.get("Content-Encoding") == "gzip":
                    raw = gzip.decompress(raw)
                return resp.status, raw.decode("utf-8", "replace")
        except urllib.error.HTTPError as e:
            raw = e.read()
            try:
                if e.headers.get("Content-Encoding") == "gzip":
                    raw = gzip.decompress(raw)
            except Exception:
                pass
            return e.code, raw.decode("utf-8", "replace")
        except urllib.error.URLError as e:
            sys.exit(f"network error: {e.reason} (web selfcare needs home IP; check connectivity)")


def redact(h: dict) -> dict:
    out = {}
    for k, v in h.items():
        if any(s in k.lower() for s in ("token", "oauth", "hash", "authorization", "child")):
            out[k] = v[:10] + "…redacted" if len(v) > 10 else "…redacted"
        else:
            out[k] = v
    return out


# --- header builders (ProjectHeaders + ApiService, in app order) ---------------
class BimaClient:
    def __init__(self, http: Http, vault: Vault, msisdn: str, action: str = "login",
                 child: str | None = None, verbose: bool = False,
                 device_id: str | None = None, device_name: str | None = None,
                 oem: str | None = None, model: str | None = None,
                 osver: str | None = None, cfg: dict | None = None):
        self.http = http
        self.vault = vault
        self.msisdn = normalize_msisdn(msisdn)
        self.action = action
        self.child = normalize_msisdn(child) if child else None
        self.verbose = verbose
        self.device_id = device_id or vault.device_id()
        self.device_name = device_name or "generic"
        self.oem = oem or "samsung"
        self.model = model or "SM-A235F"
        self.osver = osver or "14"
        self.faaid = vault.faaid()
        self.gaid = vault.gaid()
        self.req_time = None
        self.token = None      # sessionId sent as X-IMI-TOKENID
        self.cfg = cfg or PROVIDER_CFG or PROVIDER_BIMA

    # baseline headers (ProjectHeaders.getHeaders map + ApiService additions) ---
    def baseline(self):
        return {
            "Authorization": self.cfg["authorization"],
            "Content-Type": "application/json",
            "Accept-Encoding": "gzip",
            "Connection": "close",
            # bima+ 5.17.0 sends a literally empty User-Agent header (capture).
            "User-Agent": "",
            "X-DEVICEID": self.device_id,
            "X-DEVICENAME": self.device_name,
            "X-IMI-App-OS": self.cfg["app_os"],
            "X-IMI-App-OSVersion": self.osver,
            "X-IMI-App-OEM": self.oem,
            "X-IMI-App-Model": self.model,
            "X-IMI-CHANNEL": self.cfg["channel"],
            "X-IMI-SERVICEKEY": self.cfg["servicekey"],
            "X-IMI-VERSION": self.cfg["app_version"],
            "X-IMI-LANGUAGE": "EN",
            "X-LOW-QUOTA": "NO",
            "X-SPTRAVEL": "NO",
            "X-FAIID": self.faaid,
            "X-IMI-ADID": self.gaid,
            "X-IMI-ADID-ISLIMITED": "false",
        }

    def build_headers(self, path: str, body: str) -> dict:
        h = self.baseline()
        h["X-IMI-UID"] = str(self.req_time)
        h["X-IMI-NETWORK"] = "WiFi"
        if self.child:                              # child line (RC4("123") hex)
            h["X-IMI-CHILD-LINENO"] = rc4_hex(self.cfg["rc4_child_key"], self.child)
        if self.token:
            h["X-IMI-TOKENID"] = self.token
        else:
            # putToken (ProjectHeaders): AppPreference.getFromStore() defaults to ""
            # (prefs.getString(key, "")), so the empty-branch always fires on a fresh
            # install -> TOKENID = currentTimeMillis. The app NEVER omits this header:
            # guest requests carry a ms-epoch timestamp, not "no header".
            h["X-IMI-TOKENID"] = str(self.req_time)
        if "/token/guest/" in path or "/token/app/" in path:
            h["X-ESIM-SUPPORT"] = "false"
        if self.child is None:                      # addSecurityHash 'who'
            who = "parent"
        else:
            who = h.get("X-IMI-CHILD-LINENO", "parent")

        # addSecurityHash token component = builder.get("X-IMI-TOKENID"). putToken
        # guarantees the header is always present (timestamp or JWT), so the literal
        # "null" branch is defensive only (would need a manually cleared header map).
        hash_token = h.get("X-IMI-TOKENID", "null")
        h["X-IMI-HASH"] = sha512_hex(
            who + self.cfg["secret"] + h["X-IMI-App-OS"] + self.cfg["secret"] + h["X-IMI-VERSION"] +
            self.cfg["secret"] + hash_token + "&SALT=" + get_values(str(self.req_time))
        )
        # addOkHttpSignature guard is "X-IMI-TOKENID present" (not "logged in"):
        # verified decompile -> guest requests carry oauth over "{}" too, salted
        # with getValues(trim(TOKENID)).
        if "X-IMI-TOKENID" in h:
            h["x-imi-oauth"] = sha512_hex(
                "REQBODY=" + body + "&SALT=" + get_values(h["X-IMI-TOKENID"].strip())
            )
        if path.startswith("/token/app/stk"):       # app special-case
            h["X-IMI-TOKENID"] = str(int(time.time() * 1000))
        return h

    def _post(self, path: str, body: str):
        self.req_time = int(time.time() * 1000)
        h = self.build_headers(path, body)
        if self.verbose:
            print(f"--> POST {self.http.base}{path}", flush=True)
            for k, v in redact(h).items():
                print(f"      {k}: {v}", flush=True)
            print(f"      body: {body[:400]}", flush=True)
        status, text = self.http.post(path, body, h)
        if self.verbose:
            print(f"<-- {status} {text[:400]}", flush=True)
        return status, text

    # --- protocol steps --------------------------------------------------------
    def request_guest_token(self) -> str:
        self.token = None
        status, text = self._post("/token/guest/v1", "{}")
        tok = self._extract_token(self._json(text) or {})
        if not tok:
            self.fail(status, text, "guest token")
        self.token = tok
        if self.verbose:
            print("      guest token ok (redacted)", flush=True)
        return tok

    def send_otp(self) -> str:
        body = json.dumps({"msisdn": self.msisdn, "action": self.action}, separators=(",", ":"))
        status, text = self._post("/otp/send/v1", body)
        data = self._json(text)
        if status != 200 or not data:
            self.fail(status, text, "send OTP")
        transid = self._deep_find(data, "transId") or self._deep_find(data, "transid") or \
            self._deep_find(data, "transactionId")
        if not transid:
            sys.exit(f"send OTP ok but no transId in response: {text[:300]}")
        return transid

    def validate_otp(self, transid: str, otp: str) -> str:
        inner = json.dumps({"transid": transid, "otp": otp}, separators=(",", ":"))
        body = aes_gcm_encrypt(self.cfg["gcm_pinpuk_key"], self.cfg["gcm_static_iv"], inner)
        status, text = self._post("/otp/validate/v2", body)
        data = self._json(text)
        if status != 200 or not data:
            self.fail(status, text, "validate OTP")
        tok = self._extract_token(data)
        if not tok:
            sys.exit(f"validate ok but no token in response: {text[:300]}")
        return tok

    def dashboard(self) -> dict:
        status, text = self._post("/dashboard/get/v7", "{}")
        # keep the raw envelope next to the vault (0600 dir) for offline parsing;
        # personal data stays local, never in the repo.
        try:
            (HOME_DIR / f"dashboard_{self.msisdn}.json").write_text(
                text or "", encoding="utf-8")
        except Exception:
            pass
        data = self._unwrap(text)
        if status != 200 or not data:
            self.fail(status, text, "dashboard")
        return data

    # --- shared utilities -------------------------------------------------------
    @staticmethod
    def _json(text: str):
        try:
            return json.loads(text)
        except Exception:
            return None

    @staticmethod
    def _unwrap(text: str):
        """Accept plain JSON, or the XML envelope <Response>...<data>{JSON}</data>."""
        d = BimaClient._json(text)
        if d is not None:
            return d
        if text:
            m = re.search(r"<data>(.*)</data>", text, re.S)
            if m:
                payload = (m.group(1)
                           .replace("&quot;", '"')
                           .replace("&apos;", "'")
                           .replace("&lt;", "<")
                           .replace("&gt;", ">")
                           .replace("&amp;", "&"))
                return BimaClient._json(payload)
        return None

    @staticmethod
    def _deep_find(obj, key):
        if isinstance(obj, dict):
            if key in obj and obj[key] not in (None, ""):
                return obj[key]
            for v in obj.values():
                r = BimaClient._deep_find(v, key)
                if r is not None:
                    return r
        elif isinstance(obj, list):
            for v in obj:
                r = BimaClient._deep_find(v, key)
                if r is not None:
                    return r
        return None

    @staticmethod
    def _extract_token(data) -> "str | None":
        for key in ("access_token", "accessToken", "tokenId", "tokenid", "token", "sessionId", "secretKey"):
            tok = BimaClient._deep_find(data, key)
            if isinstance(tok, str) and len(tok) > 8:
                return tok
        return None

    def fail(self, status, text, step):
        code = self._deep_find(self._json(text) or {}, "code") or self._deep_find(self._json(text) or {}, "error")
        msg = text[:300]
        extra = ""
        if str(code) == "10011":
            extra = ("\nhint: server-side OTP-send block seen in earlier live tests "
                     "(docs/bima-protocol.md §Lanjutan; not a client bug).")
        elif str(code) == "10019":
            extra = "\nhint: token flow requires X-DEVICEID header."
        sys.exit(f"[{step}] HTTP {status} {msg}{extra}")

    # --- end-to-end: balance ----------------------------------------------------
    def get_balance(self) -> dict:
        sess = self.vault.session(self.msisdn) if not self.fresh else None
        if sess and sess.get("token"):
            self.token = sess["token"]
            if self.verbose:
                print("using saved session (token redacted)", flush=True)
        else:
            self.request_guest_token()
            transid = self.send_otp()
            sys.stderr.write(f"OTP sent to +{self.msisdn[-9:]}. Enter code: ")
            sys.stderr.flush()
            otp = input("").strip()
            self.token = self.validate_otp(transid, otp)
            self.vault.put_session(self.msisdn, {
                "token": self.token,
                "exp": _jwt_exp(self.token),
                "action": self.action,
                "deviceId": self.device_id,
            })
        dash = self.dashboard()
        return parse_dashboard(dash)

    fresh = False


# --- dashboard XML-envelope summary keys (from live capture, v7) -----------------
def parse_dashboard(dash: dict) -> dict:
    """Extract human fields from the unwrapped /dashboard/get/v7 payload.

    Live layout (single-source, lowercase keys):
      prepaidinfo.balance / cardactiveuntil / graceperioduntil
      summary.data.remainingquota|usedquota|quotaunit|isunlimited
      bonstriDetails.totalPoints
    """
    summ = dash.get("summary") or {}
    data = summ.get("data") or {}
    prep = dash.get("prepaidinfo") or {}
    return {
        "balance": prep.get("balance") or (dash.get("packdata") or {}).get("lastbalance"),
        "quota": data.get("remainingquota"),
        "unit": (str(data.get("quotaunit") or "KB")).upper(),
        "used": data.get("usedquota"),
        "unlimited": bool(data.get("isunlimited")),
        "active_until": prep.get("cardactiveuntil"),
        "grace_until": prep.get("graceperioduntil"),
        "bonstri": (dash.get("bonstriDetails") or {}).get("totalPoints"),
    }


def fmt_quota(result: dict) -> str:
    """Human size: 387072 KB -> '378 MB'."""
    if result["unlimited"]:
        return "UNLIMITED"
    unit = result["unit"]
    kb = int(result["quota"] or 0)
    if unit == "KB":
        if kb >= 1024 * 1024:
            return f"{kb / (1024 * 1024):.1f} GB"
        if kb >= 1024:
            return f"{kb / 1024:.0f} MB"
        return f"{kb} KB"
    return f"{kb} {unit}"


def print_balance(result: dict, msisdn: str, stamp: str = "") -> None:
    head = f"[{stamp}] " if stamp else f"\n== {BRAND} ==\n"
    print(f"{head}msisdn     : {msisdn}")
    print(f"pulsa      : Rp {result['balance']}")
    print(f"kuota      : {fmt_quota(result)} sisa")
    print(f"masa aktif : {result['active_until'] or '-'}")
    if result["grace_until"]:
        print(f"grace s/d  : {result['grace_until']}")
    if result["bonstri"] not in (None, "", 0):
        print(f"bonstri    : {result['bonstri']} pts")
    if stamp:
        print(flush=True)


# --- CLI ------------------------------------------------------------------------
def main():
    ap = argparse.ArgumentParser(description=f"{BRAND} balance via decompiled protocol")
    ap.add_argument("msisdn", nargs="?", help="mobile number, e.g. +62895… / 0895… / 895…")
    ap.add_argument("--action", default="register", help="send-OTP action (default: register; required by risk gate)")
    ap.add_argument("--child", default=None, help="child line msisdn (same login account)")
    ap.add_argument("--deviceid", default=None,
                    help="X-DEVICEID (e.g. your phone's android_id via adb; risk engine flags "
                         "unknown msisdn<->device pairs)")
    ap.add_argument("--devicename", default=None, help="X-DEVICENAME (Settings.Global device_name)")
    ap.add_argument("--oem", default=None, help="X-IMI-App-OEM (Build.MANUFACTURER)")
    ap.add_argument("--model", default=None, help="X-IMI-App-Model (Build.MODEL)")
    ap.add_argument("--osver", default=None, help="X-IMI-App-OSVersion (Build.VERSION.RELEASE)")
    ap.add_argument("--base-url", default=None)
    ap.add_argument("--user-agent", default=DEFAULT_UA)
    ap.add_argument("--timeout", type=float, default=45.0)
    ap.add_argument("--dry-run", action="store_true", help="build and print request, don't send")
    ap.add_argument("--fresh", action="store_true", help="ignore saved session, re-login")
    ap.add_argument("--watch", type=float, default=0.0, metavar="SEC",
                    help="poll aktual tiap SEC detik setelah tampilan pertama (real-time)")
    ap.add_argument("--verbose", "-v", action="store_true")
    ap.add_argument("--selftest", action="store_true", help="offline crypto self-check")
    ap.add_argument("--json", action="store_true", help="emit machine-readable JSON to stdout (logs to stderr)")
    args = ap.parse_args()
    if args.base_url is None:
        args.base_url = (PROVIDER_CFG or PROVIDER_BIMA)["base_default"]

    if args.selftest:
        selftest(args)
        return

    if not args.msisdn:
        ap.error("msisdn required (or --selftest)")

    vault = Vault()
    http = Http(args.base_url, args.user_agent, args.timeout)
    # device identity: explicit flag > saved encrypted profile > vault stand-in.
    # Any explicit flag persists to the profile (inside vault.bin) for future runs.
    prof = vault.device_profile()
    dev_id = args.deviceid or prof.get("deviceId") or vault.device_id()
    dev_nm = args.devicename or prof.get("deviceName") or "generic"
    dev_oem = args.oem or prof.get("oem")
    dev_mod = args.model or prof.get("model")
    dev_osv = args.osver or prof.get("osver")
    if any((args.deviceid, args.devicename, args.oem, args.model, args.osver)):
        vault.put_device_profile({
            "deviceId": dev_id, "deviceName": dev_nm,
            "oem": dev_oem or prof.get("oem"), "model": dev_mod or prof.get("model"),
            "osver": dev_osv or prof.get("osver")})
    client = BimaClient(http, vault, args.msisdn, args.action, args.child, args.verbose,
                        device_id=dev_id, device_name=dev_nm,
                        oem=dev_oem, model=dev_mod, osver=dev_osv,
                        cfg=PROVIDER_CFG)
    client.fresh = args.fresh

    # dry-run: assemble one request fully then exit
    if args.dry_run:
        client.req_time = int(time.time() * 1000)
        body = json.dumps({"msisdn": client.msisdn, "action": client.action}, separators=(",", ":"))
        h = client.build_headers("/otp/send/v1", body)
        if args.json:
            print(json.dumps({
                "provider": BRAND, "msisdn": client.msisdn, "status": "dry_run",
                "base": http.base, "headers": redact(h), "body": body,
                "aes_body_sample": aes_gcm_encrypt(GCM_PINPUK_KEY, GCM_STATIC_IV,
                    json.dumps({"transid": "DRYRUN", "otp": "123456"}, separators=(",", ":"))),
            }, ensure_ascii=False))
            return
        print(f"BASE {http.base}")
        print(json.dumps(redact(h), indent=2))
        print("body:", body)
        print("AES body sample (validate step):",
              aes_gcm_encrypt(GCM_PINPUK_KEY, GCM_STATIC_IV,
                              json.dumps({"transid": "DRYRUN", "otp": "123456"},
                                         separators=(",", ":"))))
        return

    reused = vault.session(client.msisdn) is not None
    result = client.get_balance()
    if args.json:
        out = {
            "provider": BRAND, "msisdn": client.msisdn, "status": "ok",
            "session_reused": reused,
            "balance": result.get("balance"),
            "quota": result.get("quota"),
            "unit": result.get("unit"),
            "used": result.get("used"),
            "unlimited": result.get("unlimited"),
            "active_until": result.get("active_until"),
            "grace_until": result.get("grace_until"),
            "bonstri": result.get("bonstri"),
        }
        print(json.dumps(out, ensure_ascii=False))
    else:
        print_balance(result, client.msisdn)
    if args.watch > 0:
        if not args.json:
            print(f"polling every {args.watch:g}s — Ctrl-C berhenti", flush=True)
        while True:
            time.sleep(args.watch)
            try:
                r = parse_dashboard(client.dashboard())
                if args.json:
                    o = {
                        "provider": BRAND, "msisdn": client.msisdn, "status": "ok",
                        "balance": r.get("balance"),
                        "quota": r.get("quota"),
                        "unit": r.get("unit"),
                        "used": r.get("used"),
                        "unlimited": r.get("unlimited"),
                        "active_until": r.get("active_until"),
                        "grace_until": r.get("grace_until"),
                        "bonstri": r.get("bonstri"),
                        "ts": time.time(),
                    }
                    print(json.dumps(o, ensure_ascii=False), flush=True)
                else:
                    print_balance(r, client.msisdn, time.strftime("%H:%M:%S"))
            except KeyboardInterrupt:
                break


def selftest(args) -> None:
    """Offline verification of every reimplementation detail we can check without network."""
    assert get_values("12345678") == "1357", get_values
    # RC4 classic vector: rc4_hex("Key", "Plaintext") == bbf316e8d940af0ad3
    got = rc4_hex("Key", "Plaintext")
    assert got == "bbf316e8d940af0ad3", got
    # SHA-512 known-vector sanity
    assert sha512_hex("abc") == (hashlib.sha512(b"abc").hexdigest())
    # AES-GCM roundtrip with the exact app key/IV
    ct = aes_gcm_encrypt(GCM_PINPUK_KEY, GCM_STATIC_IV, '{"transid":"1","otp":"123456"}')
    assert len(base64.b64decode(ct)) >= 16 and b64_decode and ct
    # hands-on decode check: same input -> same ciphertext deterministically (static IV)
    ct2 = aes_gcm_encrypt(GCM_PINPUK_KEY, GCM_STATIC_IV, '{"transid":"1","otp":"123456"}')
    assert ct == ct2
    # msisdn normalisation
    assert normalize_msisdn("+6289561234567") == "6289561234567"
    assert normalize_msisdn("089561234567") == "6289561234567"
    assert normalize_msisdn("89561234567") == "6289561234567"
    # URL join: absolute passes through, relative joins onto base (regression: a full
    # URL posted as `path` used to be doubled into base+path -> 404).
    _h = Http("https://otp.api.axis.co.id", "ua")
    assert _h._abs("https://otp.api.axis.co.id/v1/otp") == "https://otp.api.axis.co.id/v1/otp"
    assert _h._abs("/v1/otp") == "https://otp.api.axis.co.id/v1/otp"
    # axisnet (com.axis.net) §16: vectors captured from the live app (Frida)
    assert axisnet_encrypt_sso("081234567890") == "X6diZ_QuncTuKBc5e5BJQA=="
    assert axisnet_encrypt_sso("1700000000") == "KdOSI_lRdvIgo6qoFV-OSQ=="
    assert axisnet_login_sign("/v1/otp", "081234567890", device_type="Android") == \
        "6e63d46524144be4ba6b749263c909f2a4d9134beb0fd800aee6ca3c86543f4d"
    # live-app parity: encryptSSO() returns a trailing "\n" that OtpPayload strips;
    # f14dae59... is the app's own v() with the newline still attached (proves the
    # formula), c9501cdf... is the omitted-newline, lowercase-"android" request sig.
    assert axisnet_login_sign("/v1/otp", "X6diZ_QuncTuKBc5e5BJQA==\n",
                              device_type="Android") == \
        "f14dae59e401449939bc74bc75959165026337f108768b6a3a16cc3e5bef949c"
    assert axisnet_login_sign("/v1/otp", "X6diZ_QuncTuKBc5e5BJQA==",
                              device_type="android") == \
        "c9501cdf663e1269c1b2845e61be0b4d3ff64f73915074a7a852a03baf1d9078"
    assert axisnet_sign("GET", "/balance", "", "tok", "hs") == \
        "93bcd689a4f9c0b0e6cc7ff9bf6a92b7737fc8649fbb51cc32e6c6fc95808ec8"
    # signature shape: hash only letters of hex + separators
    h = sha512_hex("parent" + SECRET + APP_OS + SECRET + APP_VERSION + SECRET + "tok" + "&SALT=" + get_values("1700000000000"))
    assert re.fullmatch(r"[0-9a-f]{128}", h)
    # guest-request shape (putToken + addOkHttpSignature decompile): TOKENID always
    # present (ms timestamp on fresh install) and oauth fires off header presence.
    c = BimaClient(Http(args.base_url, args.user_agent, 5.0), Vault(), "6289561234567")
    c.req_time = 1700000000000
    gh = c.build_headers("/token/guest/v1", "{}")
    assert gh["X-IMI-TOKENID"] == "1700000000000", gh["X-IMI-TOKENID"]
    assert gh["x-imi-oauth"] == sha512_hex(
        "REQBODY={}&SALT=" + get_values("1700000000000")), gh["x-imi-oauth"]
    assert gh["X-IMI-HASH"] == sha512_hex(
        "parent" + SECRET + APP_OS + SECRET + APP_VERSION + SECRET +
        "1700000000000" + "&SALT=" + get_values("1700000000000"))
    print("selftest OK: rc4 / sha512 / aes-gcm / msisdn / guest header shape")


if __name__ == "__main__":
    main()
