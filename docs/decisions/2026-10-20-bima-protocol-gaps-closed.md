# Decision note — bima+ protocol gaps closed (2026-10-20)

## Context
Reverse-engineering of `com.linkit.bimatri` (bima+ 5.17.0) had four flagged open points before
writing the consolidated protocol doc (`docs/bima-protocol.md`):

1. msisdn normalization (`getMSISDNCountryAppended`) — actual value of `INE_CC`.
2. dashboard request body — `"{}"` was a guess.
3. child-line header name + `ApiService.encryptMSISDN` cipher.
4. `LoginFragment.mAction` source value.

## Decisions
- Resolved all four with decompiles + direct DEX parsing (see §12 of `docs/bima-protocol.md`).
- Key technical learnings encoded in `docs/bima-protocol.md` §12:
  - the app's `classes3.dex` stores `encoded_array` string payloads as little-endian ints
    (analysis note);
  - REA companion-class naming is `Class.Companion` (dot form);
  - some `private final` initializer constants only appear via `const-string` byte-scan, not in
    jadx decompiled output.
- Send-OTP msisdn format is `62xxxxxxxxxx` (no `+`); validation body uses AES-256-GCM with the
  PINPUK key + static IV; child-line header uses RC4("123") hex via `X-IMI-CHILD-LINENO`.
- New client design should send `action:"login"`; persist tokens encrypted; expect carrier
  fingerprinting (`10011` on app-path OTP replay, stale web-selfcare data).
- **Correction (live-fire diagnostic):** the `X-IMI-HASH` separator is a literal `'$'`
  (`DecodedChar.FNC1 = 36`, verified from `classes8.dex` encoded_array) — an earlier note
  claiming `\u001D` was wrong and propagated into the first client build. Both live `10001
  Authentication failed` failures on `/token/guest/v1` used the wrong separator. Also: on
  token-less requests thetokenId component of the hash input is the literal string `"null"`
  (Java/Kotlin null-concat), not an empty string. `scripts/bima_balance.py` and
  `docs/bima-protocol.md` §3 updated 2026-10-20; pending user retest.
- **Second root cause, same failure (decompile-verified same day):** `putToken`
  (`com.digitral.common.ProjectHeaders`) + `AppPreference.getFromStore`
  (`com.digitral.storage.AppPreference`, `getString(key, "")`) prove `X-IMI-TOKENID`
  is present on **every** request — fresh installs send `currentTimeMillis()` as the
  guest TOKENID. Therefore `/token/guest/v1` must be sent with `X-IMI-TOKENID`,
  `x-imi-oauth = sha512hex("REQBODY={}&SALT=" + getValues(TOKENID))`, and the hash's
  tokenId component = that timestamp (the `"null"` branch is unreachable in practice).
  Client updated + `--selftest` extended with a guest-shape assertion (green);
  user retest pending.
- **Third root cause, same 10001 (2026-10-22):** header-value suspects fully eliminated —
  re-decompiled `MixUpValues.getValues` (odd 1-based chars of raw input), `.encryption`
  (SHA-512 lowercase hex UTF-8), `BuildConfig.CHANNEL='BIMA'` /
  `BuildConfig.SERVICEKEY='FPi7ZP3Jy8Uv3KBd4QeG'` (from
  `com.digitral.common.BuildConfig` static_values; boolean tag 0x1f consumes 0 bytes —
  earlier decoder desyncs there), manifest version `5.17.0`, assembly order in
  `getOkHttpHeaders` (hash after baseline+UID+oauth, TAC/stk token replacement after
  hash). A/B probe (4 shapes: signed/unsigned/tokenless/https-host) → all `10001`.
  With curl_cffi Chrome TLS impersonation → `26100 success` + JWT. **The ingress
  TLS-fingerprints clients; python-stdlib TLS is rejected app-level as `10001`
  regardless of headers.** Transport switched to curl_cffi `impersonate="chrome124"`;
  token extraction fixed to parse `data.tokenid`; docs §3/§4 updated. Guest-token path
  verified green through the script itself.
- **Fourth root cause, host split (2026-10-22):** `/otp/send/v1` exists only on
  `https://bimaplus-api.ioh.co.id/api/v2`; `http://bimaplus.ioh.co.id/api/v2` 404s that
  path ("service rules for the path non-existent"). Probe with dummy msisdn on the https
  host → HTTP 200 + app-level `10011` — identical to all 2026-10-19 STB attempts.
  `BASE_DEFAULT` switched to the https host; the OTP-send `10011` is confirmed
  server-side risk-engine behavior, requiring real-app context (emulator capture) or
  the real app's pre-login traffic (device registration / config).
- **Real-device test (2026-10-23):** ran a spaced single-shot with the user's genuine phone
  identity (Xiaomi M2007J20CG, Android 12, android_id `4895f69fb8d626c4`, persisted in the
  vault). Host/transport/signature/headers/device all match the app; the guest JWT now
  carries `mob="4895f69fb8d626c4"` + `osv:"12"`, showing the identity propagated.
  `/otp/send/v1` replied **still `10011`** — so an authentic device id does **not** clear the
  gate. The eliminated hypothesis (unknown device id) is closed; the remaining explanation is
  a behavior-context requirement the app satisfies through surrounding traffic
  (device registration / config / analytics bootstrap before any OTP). Per
  systematic-debugging Phase-4 red-flag (four distinct fixes, each revealing a new shared
  context the minimal client lacks), this is treated as the architecturally-correct stop
  point for the app-API OTP path pending instrumentation of the real app — see
  `emulator-capture/RUNBOOK.md`.**

## Follow-ups
- Mobile app design multi-number trace (brainstorming → design doc → plan) using this protocol.
- If re-analysis needed, reuse the byte-ownership scanner pattern for `const-string`/
  fieldput references (fast under 110 s; full class_data walks are the slow path).

## 2026-10-29 — Root cause #5: `10011` CLOSED (MuMu Frida capture → header shape)

Hypothesis chain ended by a runtime tap instead of more decompile guessing. Setup:
MuMu Player 12 LAN-bridged (adb 192.168.8.101:5555, Android 12, spoofed SM-A528B,
x86_64+arm64 translation), bima+ 5.17.0 installed via root `pm install-create/write/commit`
(streaming `adb install-multiple` unsupported by MuMu adbd), frida-server 17.24.3
(x86_64) as root, OkHttp `"Request$Builder.build"` + `"newCall"` dump hook.

Findings
- Attach AFTER the splash-phase anti-tamper init survives; a delayed watchdog kills the
  process ~30 s post-attach (also kills mid-OTP input — not evidence of server rejection).
- The app's `/otp/send/v1` (real MSISDN, same home IP as our probes) differs from our
  client only in header/shape details listed in `docs/bima-protocol.md` §13.
- After mirroring those details, the bottom dummy still answers `10005` (number invalid)
  instead of `10011` → the risk gate accepted the client. User live-fire: full
  guest→OTP→validate→dashboard from Python succeeded with a persisted session and
  correct balance/quota (`9400` / AON data).

Decision: emulator path demoted to optional; the Python client is self-sufficient for
all 5 owned numbers. `--watch` added for real-time polling.

## 2026-10-29 — myIM3 constants harvested, client wrapper

Goal: extend the working bima+ client to myIM3 (`com.pure.indosat.care` 82.17.0, same
Digitral stack). REA jadx jar was lost to the /tmp wipe again; instead re-encoded the
static-fields decoder from scratch (`/tmp/dec/decode3.py`) and validated it against
bima+'s dex first (CHANNEL=BIMA, SERVICEKEY=FPi7… reproduced exactly, then run on
/tmp/myim3-dex). Result: myIM3 constants `SERVICEKEY=i4WxFMMLvWqnrvuAyg58`,
`CHANNEL=V2MYIM3`, version 82.17.0/560003790, ANTI_PROECTION_CLIENTID=im3;
PINPUK/IV/Authorization/paths identical to bima+; base hosts myim3-he1 (http app default)
and myim3api1 (https full product).

Artifacts: `scripts/myim3_balance.py` (wrapper: vault ~/.myim3, brand constants swapped);
docs/bima-protocol.md §14 constants table; bima_balance gained a BRAND display constant.
Live-fire 2026-10-29 on a user-owned myIM3 number: full chain guest → send → validate
→ token green from pure Python. First validate returned `10013`; our validate body proven
structurally correct (AES ct length arithmetic = `{"transid","otp"}` incl. the code), so the
block was a stale code — fresh send + correct retype cleared it → the "10013 = stale/mistyped
code first" verification rule.
