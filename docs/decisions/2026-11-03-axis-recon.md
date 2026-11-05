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

## 2026-11-03b — axisnet constants unlocked via Ghidra in Docker

User direction: providers are per-app (bima+/myIM3 vs axisnet/myXL share holding but not
stack); use the Ghidra/Hopper route **inside Docker only — nothing installed on the host**.

Built `ghidra-headless` image (Dockerfile at `scripts/ghidra-docker/`): temurin **25-jdk**
base (Ghidra 12.2 launch rejects JDK 21/24 with "unsupported java version") + latest
Ghidra release zip fetched inside the container. Headless worked with a Java post-script
(`DecompileByPrefix.java` — **Groovy is not a core Ghidra script provider**, only Java/Python).
Import of `libnative-lib.so` (arm64, 440 KB) analyzed in ~1 min.

Result — the prior "values are runtime-only" conclusion was wrong:
1. Every one of the 272 JNI getters is a trivial literal→`NewStringUTF` wrapper.
2. URL constants AND key material are plaintext `.rodata` literals; earlier `strings`
   greps failed because literals look like random base64 (`hOc36lLcbQ8J…`) and host
   strings are plain domains — no keyword to match.
3. Harvested exactly (base 0x100000 + program-header mapping, extents from the
   decompiled copy-spans): apiKey, 5 keyTempest tokens, saltKey/secretKey/newSaltKey/
   algorithmKey, 4 RequestInterceptor hosts, and 261/272 getters' literal values
   (full URL surface — `docs/axisnet-url-map.txt`, auth chain §15 items 1-8).
4. Layout detail: short strings use libc++ SSO — first byte = `len << 1`
   (production `0x1c`=28? → `>>1`=14 = len("api.axis.co.id") ✓).

Still encrypted (V-Key dex): header names + signature construction over the Tempest
keys/salts. Runtime capture (bima §13 playbook) is required before attempting requests.
`PROVIDER_AXISNET` stays `None`, now with verified endpoints+keys documented in
`docs/bima-protocol.md` §15.

Artifacts: `docs/axisnet-url-map.txt`, `scripts/ghidra-docker/{Dockerfile,DecompileByPrefix.java}`,
`docs/bima-protocol.md` §15 update. (Ghidra image cached locally as `ghidra-headless:latest`.)

## 2026-11-03c — runtime dex dump closed the header/signature gap (2026-11-04)

Outcome: header names + full signature/crypto scheme recovered (now
`docs/bima-protocol.md` §16); `PROVIDER_AXISNET` implemented in
`scripts/bima_balance.py` with live-captured verification vectors.

Device-side pipeline (MuMu 12, frida-inject 17.2.3, nothing installed on host):

- `inj -p <pid> -s <script> --runtime=qjs -e` (`-e` eternalizes; a `timeout` that
  kills `inj` also tears the script down).
- **`inj` exposes no global `Java`** → every script must be prefixed with the
  18-line bridge from `scripts/frida` `tapall5b.js` (`globalThis.Java = …` on line 14).
- pid: `ps -A -o PID,NAME | grep -a "com.axis.net$"` (pidof unreliable).
- pairip gate: `ppx4b.js` suppresses ppx3 checks and launches the real
  `.ui.splashLogin.SplashActivity` from `ActivityThread.currentApplication()` at
  script start (onStart hooks fire too late).
- zombie AMS records after a crash → `am start` says "delivered to currently
  running top-most instance" with no process; fix = `am stack remove <taskId>` +
  `am force-stop` + fresh start.

In-memory (V-Key-decrypted) dex dump:

- scan `/proc/self/maps` for `[anon:dalvik-DEX`; a dex starts at the region base
  (magic `dex\n037`), size = header `file_size@0x20`.
- Frida 17 in this build has **no `Memory.readByteArray`** and `new File(…).write`
  is **not a function** → use `ptr.readByteArray(n)` + Java
  `FileOutputStream.write(Java.array('byte', Array.from(new Uint8Array(buf))))`,
  chunked at 1 MiB.
- app code (incl. `RequestInterceptor`, `Signature`, `CryptoTool$Companion`,
  `EnumCrypto`) is in the ~4.8 MB anon dex; `strings` on it gave the header names.

Dex annotation parsing (to read Retrofit `@Header`/`@Headers` off abstract interface
methods REA/jadx will not decompile) — gotchas that cost the most time:

- `proto_ids` entries are **12 bytes** (`shorty`, `return_type`, `parameters_off`).
- `annotated_parameters` entries are **8 bytes**, like annotated methods (not 12).
- `encoded_value` **VALUE_ARRAY stores its element count as uleb128 after the header
  byte**, not in `value_arg`.
- annotation `type_idx` indexes **`type_ids`**, element `name_idx` indexes `strings`.
- nested classes must be given to REA **dotted** (`CryptoTool.Companion`), not `$`.

Everything was read via the crypto classes' own methods at runtime (`Signature.a`,
`CryptoTool.Companion.encryptSSO`, `AxisnetHelpers` JNI getters) and cross-checked
with a pure-Python reimplementation (HMAC-SHA256 + `cryptography` 3DES) — four exact
vectors now asserted by `--selftest`.

## Open
- `Authorization` value handed to `/getbalance` (no `Bearer` literal in the dex →
  raw token expected) — confirm with one real token.
- OTP/login chain implemented from decompile only; `/v1/otp` sent once →
  `401 "Invalid signature"`. Cause: `deviceType` must be `Consta.CHANNEL_ANDROID`
  = `"android"` (lowercase), fixed 2026-11-05; `/v1/otp` + `/otplogin/v2` still
  need a consented run before being trusted.
- myXL (`myxl-9.6.0.apk+`) still unchecked; treat as its own provider — confirm its
  protector/stack independently, never assume axisnet's shape.

## 2026-11-05e — 401 fix + runtime re-attach after VM reboot
- Runtime values re-captured after VM reboot (device IP unchanged
  192.168.8.101:5555): `Consta.CHANNEL_ANDROID = "android"` (static field, via
  reflection; `Java.use(...).INSTANCE.value` accessor FAILED — use
  `Java.use(cls).class` + `getDeclaredField`), `xApiKey()==OtpApiService.xApikey()
  ==Signature.apiKey() == ipRsTx…`. Vectors: `v("/v1/otp","X6diZ…==","android")
  =c9501cdf…` (app request, newline-stripped msisdn_enc); `f14dae59…` = same with
  unstripped `\n` (app OtpPayload ctor `u.J(enc,"\n","")` strips it). Python
  reproduces both → formula confirmed; server rejected `"Android"` case.
- msisdn parity: app encrypts the number **as typed** (12-digit `0…`); our client
  now maps `+62`/`62` → `0…` (no `62` normalization, unlike bima/myIM3).
- `Http.post` URL doubling: `post(C["otp_url"], …)` → base+path doubled → 404
  `Cannot POST /v1/otphttps://…`. Fix: `Http._abs()` (absolute passes through);
  regression assert in `--selftest`.
- **Root after VM reboot**: adbd is `shell` (uid 2000) → frida cannot attach to ANY
  process ("Unable to access process", even system_server). Always `su -c "/data/
  local/tmp/inj …"`. `am start` then attach ppx4b within ~2s loop (app restart-loops
  without the pairip bypass). `inj --help` = device-attach tool, runs as root via su.

## 2026-11-05f — msisdn form + V-Key kills the login path under hooks
- Live capture of `CryptoTool.encryptSSO` during a real LOGIN tap: the app handed
  it the **62-prefixed** number, NOT the `0…` form typed in the field. Python
  produces the app's exact ciphertext for that input, so `_msisdn()` normalizes to
  `62…` again. The two failed live runs had mixed variables: (62 + `"Android"`)
  and (0 + `"android"`); the app's pairing is **62 + `"android"`**.
- `getOtp` (jadx, `AxisnetApiServices`) has **no `@HMACHeader`** → only the manual
  `x-signature` from `Signature.w(...)`; header set is exactly Content-Type,
  Connection, X-API-KEY, x-app-version, x-signature (+ `x-app-request-id` from
  `RequestInterceptor`). `Utils.getVersionName()` = `"11.0.0"`.
- `RequestInterceptor` only rewrites the host per `ServerEnv` and adds
  `x-app-request-id = UUID.randomUUID()`; `SecureInterceptor` (V-Key) is just
  SSL-threat handling, no signing.
- **Hooking `CryptoTool.encryptSSO` (or `Signature.v`) makes the app exit on the
  LOGIN tap** — the `[ENC]` line is logged, then the process dies before the
  request is built (V-Key attestation). Runtime request capture via Frida is a dead
  end for this screen; read the flow statically instead (`/tmp/opencode/jadx`,
  host Java 21).
- jadx note: decompile all 9 dumps in one run (`jadx -d out *.dex`); single-dex
  runs miss the viewModel package.

## 2026-11-05g — full axisnet chain closed live (own number, consented)

1. **`/v1/otp` body key fix**: decompiled `OtpPayload` (runtime dex
   `d_0x7ae7076be000`) shows `@SerializedName("device_type")` on its
   `deviceType` field → JSON key is `device_type`, not `deviceType` (the old
   claim was an unverified guess; signature unaffected — value-based).
   `scripts/axisnet_balance.py` + §16.4 updated.
2. **`rc.c` (login payload) still absent from all nine dumps** (REA
   `search_android_classes` on `axisall.apk`: 2,802 classes, zero `rc.*`);
   only forward refs + `x.java` flow recovered (`getOtpLoginPayload` ctor
   order, 8-getter signature). Body keys verified **black-box** instead:
   server `200` with keys = the getter names
   (`msisdn, otp_code, fcm_token, fb_id, model, brand, device_os,
   device_type, imei`), `fcm_token:""`, `imei`=android_id.
   - `fb_id` recipe recovered from `CryptoTool` + Ghidra literals
     (`getStringXl`=`xl`, `getStringAxis`=`axis`, `getStringHash`=`MD5`):
     `md5("xl" + md5(msisdnIDFormat(msisdn)) + "axis")`;
     `msisdnIDFormat` = 62→0 form (from `Utils.msisdnIDFormat`).
   - `model`=Build.MODEL, `brand`=Build.MANUFACTURER, `device_os`=String
     (SDK_INT) — arbitrary-but-plausible values accepted by server.
3. **Login 200 → token** (`{"message":"generate new token"}`); **response
   `data` = DESede/ECB decrypt** (key = `sso_key`/saltKey NUL-padded to 24,
   urlsafe-b64, NUL-trailing plaintext) — confirmed by decrypting both
   otplogin and getbalance payloads to JSON.
4. **`/getbalance` `Authorization` = raw token, no `Bearer`** (long open item
   closed). Balance live: `ACTIVE`, `balance:"0"`, `pricePlan:"537158"`,
   exp `29 Dec 2026`.
5. **Transport quirk**: Cloudflare challenges the python TLS stack on
   `/otplogin/v2` + `/getbalance` (`403` managed challenge) but passes plain
   curl; `/v1/otp` passes both. `axisnet_balance.py` falls back to `curl`
   CLI on that exact 403. OTP codes expire fast (first attempt with a stale
   code → `404 "Otp not found or expired"` — the API answering at all proves
   header/signature validity).
6. `scripts/axisnet_balance.py` now: `--send-otp`, `--otp-code` (login →
   vault `~/.axisnet`), `--balance`, `--decrypt`, `--selftest` (offline:
   fb_id, login body shape, DESede roundtrip).
