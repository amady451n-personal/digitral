// Package myxl implements the myXL CIAM login and encrypted v8 API.
package myxl

import (
	"context"
	crand "crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"digitral/backend/internal/cryptoutil"
	"digitral/backend/internal/provider"
	"digitral/backend/internal/vault"
)

const (
	defaultCIAM = "https://gede.ciam.xlaxiata.co.id/"
	defaultAPI  = "https://api.myxl.xlaxiata.co.id/api/v8/"
)

type Client struct {
	v        *vault.Vault
	msisdn   string
	token    string
	ciamBase string
	apiBase  string
	http     *http.Client
}

func New(v *vault.Vault, msisdn string) *Client {
	c := &Client{v: v, msisdn: msisdn, http: &http.Client{Timeout: 45 * time.Second},
		ciamBase: os.Getenv("MYXL_CIAM_BASE"), apiBase: os.Getenv("MYXL_API_BASE")}
	if c.ciamBase == "" {
		c.ciamBase = defaultCIAM
	}
	if c.apiBase == "" {
		c.apiBase = defaultAPI
	}
	// Python _saved() reads raw sessions so an expired API token can still refresh.
	if s := v.RawSession(msisdn); s != nil {
		c.token, _ = s["token"].(string)
	}
	return c
}

func (c *Client) Provider() string { return "myxl" }
func (c *Client) Token() string    { return c.token }

// ParseAPI normalises the three decrypted v8 payloads into the shared balance.
func ParseAPI(plain map[string]any) (provider.Balance, error) {
	bal := asMap(asMap(plain["balance-and-credit"])["data"])
	qdata := asMap(asMap(plain["quota-summary"])["data"])
	quota := asMap(asMap(qdata["quota"])["data"])
	qd := asMap(plain["quota-details"])
	qdData := asMap(qd["data"])

	total, _ := asInt(quota["total"])
	remaining, _ := asInt(quota["remaining"])
	used := total - remaining
	if used < 0 {
		used = 0
	}
	unlimited, _ := quota["is_unlimited"].(bool)

	pkgs := []provider.PackageQuota{}
	for _, raw := range asList(qdData["quotas"]) {
		for _, braw := range asList(asMap(raw)["benefits"]) {
			b := asMap(braw)
			t, _ := asInt(b["total"])
			if t <= 0 {
				continue
			}
			rem, _ := asInt(b["remaining"])
			pkgs = append(pkgs, provider.PackageQuota{
				Name:      strings.TrimSpace(fmt.Sprint(b["name"])),
				Remaining: fmt.Sprintf("%d MB", rem/(1024*1024)), Unit: "",
			})
		}
	}
	return provider.Balance{
		Balance:            asMap(bal["balance"])["remaining"],
		Quota:              float64(total / 1024),
		Unit:               "KB",
		Used:               float64(used / 1024),
		Unlimited:          unlimited,
		ActiveUntil:        fmtTS(asMap(bal["balance"])["expired_at"]),
		GraceUntil:         fmtTS(bal["grace_end_date"]),
		PackageQuotas:      pkgs,
		Bonstri:            nil,
		SubscriptionStatus: bal["subscription_status"],
	}, nil
}

func fmtTS(v any) any {
	n, ok := asInt(v)
	if !ok || n <= 0 {
		return nil
	}
	return time.Unix(int64(n), 0).Format("02 Jan 2006")
}

// Balance refreshes a stale CIAM token when needed, then reads the encrypted API.
func (c *Client) Balance(ctx context.Context, msisdn string) (provider.Balance, error) {
	sess := c.v.RawSession(msisdn)
	if c.token == "" && sess != nil {
		c.token, _ = sess["token"].(string)
	}
	if c.token == "" {
		return provider.Balance{}, fmt.Errorf("no myXL token for %s; OTP required", msisdn)
	}
	tokExp, _ := asInt(sessionVal(sess, "token_exp"))
	if tokExp > 0 && time.Now().Unix() > int64(tokExp)-120 {
		if err := c.refresh(ctx, msisdn); err != nil {
			return provider.Balance{}, fmt.Errorf("refresh failed: %w", err)
		}
	}
	post := func(path string) (map[string]any, error) {
		req, err := http.NewRequestWithContext(ctx, "POST", c.apiBase+path, strings.NewReader(""))
		if err != nil {
			return nil, err
		}
		ts := fmt.Sprint(time.Now().Unix())
		for k, v := range map[string]string{
			"Authorization": "Bearer " + c.token, "x-api-key": apiKey,
			"Accept": "application/json", "X-REQUEST-ID": newID(),
			"X-REQUEST-AT":  time.Now().Format("2006-01-02T15:04:05.000-0700"),
			"X-VERSION-APP": "9.6.0", "x-hv": "v3",
			"x-signature-time": ts, "x-signature": apiSignature(c.token, "POST", "api/v8/"+path, ts),
			"User-Agent": appUA, "Content-Type": "application/json",
		} {
			req.Header[k] = []string{v}
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
		var env struct {
			Xdata string `json:"xdata"`
			Xtime int64  `json:"xtime"`
		}
		if err := json.Unmarshal(b, &env); err != nil {
			return nil, err
		}
		if env.Xdata == "" {
			var plain map[string]any
			if err := json.Unmarshal(b, &plain); err != nil {
				return nil, err
			}
			return plain, nil
		}
		return decrypt(env.Xdata, env.Xtime)
	}
	bal, err := post("packages/balance-and-credit")
	if err != nil {
		return provider.Balance{}, err
	}
	q, err := post("packages/quota-summary")
	if err != nil {
		return provider.Balance{}, err
	}
	qd, err := post("packages/quota-details")
	if err != nil {
		return provider.Balance{}, err
	}
	return ParseAPI(map[string]any{"balance-and-credit": bal, "quota-summary": q, "quota-details": qd})
}

// refresh replays the stored refresh_token with no OTP.
func (c *Client) refresh(ctx context.Context, msisdn string) error {
	rt, _ := sessionVal(c.v.RawSession(msisdn), "refresh_token").(string)
	if rt == "" {
		return fmt.Errorf("no refresh_token")
	}
	body := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {rt}}
	tok, err := c.tokenGrant(ctx, body.Encode(), msisdn)
	if err != nil {
		return err
	}
	c.storeSession(msisdn, tok, rt)
	return nil
}

func (c *Client) tokenGrant(ctx context.Context, form, msisdn string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", c.ciamBase+"realms/xl-ciam/protocol/openid-connect/token", strings.NewReader(form))
	if err != nil {
		return nil, err
	}
	fp, _ := fingerprint(msisdn)
	// CIAM binds a refresh_token to the client context used at issuance. The
	// Python client sends this whole device header set on every token grant;
	// dropping it yields "User Agent mismatch during token refresh".
	for k, v := range map[string]string{
		"Authorization":           basicAuth(),
		"Accept":                  "application/json",
		"Content-Type":            "application/x-www-form-urlencoded",
		"User-Agent":              ciamUA,
		"ax-request-at":           time.Now().Format("2006-01-02T15:04:05.000-0700"),
		"ax-device-id":            c.v.DeviceID(),
		"ax-request-id":           newID(),
		"ax-request-device":       "Xiaomi",
		"ax-request-device-model": "M2101K7AG",
		"ax-fingerprint":          fp,
	} {
		req.Header[k] = []string{v}
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
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("CIAM HTTP %d: %.200s", resp.StatusCode, b)
	}
	var tok map[string]any
	if err := json.Unmarshal(b, &tok); err != nil {
		return nil, err
	}
	bearer, _ := tok["id_token"].(string)
	if bearer == "" {
		bearer, _ = tok["access_token"].(string)
	}
	c.token = bearer
	return tok, nil
}

func (c *Client) storeSession(msisdn string, tok map[string]any, rt string) {
	if rt == "" {
		rt, _ = tok["refresh_token"].(string)
	}
	bearer, _ := tok["id_token"].(string)
	if bearer == "" {
		bearer, _ = tok["access_token"].(string)
	}
	if c.v == nil {
		return
	}
	_ = c.v.Update(func(data map[string]any) {
		sessions, _ := data["sessions"].(map[string]any)
		if sessions == nil {
			sessions = map[string]any{}
			data["sessions"] = sessions
		}
		sessions[msisdn] = map[string]any{
			"token": bearer, "id_token": tok["id_token"], "access_token": tok["access_token"],
			"refresh_token": rt, "exp": cryptoutil.JWTExp(rt),
			"token_exp": cryptoutil.JWTExp(fmt.Sprint(tok["access_token"])),
		}
	})
}

func sessionVal(s map[string]any, key string) any {
	if s == nil {
		return nil
	}
	return s[key]
}

func newID() string {
	b := make([]byte, 16)
	if _, err := crand.Read(b); err != nil {
		return "00000000-0000-0000-0000-000000000000"
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := fmt.Sprintf("%x", b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

var _ provider.Client = (*Client)(nil)

// SendOTP asks CIAM to send a code. Only call on explicit user request.
func (c *Client) SendOTP(ctx context.Context, msisdn string) (string, error) {
	contactType := "SMS"
	if strings.Contains(msisdn, "@") {
		contactType = "EMAIL"
	}
	at, sig := loginSignature("password", contactType, msisdn, "", "openid")
	u := c.ciamBase + "realms/xl-ciam/auth/otp?contact=" + url.QueryEscape(msisdn) +
		"&contactType=" + contactType + "&alternateContact=" + map[bool]string{true: "true", false: "false"}[contactType == "EMAIL"]
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return "", err
	}
	fp, _ := fingerprint(msisdn)
	for k, v := range map[string]string{
		"Authorization": basicAuth(), "Accept": "application/json",
		"ax-request-at": at, "ax-api-signature": sig,
		"ax-device-id": c.v.DeviceID(), "ax-request-id": newID(),
		"ax-request-device": "Xiaomi", "ax-request-device-model": "M2101K7AG",
		"ax-fingerprint": fp,
	} {
		req.Header[k] = []string{v}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("myXL OTP HTTP %d: %.200s", resp.StatusCode, b)
	}
	return "", nil // CIAM validate is stateless, no transid
}

// ValidateOTP exchanges the OTP for an id_token and stores the session.
func (c *Client) ValidateOTP(ctx context.Context, msisdn, _ string, otp string) (string, error) {
	contactType := "SMS"
	if strings.Contains(msisdn, "@") {
		contactType = "EMAIL"
	}
	at, sig := loginSignature("password", contactType, msisdn, strings.TrimSpace(otp), "openid")
	form := url.Values{
		"grant_type": {"password"}, "contact": {msisdn}, "contactType": {contactType},
		"code": {strings.TrimSpace(otp)}, "scope": {"openid"},
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.ciamBase+"realms/xl-ciam/protocol/openid-connect/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	for k, v := range map[string]string{
		"Authorization": basicAuth(), "Content-Type": "application/x-www-form-urlencoded",
		"ax-request-at": at, "ax-api-signature": sig,
		"ax-device-id": c.v.DeviceID(), "ax-request-id": newID(),
		"ax-request-device": "Xiaomi", "ax-request-device-model": "M2101K7AG",
	} {
		req.Header[k] = []string{v}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("myXL validate HTTP %d: %.200s", resp.StatusCode, b)
	}
	var tok map[string]any
	if err := json.Unmarshal(b, &tok); err != nil {
		return "", err
	}
	bearer, _ := tok["id_token"].(string)
	if bearer == "" {
		bearer, _ = tok["access_token"].(string)
	}
	c.token = bearer
	c.storeSession(msisdn, tok, "")
	return bearer, nil
}
