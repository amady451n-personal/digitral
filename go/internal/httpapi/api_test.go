package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"digitral/backend/internal/provider"
	"digitral/backend/internal/registry"
	"digitral/backend/internal/vault"
)

type stubClient struct{ bal provider.Balance }

func (s stubClient) Provider() string { return "bima" }
func (s stubClient) Balance(context.Context, string) (provider.Balance, error) {
	return s.bal, nil
}

type stubFactory struct{ c provider.Client }

func (f stubFactory) Client(string, string) (provider.Client, error) { return f.c, nil }

func newTestAPI(t *testing.T) *API {
	t.Helper()
	b, err := os.ReadFile("../registry/testdata/registry.json")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "registry.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := registry.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return &API{Reg: r, SessionCheck: func(string, string) bool { return true }, Factory: stubFactory{c: stubClient{bal: provider.Balance{
		Balance: "9400", Quota: 26992640, Unit: "KB", PackageQuotas: []provider.PackageQuota{},
	}}}, Frontend: "../../../frontend", PollSec: 20 * time.Millisecond}
}

func TestHealthAndProviders(t *testing.T) {
	h := newTestAPI(t).Handler()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/health", nil))
	if w.Code != 200 {
		t.Fatalf("health status = %d", w.Code)
	}
	var health struct {
		OK        bool     `json:"ok"`
		Providers []string `json:"providers"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &health)
	if !health.OK || len(health.Providers) != 4 {
		t.Errorf("health = %#v", health)
	}

	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/providers", nil))
	var list []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 4 || list[0]["color"] == nil || list[0]["logo"] == nil || list[0]["available"] != true {
		t.Errorf("providers = %#v", list)
	}
}

func TestNumbersContract(t *testing.T) {
	h := newTestAPI(t).Handler()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/numbers", nil))
	var out struct {
		Numbers []map[string]any `json:"numbers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Numbers) != 5 {
		t.Fatalf("numbers = %d", len(out.Numbers))
	}
	first := out.Numbers[0]
	for _, k := range []string{"id", "provider", "label", "msisdn", "added_at", "pending", "color", "logo"} {
		if _, ok := first[k]; !ok {
			t.Errorf("missing key %s in %#v", k, first)
		}
	}
	if first["pending"] != false {
		t.Errorf("pending = %v", first["pending"])
	}
}

func TestBalanceRoute(t *testing.T) {
	a := newTestAPI(t)
	h := a.Handler()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/numbers/"+a.Reg.Numbers[0].ID+"/balance", nil))
	if w.Code != 200 {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	bal, _ := out["balance"].(map[string]any)
	if bal["balance"] != "9400" || bal["unit"] != "KB" {
		t.Errorf("balance = %#v", bal)
	}
}

func TestNotFoundAndBadRequest(t *testing.T) {
	h := newTestAPI(t).Handler()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/numbers/nope/balance", nil))
	if w.Code != 404 {
		t.Errorf("missing number status = %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/numbers", strings.NewReader(`{"provider":"nope","msisdn":"6281"}`)))
	if w.Code != 400 {
		t.Errorf("bad provider status = %d", w.Code)
	}
}

func TestStreamEmitsEvents(t *testing.T) {
	a := newTestAPI(t)
	srv := httptest.NewServer(a.Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)
	for i := 0; i < 2; i++ {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("event %d: %v", i, err)
		}
		if !strings.HasPrefix(line, "data: {") {
			t.Errorf("event %d = %q", i, line)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &payload); err != nil {
			t.Errorf("event %d json: %v", i, err)
		}
		if _, err := reader.ReadString('\n'); err != nil { // blank separator
			t.Fatal(err)
		}
	}
}

func TestStaticIndex(t *testing.T) {
	h := newTestAPI(t).Handler()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "<html") {
		t.Errorf("index status = %d, head = %.80s", w.Code, w.Body.String())
	}
}

// A number without a stored session creates a pending OTP entry on explicit POST.
type otpStub struct{ stubClient }

func (otpStub) GuestToken(context.Context) (string, error) { return "guest", nil }
func (otpStub) SendOTP(context.Context, string) (string, error) {
	return "T-1", nil
}

func TestAddNumberSendsOTPWhenNoSession(t *testing.T) {
	a := newTestAPI(t)
	a.SessionCheck = func(string, string) bool { return false }
	a.Factory = stubFactory{c: otpStub{stubClient{}}}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/numbers",
		strings.NewReader(`{"provider":"axisnet","msisdn":"081234567890"}`)))
	if w.Code != 200 {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out["status"] != "otp_sent" || out["otp_required"] != true {
		t.Errorf("response = %#v", out)
	}
	if _, ok := a.Reg.Pending["axisnet-6281234567890"]; !ok {
		t.Errorf("pending not created: %#v", a.Reg.Pending)
	}
}

type errClient struct{}

func (errClient) Provider() string { return "bima" }
func (errClient) Balance(context.Context, string) (provider.Balance, error) {
	return provider.Balance{}, fmt.Errorf("dashboard code 26000: token=eyJhbGciOiJIUzUxMiJ9.SECRET")
}

type errFactory struct{}

func (errFactory) Client(string, string) (provider.Client, error) { return errClient{}, nil }

func TestStreamCarriesBalanceShape(t *testing.T) {
	a := newTestAPI(t)
	srv := httptest.NewServer(a.Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Numbers []map[string]any `json:"numbers"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &payload); err != nil {
		t.Fatal(err)
	}
	item := payload.Numbers[0]
	for _, k := range []string{"ok", "data", "error"} {
		if _, ok := item[k]; !ok {
			t.Errorf("stream item missing %s: %#v", k, item)
		}
	}
	if item["ok"] != true {
		t.Errorf("ok = %v", item["ok"])
	}
	data, _ := item["data"].(map[string]any)
	if data["balance"] != "9400" {
		t.Errorf("data = %#v", data)
	}
}

func TestIncludePending(t *testing.T) {
	a := newTestAPI(t)
	a.SessionCheck = func(string, string) bool { return false }
	a.Factory = stubFactory{c: otpStub{stubClient{}}}
	h := a.Handler()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/numbers", strings.NewReader(`{"provider":"axisnet","msisdn":"081234567890"}`)))
	if w.Code != 200 {
		t.Fatalf("add status = %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/numbers?include_pending=1", nil))
	var out struct {
		Numbers []map[string]any `json:"numbers"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	found := false
	for _, n := range out.Numbers {
		if n["pending"] == true && n["id"] == "axisnet-6281234567890" {
			found = true
		}
	}
	if !found {
		t.Errorf("pending number missing from /numbers: %#v", out.Numbers)
	}
}

func TestNoTokenLeakInErrorResponses(t *testing.T) {
	a := newTestAPI(t)
	a.Factory = errFactory{}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/numbers/"+a.Reg.Numbers[0].ID+"/balance", nil))
	body := w.Body.String()
	if strings.Contains(body, "SECRET") || strings.Contains(body, "eyJhbGciOiJIUzUxMiJ9") {
		t.Errorf("token leaked to HTTP client: %s", body)
	}
}

func TestDeleteDropsVaultSession(t *testing.T) {
	a := newTestAPI(t)
	dir := t.TempDir()
	vaultDir := filepath.Join(dir, "bima")
	if err := os.MkdirAll(vaultDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vaultDir, "vault.key"), make([]byte, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	v, err := vault.Open(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	_ = v.Update(func(data map[string]any) {
		data["sessions"] = map[string]any{"6280000000000": map[string]any{"token": "t"}}
	})
	a.Home = dir
	n := a.Reg.Numbers[0]
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("DELETE", "/numbers/"+n.ID, nil))
	if w.Code != 200 {
		t.Fatalf("delete status = %d", w.Code)
	}
	got, err := vault.Open(filepath.Join(dir, "bima"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Session(n.MSISDN) != nil {
		t.Fatal("session survived DELETE")
	}
}
