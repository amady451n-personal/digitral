#!/usr/bin/env python3
"""axisnet (com.axis.net 11.0.0 / vc 836) — OTP + login + balance.

axisnet is NOT the Digitral family, so it does not reuse BimaClient: this entry
point wires bima_balance's Http/Vault + PROVIDER_AXISNET + axisnet_* helpers to the
scheme recovered in docs/bima-protocol.md §16 (now including the full login chain).

Usage:
    python3 scripts/axisnet_balance.py --selftest                    # offline vectors
    python3 scripts/axisnet_balance.py 081234567890 --dry-run        # show OTP request
    python3 scripts/axisnet_balance.py 081234567890 --send-otp       # REAL OTP (own SIM)
    python3 scripts/axisnet_balance.py 081234567890 --otp-code 123456 --balance
    python3 scripts/axisnet_balance.py --token <tok> [--json]           # dashboard
    python3 scripts/axisnet_balance.py --decrypt <b64url>            # offline data blob

Dashboard output = msisdn / pulsa / kuota / masa aktif / grace s/d (+ paket list),
parsed from /v1/bff/homepage (profile.api) + /v1/quota/homepage (nq.api); /getbalance
is still fetched as fallback and shown raw in --json.

OTP is only ever sent with --send-otp; the code is only ever read from --otp-code,
both for a number you own. Login token is saved to the encrypted vault (~/.axisnet).

Transport note: the Cloudflare edge in front of otp.api/balance.api answers python
TLS stacks with a managed challenge (403 "Just a moment...") on /otplogin/v2 and
/getbalance while plain curl passes; all calls therefore fall back to curl.
"""
import argparse
import base64
import json
import os
import re
import subprocess
import sys

# vault dir must be set BEFORE importing bima_balance (HOME_DIR snapshotted at import)
os.environ.setdefault("BIMA_HOME", os.path.join(os.path.expanduser("~"), ".axisnet"))

import bima_balance as bm  # noqa: E402

C = bm.PROVIDER_AXISNET
BRAND = "axisnet"

# Dashboard sources (docs/axisnet-url-map.txt). Do NOT call profile.api bff/homepage:
# it answers 401 "Invalid signature" without X-Handshake/X-Signature (verified live
# 2026-11-06) and adds nothing - getbalance + nq quota/homepage already carry
# msisdn/pulsa/masa aktif/grace and the per-package breakdown.
QUOTA_HOME_URL = "https://nq.api.axis.co.id/v1/quota/homepage"


def _msisdn(raw: str) -> str:
    """App normalizes to 62-prefixed form before encryptSSO (runtime: 6283185770311)."""
    d = re.sub(r"\D", "", raw)
    return d if d.startswith("62") else "62" + d.lstrip("0")


def otp_request(msisdn: str) -> "tuple[str, dict, str]":
    """Build the /v1/otp request exactly as OtpPayload + sendDataOtp do (no network)."""
    msisdn_enc = bm.axisnet_encrypt_sso(msisdn).replace("\n", "")
    # JSON key is "device_type" (OtpPayload @SerializedName, runtime dex
    # d_0x7ae7076be000); x-signature message uses the VALUE only, unchanged.
    body = json.dumps({"msisdn": msisdn_enc, "device_type": C["app_os"]},
                      separators=(",", ":"), ensure_ascii=False)
    headers = {
        "Content-Type": "application/json",
        "Connection": "keep-alive",
        "X-API-KEY": C["api_key"],
        "x-app-version": C["app_version"],
        "x-signature": bm.axisnet_login_sign("/v1/otp", msisdn=msisdn_enc, device_type=C["app_os"]),
        "x-app-request-id": str(__import__("uuid").uuid4()),
    }
    return C["otp_url"], headers, body


# --- dashboard: msisdn / pulsa / kuota / masa aktif / grace -------------------
def _hdrs(token: str) -> dict:
    return {
        "Authorization": token,
        "x-app-version": C["app_version"],
        "x-app-request-id": str(__import__("uuid").uuid4()),
    }


def parse_home(home: dict, bal: dict | None = None) -> dict:
    """Flatten bff/homepage + getbalance + nq quota/homepage into bima-style fields.

    Live shapes (2026-11-06):
      nq quota/homepage (decrypted, snake_case):
        {total, summary:{total_remaining, is_unlimited, formatted:{total_remaining}},
         packages:[{name, percent, remaining_text, is_unlimited, formatted:{expired}}]}
      getbalance (decrypted): {msisdn, graceStatus:{status,information}, status,
         result:{balance, activestopdate, suspendstopdate, disablestopdate, ...}}
      bff/homepage: {profile:{msisdn}, myBalance:{balance, formatted:{balance,expired}},
         quota:{...}, gracePeriode:{title,description}}  <- needs X-Handshake/X-Signature
    """
    home = home or {}
    r = ((bal or {}).get("result") or {})
    gs = ((bal or {}).get("graceStatus") or {})
    mb = home.get("myBalance") or {}
    mbf = mb.get("formatted") or {}
    q = ((home.get("quota") or {}).get("formatted") or {})
    return {
        "msisdn": (home.get("profile") or {}).get("msisdn") or (bal or {}).get("msisdn"),
        "pulsa": mbf.get("balance") or mb.get("balance") or r.get("balance"),
        "kuota": q.get("remainingQuota"),
        "masa_aktif": mbf.get("expired") or r.get("activestopdate"),
        "grace": gs.get("information") or r.get("disablestopdate"),
        "grace_status": gs.get("status"),
    }


def parse_quota_home(qh: dict | None) -> dict:
    """nq.api /v1/quota/homepage (packages use snake_case JSON keys)."""
    qh = qh or {}
    summ = qh.get("summary") or {}
    fmt = summ.get("formatted") or {}
    paket = []
    for p in qh.get("packages") or []:
        if not isinstance(p, dict):
            continue
        pct = p.get("percent")
        paket.append({
            "nama": p.get("name"),
            "sisa": p.get("remaining_text") or p.get("remaining_formatted"),
            "persen": round(pct, 1) if isinstance(pct, (int, float)) else pct,
            "s/d": (p.get("formatted") or {}).get("expired"),
            "unlimited": bool(p.get("is_unlimited")),
        })
    return {"kuota": fmt.get("total_remaining"), "unlimited": bool(summ.get("is_unlimited")),
            "jumlah_paket": qh.get("total"), "paket": paket}


def axisnet_dashboard(http, token: str, msisdn: "str | None" = None) -> dict:
    """Pulsa + kuota + masa aktif + grace (getbalance + nq quota/homepage)."""
    out = {"provider": BRAND, "msisdn": msisdn}
    payloads = {}
    for key, url in (("quota_home", QUOTA_HOME_URL), ("balance", C["balance_url"])):
        status, text = _cf_get(http, url, _hdrs(token))
        env = _plain(text) or text[:400]
        out[key + "_raw"] = {"status": status, "data": env}
        inner = env.get("data") if isinstance(env, dict) else None
        payloads[key] = inner if isinstance(inner, dict) else {}
    out.update(parse_home(payloads.get("home") or {}, payloads["balance"]))
    out.update(parse_quota_home(payloads["quota_home"]))
    return out


def print_dashboard(res: dict, msisdn: str = "") -> None:
    """bima-style human block: msisdn / pulsa / kuota / masa aktif / grace s/d."""
    print(f"\n== {BRAND} ==")
    print(f"msisdn     : {res.get('msisdn') or msisdn or '-'}")
    print(f"pulsa      : {('Rp ' + str(res['pulsa'])) if res.get('pulsa') else '-'}")
    kuota = res.get("kuota") or "-"
    if res.get("unlimited"):
        kuota += " (UNLIMITED)"
    print(f"kuota      : {kuota}")
    print(f"masa aktif : {res.get('masa_aktif') or '-'}")
    print(f"grace s/d  : {res.get('grace') or '-'}"
          + (f" ({res['grace_status']})" if res.get("grace_status") else ""))
    for p in res.get("paket") or []:
        print(f"paket      : {p['nama']} | sisa {p['sisa']} | {p['persen']}% | s/d {p['s/d']}")


# --- transport: bm.Http (curl_cffi) first, curl CLI on a Cloudflare challenge ---
_CF_MARKERS = ("_cf_chl", "cf-chl", "Just a moment")


def _is_cf(text: str) -> bool:
    return any(m in text for m in _CF_MARKERS)


def _curl(method: str, url: str, headers: dict, body: "str | None" = None,
          timeout: float = 45.0) -> "tuple[int, str]":
    cmd = ["curl", "-sS", "--max-time", str(int(timeout)), "-o", "-",
           "-w", "\n%{http_code}", "-X", method]
    for k, v in headers.items():
        cmd += ["-H", f"{k}: {v}"]
    if body is not None:
        cmd += ["--data", body]
    r = subprocess.run(cmd + [url], capture_output=True, text=True)
    if r.returncode != 0:
        return 0, r.stderr.strip()
    text, _, code = r.stdout.rpartition("\n")
    return int(code or 0), text


def _cf_post(http, url: str, body: str, headers: dict) -> "tuple[int, str]":
    status, text = http.post(url, body, headers)
    if status == 403 and _is_cf(text):
        return _curl("POST", url, headers, body)
    return status, text


def _cf_get(http, url: str, headers: dict) -> "tuple[int, str]":
    status, text = http.get(url, headers)
    if status == 403 and _is_cf(text):
        return _curl("GET", url, headers)
    return status, text


def _plain(text: str):
    """Parse a JSON envelope and decode its `data` blob when present (offline).

    `data` is either DESede/ECB (otp.api/balance.api) or plain base64(JSON)
    (nq.api quota, verified live 2026-11-06).
    """
    try:
        obj = json.loads(text)
    except ValueError:
        return None
    if isinstance(obj, dict) and obj.get("data"):
        blob = obj["data"]
        for decode in (bm.axisnet_decrypt_data,
                       lambda s: base64.b64decode(
                           s.replace("-", "+").replace("_", "/")
                           + "=" * (-len(s) % 4)).decode("utf-8")):
            try:
                obj["data"] = json.loads(decode(blob))
                break
            except Exception:
                continue
        else:
            try:                             # already-plain JSON string
                obj["data"] = json.loads(blob)
            except ValueError:
                pass                         # opaque string: keep raw
    return obj


def main() -> int:
    ap = argparse.ArgumentParser(description="axisnet balance / OTP (com.axis.net 11.0.0)")
    ap.add_argument("msisdn", nargs="?", help="nomor (08.../628...); hanya nomor sendiri")
    ap.add_argument("--token", default=None, help="Authorization untuk /getbalance")
    ap.add_argument("--otp-code", default=None,
                    help="kode OTP dari SMS: jalankan /otplogin/v2 lalu simpan token")
    ap.add_argument("--send-otp", action="store_true",
                    help="KIRIM OTP nyata ke nomor tsb (butuh nomor sendiri + izin)")
    ap.add_argument("--balance", action="store_true",
                    help="setelah --otp-code/--token sukses, lanjut ambil dashboard (pulsa/kuota/grace)")
    ap.add_argument("--decrypt", default=None, metavar="B64URL",
                    help="dekripsi offline field data (DESede/ECB, kunci saltKey)")
    ap.add_argument("--dry-run", action="store_true", help="bangun request, tanpa jaringan")
    ap.add_argument("--json", action="store_true", help="output JSON")
    ap.add_argument("--timeout", type=float, default=45.0)
    ap.add_argument("--base-url", default=None)
    ap.add_argument("--user-agent",
                    default="Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 "
                            "(KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
    ap.add_argument("--selftest", action="store_true", help="offline crypto self-check")
    args = ap.parse_args()
    if args.base_url is None:            # bm.selftest expects it
        args.base_url = C["otp_url"]

    if args.decrypt:
        print(bm.axisnet_decrypt_data(args.decrypt))
        return 0

    if args.selftest:
        # msisdn parity: the app encrypts the 62-prefixed form (runtime capture:
        # in=6283185770311 -> NR3VaMe9G4GthRU6apif8g==). 62 appears in bm.selftest too.
        assert _msisdn("081234567890") == "6281234567890"
        assert _msisdn("+62 812-3456-7890") == "6281234567890"
        assert _msisdn("6281234567890") == "6281234567890"
        # fb_id recipe: md5("xl" + md5(msisdnIDFormat(62-form)) + "axis")
        assert bm.axisnet_fb_id("6281234567890") == \
            bm.axisnet_fb_id("081234567890") == \
            __import__("hashlib").md5(("xl" + __import__("hashlib").md5(
                b"081234567890").hexdigest() + "axis").encode()).hexdigest()
        # /otplogin/v2 body shape: rc.c keys = the signature component names
        _, _, body = bm.axisnet_otplogin_request("6281234567890", "123456")
        assert json.loads(body) == {
            "msisdn": bm.axisnet_encrypt_sso("6281234567890").replace("\n", ""),
            "otp_code": bm.axisnet_encrypt_sso("123456").replace("\n", ""),
            "fcm_token": "", "fb_id": bm.axisnet_fb_id("6281234567890"),
            "model": "SM-A235F", "brand": "samsung", "device_os": "31",
            "device_type": "android", "imei": "b3a9c7d5e1f20364"}
        # data decrypt roundtrip (DESede/ECB, saltKey NUL-padded to 24)
        from cryptography.hazmat.primitives.ciphers import Cipher, algorithms, modes
        td = getattr(algorithms, "TripleDES", None)
        if td is None:
            from cryptography.hazmat.decrepit.ciphers.algorithms import TripleDES as td
        sample = '{"token":"aaaa"}'         # exactly 16 bytes = 2 DESede blocks
        key = bm._axisnet_pad(C["sso_key"], 24).encode()
        enc = Cipher(td(key), modes.ECB()).encryptor()
        ct = enc.update(sample.encode()) + enc.finalize()
        import base64 as _b64
        blob = _b64.b64encode(ct).decode().replace("+", "-").replace("/", "_")
        assert bm.axisnet_decrypt_data(blob) == sample
        bm.selftest(args)
        # dashboard flattening: keys verified live 2026-11-06 (snake_case on nq quota)
        qh = {"total": 1, "summary": {"is_unlimited": False, "formatted": {"total_remaining": "3.2 GB"}},
              "packages": [{"name": "Bonus", "remaining_text": "1 GB", "percent": 50,
                            "formatted": {"expired": "Berlaku sampai 30 Nov 2026 23:59"}}]}
        bal = {"msisdn": "6281234567890", "graceStatus": {"status": "Active", "information": ""},
               "result": {"balance": "10000", "activestopdate": "Exp on 29 Dec 2026",
                          "disablestopdate": "Exp on 29 Dec 2026"}}
        r = parse_home({"profile": {"msisdn": "6281234567890"}}, bal)
        assert r["msisdn"] == "6281234567890" and r["pulsa"] == "10000"
        assert r["masa_aktif"] == "Exp on 29 Dec 2026" and r["grace"] == "Exp on 29 Dec 2026"
        q = parse_quota_home(qh)
        assert q["kuota"] == "3.2 GB" and q["jumlah_paket"] == 1
        assert q["paket"][0] == {"nama": "Bonus", "sisa": "1 GB", "persen": 50,
                                 "s/d": "Berlaku sampai 30 Nov 2026 23:59", "unlimited": False}
        return 0

    if args.dry_run:
        if not args.msisdn:
            ap.error("msisdn required with --dry-run")
        url, headers, body = otp_request(_msisdn(args.msisdn))
        print(f"POST {url}")
        print(json.dumps(bm.redact(headers), indent=2))
        print("body:", body)
        return 0

    http = bm.Http(C["otp_url"], args.user_agent, args.timeout)

    if args.send_otp:
        if not args.msisdn:
            ap.error("msisdn required with --send-otp")
        url, headers, body = otp_request(_msisdn(args.msisdn))
        status, text = _cf_post(http, url, body, headers)
        out = {"provider": BRAND, "status": status, "raw": text[:2000]}
        print(json.dumps(out, ensure_ascii=False) if args.json else f"HTTP {status}\n{text[:2000]}")
        return 0

    if args.otp_code:
        if not args.msisdn:
            ap.error("msisdn required with --otp-code")
        m = _msisdn(args.msisdn)
        lu, lh, lb = bm.axisnet_otplogin_request(m, args.otp_code)
        status, text = _cf_post(http, lu, lb, lh)
        obj = _plain(text)
        blob = (obj or {}).get("data")
        token = (blob or {}).get("token") if isinstance(blob, dict) else None
        if status != 200 or not token:
            print(f"/otplogin/v2 failed: HTTP {status}\n{text[:500]}")
            return 1
        import time
        bm.Vault().put_session(m, {
            "provider": BRAND, "token": token,
            "issued": time.strftime("%Y-%m-%dT%H:%M:%S+07"),
            "scheme": {"authorization": "raw", "response_decrypt": "desede-ecb/sso_key"},
        })
        print(json.dumps({"provider": BRAND, "status": status, "token": token[:8] + "..."},
                         ensure_ascii=False) if args.json
              else f"HTTP {status}: token {token[:8]}... saved to vault")
        if not args.balance:
            return 0
        args.token = token

    token = args.token or os.environ.get("AXISNET_TOKEN")
    if not token and args.msisdn:
        token = (bm.Vault().session(bm.normalize_msisdn(args.msisdn)) or {}).get("token")
    if not token:
        ap.error("need --token/AXISNET_TOKEN, --otp-code, or a saved session")

    res = axisnet_dashboard(http, token, args.msisdn)
    if args.json:
        print(json.dumps(res, ensure_ascii=False, indent=2))
    else:
        print_dashboard(res, _msisdn(args.msisdn) if args.msisdn else "")
    return 0


# --- client adapter for server.py (duck-types bima_balance.BimaClient) --------
_AX_UA = ("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 "
          "(KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")


class AxisnetClient:
    """server.py-compatible client for the axisnet (com.axis.net) family.

    Same surface as BimaClient/MyXLClient: request_guest_token/send_otp/
    validate_otp/get_balance, plus token/msisdn/vault/fresh attributes.
    """

    def __init__(self, vault, msisdn: str, action: str = "login", cfg=None):
        self.vault = vault
        self.msisdn = bm.normalize_msisdn(msisdn)
        self.token = None
        self.fresh = False
        self.cfg = cfg or C
        self.http = bm.Http(C["otp_url"], _AX_UA, 45.0)

    def request_guest_token(self) -> str:
        return ""   # axisnet has no guest token

    def send_otp(self) -> str:
        url, headers, body = otp_request(_msisdn(self.msisdn))
        status, text = _cf_post(self.http, url, body, headers)
        if status not in (200, 201):
            raise RuntimeError(f"axisnet /v1/otp {status}: {text[:200]}")
        return ""   # /otplogin/v2 is stateless (msisdn + otp_code)

    def validate_otp(self, transid: str, otp: str) -> str:
        url, headers, body = bm.axisnet_otplogin_request(_msisdn(self.msisdn), otp.strip())
        status, text = _cf_post(self.http, url, body, headers)
        obj = _plain(text)
        blob = (obj or {}).get("data")
        token = blob.get("token") if isinstance(blob, dict) else None
        if status != 200 or not token:
            raise RuntimeError(f"axisnet /otplogin/v2 {status}: {text[:200]}")
        import time
        self.token = token
        self.vault.put_session(self.msisdn, {
            "provider": BRAND, "token": token,
            "issued": time.strftime("%Y-%m-%dT%H:%M:%S+07"),
        })
        return token

    def get_balance(self) -> dict:
        if not self.token:
            self.token = (self.vault.session(self.msisdn) or {}).get("token")
        if not self.token:
            raise RuntimeError("no token")
        dash = axisnet_dashboard(self.http, self.token, self.msisdn)
        # normalize to the same simple shape as bima/myIM3/myXL
        return {
            "balance": dash.get("pulsa"),
            "quota": dash.get("kuota"),
            "unit": "",
            "used": None,
            "unlimited": bool(dash.get("unlimited")),
            "active_until": dash.get("masa_aktif"),
            "grace_until": dash.get("grace"),
            "bonstri": None,
        }


if __name__ == "__main__":
    sys.exit(main() or 0)
