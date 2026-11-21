package imi

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"digitral/backend/internal/cryptoutil"
	"digitral/backend/internal/provider"
	"digitral/backend/internal/vault"
)

const (
	otpKey = "A9$gT#2vXp!qW7LmZ@e4Ky8Nb3Hj5Uq9" // 32 bytes, AES-256
	otpIV  = "Rx4Cm9Pn2Vf8Kt6B"                 // 16 bytes, static
)

// Client speaks the Digitral protocol (bima+ / myIM3).
type Client struct {
	cfg       Config
	v         *vault.Vault
	dev       Device
	token     string
	http      *http.Client
	transport http.RoundTripper
}

func New(cfg Config, v *vault.Vault, dev Device, rt http.RoundTripper) *Client {
	if rt == nil {
		rt = http.DefaultTransport
	}
	return &Client{cfg: cfg, v: v, dev: dev, transport: rt, http: &http.Client{Timeout: 45 * time.Second, Transport: rt}}
}

func (c *Client) Provider() string { return c.cfg.Brand }
func (c *Client) Token() string    { return c.token }

func (c *Client) providerName() string {
	if strings.EqualFold(c.cfg.Brand, "myIM3") {
		return "myim3"
	}
	return "bima"
}

// post sends one signed request and returns the response body (already gunzipped).
func (c *Client) post(ctx context.Context, path, body string) ([]byte, error) {
	reqTime := time.Now().UnixMilli()
	req, err := http.NewRequestWithContext(ctx, "POST", c.cfg.Base+path, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	for k, v := range headers(c.cfg, c.dev, path, body, reqTime, c.token) {
		// Assign directly so header name casing matches the Python client.
		req.Header[k] = []string{v}
	}
	if c.token == "" {
		// h1 only; the stdlib transport handles Connection itself on h2.
		req.Header["Connection"] = []string{"close"}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Encoding")), "gzip") {
		if zr, zerr := gzip.NewReader(bytes.NewReader(b)); zerr == nil {
			if plain, zerr := io.ReadAll(zr); zerr == nil {
				b = plain
			}
			zr.Close()
		}
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s HTTP %d: %.200s", path, resp.StatusCode, b)
	}
	var env map[string]any
	if json.Unmarshal(b, &env) == nil {
		if code := fmt.Sprint(env["code"]); code != "" && code != "<nil>" && code != "0" {
			// 26100 (guest/OTP) and 26000 (dashboard) mean success.
			if code != "26100" && code != "26000" {
				return nil, fmt.Errorf("%s code %s: %v", path, code, env["message"])
			}
		}
	}
	return b, nil
}

// GuestToken obtains a GUEST JWT without sending any OTP.
func (c *Client) GuestToken(ctx context.Context) (string, error) {
	c.token = ""
	b, err := c.post(ctx, "/token/guest/v1", "{}")
	if err != nil {
		return "", err
	}
	tok := deepFindString(mustJSON(b), tokenKeys...)
	if tok == "" {
		return "", fmt.Errorf("guest token missing: %.200s", b)
	}
	c.token = tok
	return tok, nil
}

// SendOTP asks the API to send an OTP to msisdn. Call only on explicit request.
func (c *Client) SendOTP(ctx context.Context, msisdn, action string) (string, error) {
	body, err := json.Marshal(struct {
		MSISDN string `json:"msisdn"`
		Action string `json:"action"`
	}{msisdn, action})
	if err != nil {
		return "", err
	}
	b, err := c.post(ctx, "/otp/send/v1", string(body))
	if err != nil {
		return "", err
	}
	id := deepFindValue(mustJSON(b), "transId", "transid", "transactionId")
	if id == "" {
		return "", fmt.Errorf("OTP send ok but no transId: %.200s", b)
	}
	return id, nil
}

// ValidateOTP exchanges the OTP for a login token and stores the session.
func (c *Client) ValidateOTP(ctx context.Context, msisdn, transid, otp string) (string, error) {
	inner, err := json.Marshal(struct {
		TransID string `json:"transid"`
		OTP     string `json:"otp"`
	}{transid, otp})
	if err != nil {
		return "", err
	}
	body, err := cryptoutil.AESGCMEncrypt([]byte(otpKey), []byte(otpIV), inner)
	if err != nil {
		return "", err
	}
	b, err := c.post(ctx, "/otp/validate/v2", body)
	if err != nil {
		return "", err
	}
	tok := deepFindString(mustJSON(b), tokenKeys...)
	if tok == "" {
		return "", fmt.Errorf("validate ok but no token: %.200s", b)
	}
	c.token = tok
	c.storeSession(msisdn, tok)
	return tok, nil
}

func (c *Client) storeSession(msisdn, tok string) {
	if c.v == nil {
		return
	}
	sessions, _ := c.v.Data["sessions"].(map[string]any)
	if sessions == nil {
		sessions = map[string]any{}
		c.v.Data["sessions"] = sessions
	}
	sessions[msisdn] = map[string]any{
		"token": tok, "exp": cryptoutil.JWTExp(tok),
		"action": "login", "deviceId": c.dev.ID,
	}
	_ = c.v.Save()
}

// Dashboard returns the raw /dashboard/get/v7 payload.
func (c *Client) Dashboard(ctx context.Context) ([]byte, error) {
	return c.post(ctx, "/dashboard/get/v7", "{}")
}

// Balance reuses a stored session when present. It never sends OTP by itself.
func (c *Client) Balance(ctx context.Context, msisdn string) (provider.Balance, error) {
	if c.token == "" && c.v != nil {
		if sess := c.v.Session(msisdn); sess != nil {
			c.token, _ = sess["token"].(string)
		}
	}
	if c.token == "" {
		return provider.Balance{}, fmt.Errorf("no saved session for %s; OTP required", msisdn)
	}
	b, err := c.Dashboard(ctx)
	if err != nil {
		return provider.Balance{}, err
	}
	return provider.ParseDashboard(c.providerName(), b)
}

var tokenKeys = []string{"access_token", "accessToken", "tokenId", "tokenid", "token", "sessionId", "secretKey"}

func mustJSON(b []byte) map[string]any {
	var d map[string]any
	_ = json.Unmarshal(b, &d)
	return d
}

func deepFindString(obj any, keys ...string) string {
	switch v := obj.(type) {
	case map[string]any:
		for _, k := range keys {
			if s, ok := v[k].(string); ok && len(s) > 8 {
				return s
			}
		}
		for _, val := range v {
			if s := deepFindString(val, keys...); s != "" {
				return s
			}
		}
	case []any:
		for _, val := range v {
			if s := deepFindString(val, keys...); s != "" {
				return s
			}
		}
	}
	return ""
}

// deepFindValue is like deepFindString but accepts short values such as transId.
func deepFindValue(obj any, keys ...string) string {
	switch v := obj.(type) {
	case map[string]any:
		for _, k := range keys {
			if s, ok := v[k].(string); ok && s != "" {
				return s
			}
		}
		for _, val := range v {
			if s := deepFindValue(val, keys...); s != "" {
				return s
			}
		}
	case []any:
		for _, val := range v {
			if s := deepFindValue(val, keys...); s != "" {
				return s
			}
		}
	}
	return ""
}
