// Command poc-bima is the live-verification CLI for the Go port. It is not part
// of the backend; it exists so provider work can be checked against real
// sessions without shipping a test that touches the network.
//
// Modes:
//
//	POC_VAULT=<dir> POC_MSISDN=<62...>   open an existing vault, reuse its session,
//	                                     call /dashboard/get/v7, print parsed balance
//	POC_VAULT_SCAN=<base>                inspect every provider vault under a dir
//	POC_PROBE=validate|dashboard         guest token then a post-auth endpoint (no OTP)
//	POC_TLS=stdlib                       transport control run
//	POC_ALPN=h1                          uTLS variant with ALPN stripped (HTTP/1.1)
package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"digitral/backend/internal/cryptoutil"
	"digitral/backend/internal/vault"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"
)

// Static app constants, from scripts/bima_balance.py (decompiled app).
const (
	apiHost    = "bimaplus-api.ioh.co.id"
	apiBase    = "https://" + apiHost + "/api/v2"
	authoriz   = "642d1cc69d90666962726e"
	serviceKey = "FPi7ZP3Jy8Uv3KBd4QeG"
	channel    = "BIMA"
	appVersion = "5.17.0"
	appOS      = "Android"
	secret     = "$"
)

// Device is the per-install identity sent on every request.
type Device struct {
	ID, Name, OEM, Model, OSVer, Faaid, Gaid string
}

func defaultDevice() Device {
	return Device{
		ID: "0123456789abcdef", Name: "generic", OEM: "samsung",
		Model: "SM-A235F", OSVer: "14",
		Faaid: "fa000000000000000000000000000000",
		Gaid:  "00000000-0000-0000-0000-000000000001",
	}
}

// deviceFromVault takes identity material from the vault.
//
// Parity note: Python BimaClient only reads device_id/faaid/gaid from the vault;
// device_name/oem/model/osver always fall back to generic/samsung/SM-A235F/14
// because server.py never passes them. POC_DEVICE_PROFILE=1 opts into the
// stored real-device profile instead.
func deviceFromVault(v *vault.Vault, useProfile bool) Device {
	d := defaultDevice()
	if s := v.DeviceID(); s != "" {
		d.ID = s
	}
	if s := v.Faaid(); s != "" {
		d.Faaid = s
	}
	if s := v.Gaid(); s != "" {
		d.Gaid = s
	}
	if useProfile {
		d.Name, d.OEM, d.Model, d.OSVer = v.DeviceName(), v.OEM(), v.Model(), v.OSVer()
	}
	return d
}

// buildHeaders mirrors BimaClient.baseline + build_headers (parent line, no child).
func buildHeaders(dev Device, path, body string, reqTime int64, token string) [][2]string {
	rt := strconv.FormatInt(reqTime, 10)
	h := [][2]string{
		{"Authorization", authoriz},
		{"Content-Type", "application/json"},
		{"Accept-Encoding", "gzip"},
		{"User-Agent", ""},
		{"X-DEVICEID", dev.ID},
		{"X-DEVICENAME", dev.Name},
		{"X-IMI-App-OS", appOS},
		{"X-IMI-App-OSVersion", dev.OSVer},
		{"X-IMI-App-OEM", dev.OEM},
		{"X-IMI-App-Model", dev.Model},
		{"X-IMI-CHANNEL", channel},
		{"X-IMI-SERVICEKEY", serviceKey},
		{"X-IMI-VERSION", appVersion},
		{"X-IMI-LANGUAGE", "EN"},
		{"X-LOW-QUOTA", "NO"},
		{"X-SPTRAVEL", "NO"},
		{"X-FAIID", dev.Faaid},
		{"X-IMI-ADID", dev.Gaid},
		{"X-IMI-ADID-ISLIMITED", "false"},
		{"X-IMI-UID", rt},
		{"X-IMI-NETWORK", "WiFi"},
	}
	tokenID := token
	if tokenID == "" {
		tokenID = rt
	}
	h = append(h, [2]string{"X-IMI-TOKENID", tokenID})
	if strings.Contains(path, "/token/guest/") || strings.Contains(path, "/token/app/") {
		h = append(h, [2]string{"X-ESIM-SUPPORT", "false"})
	}
	h = append(h, [2]string{"X-IMI-HASH",
		cryptoutil.SHA512Hex("parent" + secret + appOS + secret + appVersion + secret + tokenID + "&SALT=" + cryptoutil.GetValues(rt))})
	h = append(h, [2]string{"x-imi-oauth",
		cryptoutil.SHA512Hex("REQBODY=" + body + "&SALT=" + cryptoutil.GetValues(strings.TrimSpace(tokenID)))})
	return h
}

// --- transports ---------------------------------------------------------------

func dialUTLS(ctx context.Context, addr string, h1Only bool) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	raw, err := (&net.Dialer{Timeout: 20 * time.Second}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	cfg := &utls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	var uc *utls.UConn
	if h1Only {
		spec, err := utls.UTLSIdToSpec(utls.HelloChrome_120)
		if err != nil {
			raw.Close()
			return nil, err
		}
		for i, ext := range spec.Extensions {
			if _, ok := ext.(*utls.ALPNExtension); ok {
				spec.Extensions = append(spec.Extensions[:i], spec.Extensions[i+1:]...)
				break
			}
		}
		uc = utls.UClient(raw, cfg, utls.HelloCustom)
		if err := uc.ApplyPreset(&spec); err != nil {
			raw.Close()
			return nil, err
		}
	} else {
		uc = utls.UClient(raw, cfg, utls.HelloChrome_120)
	}
	if err := uc.HandshakeContext(ctx); err != nil {
		raw.Close()
		return nil, err
	}
	return uc, nil
}

func httpClient(useUTLS, h1Only bool) (*http.Client, string) {
	if !useUTLS {
		tr := &http.Transport{ForceAttemptHTTP2: true}
		if h1Only {
			tr.ForceAttemptHTTP2 = false
			tr.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
		}
		return &http.Client{Timeout: 40 * time.Second, Transport: tr}, "stdlib"
	}
	if h1Only {
		tr := &http.Transport{
			DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialUTLS(ctx, addr, true)
			},
			ForceAttemptHTTP2: false,
			TLSNextProto:      map[string]func(string, *tls.Conn) http.RoundTripper{},
		}
		return &http.Client{Timeout: 40 * time.Second, Transport: tr}, "utls-h1"
	}
	tr := &http2.Transport{
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			return dialUTLS(ctx, addr, false)
		},
	}
	return &http.Client{Timeout: 40 * time.Second, Transport: tr}, "utls-h2"
}

func post(dev Device, path, body, token string, useUTLS, h1Only bool) (int, string, string, error) {
	reqTime := time.Now().UnixMilli()
	req, err := http.NewRequest("POST", apiBase+path, strings.NewReader(body))
	if err != nil {
		return 0, "", "", err
	}
	for _, kv := range buildHeaders(dev, path, body, reqTime, token) {
		req.Header[kv[0]] = []string{kv[1]}
	}
	if h1Only || !useUTLS {
		req.Header["Connection"] = []string{"close"}
	}
	client, proto := httpClient(useUTLS, h1Only)
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", proto, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, "", proto, err
	}
	// The header set pins Accept-Encoding: gzip, so net/http does not auto-decompress.
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Encoding")), "gzip") {
		if zr, zerr := gzip.NewReader(bytes.NewReader(b)); zerr == nil {
			if plain, zerr := io.ReadAll(zr); zerr == nil {
				b = plain
			}
			zr.Close()
		}
	}
	return resp.StatusCode, string(b), proto, nil
}

func main() {
	if base := os.Getenv("POC_VAULT_SCAN"); base != "" {
		runVaultScan(base)
		return
	}
	if dir := os.Getenv("POC_VAULT"); dir != "" {
		runVaultMode(dir)
		return
	}

	if probe := os.Getenv("POC_PROBE"); probe != "" {
		path, body := "/otp/validate/v2", "{}"
		if probe == "dashboard" {
			path, body = "/dashboard/get/v7", "{}"
		}
		_, text, _, err := post(defaultDevice(), "/token/guest/v1", "{}", "", false, false)
		if err != nil {
			fmt.Println("ERR guest:", err)
			os.Exit(1)
		}
		var g map[string]any
		_ = json.Unmarshal([]byte(text), &g)
		token := ""
		if d, ok := g["data"].(map[string]any); ok {
			token, _ = d["tokenid"].(string)
		}
		fmt.Printf("GUEST code=%v token_len=%d\n", g["code"], len(token))

		status, text, _, err := post(defaultDevice(), path, body, token, false, false)
		if err != nil {
			fmt.Println("ERR probe:", err)
			os.Exit(1)
		}
		var p map[string]any
		_ = json.Unmarshal([]byte(text), &p)
		fmt.Printf("PROBE %s STATUS=%d code=%v status=%v msg=%v\n", path, status, p["code"], p["status"], p["message"])
		if len(text) > 240 {
			text = text[:240]
		}
		fmt.Println("RAW:", text)
		return
	}

	useUTLS := os.Getenv("POC_TLS") != "stdlib"
	h1Only := os.Getenv("POC_ALPN") == "h1"
	status, text, proto, err := post(defaultDevice(), "/token/guest/v1", "{}", "", useUTLS, h1Only)
	if err != nil {
		fmt.Println("PROTO:", proto)
		fmt.Println("ERR:", err)
		os.Exit(1)
	}
	fmt.Println("PROTO:", proto)
	fmt.Println("STATUS:", status)
	var data map[string]any
	if json.Unmarshal([]byte(text), &data) == nil {
		fmt.Println("CODE:", data["code"])
		if d, ok := data["data"].(map[string]any); ok {
			if tk, ok := d["tokenid"].(string); ok {
				fmt.Println("TOKEN_FOUND:", tk != "", "len:", len(tk))
			}
		}
	}
	if len(text) > 400 {
		text = text[:400]
	}
	fmt.Println("RAW(400):", text)
}
