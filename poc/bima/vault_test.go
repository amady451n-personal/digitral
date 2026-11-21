package main

// Synthetic vault tests: one fixture per provider session shape, plus the
// Python-compatible expiry rules. No real vault (and no secret) is committed.

import (
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestVault(t *testing.T, data map[string]any) *Vault {
	t.Helper()
	dir := t.TempDir()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vault.key"), key, 0o600); err != nil {
		t.Fatal(err)
	}
	v := &Vault{Dir: dir, Key: key, Data: data}
	if err := v.Save(); err != nil {
		t.Fatal(err)
	}
	return v
}

func reopen(t *testing.T, v *Vault) *Vault {
	t.Helper()
	got, err := OpenVault(v.Dir)
	if err != nil {
		t.Fatalf("OpenVault: %v", err)
	}
	return got
}

// The container format is provider-independent: key 32B, blob iv+ct+tag, JSON body.
func TestVaultContainerIsProviderAgnostic(t *testing.T) {
	shapes := map[string]map[string]any{
		"bima":    {"deviceId": "2adbc87696db08b3", "faaid": "f", "gaid": "g", "sessions": map[string]any{}},
		"myim3":   {"deviceId": "d88694b9fc6aacfa", "sessions": map[string]any{}},
		"myxl":    {"deviceId": "01b2f3d4232e473e", "sessions": map[string]any{}},
		"axisnet": {"deviceId": "d8c4af9fd7be0372", "faaid": "f", "gaid": "g", "sessions": map[string]any{}},
	}
	for name, data := range shapes {
		t.Run(name, func(t *testing.T) {
			v := newTestVault(t, data)
			blob, err := os.ReadFile(filepath.Join(v.Dir, "vault.bin"))
			if err != nil {
				t.Fatal(err)
			}
			// 12-byte IV + ciphertext + 16-byte tag; ciphertext matches the JSON payload length.
			if got, want := len(blob), 12+16+len(mustJSON(t, data)); got != want {
				t.Fatalf("blob size = %d, want %d", got, want)
			}
			got := reopen(t, v)
			if got.DeviceID() != data["deviceId"] {
				t.Fatalf("deviceId = %q, want %q", got.DeviceID(), data["deviceId"])
			}
		})
	}
}

// bima / myIM3: exp is the login JWT expiry in seconds, checked 60s early.
func TestSessionBimaShape(t *testing.T) {
	now := float64(time.Now().Unix())
	v := newTestVault(t, map[string]any{
		"deviceId": "dev",
		"sessions": map[string]any{
			"628111111111": map[string]any{"action": "login", "deviceId": "dev", "exp": now + 3600, "token": "jwt"},
			"628222222222": map[string]any{"action": "login", "deviceId": "dev", "exp": now - 10, "token": "old"},
			"628333333333": map[string]any{"action": "login", "deviceId": "dev", "exp": now + 30, "token": "edge"},
		},
	})
	v = reopen(t, v)

	if v.Session("628111111111") == nil {
		t.Fatal("valid session rejected")
	}
	if v.Session("628222222222") != nil {
		t.Fatal("expired session accepted")
	}
	// 30s left is inside the 60s early-expiry margin.
	if v.Session("628333333333") != nil {
		t.Fatal("session inside the 60s margin accepted")
	}
	if v.RawSession("628222222222") == nil {
		t.Fatal("RawSession must ignore expiry")
	}
}

// myXL: exp tracks the refresh_token, token_exp the short-lived access token.
// An expired session must still be reachable so refresh_token can be replayed.
func TestSessionMyXLShape(t *testing.T) {
	now := float64(time.Now().Unix())
	v := newTestVault(t, map[string]any{
		"deviceId": "dev",
		"sessions": map[string]any{
			"6287777313358": map[string]any{
				"token": "bearer", "id_token": "id", "access_token": "at",
				"refresh_token": "rt", "exp": now - 3600, "token_exp": now - 60,
			},
		},
	})
	v = reopen(t, v)

	if v.Session("6287777313358") != nil {
		t.Fatal("expired myXL session should be hidden by Session()")
	}
	raw := v.RawSession("6287777313358")
	if raw == nil || raw["refresh_token"] != "rt" {
		t.Fatalf("refresh_token must survive expiry, got %v", raw)
	}
	if got := num(raw["token_exp"]); got <= 0 {
		t.Fatalf("token_exp unreadable: %v", raw["token_exp"])
	}
}

// axisnet: no exp at all; the session is returned as-is (Python behaves the same
// because a missing/zero exp short-circuits the expiry check).
func TestSessionAxisnetShape(t *testing.T) {
	v := newTestVault(t, map[string]any{
		"deviceId": "dev",
		"sessions": map[string]any{
			"628333333311": map[string]any{
				"provider": "AXISNET", "token": "tok",
				"issued": "2026-11-05T17:00:10+07",
				"scheme": "Bearer", "device": "M2101K7AG",
			},
		},
	})
	v = reopen(t, v)

	if v.Session("628333333311") == nil {
		t.Fatal("exp-less session must be returned")
	}
	if got := v.SessionMSISDNs(); len(got) != 1 || got[0] != "628333333311" {
		t.Fatalf("SessionMSISDNs = %v", got)
	}
}

func TestNumAcceptsJSONAndStringForms(t *testing.T) {
	if num(float64(5)) != 5 || num("7") != 7 || num(nil) != 0 {
		t.Fatal("num() failed on mixed encodings")
	}
}

func mustJSON(t *testing.T, v map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
