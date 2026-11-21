package axisnet

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"digitral/backend/internal/provider"
	"digitral/backend/internal/vault"
)

// Vectors were produced by scripts/axisnet_balance.py + bima_balance.py.
func TestHeaderVectors(t *testing.T) {
	b, err := os.ReadFile("testdata/headers.json")
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]struct {
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
		Body    string            `json:"body"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"otp", "login"} {
		t.Run(name, func(t *testing.T) {
			want := v[name]
			gotURL, gotHeaders, gotBody := otpRequest("628123456789", "123456", name == "login")
			delete(gotHeaders, "x-app-request-id")
			if gotURL != want.URL || gotBody != want.Body || !reflect.DeepEqual(gotHeaders, want.Headers) {
				t.Errorf("mismatch\n url: %q vs %q\n body: %q vs %q\n headers: %#v vs %#v",
					gotURL, want.URL, gotBody, want.Body, gotHeaders, want.Headers)
			}
		})
	}
}

func TestBalanceParity(t *testing.T) {
	raw, err := os.ReadFile("testdata/dashboards.json")
	if err != nil {
		t.Fatal(err)
	}
	wantB, err := os.ReadFile("../testdata/dashboards/axisnet.expected.json")
	if err != nil {
		t.Fatal(err)
	}
	var want provider.Balance
	if err := json.Unmarshal(wantB, &want); err != nil {
		t.Fatal(err)
	}
	got, err := ParseDashboards(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parsed mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestAxisnetUnlimitedSuffix(t *testing.T) {
	p := map[string]any{"name": "AON", "remaining_text": "500 MB", "is_unlimited": true}
	res := parsePackages(map[string]any{"packages": []any{p}, "summary": map[string]any{}})
	if len(res) != 1 || res[0].Remaining != "500 MB (UNLIMITED)" {
		t.Errorf("package = %#v", res)
	}
}

func TestToMB(t *testing.T) {
	for in, want := range map[string]string{
		"12.23 GB": "12523.52 MB", "410.00 MB": "410.00 MB", "530 KB": "0.52 MB",
	} {
		if got := toMB(in); got != want {
			t.Errorf("toMB(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSessionHasNoExp(t *testing.T) {
	v, err := vault.Open("../../vault/testdata/vault_py")
	if err != nil {
		t.Fatal(err)
	}
	c := New(v, "6280000000003")
	if c.token != "tok-noexp" {
		t.Fatalf("token from exp-less session = %q, want tok-noexp", c.token)
	}
	if s := v.Session("6280000000003"); s == nil || s["token"] != "tok-noexp" {
		t.Fatalf("axisnet session without exp must be readable: %v", s)
	}
}

func TestBalanceOverHTTP(t *testing.T) {
	raw, err := os.ReadFile("testdata/dashboards.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture map[string]struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "tok-noexp" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		key := "quota_home_raw"
		if strings.Contains(r.URL.Path, "balance") {
			key = "balance_raw"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(fixture[key].Data)
	}))
	defer s.Close()

	v, err := vault.Open("../../vault/testdata/vault_py")
	if err != nil {
		t.Fatal(err)
	}
	c := New(v, "6280000000003")
	c.quotaURL, c.balanceURL = s.URL+"/quota", s.URL+"/balance"
	bal, err := c.Balance(context.Background(), "6280000000003")
	if err != nil {
		t.Fatal(err)
	}
	if bal.Quota == nil || len(bal.PackageQuotas) != 2 || !bal.Unlimited {
		t.Errorf("balance = %#v", bal)
	}
}
