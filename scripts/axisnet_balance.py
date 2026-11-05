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
    python3 scripts/axisnet_balance.py --token <tok> [--balance]     # GET /getbalance
    python3 scripts/axisnet_balance.py --decrypt <b64url>            # offline data blob

OTP is only ever sent with --send-otp; the code is only ever read from --otp-code,
both for a number you own. Login token is saved to the encrypted vault (~/.axisnet).

Transport note: the Cloudflare edge in front of otp.api/balance.api answers python
TLS stacks with a managed challenge (403 "Just a moment...") on /otplogin/v2 and
/getbalance while plain curl passes; all calls therefore fall back to curl.
"""
import argparse
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
    """Parse a JSON envelope and decrypt its `data` blob when present (offline)."""
    try:
        obj = json.loads(text)
    except ValueError:
        return None
    if isinstance(obj, dict) and obj.get("data"):
        try:
            inner = bm.axisnet_decrypt_data(obj["data"])
            obj["data"] = json.loads(inner)
        except Exception:
            obj["data"] = "<undecryptable>"
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
                    help="setelah --otp-code/--token sukses, lanjut GET /getbalance")
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

    status, text = _cf_get(http, C["balance_url"], {
        "Authorization": token,
        "x-app-version": C["app_version"],
        "x-app-request-id": str(__import__("uuid").uuid4()),
    })
    out = {"provider": BRAND, "status": status, "data": _plain(text) or text[:2000]}
    print(json.dumps(out, ensure_ascii=False) if args.json
          else f"HTTP {status}\n{json.dumps(out['data'], indent=2, ensure_ascii=False) if isinstance(out['data'], (dict, list)) else out['data']}")
    return 0


if __name__ == "__main__":
    sys.exit(main() or 0)
