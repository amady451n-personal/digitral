package axisnet

import (
	"crypto/cipher"
	"crypto/des"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// Constants from PROVIDER_AXISNET in scripts/bima_balance.py (decompiled app).
const (
	appVersion = "11.0.0"
	appOS      = "android"
	apiKey     = "ipRsTxOLbgogxEiXj1P73JVWgQk9CEXMSuIsaFO8t6I="
	hmacLogin  = "7L20wW59wJkXG1z2vR8Q6pmnssgRptOHdC3xO9CrtOFE2u6mF7Xk9Is11S44aPTPu5"
	ssoKey     = "ae432353a4eb4eb9"
	otpURL     = "https://otp.api.axis.co.id/v1/otp"
	loginURL   = "https://otp.api.axis.co.id/otplogin/v2"
	quotaURL   = "https://nq.api.axis.co.id/v1/quota/homepage"
	balanceURL = "https://balance.api.axis.co.id/getbalance"
)

// pad matches CryptoTool.doPadWithString: append NULs only when the length is
// not already a multiple of block.
func pad(value string, block int) string {
	p := block - len(value)%block
	if p > 0 && p < block {
		return value + strings.Repeat("\x00", p)
	}
	return value
}

func ssoCipher() (cipher.Block, error) {
	key := []byte(pad(ssoKey, 24))
	if len(key) != 24 {
		return nil, fmt.Errorf("sso key: got %d bytes, want 24", len(key))
	}
	return des.NewTripleDESCipher(key)
}

// ssoEncrypt is CryptoTool.encryptString: DESede/ECB/NoPadding + urlsafe base64.
func ssoEncrypt(value string) (string, error) {
	block, err := ssoCipher()
	if err != nil {
		return "", err
	}
	plain := []byte(pad(value, 8))
	if len(plain)%8 != 0 {
		return "", fmt.Errorf("sso input not block-aligned: %d", len(plain))
	}
	ct := make([]byte, len(plain))
	for i := 0; i < len(plain); i += 8 {
		block.Encrypt(ct[i:i+8], plain[i:i+8])
	}
	return strings.NewReplacer("+", "-", "/", "_").Replace(base64.StdEncoding.EncodeToString(ct)), nil
}

// ssoDecrypt is CryptoTool.decryptString (NUL-trailing plaintext removed).
func ssoDecrypt(data string) (string, error) {
	block, err := ssoCipher()
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(strings.NewReplacer("-", "+", "_", "/").Replace(data) + strings.Repeat("=", (4-len(data)%4)%4))
	if err != nil {
		return "", err
	}
	if len(raw)%8 != 0 {
		return "", fmt.Errorf("sso ciphertext not block-aligned: %d", len(raw))
	}
	plain := make([]byte, len(raw))
	for i := 0; i < len(raw); i += 8 {
		block.Decrypt(plain[i:i+8], raw[i:i+8])
	}
	return strings.TrimRight(string(plain), "\x00"), nil
}

// loginSign is Signature.v()/j(): hex(HMAC-SHA256(hmac_login, msg)).
func loginSign(path, msisdn, otp, fcm, firebaseID, model, brand, osVer, deviceType string) string {
	msg := strings.Join([]string{"POST", path, msisdn, otp, fcm, firebaseID, model, brand, osVer, deviceType, apiKey}, ";")
	m := hmac.New(sha256.New, []byte(hmacLogin))
	_, _ = m.Write([]byte(msg))
	return hex.EncodeToString(m.Sum(nil))
}

func fbID(msisdn string) string {
	zero := msisdn
	if strings.HasPrefix(zero, "62") {
		zero = "0" + zero[2:]
	} else if strings.HasPrefix(zero, "+62") {
		zero = "0" + zero[3:]
	}
	inner := md5.Sum([]byte(zero))
	return fmt.Sprintf("%x", md5.Sum([]byte("xl"+hex.EncodeToString(inner[:])+"axis")))
}

func requestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "00000000-0000-0000-0000-000000000000"
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// otpRequest builds (url, headers, body). login=true builds /otplogin/v2 with
// the encrypted OTP; otherwise /v1/otp. Both are deterministic except the id.
func otpRequest(msisdn, otp string, login bool) (string, map[string]string, string) {
	ms, _ := ssoEncrypt(msisdn)
	ms = strings.ReplaceAll(ms, "\n", "")
	path, url := "/v1/otp", otpURL
	h := loginSign(path, ms, "", "", "", "", "", "", appOS)
	body := fmt.Sprintf(`{"msisdn":%q,"device_type":%q}`, ms, appOS)
	if login {
		const (
			model = "SM-A235F"
			brand = "samsung"
			osVer = "31"
			imei  = "b3a9c7d5e1f20364"
		)
		ot, _ := ssoEncrypt(otp)
		ot = strings.ReplaceAll(ot, "\n", "")
		fb := fbID(msisdn)
		path, url = "/otplogin/v2", loginURL
		h = loginSign(path, ms, ot, "", fb, model, brand, osVer, appOS)
		body = fmt.Sprintf(`{"msisdn":%q,"otp_code":%q,"fcm_token":"","fb_id":%q,"model":%q,"brand":%q,"device_os":%q,"device_type":%q,"imei":%q}`,
			ms, ot, fb, model, brand, osVer, appOS, imei)
	}
	return url, map[string]string{
		"Content-Type":     "application/json",
		"Connection":       "keep-alive",
		"X-API-KEY":        apiKey,
		"x-app-version":    appVersion,
		"x-signature":      h,
		"x-app-request-id": requestID(),
	}, body
}
