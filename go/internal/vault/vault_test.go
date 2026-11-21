package vault

// Synthetic vault tests, plus one fixture written by Python's Vault so the
// on-disk format is pinned against the implementation we must stay compatible
// with. No real vault and no secret is committed.

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	got, err := Open(v.Dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return got
}

// TestVaultPythonLayout opens a vault written by scripts/bima_balance.py's Vault
// and asserts the identity material and all three session shapes.
func TestVaultPythonLayout(t *testing.T) {
	v, err := Open("testdata/vault_py")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if got := v.DeviceID(); got != "aabbccddeeff0011" {
		t.Errorf("DeviceID = %q, want aabbccddeeff0011", got)
	}
	if got := v.Faaid(); got != strings.Repeat("f", 32) {
		t.Errorf("Faaid = %q, want 32 f", got)
	}
	if got := v.Gaid(); got != "11111111-2222-3333-4444-555555555555" {
		t.Errorf("Gaid = %q", got)
	}
	if got := v.Model(); got != "M2007J20CG" {
		t.Errorf("Model = %q, want M2007J20CG", got)
	}
	if got := v.OEM(); got != "Xiaomi" {
		t.Errorf("OEM = %q, want Xiaomi", got)
	}

	want := []string{"6280000000001", "6280000000002", "6280000000003", "6280000000004"}
	if got := v.SessionMSISDNs(); len(got) != len(want) {
		t.Fatalf("SessionMSISDNs = %v, want %v", got, want)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("SessionMSISDNs[%d] = %q, want %q", i, got[i], want[i])
			}
		}
	}

	if s := v.Session("6280000000001"); s == nil || s["token"] != "tok-valid" {
		t.Errorf("valid session: %v", s)
	}
	if s := v.Session("6280000000002"); s != nil {
		t.Errorf("expired session accepted: %v", s)
	}
	if s := v.RawSession("6280000000002"); s == nil || s["token"] != "tok-expired" {
		t.Errorf("RawSession must ignore expiry: %v", s)
	}
	if s := v.Session("6280000000003"); s == nil || s["token"] != "tok-noexp" {
		t.Errorf("exp-less session (axisnet shape) must be returned: %v", s)
	}
	if s := v.RawSession("6280000000004"); s["refresh_token"] != "rt" {
		t.Errorf("myXL refresh_token missing: %v", s)
	}
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
			plain, _ := json.Marshal(data)
			if got, want := len(blob), 12+16+len(plain); got != want {
				t.Fatalf("blob size = %d, want %d", got, want)
			}
			got := reopen(t, v)
			if got.DeviceID() != data["deviceId"] {
				t.Fatalf("deviceId = %q, want %q", got.DeviceID(), data["deviceId"])
			}
		})
	}
}

// Save must be atomic-looking and must not change the key.
func TestSaveRoundTrip(t *testing.T) {
	v := newTestVault(t, map[string]any{
		"deviceId": "dev",
		"sessions": map[string]any{
			"6281": map[string]any{"token": "t", "exp": float64(time.Now().Unix()) + 3600},
		},
	})
	before := v.Key
	if err := v.Save(); err != nil {
		t.Fatal(err)
	}
	if string(v.Key) != string(before) {
		t.Error("Save changed vault.key")
	}
	got := reopen(t, v)
	if got.Session("6281") == nil {
		t.Fatal("session lost after Save")
	}
}

// Session mirrors Python Vault.session(): exp seconds, 60s early margin.
func TestSessionBimaShape(t *testing.T) {
	now := float64(time.Now().Unix())
	v := newTestVault(t, map[string]any{
		"deviceId": "dev",
		"sessions": map[string]any{
			"628111111111": map[string]any{"token": "jwt", "exp": now + 3600},
			"628222222222": map[string]any{"token": "old", "exp": now - 10},
			"628333333333": map[string]any{"token": "edge", "exp": now + 30},
		},
	})
	v = reopen(t, v)

	if v.Session("628111111111") == nil {
		t.Fatal("valid session rejected")
	}
	if v.Session("628222222222") != nil {
		t.Fatal("expired session accepted")
	}
	if v.Session("628333333333") != nil {
		t.Fatal("session inside the 60s margin accepted")
	}
	if v.RawSession("628222222222") == nil {
		t.Fatal("RawSession must ignore expiry")
	}
}

func TestNumAcceptsJSONAndStringForms(t *testing.T) {
	if num(float64(5)) != 5 || num("7") != 7 || num(nil) != 0 {
		t.Fatal("num() failed on mixed encodings")
	}
}

func TestDropSession(t *testing.T) {
	v := newTestVault(t, map[string]any{
		"deviceId": "dev",
		"sessions": map[string]any{"6281": map[string]any{"token": "t"}},
	})
	v.DropSession("6281")
	if v.RawSession("6281") != nil {
		t.Fatal("session not dropped in memory")
	}
	if reopen(t, v).RawSession("6281") != nil {
		t.Fatal("session not dropped on disk")
	}
}

func TestConcurrentSaves(t *testing.T) {
	v := newTestVault(t, map[string]any{"deviceId": "dev", "sessions": map[string]any{}})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := v.Update(func(data map[string]any) {
				sessions, _ := data["sessions"].(map[string]any)
				sessions[fmt.Sprintf("628%d", i)] = map[string]any{"token": "t"}
			}); err != nil {
				t.Errorf("Save: %v", err)
			}
		}(i)
	}
	wg.Wait()
	got, err := Open(v.Dir)
	if err != nil {
		t.Fatalf("vault corrupted after concurrent saves: %v", err)
	}
	if len(got.SessionMSISDNs()) == 0 {
		t.Fatal("no sessions survived")
	}
}
