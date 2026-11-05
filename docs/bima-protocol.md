# bima+ (Tri / Indosat-Ooredoo) API protocol — verified summary

App: `com.linkit.bimatri` 5.17.0 (versionCode 560003792) — Digitral platform (same stack as myIM3).
Reference ABI: `bima+5.17.0.apk+` (sha256 `583cfe03c5be5053f9c90c3a97b15eb4eaaa2069cb07096a8e7496dfdd9334c3`),
analysis target: extracted DEX from that APK (the old `/tmp/bima-slim/slim.apk` path is gone),
plus a live runtime OkHttp capture on MuMu 12 (§13). REA + jadx-headless 0.7.1.
Status: all five previously-flagged gaps are now CLOSED — #1–#4 decompiler/probe-verified
(§12), #5 `10011` closed via a real-app wire capture (§13).

## 1. Base URLs

- `http://bimaplus.ioh.co.id/api/v2` (cleartext HTTP in app — network security config tolerant)
- `https://bimaplus-api.ioh.co.id/api/v2` (variant)
All endpoint paths below are relative to the base. Endpoint key → URL map lives in
`com.digitral.network.common.AppSettings` (~180 keys, property-style map).

## 2. Constant headers (verified strings)

| Header | Value | Source |
|---|---|---|
| `Authorization` | `642d1cc69d90666962726e` | hardcoded in `ProjectHeaders.getHeaders` |
| `X-IMI-SERVICEKEY` | `FPi7ZP3Jy8Uv3KBd4QeG` | `BuildConfig.SERVICEKEY` |
| `X-IMI-CHANNEL` | `BIMA` | `BuildConfig.CHANNEL` |
| `X-IMI-VERSION` | application versionName, e.g. `5.17.0` | `applicationVName(context)` |
| `X-IMI-App-OS` | `Android` | `ProjectHeaders.getHeaders` |
| `X-IMI-App-OSVersion` | `Build.VERSION.RELEASE` | idem |
| `X-IMI-App-OEM` | `Build.MANUFACTURER` | idem |
| `X-IMI-App-Model` | `Build.MODEL` | idem |
| `Content-Type` | `application/json` | idem |
| `Accept-Encoding` | gzip | `GzipRequestInterceptor.ENCODING_GZIP` |
| `X-DEVICEID`, `X-DEVICENAME` | device identifiers | `putDeviceId/putDeviceName` |
| `X-IMI-LANGUAGE` | language | `putLanguage` |
| `X-IMI-UID` | `reqTime` (per-request ms epoch) | `ApiService.getOkHttpHeaders` |
| `X-IMI-NETWORK` | network type string | idem |
| `X-FAIID` | Facebook anonymous id (32-hex) | `putFirebaseInstanceId` family |
| `X-IMI-ADID` | Google advertising id (uuid-like) | `putAdId` |
| `X-IMI-ADID-ISLIMITED` | `true` / `false` | `putAdId` |
| `X-IMI-TOKENID` | token id from prefs, or `currentTimeMillis` if absent | `putToken`; forced to `currentTimeMillis` for `/token/app/stk` |
| `X-IMI-CHILD-LINENO` | RC4-encrypted child msisdn (§5) | `ApiService.mChildHeaderKey` |
| `X-LOW-QUOTA` | `YES` / `NO` (pref `isLowQuotamyIM3`) | `putUserSpecificHeaders` |
| `X-SPTRAVEL` | `YES` / `NO` (pref `isSpTravel`) | idem |
| `X-IMI-REFERER` / `X-IMI-EXTERNALID` | deeplink source / trans id (when set) | `putToken` |
| Firebase id, AdId | `putFirebaseInstanceId` / `putAdId` | idem |

Baseline header build: `ProjectHeaders.getHeaders(context)` maps are cached in `mMapHeaders`
(first fill loop, then per-request refresh of token/fb/language/user-specific/AdId).

## 3. Signatures (verified formulas)

Key helper — `MixUpValues.getValues(x)`: SHA-512 hex of `x`, then take the characters at the
**odd 1-based positions** of the hex string.

- `x-imi-oauth` (`ApiService.addOkHttpSignature(body, builder)`):
  `sha512hex("REQBODY=" + body + "&SALT=" + getValues(trim(tokenId)))` — fires whenever
  `X-IMI-TOKENID` is present, and **`X-IMI-TOKENID` is always present**: `putToken`
  (`ProjectHeaders`) reads `AppPreference.getFromStore(TOKEN_ID)` whose default is `""`
  (`prefs.getString(key,"")`), and the empty branch substitutes `System.currentTimeMillis()`.
  So guest requests carry a ms-epoch TOKENID + oauth over `{}` — omitting them = `10001`.
- `X-IMI-HASH` (`ApiService.addSecurityHash(builder)`):
  `sha512hex((childLineToken | "parent") + "$" + AppOS + "$" + appVersion + "$"
    + tokenId + "&SALT=" + getValues(reqTime))` — **separator IS a literal `'$'`**
  (`DecodedChar.FNC1 = '$'` = 36, zxing RSS-expanded decoder; verified from
  `classes8.dex` encoded_array — an earlier note claiming `\u001D` was wrong).
  `tokenId` component = the `X-IMI-TOKENID` header value (timestamp on guest, JWT after
  login); the literal `"null"` concat only occurs if the header were absent, which
  `putToken` prevents in practice.
- Both computed in `ApiService` after baseline+child headers are built (see order in §4).

### Response protocol (verified live 2026-10-22)
- Success on `/token/guest/v1`: HTTP 200, `status:"0"`, `code:"26100"`, `message:"success"`,
  and `data.tokenid` = the JWT (HS512, `mob` claim = X-DEVICEID). Failure variants use
  `status:"1"` + app codes (e.g. `10001` auth failed, `10005` invalid MSISDN, `10011`
  OTP-send risk block — now shape-correctable, §13 — `10013` wrong OTP, `10019` no device id).
- **TLS-ingress gate (root cause of the long 10001 hunt):** the `/api/v2` ingress
  fingerprint-rejects non-browser TLS stacks app-level: python-stdlib TLS → `10001
  Authentication failed` on EVERY endpoint regardless of headers (proven by A/B probe:
  signed / unsigned / tokenless / https host all 10001; same request impersonating
  Chrome via curl_cffi → `26100 success`). Transport requirement: `curl_cffi` with
  `impersonate="chrome124"` (mirrors the app's OkHttp/BoringSSL).
- Verified earlier the header set matches the decompile exactly (channel `BIMA`,
  servicekey `FPi7ZP3Jy8Uv3KBd4QeG` extracted from `com.digitral.common.BuildConfig`
  static_values; `VERSION_NAME` in manifest; `putDeviceId` = `android_id`;
  `putDeviceName` = `Settings.Global device_name` or manufacturer+model;
  fresh-installs send empty `User-Agent`; `X-FAIID`/ `X-IMI-ADID`/`X-IMI-ADID-ISLIMITED`
  are **required in practice** — their absence is what triggers the `10011` risk block
  (§13)).

- **Host split (verified by probe 2026-10-22):** the auth paths (`/otp/send/v1`) exist only on
  `https://bimaplus-api.ioh.co.id/api/v2` (HTTP 200 + app framing); the
  `http://bimaplus.ioh.co.id/api/v2` gateway product answers `/otp/send/v1` with
  `404 (backend NotFound), service rules for the path non-existent`. Both constants are in
  `classes4.dex`; `/otp/send/v1` literal confirmed in `classes4.dex`.
- **`10011` OTP-send block — CLOSED 2026-10-29 (root cause #5 = request shape).** It was
  never IP/device/context: the missing pieces were `X-FAIID` + `X-IMI-ADID(+ISLIMITED)`,
  `X-IMI-NETWORK` case `WiFi`, `X-IMI-LANGUAGE: EN`, an empty `User-Agent` header, and
  body `action:"register"`. With those the risk engine accepts even a dummy MSISDN
  (which then hits `10005` number validation). Full diff table + live proof in §13.

## 4. Header assembly order (verified: `ApiService.getOkHttpHeaders`)


1. Start from `ProjectHeaders.Companion.getHeaders(context)` — adds every entry
   (`addUnsafeNonAscii`).
2. `builder.add("X-IMI-UID", reqTime)` and `X-IMI-NETWORK`.
3. `mChildHeaderKey` (= `X-IMI-CHILD-LINENO`, set in `ApiService.<init>` as a `private final`
   field with the literal `X-IMI-CHILD-LINENO`):
   - if baseline headers already carry it → replace value with `encryptMSISDN(value)`;
   - if `mChildNumber != null` (`setChildNumber(childMsisdn)`) → remove then re-add with
     `encryptMSISDN(mChildNumber)`;
   - if `noChild == true` (`setNoChild()`) → **remove it** (parent-only request).
4. `addOkHttpSignature(requestBody, builder)` → `x-imi-oauth`.
5. `addSecurityHash(builder)` → `X-IMI-HASH`.
6. For URLs containing `/token/guest/` or `/token/app/` → `addTACIdHeader(context, builder)`;
   for URLs containing `token/app/stk` → force `X-IMI-TOKENID` to current `currentTimeMillis`.

## 5. MSISDN normalization (gap #1 — CLOSED)

`Utils.Companion.getMSISDNCountryAppended(msisdn)` (in `com.digitral.utils.Utils`,
`companion` naming for jadx): applied to msisdn before it enters the request body.

```
+62Ø prefix removal + "62" country-code prepend:
    if starts with "+"  -> strip "+"
    if starts with "0"  -> strip leading "0"
    if NOT starting with INE_CC -> prepend INE_CC
    if length() == 8 -> prepend INE_CC again
```
`INE_CC = "62"` (Kotlin `const val` field of `com.digitral.antispamlib.callmonitor.utils.Constants`;
sibling `IND_CC = "91"` is India). **Result has NO leading `+`** — e.g. `+6289561234567` →
`6289561234567`.

Related helper elsewhere: `AppUtils.getMsisdnPlus62Appended` (prints `+62...` — used in other
flows such as validate), `AppUtils.getDefaultNumberPrefix` (Tri prefixes 895–899 for input
validation), validation min/max lengths pulled from server settings
(`MSISDN_MIN_LENGTH` / `MSISDN_MAX_LENGTH`).

## 6. Cryptography primitives (all decompiled)

**RC4 — `com.digitral.common.ODPRC4`**: standard RC4. `rc4Initialize()` builds the S-box from
`password` chars (`k[i % len]`); `enDeCrypt()` is classic PRGA:
`out = sbox[(sbox[i] + sbox[j]) % 256] XOR input.charAt` — encrypt/decrypt identical; the result
is converted to an **uppercase hex string** (`encrypt = strToHexStr(enDeCrypt(str))`).
Constructor stores the raw key string.

**`ApiService.encryptMSISDN(msisdn)` = `new ODPRC4("123").encrypt(msisdn)`** — i.e. RC4 with the
literal key `"123"`, hex-encoded output. Used **only** for the child-line header value.

**AES — `ODPAESEncryption`** (previous session): AES-256-GCM (`GCMParameterSpec(128, iv)`), key
`A9$gT#2vXp!qW7LmZ@e4Ky8Nb3Hj5Uq9` (32 bytes, the "PINPUK" family key), **static IV**
`Rx4Cm9Pn2Vf8Kt6B` (16 bytes), `Cipher AES/GCM/NoPadding`, output Base64 **NO_WRAP**.
`encryptNew(...)` is used on login/validate bodies. `generateSecretKey()` derives the key from the
literal.

## 7. OTP flow

### 7.1 Send OTP — `POST /otp/send/v1` (endpoint key `sendOtp`)

- Caller chain (verified): `LoginFragment.onClick` →
  `LoginViewModel.requestSendOTP(context, input, mAction)` →
  `LoginRepository.requestSendOTP(requestId, context, body, callback)`.
- **Body is PLAINTEXT JSON** (not AES-encrypted):
  `{"msisdn":"<getMSISDNCountryAppended(input)>","action":"<action>"}`
  e.g. `{"msisdn":"6289561234567","action":"register"}` (live capture; the app sends
  `register` for a first login on a device; `login` also accepted for repeat logins)
- `action` comes from `LoginFragment` arguments (bundle key `action`,
  `LoginFragment.Companion.newInstance(action)`); doc-recorded values: `login`, `registration`.
- Headers: full baseline (§2) + OAuth + HASH; guest context (no token yet).
- `mRestoreInput` keeps the raw input for re-display.

### 7.2 Validate OTP — `POST /otp/validate/v2`

- Body **IS AES-encrypted**: `ODPAESEncryption.encryptNew(...)` over the request payload with the
  PINPUK AES-256-GCM key + static IV (Base64). (Verified previous session:
  `LoginRepository.requestValidateOTP`.)
- Response models: `ValidateOTPData` / `ValidateOTPObject`, `OTPData`.

### 7.3 Token acquisition

- `POST /token/guest/v1`, `POST /token/app/v1`, `POST /token/app/stk` (special TOKENID handling,
  §4.6). `addTACIdHeader` applies to the first two.
- Token is stored (`Constants.TOKEN_ID` pref) and reused as `X-IMI-TOKENID` + in both signatures.

## 8. Quota / balance (gap #2 — CLOSED)

- `SharedViewModel` holds `mRepository : SharedRepository`
  (`com.digitral.modules.myim3.repositories.SharedRepository`).
- `SharedRepository.getDashboard(context, requestId, callback)`:
  `new ApiService(...).initRequest(id, "getDashboard", "{}", DashboardObject.class, "")`
  — **the request body literally is `{}`**; all identity/targeting travels in the headers.
- Endpoint key `getDashboard` → `POST /dashboard/get/v7`.
- Response (live-verified 2026-10-29): body is an **XML envelope**
  `<Response><status>0</status><code>26000</code>...<data>{JSON...</data></Response>`;
  unwrap `<data>` (see `BimaClient._unwrap`). Live keys are **lowercase**:
  `prepaidinfo.balance` / `cardactiveuntil` / `graceperioduntil`,
  `summary.data.remainingquota|usedquota|initialquota|quotaunit("KB")|isunlimited`,
  `bonstriDetails.totalPoints`, `packdata.lastbalance` / `packageslist[]`
  (`total_data` in bytes, `PackageName`, `EndDate`, `expMsg`). The decompile-guessed
  camelCase (`prepaidInfo`/`remainingQuota`) does not appear on the wire.
- Other verified endpoint keys: `wallets/balance`, `user/quota`, `customerprofile/get`,
  `dashboard/activepack/v2`, `primary/lines`, `primary/addline`, `primary/removeline`.

## 9. Multi-number ("child line") behaviour (gap #3 — CLOSED)

- Header name constant: `X-IMI-CHILD-LINENO` (`ApiService.mChildHeaderKey`, literal set in ctor
  — confirmed at bytecode level via `const-string` owner scan; the jadx decompile output
  omits the assignment but the smali/literal proves it).
- Encrypted form: `encryptMSISDN(childMsisdn)` = RC4("123") hex. Parent msisdn IS sent raw on
  other headers (X-IMI security Note: server-side token binds the primary account).
- To act AS the parent: `setNoChild()` → header removed entirely. This is what single-number
  (parent-line) dashboards use.
- So a session with token for account A can query line details for one of its registered child
  lines by setting the RC4-hex child header per request, or the parent by clearing it.

## 10. Login action values (gap #4 — CLOSED-enough)

`LoginFragment.mAction` is forwarded **verbatim** to `requestSendOTP` (verified in `onClick`).
It is set from the fragment arguments (bundle key `action`, set by
`LoginFragment.Companion.newInstance(action)`); recorded literal values from the app resources:
`login` and `registration`. In the constructor jadx shows only `mState = Constants.LOGIN_PAGE`
(no mAction initializer), so the value always arrives via the bundle argument.
For a reimplementation, send `action":"register"` for the first login of a number on a
new device (what the app does; what cleared the risk gate live), `login` for repeats.

## 11. Known behaviour caveats

- ~~web selfcare returns stale shadow data; app-API was `10011`-blocked~~ — **superseded
  2026-10-29**: the app-API path is now fully working from the Python client (§13);
  the web `/apibima` lane stays documented but is no longer needed. Expect the TLS
  ingress gate to require chrome-impersonation (§3) — that part is unchanged.
- Tokens must be persisted **encrypted at rest**, never logged; the AES/RC4 keys above are
  extracted static app secrets, treat usage as private research.

## 12. Session log — what closed the four gaps (provenance)

1. `getMSISDNCountryAppended` decompiled (`Utils.Companion`); `INE_CC` resolved to `"62"` by
   decoding the `encoded_array` of the Constants class in `classes3.dex`. NOTE: that DEX encodes
   `VALUE_STRING` payloads as **little-endian ints**, not ULEB — reading them as ULEB produces
   garbage (initial bug in the analysis pipeline, fixed; all 110 constants then decode sensibly
   e.g. `ACCESS_TOKEN = "accessToken"`).
2. Dashboard body: `SharedRepository.getDashboard` decompiled — `"{}"` literal confirmed.
3. `ApiService.encryptMSISDN` + `mChildHeaderKey` init: RC4("123") confirmed via
   `rc4Initialize`/`enDeCrypt`/`<init>`; literal `X-IMI-CHILD-LINENO` ownership proven by
   byte-scan of `const-string` instructions (2 refs: ApiService ctor + LifeModuleResponse default,
   plus OnboardingBrowserFragment in classes12).
4. `LoginFragment.mAction`: bundle-driven; `onClick` passes it directly to `requestSendOTP`;
   `newInstance(action)` builds the bundle with key `action`.

Tooling note for future re-analysis:
- `search_android_classes` times out/empty often — prefer DEX-string scanning + targeted
  `inspect_android_class` / `inspect_android_method`.
- Class naming with Kotlin companions works as `Class.Companion` (dot), jinames `$Companion`
  are inconsistent.
- Field *values* are not returned by REA summaries — use the DEX `encoded_array` decode (with the
  uppercase-->lower quirk: LE payload) or read the smali.

## 13. Live app capture (MuMu 12, 2026-10-29) — `10011` CLOSED at last

First successful Frida OkHttp tap of the real app on MuMu Player 12 (attach-after-spawn
dodges the anti-tamper startup sweep; a delayed in-process watchdog still kills the target
~30 s after attach — recover by re-launching without hooks). Ground truth vs our client:

| difference | app (capture) | our old client | fix |
|---|---|---|---|
| `X-FAIID`, `X-IMI-ADID`, `X-IMI-ADID-ISLIMITED` | present | **absent** | added (stable per vault) |
| `X-IMI-NETWORK` | `WiFi` | `WIFI` (case!) | now `WiFi` |
| `X-IMI-LANGUAGE` | `EN` | `id` | now `EN` |
| `User-Agent` | literal empty header | (none) | empty string |
| `/otp/send/v1` body | `action:"register"` | `login` default | `--action register` |

Result: the previously in-block dummy number (formerly `10011` every time) now answers
`10005 "Mobile Number is not valid."` = risk engine **passed**, server-side number
validation rejects the bogus number only — root cause #5 (request shape) was the gate.
Live-fire (user-owned MSISDN): guest → send (`register`) → SMS → validate → session JWT
→ `/dashboard/get/v7` **all green from pure Python, same home IP as the emulator**.

Dashboard response shape (v7): body is an XML envelope
`<Response><status>0</status><code>26000</code>…<data>{JSON}</data></Response>`; parse via
`<data>` unwrap (see `BimaClient._unwrap`). Terrific keys (lowercase): `prepaidinfo.balance`,
`prepaidinfo.cardactiveuntil`, `prepaidinfo.graceperioduntil`,
`summary.data.remainingquota|usedquota|quotaunit(KB)|isunlimited`,
`bonstriDetails.totalPoints`, `packdata.lastbalance`.

Client usage now: `scripts/bima_balance.py <msisdn> --action register` once per number
(OTP prompt), then reruns hit the persisted session; `--watch N` polls every N seconds.
Session JWT TTL observed: ~60 days (guest JWT `exp-iat` ≈ 5,184,000 s).

## 14. myIM3 (com.pure.indosat.care, 82.17.0) — same stack, brand constants swapped

Same Digitral platform as bima+ (verified by re-decoding `myim3-82.17.0.apk+` dex statics with
the same decoder validated against bima+'s own BuildConfig values). Cryptographic material and
all endpoint paths are byte-identical to bima+; only the brand constants differ:

| item         | myIM3                             | bima+                           |
|--------------|-----------------------------------|---------------------------------|
| package      | `com.pure.indosat.care`           | `com.linkit.bimatri`            |
| version      | 82.17.0 / vc 560003790            | 5.17.0 / vc 560003792           |
| SERVICEKEY   | `i4WxFMMLvWqnrvuAyg58`            | `FPi7ZP3Jy8Uv3KBd4QeG`          |
| CHANNEL      | `V2MYIM3`                         | `BIMA`                          |
| ANTI_PROECTION_CLIENTID | `im3`                  | `bima`                          |
| base (http, app default) | `http://myim3-he1.ioh.co.id/api/v2`  | `http://bimaplus.ioh.co.id/api/v2` |
| base (https product)     | `https://myim3api1.ioh.co.id/api/v2` | `https://bimaplus-api.ioh.co.id/api/v2` |
| PINPUK / IV / Authorization / header set / paths | identical | identical |

Client: `scripts/myim3_balance.py` (thin wrapper over `bima_balance`, vault `~/.myim3`,
`BIMA_HOME` respected, `--base-url http://myim3-he1.ioh.co.id/api/v2` to switch host).
Live-verified 2026-10-29: full guest → `/otp/send/v1` → `/otp/validate/v2` chain green from
pure Python on a user-owned MSISDN. First validate try returned `10013` with our body proven
correct by ciphertext length (54 ct bytes exactly `{"transid":…,"otp":…}`); fresh re-send +
retype of the newest SMS code resolved it — rule of thumb: 10013 = stale/mistyped code,
not a protocol shape problem.

## 15. axisnet (com.axis.net, 11.0.0 / vc 836) — static recon: obfuscated, NOT Digitral stack

Checked 2026-11-03 from `axisnet-11.0.0.apk+` (sha256 wrapper→`base.apk`
`c697134ad38ce77e023054c6e372bcaf7d8bf352e38d9b1fc6248fb908037bc2`, REA jadx 0.7.1 +
dex static-field decoder). Conclusions per evidence class:

| item | evidence |
|------|----------|
| package / version | `com.axis.net`, versionName 11.0.0, versionCode 836, minSdk 24, targetSdk 36 (REA `inspect_android_package`, axis-mini.apk `be8d689bfb…`) |
| NOT the Digitral/linkit family | no `com.digitral.*` / `com.linkit.*` classes; per-provider `SERVICEKEY/CHANNEL/PINPUK/IV` scheme does not exist here — wiring it into `make_provider_cfg` is invalid |
| app Java code is protected | `libudihrdowpk.so` banner `********** V-Key DexObfuscator version: %s **********` (VKEYObfuscation,;mbedtls AES-XTS/GCM); **zero** `Lcom/axis/net/*` class_defs in all 8 base dexes, while 781 `Lcom/axis/net/*` *type references* exist (classes3) → second-stage dex decrypted in memory at runtime |
| constants layer | `libnative-lib.so` (sha256 `997003699c3025c435cc790470af8a883409a17cfb7b9131feaa73305fd071b9`, arm64) exports **272 JNI getters** (`Java_com_axis_net…`) incl. `RequestInterceptor_hostName{New,Production,Staging,Uat}`, `Signature_apiKey`, `Signature_keyTempest{,Capital,Guardian,Market,MarketCapital}`, per-feature `…Repository_<verb>Url`, `BalanceConvertApiImplKt_url{Get,Proceed}BalanceConvert`, `TransferBalanceRepository_otpTransferBalanceUrl`, `PackageBalanceApiImpl_getTopUpBalancePackageApi` — full list `docs/axisnet-jni-surface.txt`, value map `docs/axisnet-url-map.txt`. First-pass greps missed the values (random-looking literals); Ghidra shows they are plaintext `.rodata` strings wrapped by trivial literal→`NewStringUTF` getters |
| RN layer | `assets/index.android.bundle` (3.5 MB metro) holds `clientCode 'JGH5T9ZQ2'` + `gateway.{sit,uat,pre-,}modula.xlsmart.co.id` — that is the **Modula survey SDK only** (env table `{name:"sit|uat|preprod|prod"}`), not the balance API |
| misc | AliPay security-guard assets (`firmware`, `profile`, `signature`, `sig_cfs`, `vkeylicensepack`, `voscodesign.vky`); `aes-everywhere` (AES-CBC-256) present — payload crypto differs from bima's AES-GCM; xendit & iapconnect config in assets; 7 encrypted `assets/tmp*` blobs (single-byte-XOR no hits) |
| modula header scheme (SDK-local, NOT necessarily axisnet main API) | `X-Brand-Code`, `X-Source-Env`, `X-Device-Id`, `X-Msisdn`, `X-Sdk-Version`, `X-Platform`, `X-App-Version`, `X-Timestamp` (epoch s), `X-Nonce = hex(HMAC-SHA256(brandSecret, lower(brandCode)+brandSecret+deviceId+ts))`, `Authorization: Bearer <token>`; responses `base64 → AES(aesKey)` envelope (`/api/data/sdk/init` flow) |

**Why the first-pass hex/strings recon failed but Ghidra succeeded**: V-Key DexObfuscator
keeps every app class away from the shipped dexes; the first-pass concluded the values
were runtime-only. Actual truth (Ghidra 12.2 headless, Docker, 2026-11-03): the URL
constants and key material are **plaintext in `libnative-lib.so` `.rodata`** — the
strings look like random noise (`hOc36lLcbQ8J…`), so keyword greps missed them; every
JNI getter is a trivial literal→`NewStringUTF` wrapper. Ghidra named each literal and
the decompiled copy-spans gave the exact extents (verify with
`docs/axisnet-verify.py`-style re-derivation: image base 0x100000):

| function (JNI)                                  | literal (exact) |
|-------------------------------------------------|-----------------|
| `Signature_apiKey`                              | `ipRsTxOLbgogxEiXj1P73JVWgQk9CEXMSuIsaFO8t6I=` |
| `Signature_keyTempest`                          | `hOc36lLcbQ8JLLGvhekVMVGy0w9hTM9EazVL1Z2aF9BiUG5Ov88rQg==` |
| `Signature_keyTempestCapital`                   | `d9kKoBWgOltACb1_e7kjkYxD6HaX1eVQ9Ap0xh3xcetx__gFYyStSRw2x5t54y9VBl7xZ5dIDZqcEtYpxTzLHyuQaBvNrPkl` |
| `Signature_keyTempestMarket`                    | `VEOXH8EAnFARSFDtbNnvxA==` |
| `Signature_keyTempestMarketCapital`             | `bA7mQfB73531XTWqAt4ItWgEARlLPqrLNOiuznUg0QZ3kO4I8beMwgQT066-ulysRzQTvflCIPsr52R9E4iNbq3IXUNwP9wp` |
| `Signature_keyTempestGuardian`                  | `C6EwP6Rw4OeYajoNm7imW9LUWlQll0A=` |
| `AxisnetHelpers_getSaltKey` / `getMsisdnSaltKey` / `OtpApiService_saltKey` / `NativeLibImpl_getSaltKey` | `ae432353a4eb4eb9` |
| `AxisnetHelpers_getSecretKey`                   | `2eb9362dd64b1e56` |
| `AxisnetHelpers_getNewSaltKey`                  | `saltkey` |
| `NativeLibImpl_getAlgorithmKey`                 | `5LuLv64KzyK656HwtHJ35GxG54S4tACA4` |
| `AxisnetHelpers_getNewAccessToken`              | `accesstoken` (pref key name) |
| `AxisnetHelpers_getAutorepurchaseKey`           | `eyJzZXJ2aWNlX2lkIjogIjMyMTMxNzcifQ==` = `{"service_id": "3213177"}` |
| `RequestInterceptor_hostNameProduction`         | `api.axis.co.id` |
| `RequestInterceptor_hostNameStaging`            | `staging-api.axis.co.id` |
| `RequestInterceptor_hostNameUat`                | `uat-api.axis.co.id` |
| `RequestInterceptor_hostNameNew`                | `new-api.axis.co.id` |

Full endpoint surface: `docs/axisnet-url-map.txt` — 261/272 JNI getters map to
embedded literals; the auth-critical chain is

1. OTP request   → `https://otp.api.axis.co.id/v1/otp`            (`AuthApiService_otpUrl`, `OtpApiService_uOtp`)
2. OTP login     → `https://otp.api.axis.co.id/otplogin/v2`       (`AuthApiService_otpLoginUrl`)
3. logout/fcm    → `https://otp.api.axis.co.id/{logout,fcm}`
4. auto-login tok→ `https://ns.api.axis.co.id/v1/al/create-token` (`AuthApiService_createTokenAutoLoginUrl`)
5. balance       → `https://balance.api.axis.co.id/getbalance`    (`HomeApiImpl_getBalanceUrl`, `PaymentApiService_balanceUrl`)
6. package balance→`https://pkgbalance.api.axis.co.id/packages`   (`PackageBalanceApiImpl_getTopUpBalancePackageApi`)
7. registration  → `https://user.api.axis.co.id/registration`     (preflight `…/prefix`)
8. profile       → `https://profile.api.axis.co.id/getuser`

**CLOSED 2026-11-04** (see §16): header names and the signature construction were
recovered by runtime reflection + an in-memory dex dump of the V-Key-decrypted app
dex (procedure: `docs/decisions/2026-11-03-axis-recon.md` §2026-11-03c).

Toolchain reproduced in-repo: `scripts/ghidra-docker/` (Dockerfile pins Ghidra
latest at build time — 12.2 PUBLIC used here, requires temurin **25-jdk-noble**;
JDK 21/24 are rejected by `launch.sh`) + `DecompileByPrefix.java` headless post-script.

Tools committed for follow-ups: `scripts/dex_static_fields.py` (validated against
bima+ BuildConfig: reproduces `SERVICEKEY`, `CHANNEL=BIMA`, `VERSION_CODE` exactly,
before use).

## 16. axisnet signed-request scheme (runtime dex, 2026-11-04)

Header names + signature construction were extracted from the **V-Key-decrypted
in-memory dex** (dumped from `[anon:dalvik-DEX` via Frida; §2026-11-03c in
`docs/decisions/`), then replayed against the live classes to confirm every
constant. Nothing here was guessed and no OTP was sent.

**Two interceptors only** (`AxisnetApiServices` is the single Retrofit interface):

| interceptor | adds | fires |
|---|---|---|
| `com.axis.net.config.RequestInterceptor` | `x-app-request-id` = `UUID.randomUUID()` | every request |
| `com.axis.net.helper.HMACSignatureInterceptor` | `X-Signature`, `X-Handshake` | only methods annotated `@com.axis.net.helper.HMACHeader` |

`RequestInterceptor.a(k)` = `builder.headers(old).addHeader("x-app-request-id", uuid).addHeader(j(), c())`
and `b(url)` rewrites the host for staging/uat/new (`n()`/`t()` etc. are the R8
names of okhttp `url()`/`toString()`).

### 16.1 Signature primitives (`com.axis.net.config.Signature`)

- `f(value, key)` = **hex(HmacSHA256(key, value))** ← all hashing.
- `c(bytes)` = lowercase hex.
- `v()/j()` (unauthenticated: `/v1/otp`, `/otplogin/v2`, `/prefix`):
  `msg = "POST;" + path + ";" + msisdn + ";" + otp + ";" + fcmToken + ";" +
  firebaseId + ";" + model + ";" + brand + ";" + deviceOs + ";" + deviceType +
  ";" + apiKey()`, key = `u()` = `decrypt(keyTempestCapital)` (prod).
- `h(method, path, json, token, handshake)` (authenticated, `@HMACHeader`):
  `msg = method + ";" + path + ";" + json + ";" + token + ";" + handshake`,
  key = `g()` = `decrypt(keyTempestMarketCapital)` (prod), with
  `token` = the request's `Authorization` header and `json` = raw body ("" for GET).
- `i()` = `encryptSSO(str(DateHelper.getTodayTimeStamp()))` with newlines stripped
  → **`X-Handshake`**. `getTodayTimeStamp()` is **epoch milliseconds**.
- `x()` = staging check (`ServerEnv.STAGING.getKey()` vs host) → prod path uses the
  `*Capital` Tempest key.

### 16.2 Crypto (`CryptoTool` / `KeyClass` / `EnumCrypto`)

- `ALGORITHM=DESede`, `MODE=ECB`, `PADDING=NoPadding`.
- `encryptString(v, key)`: NUL-pad `v` to a multiple of 8, NUL-pad `key` to **24**,
  3DES-ECB, `Base64 NO_WRAP`, then `+`→`-` `+` `/`→`_` (URL-safe, keeps `=`).
- `encryptSSO(v)` = `encryptString(v, KeyClass.sharedKey)` and `sharedKey` =
  `AxisnetHelpers.getMsisdnSaltKey()` = `ae432353a4eb4eb9`.
- `encrypMsisdnMD5(x)` = `md5hex("xl" + md5hex(msisdnIDFormat(x)) + "axis")` (MD5 via
  `getStringHash()`).
- `decryptString(x, key)`: same 3DES, applied to the Base64 (URL-safe) input; it is
  what turns the `keyTempest*` JNI constants into the HMAC keys.

### 16.3 Per-endpoint header map (Retrofit annotation → header)

| endpoint | method | headers (besides `x-app-request-id`) |
|---|---|---|
| `/getbalance` (balance.api) | GET | `Authorization`, `x-app-version` — **no** `@HMACHeader` |
| `/getbalance/convert`, `/getbalance/pulse` | GET | same, `getBalanceConvert` is `@HMACHeader` |
| `/quota` (quota.api) | GET | `Authorization`, `x-app-version` |
| `/v1/otp` (otp.api) | POST `@Body` | `Content-Type: application/json`, `Connection: keep-alive`, `X-API-KEY`, `x-app-version`, `x-signature` |
| `/otplogin/v2` (otp.api) | POST `@Body` | same as above (body = `rc.c`) |
| `/prefix` (user.api) | POST `@Field("content")` | `x-handshake`, `x-signature`, `x-app-version`, `x-device` (= `"Android " + Build.VERSION.RELEASE`), `X-API-KEY` |

`x-app-version` = `Utils.getVersionName()` = `11.0.0`. `X-API-KEY` is the literal
above for `/otplogin/v2` and `OtpApiService.xApikey()`'s literal for `/v1/otp`.

### 16.4 Login payloads

- `/v1/otp` body (`OtpPayload`): `{ "msisdn": encryptSSO(msisdn) (newlines stripped),
  "device_type": "android" }` — the JSON key is **`device_type`**, from
  `OtpPayload`'s `@SerializedName("device_type")` on its `deviceType` field
  (runtime dex `d_0x7ae7076be000`, decompiled 2026-11-05; the earlier
  `"deviceType"` key claim was a guess and never runtime-verified). The
  signature message carries the VALUE only, so it is unaffected:
  signature = `v("/v1/otp", msisdn_enc, deviceType="android")`.
  **`deviceType` is `Consta.CHANNEL_ANDROID` = `"android"`
  (lowercase, read from the live app): `"Android"` makes the server reply
  `401 {"error_message":"Invalid signature"}`. The app normalizes the msisdn to the
  `62…` form BEFORE `encryptSSO` (runtime capture: plaintext handed to
  `CryptoTool.encryptSSO` was `62…`, not `0…`); Python reproduces the app's
  ciphertext byte-for-byte for that input, so `62…` is the form to send.**
- `/otplogin/v2` body (`rc.c`): constructed in
  `splashLogin/viewModel/x.validateOtpCode` → `getOtpLoginPayload(otp, imei)`:
  `msisdn` = `encryptSSO(msisdn)`, `otp_code` = `encryptSSO(otp)`, `fb_id` =
  `encrypMsisdnMD5(msisdnIDFormat(msisdn))`, plus `manufacture`, `deviceName`,
  `channel` (`Consta.CHANNEL_ANDROID`), `device_os`, `fcm_token`, `imei`;
  signature = `v("/otplogin/v2", msisdn, otp, fcm, fb_id, model, brand,
  device_os, device_type)`. **Verified live 2026-11-05:** the body keys are
  exactly the getter names —
  `{"msisdn","otp_code","fcm_token","fb_id","model","brand","device_os","device_type","imei"}`
  — with `fcm_token: ""` (app's no-FCM state) and `imei` = `android_id`; the
  server answered `200` and issued a token. `rc.c` still does not decompile from
  any of the nine dumps, so verification is black-box plus the recovered getter
  semantics above (`model` ← `Build.MODEL`, `brand` ← `Build.MANUFACTURER`,
  `device_os` ← `String.valueOf(SDK_INT)`).

### 16.6 Login chain closed live (2026-11-05, own number, consented)

- `/v1/otp` with the `device_type` key and `62…` msisdn → `200`, SMS sent
  (`data` = base64 of `{"can_login":true,…}`, **plain**, not encrypted).
- `/otplogin/v2` with the body above → `200`. Response envelope
  `{"status":true,"data":"<b64url>"}`; after decrypt:
  `{"id":"","name":"",…,"token":"<uuid>","message":"generate new token"}`.
- **Response `data` decrypt** = `CryptoTool.decryptString(v, saltKey)`:
  **DESede/ECB** (no padding option in openssl terms — the plaintext carries
  trailing NUL bytes, strip them), key = `saltKey` (`ae432353a4eb4eb9`) NUL-padded
  to 24 bytes, input is urlsafe Base64 (`-`→`+`, `_`→`/`). Applies to both
  otp.api and balance.api `data` fields.
- **`/getbalance` `Authorization` = the raw token string, NO `Bearer` prefix**
  (open item since §16.1 – now confirmed `200`). Response `data` decrypt yields
  `{"txid","msisdn","graceStatus","status","result":{…,"balance","pricePlan","subcriberID"}}`.
- Cloudflare edge: `otp.api`/`balance.api` serve a managed challenge (`403`,
  "_cf_chl"/"Just a moment") to the python TLS stack on `/otplogin/v2` and
  `/getbalance`, while plain `curl` passes; `/v1/otp` passes both.
  `scripts/axisnet_balance.py` therefore falls back to the `curl` CLI on that
  specific challenge.

Implemented as `PROVIDER_AXISNET` + `axisnet_*` helpers in
`scripts/bima_balance.py` (entry point `scripts/axisnet_balance.py`, mirroring
`myim3_balance.py`): `--send-otp`, `--otp-code` (login + vault), `--balance`,
`--decrypt`, `--selftest` (offline: fb_id recipe, `/otplogin/v2` body shape,
DESede decrypt roundtrip).

### 16.5 Verification vectors (live Frida, exact)

```
encryptSSO("081234567890")                      = X6diZ_QuncTuKBc5e5BJQA==
encryptSSO("1700000000")                        = KdOSI_lRdvIgo6qoFV-OSQ==
v("/v1/otp","081234567890",…,"Android")         = 6e63d46524144be4ba6b749263c909f2a4d9134beb0fd800aee6ca3c86543f4d
v("/v1/otp","X6diZ_QuncTuKBc5e5BJQA==\n",…,"Android") = f14dae59e401449939bc74bc75959165026337f108768b6a3a16cc3e5bef949c
v("/v1/otp","X6diZ_QuncTuKBc5e5BJQA==",…,"android")   = c9501cdf663e1269c1b2845e61be0b4d3ff64f73915074a7a852a03baf1d9078  (== real request)
h("GET","/balance","","tok","hs")               = 93bcd689a4f9c0b0e6cc7ff9bf6a92b7737fc8649fbb51cc32e6c6fc95808ec8
u() (hmac_login key)                            = 7L20wW59wJkXG1z2vR8Q6pmnssgRptOHdC3xO9CrtOFE2u6mF7Xk9Is11S44aPTPu5
g() (hmac_market key)                           = a0A38QY0HwkMGLwLq5zrmpC17cGBj01I2pZ91v42K26X57SThVsdfBuSut7RXkJ9eR
```

Implemented as `PROVIDER_AXISNET` + `axisnet_*` helpers in
`scripts/bima_balance.py` (entry point `scripts/axisnet_balance.py`, mirroring
`myim3_balance.py`); `python3 scripts/bima_balance.py --selftest` asserts the
`/v1/otp` vectors offline. The full live chain (OTP → login token → balance) is
documented in §16.6 above.

