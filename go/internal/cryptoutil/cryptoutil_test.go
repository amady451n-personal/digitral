package cryptoutil

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"
)

type vectors struct {
	GetValues     [][]any `json:"get_values"`
	SHA512Hex     [][]any `json:"sha512_hex"`
	AESGCMEncrypt [][]any `json:"aes_gcm_encrypt"`
	JWTExp        [][]any `json:"jwt_exp"`
	RC4Hex        [][]any `json:"rc4_hex"`
}

func loadVectors(t *testing.T) vectors {
	t.Helper()
	b, err := os.ReadFile("testdata/crypto_vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v vectors
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func str(t *testing.T, v any) string {
	t.Helper()
	s, ok := v.(string)
	if !ok {
		t.Fatalf("vector value is %T, want string", v)
	}
	return s
}

// Every case was produced by scripts/bima_balance.py, so this test compares
// Go against the Python implementation byte-for-byte.
func TestCryptoVectors(t *testing.T) {
	v := loadVectors(t)

	for _, c := range v.GetValues {
		in, want := str(t, c[0]), str(t, c[1])
		if got := GetValues(in); got != want {
			t.Errorf("GetValues(%q) = %q, want %q", in, got, want)
		}
	}
	for _, c := range v.SHA512Hex {
		in, want := str(t, c[0]), str(t, c[1])
		if got := SHA512Hex(in); got != want {
			t.Errorf("SHA512Hex(%q) = %q, want %q", in, got, want)
		}
	}
	for _, c := range v.AESGCMEncrypt {
		key, err := base64.StdEncoding.DecodeString(str(t, c[0]))
		if err != nil {
			t.Fatalf("key: %v", err)
		}
		iv, err := base64.StdEncoding.DecodeString(str(t, c[1]))
		if err != nil {
			t.Fatalf("iv: %v", err)
		}
		plain, want := str(t, c[2]), str(t, c[3])
		got, err := AESGCMEncrypt(key, iv, []byte(plain))
		if err != nil {
			t.Fatalf("AESGCMEncrypt(%q): %v", plain, err)
		}
		if got != want {
			t.Errorf("AESGCMEncrypt(%q) = %q, want %q", plain, got, want)
		}
	}
	for _, c := range v.RC4Hex {
		key, text, want := str(t, c[0]), str(t, c[1]), str(t, c[2])
		if got := RC4Hex(key, text); got != want {
			t.Errorf("RC4Hex(%q, %q) = %q, want %q", key, text, got, want)
		}
	}
	for _, c := range v.JWTExp {
		in := str(t, c[0])
		want := int64(c[1].(float64))
		if got := JWTExp(in); got != want {
			t.Errorf("JWTExp(%q) = %d, want %d", in, got, want)
		}
	}
}
