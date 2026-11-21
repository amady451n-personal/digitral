package myxl

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"digitral/backend/internal/provider"
	"digitral/backend/internal/vault"
)

func TestParseAPIParity(t *testing.T) {
	raw, err := os.ReadFile("testdata/api.json")
	if err != nil {
		t.Fatal(err)
	}
	var api map[string]json.RawMessage
	if err := json.Unmarshal(raw, &api); err != nil {
		t.Fatal(err)
	}
	plain := map[string]any{}
	for k, v := range api {
		var env struct {
			Xdata string `json:"xdata"`
			Xtime int64  `json:"xtime"`
		}
		if err := json.Unmarshal(v, &env); err != nil {
			t.Fatal(err)
		}
		if env.Xdata == "" {
			t.Fatalf("%s has no xdata", k)
		}
		d, err := decrypt(env.Xdata, env.Xtime)
		if err != nil {
			t.Fatalf("%s: %v", k, err)
		}
		plain[k] = d
	}
	wantB, err := os.ReadFile("../testdata/dashboards/myxl.expected.json")
	if err != nil {
		t.Fatal(err)
	}
	var want provider.Balance
	if err := json.Unmarshal(wantB, &want); err != nil {
		t.Fatal(err)
	}
	got, err := ParseAPI(plain)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parse mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	ts := int64(1732000000000)
	body, err := encrypt(map[string]any{"hello": "world", "n": 42}, ts)
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Xdata string `json:"xdata"`
		Xtime int64  `json:"xtime"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatal(err)
	}
	got, err := decrypt(env.Xdata, env.Xtime)
	if err != nil {
		t.Fatal(err)
	}
	if got["hello"] != "world" {
		t.Errorf("decrypted = %#v", got)
	}
}

// An expired `exp` with a refresh_token must trigger a grant, not an OTP error.
func TestMyXLRefreshPath(t *testing.T) {
	apiRaw, err := os.ReadFile("testdata/api.json")
	if err != nil {
		t.Fatal(err)
	}
	var api map[string]json.RawMessage
	_ = json.Unmarshal(apiRaw, &api)
	plain := map[string]any{}
	for k, v := range api {
		var env struct {
			Xdata string `json:"xdata"`
			Xtime int64  `json:"xtime"`
		}
		_ = json.Unmarshal(v, &env)
		d, err := decrypt(env.Xdata, env.Xtime)
		if err != nil {
			t.Fatal(err)
		}
		plain[k] = d
	}

	refreshed := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "protocol/openid-connect/token"):
			_ = r.ParseForm()
			if r.Form.Get("grant_type") != "refresh_token" {
				t.Errorf("grant_type = %q", r.Form.Get("grant_type"))
			}
			refreshed++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id_token":"hdr.eyJleHAiOiA0MTAyNDQ0ODAwfQ.sig","refresh_token":"rt-new","expires_in":3600}`))
		case strings.Contains(r.URL.Path, "/api/v8/"):
			key := map[string]string{
				"balance-and-credit": "balance-and-credit", "quota-summary": "quota-summary",
				"quota-details": "quota-details",
			}[strings.TrimSpace(strings.Split(r.URL.Path, "/")[len(strings.Split(r.URL.Path, "/"))-1])]
			body, _ := encrypt(plain[key], 1732000000000)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer s.Close()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "vault.key"), make([]byte, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	v, err := vault.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	v.Data["sessions"] = map[string]any{
		"6287777313358": map[string]any{
			"token": "old", "refresh_token": "rt", "exp": float64(1000000000),
			"token_exp": float64(1000000000),
		},
	}
	c := New(v, "6287777313358")
	c.ciamBase, c.apiBase = s.URL+"/", s.URL+"/api/v8/"
	bal, err := c.Balance(context.Background(), "6287777313358")
	if err != nil {
		t.Fatalf("Balance with expired session: %v", err)
	}
	if refreshed != 1 {
		t.Errorf("refresh grant called %d times, want 1", refreshed)
	}
	if c.token == "old" || c.token == "" {
		t.Errorf("token after refresh = %q", c.token)
	}
	if bal.Quota == nil || len(bal.PackageQuotas) != 1 {
		t.Errorf("balance = %#v", bal)
	}
}
