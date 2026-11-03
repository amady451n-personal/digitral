# 2026-11-03 — axisnet static recon: protected; provider slot stays None

## Goal
Carry out the deferred axisnet check (`PROVIDER_AXISNET` slot comment in
`scripts/bima_balance.py`), using the same constant-harvest playbook that worked for
myIM3 (2026-10-29: dex static-field decoder validated against bima+).

## What was found (evidence)
1. REA jadx jar was wiped from `/tmp` again (same as 2026-10-29). Repair per skill:
   downloaded exact package release
   `1013503897/jadx-headless-mcp v0.7.1` → `/tmp/opencode/rea-jadx/…` and symlinked to
   the REA-read path `/tmp/rea-jadx/jadx-headless-mcp-0.7.1-all.jar`. No config change.
2. Whole `.apk+` is a zip bundle: `base.apk` (`c697134…`) + split APKs. jadx choked on
   base.apk load (55,907 classes > the 120 s provider deadline) → REA retry admitted a
   trimmed APK (`AndroidManifest.xml + classes3.dex`) which loads 1,975 classes fine
   and answered identity: **`com.axis.net` v11.0.0, code 836**.
3. dex static-field decoder (rewritten in-session, validated by reproducing bima+
   `BuildConfig` SERVICEKEY/CHANNEL/VERSION exactly) is committed as
   `scripts/dex_static_fields.py`. Running it on axisnet dexes yields **no
   `SERVICEKEY/CHANNEL`** class — axisnet is not the Digitral family.
4. All 8 shipped dex classes define **0** `Lcom/axis/net/*` classes, while classes3
   references 781 of them (incl. `PackageBalanceApi`, `AxisnetApiServices`,
   `ServerEnv`, `Signature`: names kept, impls loaded later).
5. `lib/arm64-v8a/libudihrdowpk.so` prints `V-Key DexObfuscator version` (mbedtls
   AES-XTS + GCM, "dex_buffer" decompress path, `com.axis.net.helper.AxisNetApplication`)
   → second-stage dex is decrypted in memory by V-Key.
6. `lib/arm64-v8a/libnative-lib.so` declares **272** JNI accessor functions
   `Java_com_axis_net…` (hosts via `RequestInterceptor_hostName{New,Production,Staging,Uat}`,
   keys via `Signature_apiKey`/`Sig_retryTempest*`, feature URLs per repository —
   `docs/axisnet-jni-surface.txt`). Values are not plaintext in `.rodata`
   (strings + 1-byte-XOR both empty).
7. `assets/index.android.bundle` (RN, metro) carries only the **Modula survey SDK**
   config (env table gateway.{sit,uat,pre-,}modula.xlsmart.co.id, clientCode
   `JGH5T9ZQ2`), its header/nonce scheme (X-Brand-Code…X-Nonce HMAC-SHA256).
   This is NOT the axisnet balance backend.

## Decision
- Do **not** wire `PROVIDER_AXISNET` (no verified value constants; guessing keys would
  risk OTP spamms to real MSISDNs which stays forbidden without user consent).
- Slot comment stays `None`, updated to name the two possible disclosure routes:
  runtime in-memory dex extraction (bima §13 playbook) or native decompile of
  `libnative-lib.so` with Ghidra/Hopper (candidates' native engines are not installed
  on this host; doctor scope: `GHIDRA_INSTALL_DIR`, `/opt/hopper/bin/Hopper`).
- Static findings (surface, evidence, negative results) persisted in
  `docs/bima-protocol.md` §15 and committed: `scripts/dex_static_fields.py`,
  `docs/axisnet-jni-surface.txt`.

## Open
- axisnet real API base URLs and Tempest key values (runtime only).
- myXL (`myxl-9.6.0.apk+`) still unchecked; same V-Key family suspected — confirm
  before any harvest attempt.
