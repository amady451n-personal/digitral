package myxl

// CIAM and myXL API request crypto, ported from scripts/myxl_balance.py.
// Constants below are the app values embedded in PROVIDER_MYXL; no user secret
// is stored here.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	clientID     = "db80b743-bcb9-4c2c-b55e-5609743f0ce4"
	clientSecret = "3dUJ8XUhX4Sj0Dp449bN1fu82tLzUgOf"
	hmacKey      = "18b4d589826af50241177961590e6693"
	apiKey       = "vT8tINqHaOxXbGE7eOWAhA=="
	encKey       = "880d8e7e9b4b787aa50a3917b09fc0ec"
	hmac512Key   = "3c855bf981ac7bc595925d1158a8c590d04a19520d503eea309fe81d59b9f1efca05a45f753a570653a481c3eb9a9d8f82c23aa302e01ca4095c5519f44b6438"
	appUA        = "myXL/9.6.0 (Android 13)"
	ciamUA       = "okhttp/4.12.0"
)

func hashkey(s string) []byte { return []byte(hex.EncodeToString(sha256Sum(s)))[:32] }

func sha256Sum(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}

func pkcs7(b []byte) []byte {
	p := 16 - len(b)%16
	return append(b, bytesRepeat(byte(p), p)...)
}

func bytesRepeat(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

// encrypt wraps a JSON object as {"xdata","xtime"} exactly like the app's
// RequestBodyEncryptionInterceptor (AES-256-CBC, base64url).
func encrypt(obj any, ts int64) (string, error) {
	plain, err := json.Marshal(obj)
	if err != nil {
		return "", err
	}
	key := hashkey(encKey)
	iv := hashkey(fmt.Sprint(ts))[:16]
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	ct := make([]byte, len(pkcs7(plain)))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ct, pkcs7(plain))
	return fmt.Sprintf(`{"xdata":%q,"xtime":%d}`, base64.URLEncoding.EncodeToString(ct), ts), nil
}

// decrypt reverses encrypt, including plain (unwrapped) JSON passthrough.
func decrypt(xdata string, xtime int64) (map[string]any, error) {
	raw, err := base64.URLEncoding.DecodeString(xdata + strings.Repeat("=", (4-len(xdata)%4)%4))
	if err != nil {
		return nil, err
	}
	key := hashkey(encKey)
	iv := hashkey(fmt.Sprint(xtime))[:16]
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	plain := make([]byte, len(raw))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, raw)
	if len(plain) == 0 {
		return nil, fmt.Errorf("empty plaintext")
	}
	plain = plain[:len(plain)-int(plain[len(plain)-1])]
	var out map[string]any
	if err := json.Unmarshal(plain, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// apiSignature reproduces x-hv v3: HMAC-SHA512 hex over token;ts; with a
// composite key. ts is unix seconds.
func apiSignature(token, method, path, ts string) string {
	key := strings.Join([]string{hmac512Key, token, strings.ToUpper(method), path, ts}, ";")
	msg := token + ";" + ts + ";"
	m := hmac.New(sha512.New, []byte(key))
	_, _ = m.Write([]byte(msg))
	return hex.EncodeToString(m.Sum(nil))
}

// basicAuth is the CIAM client credential header.
func basicAuth() string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(clientID+":"+clientSecret))
}

func asMap(v any) map[string]any { m, _ := v.(map[string]any); return m }
func asList(v any) []any         { l, _ := v.([]any); return l }

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(n))
		return i, err == nil
	}
	return 0, false
}

// loginSignature is the CIAM ax-request-at / ax-api-signature pair.
func loginSignature(grantType, contactType, contact, code, scope string) (string, string) {
	now := time.Now()
	at := now.Format("2006-01-02T15:04:05.000-0700")
	expiry := now.Add(300 * time.Second).Format("2006-01-02T15:04:05.000-0700")
	msg := expiry + grantType + contactType + contact + code + scope
	m := hmac.New(sha256.New, []byte(hmacKey))
	_, _ = m.Write([]byte(msg))
	return at, base64.StdEncoding.EncodeToString(m.Sum(nil))
}

// fingerprint is the ax-fingerprint device string (AES-256-CBC, zero IV, PKCS7).
func fingerprint(msisdn string) (string, error) {
	fp := strings.Join([]string{"Xiaomi", "M2101K7AG", "en", "1080x2400", "WIB", "0.0.0.0", "1.0", "Android 13", msisdn}, "|")
	block, err := aes.NewCipher([]byte(hmacKey)[:32])
	if err != nil {
		return "", err
	}
	pt := pkcs7([]byte(fp))
	ct := make([]byte, len(pt))
	cipher.NewCBCEncrypter(block, make([]byte, 16)).CryptBlocks(ct, pt)
	return base64.StdEncoding.EncodeToString(ct), nil
}
