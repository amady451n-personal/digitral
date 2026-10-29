#!/usr/bin/env python3
"""myIM3 (Indosat-Ooredoo) balance fetcher — same Digitral protocol as bima+.

Reuses scripts/bima_balance.py end to end and only swaps the brand constants,
re-decoded from the myim3-82.17.0.apk+ dex statics (com.digitral.common.BuildConfig
and the shared com.linkit.bimatri module):

    SERVICEKEY      i4WxFMMLvWqnrvuAyg58   (X-IMI-SERVICEKEY; bima+ uses FPi7ZP3Jy8Uv3KBd4QeG)
    CHANNEL         V2MYIM3                (X-IMI-CHANNEL; bima+ uses BIMA)
    APP_VERSION     82.17.0                (X-IMI-VERSION)
    VERSION_CODE    560003790
    PINPUK_KEY      A9$gT#2vXp!qW7LmZ@e4Ky8Nb3Hj5Uq9   (AES-256-GCM, IDENTICAL to bima+)
    static IV       Rx4Cm9Pn2Vf8Kt6B                   (identical)
    Authorization   642d1cc69d90666962726e             (platform constant, identical)
    endpoints       /token/guest/v1 /otp/send/v1 /otp/validate/v2 /dashboard/get/v7
                    (same path table, classes7.dex)
    base hosts      http://myim3-he1.ioh.co.id/api/v2  (app default, cleartext)
                    https://myim3api1.ioh.co.id/api/v2 (full-API https product, default here —
                                                        mirrors the bima+ -api host split)

Session vault lives at ~/.myim3 (override with BIMA_HOME, same as bima_balance).
Usage mirrors bima_balance exactly:
    python3 scripts/myim3_balance.py <msisdn> --action register   # prompts for OTP
    python3 scripts/myim3_balance.py <msisdn>                     # reuse stored session
    python3 scripts/myim3_balance.py <msisdn> --watch 60          # real-time polling
"""
import os
import sys

# vault dir must be set BEFORE importing bima_balance (it snapshots HOME_DIR at import)
_HOME_DEFAULT = os.path.join(os.path.expanduser("~"), ".myim3")
os.environ.setdefault("BIMA_HOME", _HOME_DEFAULT)

import bima_balance as bm  # noqa: E402  (path: same scripts/ dir)

# --- brand constants (decoded, see module docstring) ---------------------------
bm.BRAND       = "myIM3"
bm.BASE_DEFAULT = "https://myim3api1.ioh.co.id/api/v2"
bm.SERVICEKEY  = "i4WxFMMLvWqnrvuAyg58"
bm.CHANNEL     = "V2MYIM3"
bm.APP_VERSION = "82.17.0"
bm.VERSION_CODE = "560003790"

# crypto material, Authorization and endpoint paths are platform-wide: unchanged.

if __name__ == "__main__":
    sys.exit(bm.main() or 0)
