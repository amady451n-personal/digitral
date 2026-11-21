// Package httpapi serves the JSON API and static PWA, matching server.py routes.
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"digitral/backend/internal/provider"
	"digitral/backend/internal/provider/axisnet"
	"digitral/backend/internal/provider/imi"
	"digitral/backend/internal/provider/myxl"
	"digitral/backend/internal/registry"
	"digitral/backend/internal/vault"
)

type ProviderInfo struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Available bool   `json:"available"`
	Color     string `json:"color"`
	Logo      string `json:"logo"`
}

var Providers = []ProviderInfo{
	{"bima", "bima+ (Tri)", true, "#e01b2e", "/assets/bima.png"},
	{"myim3", "myIM3 (Indosat)", true, "#ffd200", "/assets/myim3.png"},
	{"axisnet", "AXISNET", true, "#7a1fa2", "/assets/axisnet.png"},
	{"myxl", "myXL", true, "#0057a8", "/assets/myxl.png"},
}

func info(id string) (ProviderInfo, bool) {
	for _, p := range Providers {
		if p.ID == id {
			return p, true
		}
	}
	return ProviderInfo{}, false
}

// ClientFactory builds a provider client over the right vault.
type ClientFactory interface {
	Client(providerID, msisdn string) (provider.Client, error)
}

type Factory struct{ Home string }

func (f Factory) Client(id, msisdn string) (provider.Client, error) {
	v, err := vault.Open(filepath.Join(f.Home, id))
	if err != nil {
		return nil, err
	}
	switch id {
	case "bima":
		return imi.New(imi.ConfigBima, v, deviceFromVault(v), nil), nil
	case "myim3":
		return imi.New(imi.ConfigMyIM3, v, deviceFromVault(v), nil), nil
	case "axisnet":
		return axisnet.New(v, msisdn), nil
	case "myxl":
		return myxl.New(v, msisdn), nil
	}
	return nil, fmt.Errorf("unknown provider %q", id)
}

func deviceFromVault(v *vault.Vault) imi.Device {
	d := imi.DefaultDevice()
	if s := v.DeviceID(); s != "" {
		d.ID = s
	}
	if s := v.Faaid(); s != "" {
		d.Faaid = s
	}
	if s := v.Gaid(); s != "" {
		d.Gaid = s
	}
	return d
}

type API struct {
	Reg          *registry.Registry
	Factory      ClientFactory
	Home         string
	Frontend     string
	PollSec      time.Duration
	SessionCheck func(providerID, msisdn string) bool
	PushPublic   func() (string, error)
	PushAdd      func(sub map[string]any) (bool, int, error)
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		ids := make([]string, 0, len(Providers))
		for _, p := range Providers {
			ids = append(ids, p.ID)
		}
		writeJSON(w, 200, map[string]any{"ok": true, "providers": ids})
	})
	mux.HandleFunc("GET /providers", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, Providers)
	})
	mux.HandleFunc("GET /numbers", a.listNumbers)
	mux.HandleFunc("POST /numbers", a.addNumber)
	mux.HandleFunc("POST /numbers/{id}/resend", a.resend)
	mux.HandleFunc("POST /numbers/{id}/verify", a.verify)
	mux.HandleFunc("GET /numbers/{id}/balance", a.balance)
	mux.HandleFunc("DELETE /numbers/{id}", a.deleteNumber)
	mux.HandleFunc("GET /stream", a.stream)
	mux.HandleFunc("GET /push/public-key", func(w http.ResponseWriter, r *http.Request) {
		key := ""
		if a.PushPublic != nil {
			key, _ = a.PushPublic()
		}
		writeJSON(w, 200, map[string]any{"public_key": key})
	})
	mux.HandleFunc("POST /push/subscribe", func(w http.ResponseWriter, r *http.Request) {
		var sub map[string]any
		if err := json.NewDecoder(r.Body).Decode(&sub); err != nil || sub["endpoint"] == "" {
			http.Error(w, "endpoint required", 400)
			return
		}
		newSub, total := false, 0
		if a.PushAdd != nil {
			newSub, total, _ = a.PushAdd(sub)
		}
		writeJSON(w, 200, map[string]any{"ok": true, "new": newSub, "total": total})
	})
	mux.HandleFunc("GET /manifest.json", func(w http.ResponseWriter, r *http.Request) {
		a.serveFile(w, "manifest.json", "application/manifest+json")
	})
	mux.HandleFunc("GET /sw.js", func(w http.ResponseWriter, r *http.Request) {
		a.serveFile(w, "sw.js", "application/javascript")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			http.StripPrefix("/assets/", http.FileServer(http.Dir(filepath.Join(a.Frontend, "assets")))).ServeHTTP(w, r)
			return
		}
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		a.serveFile(w, "index.html", "text/html; charset=utf-8")
	})
	return mux
}

func (a *API) serveFile(w http.ResponseWriter, name, contentType string) {
	p := filepath.Join(a.Frontend, name)
	b, err := os.ReadFile(p)
	if err != nil {
		http.Error(w, name+" missing", 500)
		return
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (a *API) numbersView() []map[string]any {
	out := []map[string]any{}
	for _, n := range a.Reg.Snapshot() {
		p, ok := info(n.Provider)
		if !ok {
			p = ProviderInfo{ID: n.Provider, Label: n.Provider}
		}
		out = append(out, map[string]any{
			"id": n.ID, "provider": n.Provider, "label": p.Label,
			"msisdn": n.MSISDN, "added_at": n.AddedAt, "pending": false,
			"color": p.Color, "logo": p.Logo,
		})
	}
	return out
}

func (a *API) listNumbers(w http.ResponseWriter, r *http.Request) {
	out := a.numbersView()
	if r.URL.Query().Get("include_pending") != "" {
		for id, p := range a.Reg.PendingList() {
			info, _ := info(p.Provider)
			out = append(out, map[string]any{
				"id": id, "provider": p.Provider, "label": info.Label,
				"msisdn": p.MSISDN, "added_at": float64(p.CreatedAt.Unix()),
				"pending": true, "color": info.Color, "logo": info.Logo,
			})
		}
	}
	writeJSON(w, 200, map[string]any{"numbers": out})
}

func (a *API) addNumber(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Provider string `json:"provider"`
		MSISDN   string `json:"msisdn"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	p, ok := info(body.Provider)
	if !ok || !p.Available {
		http.Error(w, "unknown/unavailable provider", 400)
		return
	}
	ms := registry.Normalize(body.MSISDN)
	if err := a.Reg.Add(body.Provider, ms); err != nil {
		http.Error(w, safeError(err), 409)
		return
	}
	hasSession := a.SessionCheck != nil && a.SessionCheck(body.Provider, ms)
	if hasSession {
		writeJSON(w, 200, map[string]any{
			"id": body.Provider + "-" + ms, "msisdn": ms, "provider": body.Provider,
			"label": p.Label, "color": p.Color, "logo": p.Logo,
			"status": "session_reused", "otp_required": false,
		})
		return
	}
	// An explicit POST /numbers is the user's request to send OTP.
	client, err := a.Factory.Client(body.Provider, ms)
	if err != nil {
		http.Error(w, safeError(err), 502)
		return
	}
	transid, err := sendOTP(r.Context(), client, ms)
	if err != nil {
		http.Error(w, safeError(err), 502)
		return
	}
	id := body.Provider + "-" + ms
	a.Reg.PendingSet(id, &registry.Pending{Provider: body.Provider, MSISDN: ms, TransID: transid, CreatedAt: time.Now()})
	writeJSON(w, 200, map[string]any{
		"id": id, "msisdn": ms, "provider": body.Provider,
		"label": p.Label, "color": p.Color, "logo": p.Logo,
		"transid": transid, "status": "otp_sent", "otp_required": true,
	})
}

func (a *API) resend(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pending, ok := a.Reg.PendingGet(id)
	if !ok {
		http.Error(w, "no pending OTP for this number (re-add)", 400)
		return
	}
	client, err := a.Factory.Client(pending.Provider, pending.MSISDN)
	if err != nil {
		http.Error(w, safeError(err), 502)
		return
	}
	transid, err := sendOTP(r.Context(), client, pending.MSISDN)
	if err != nil {
		http.Error(w, safeError(err), 502)
		return
	}
	pending.TransID = transid
	pending.CreatedAt = time.Now()
	info, _ := info(pending.Provider)
	writeJSON(w, 200, map[string]any{
		"id": id, "msisdn": pending.MSISDN, "provider": pending.Provider,
		"label": info.Label, "color": info.Color, "logo": info.Logo,
		"transid": transid, "status": "otp_resent", "otp_required": true,
	})
}

func (a *API) verify(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pending, ok := a.Reg.PendingGet(id)
	if !ok {
		http.Error(w, "no pending OTP for this number", 400)
		return
	}
	var body struct {
		OTP string `json:"otp"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	client, err := a.Factory.Client(pending.Provider, pending.MSISDN)
	if err != nil {
		http.Error(w, safeError(err), 400)
		return
	}
	if _, err := validateOTP(r.Context(), client, pending.MSISDN, pending.TransID, body.OTP); err != nil {
		http.Error(w, safeError(err), 400)
		return
	}
	if err := a.Reg.Add(pending.Provider, pending.MSISDN); err != nil && !strings.Contains(err.Error(), "already registered") {
		http.Error(w, safeError(err), 400)
		return
	}
	a.Reg.PendingDelete(id)
	writeJSON(w, 200, map[string]any{"ok": true, "id": id, "status": "verified"})
}

func (a *API) balance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	n, ok := a.Reg.Find(id)
	if !ok {
		http.Error(w, "number not found", 404)
		return
	}
	client, err := a.Factory.Client(n.Provider, n.MSISDN)
	if err != nil {
		http.Error(w, safeError(err), 502)
		return
	}
	bal, err := client.Balance(r.Context(), n.MSISDN)
	if err != nil {
		http.Error(w, safeError(err), 502)
		return
	}
	p, _ := info(n.Provider)
	writeJSON(w, 200, map[string]any{
		"id": id, "provider": n.Provider, "msisdn": n.MSISDN,
		"label": p.Label, "color": p.Color, "logo": p.Logo, "balance": bal,
	})
}

func (a *API) deleteNumber(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	n, registered := a.Reg.Find(id)
	pending, pendingOK := a.Reg.PendingGet(id)
	if !registered && !pendingOK {
		http.Error(w, "number not found", 404)
		return
	}
	if registered {
		if v, err := vault.Open(filepath.Join(a.Home, n.Provider)); err == nil {
			v.DropSession(n.MSISDN)
		}
		_ = a.Reg.Remove(id)
	} else if v, err := vault.Open(filepath.Join(a.Home, pending.Provider)); err == nil {
		v.DropSession(pending.MSISDN)
	}
	a.Reg.PendingDelete(id)
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *API) stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	ticker := time.NewTicker(a.poll())
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			payload := map[string]any{"numbers": a.snapshot(), "ts": time.Now().Unix()}
			b, _ := json.Marshal(payload)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
		}
	}
}

// snapshot mirrors server.py:_snapshot(): SSE items must carry ok/data/error
// or the unchanged frontend clears balances it already fetched.
func (a *API) snapshot() []map[string]any {
	out := []map[string]any{}
	for _, n := range a.Reg.Snapshot() {
		info, _ := info(n.Provider)
		item := map[string]any{
			"id": n.ID, "provider": n.Provider, "label": info.Label,
			"msisdn": n.MSISDN, "added_at": n.AddedAt,
			"ok": false, "data": nil, "error": nil,
			"color": info.Color, "logo": info.Logo,
		}
		hasSession := a.SessionCheck != nil && a.SessionCheck(n.Provider, n.MSISDN)
		if !hasSession {
			item["error"] = "no_session"
			out = append(out, item)
			continue
		}
		client, err := a.Factory.Client(n.Provider, n.MSISDN)
		if err != nil {
			item["error"] = "client_error"
			out = append(out, item)
			continue
		}
		bal, err := client.Balance(context.Background(), n.MSISDN)
		if err != nil {
			item["error"] = "balance_failed"
		} else {
			item["ok"] = true
			item["data"] = bal
		}
		out = append(out, item)
	}
	return out
}

// safeError never forwards provider response bodies (which can contain tokens)
// to the browser. Details are not logged either, so stderr stays clean.
func safeError(err error) string {
	if err == nil {
		return "request failed"
	}
	return "request failed"
}

func (a *API) poll() time.Duration {
	if a.PollSec <= 0 {
		return 60 * time.Second
	}
	return a.PollSec
}
