package imi

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"digitral/backend/internal/vault"
)

func newVault(t *testing.T) *vault.Vault {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "vault.key"), make([]byte, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	v, err := vault.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// cfgAt pins the client to the local test server. Never let a unit test use
// ConfigBima.Base: that is the live API and would send real OTP requests.
func cfgAt(s *httptest.Server, cfg Config) Config {
	cfg.Base = s.URL
	return cfg
}

func serverJSON(t *testing.T, status int, body string) (*httptest.Server, *string) {
	t.Helper()
	seen := new(string)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*seen = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(s.Close)
	return s, seen
}

func TestGuestToken(t *testing.T) {
	s, seen := serverJSON(t, 200, `{"status":"0","code":"26100","data":{"tokenid":"a-long-guest-token"}}`)
	c := New(cfgAt(s, ConfigBima), newVault(t), DefaultDevice(), s.Client().Transport)
	tok, err := c.GuestToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tok != "a-long-guest-token" || c.Token() != tok {
		t.Fatalf("token = %q, client token = %q", tok, c.Token())
	}
	if *seen != "{}" {
		t.Errorf("body = %q, want {}", *seen)
	}
}

func TestGuestTokenRejectsFailureCode(t *testing.T) {
	s, _ := serverJSON(t, 200, `{"status":"1","code":"10001","message":"Authentication failed"}`)
	c := New(cfgAt(s, ConfigBima), newVault(t), DefaultDevice(), s.Client().Transport)
	if _, err := c.GuestToken(context.Background()); err == nil {
		t.Fatal("10001 accepted as success")
	}
}

func TestSendOTPBodyAndTransID(t *testing.T) {
	s, seen := serverJSON(t, 200, `{"status":"0","code":"26100","data":{"transId":"T-42"}}`)
	c := New(cfgAt(s, ConfigBima), newVault(t), DefaultDevice(), s.Client().Transport)
	id, err := c.SendOTP(context.Background(), "628123456789", "login")
	if err != nil {
		t.Fatal(err)
	}
	if id != "T-42" {
		t.Fatalf("transid = %q", id)
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(*seen), &body); err != nil {
		t.Fatalf("body %q: %v", *seen, err)
	}
	if body["msisdn"] != "628123456789" || body["action"] != "login" {
		t.Errorf("body = %v", body)
	}
}

func TestValidateOTPStoresSession(t *testing.T) {
	s, seen := serverJSON(t, 200, `{"status":"0","code":"26100","data":{"token":"eyJhbGciOiJIUzUxMiJ9.eyJleHAiOjQxMDI0NDQ4MDB9.c2ln"}}`)
	v := newVault(t)
	c := New(cfgAt(s, ConfigBima), v, DefaultDevice(), s.Client().Transport)
	tok, err := c.ValidateOTP(context.Background(), "628123456789", "T-42", "123456")
	if err != nil {
		t.Fatal(err)
	}
	if tok == "" {
		t.Fatal("empty token")
	}
	sess := v.Session("628123456789")
	if sess == nil || sess["token"] != tok {
		t.Fatalf("session not stored: %v", sess)
	}
	if got := fmt.Sprint(sess["exp"]); got != "4102444800" {
		t.Errorf("exp = %v, want JWT exp 4102444800", sess["exp"])
	}
	// The wire body is AES-GCM(base64) of {"transid":...,"otp":...}.
	raw, err := base64.StdEncoding.DecodeString(*seen)
	if err != nil {
		t.Fatalf("body not base64: %v", err)
	}
	block, _ := aes.NewCipher([]byte("A9$gT#2vXp!qW7LmZ@e4Ky8Nb3Hj5Uq9"))
	aead, err := cipher.NewGCMWithNonceSize(block, 16)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := aead.Open(nil, []byte("Rx4Cm9Pn2Vf8Kt6B"), raw, []byte{})
	if err != nil {
		t.Fatalf("body not the expected AES-GCM payload: %v", err)
	}
	if string(plain) != `{"transid":"T-42","otp":"123456"}` {
		t.Errorf("plain = %q", plain)
	}
}

func TestBalanceUsesStoredSession(t *testing.T) {
	raw, err := os.ReadFile("../testdata/dashboards/bima.json")
	if err != nil {
		t.Fatal(err)
	}
	s, _ := serverJSON(t, 200, string(raw))
	v := newVault(t)
	v.Data["sessions"] = map[string]any{
		"628123456789": map[string]any{"token": "stored-token", "exp": float64(4102444800)},
	}
	var gotToken string
	s.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-IMI-TOKENID")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, string(raw))
	})
	c := New(cfgAt(s, ConfigBima), v, DefaultDevice(), s.Client().Transport)
	bal, err := c.Balance(context.Background(), "628123456789")
	if err != nil {
		t.Fatal(err)
	}
	if gotToken != "stored-token" {
		t.Errorf("X-IMI-TOKENID = %q, want stored-token", gotToken)
	}
	if bal.Quota == nil || len(bal.PackageQuotas) != 1 {
		t.Errorf("balance = %#v", bal)
	}
}

func TestBalanceWithoutSessionDoesNotSendOTP(t *testing.T) {
	called := false
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = io.WriteString(w, `{}`)
	}))
	defer s.Close()
	c := New(cfgAt(s, ConfigBima), newVault(t), DefaultDevice(), s.Client().Transport)
	_, err := c.Balance(context.Background(), "628123456789")
	if err == nil {
		t.Fatal("Balance with no session should fail")
	}
	if called {
		t.Fatal("Balance must not make a network call without a session")
	}
	if !strings.Contains(err.Error(), "OTP") {
		t.Errorf("error should mention OTP, got %v", err)
	}
}

// TestGuestTokenLive is opt-in: it calls the real API and sends NO OTP.
// Run with: POC_LIVE=1 go test ./internal/provider/imi/ -run TestGuestTokenLive -v
func TestGuestTokenLive(t *testing.T) {
	if os.Getenv("POC_LIVE") != "1" {
		t.Skip("set POC_LIVE=1 to call the live guest-token endpoint")
	}
	c := New(ConfigBima, newVault(t), DefaultDevice(), nil)
	tok, err := c.GuestToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) < 100 {
		t.Errorf("token length = %d, want >= 100", len(tok))
	}
}
