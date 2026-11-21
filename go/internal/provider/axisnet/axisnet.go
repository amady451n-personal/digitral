// Package axisnet implements the non-Digitral AXIS client: DESede/ECB SSO,
// HMAC-signed login, and the nq/balance dashboard pair.
package axisnet

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"digitral/backend/internal/provider"
	"digitral/backend/internal/vault"
)

type Client struct {
	v          *vault.Vault
	msisdn     string
	token      string
	http       *http.Client
	quotaURL   string
	balanceURL string
}

func New(v *vault.Vault, msisdn string) *Client {
	c := &Client{v: v, msisdn: msisdn, http: &http.Client{Timeout: 45 * time.Second},
		quotaURL: quotaURL, balanceURL: balanceURL}
	if s := v.Session(msisdn); s != nil {
		c.token, _ = s["token"].(string)
	}
	return c
}

func (c *Client) Provider() string { return "axisnet" }
func (c *Client) Token() string    { return c.token }

// ParseDashboards parses the captured (already decrypted) nq/balance payloads.
func ParseDashboards(raw []byte) (provider.Balance, error) {
	var d map[string]any
	if err := json.Unmarshal(raw, &d); err != nil {
		return provider.Balance{}, err
	}
	quota := innerData(asMap(asMap(d["quota_home_raw"])["data"]))
	balance := innerData(asMap(asMap(d["balance_raw"])["data"]))

	summ := asMap(quota["summary"])
	formatted := asMap(summ["formatted"])
	result := asMap(balance["result"])
	grace := asMap(balance["graceStatus"])

	unlimited, _ := summ["is_unlimited"].(bool)
	return provider.Balance{
		Balance:       firstNonEmpty(result["balance"]),
		Quota:         formatted["total_remaining"],
		Unit:          "",
		Used:          nil,
		Unlimited:     unlimited,
		ActiveUntil:   stripExp(firstNonEmpty(result["activestopdate"])),
		GraceUntil:    stripExp(firstNonEmpty(grace["information"], result["disablestopdate"])),
		PackageQuotas: parsePackages(quota),
		Bonstri:       nil,
	}, nil
}

// innerData unwraps an API envelope: {"status",...,"data":{...}}.
func innerData(env map[string]any) map[string]any {
	if d, ok := env["data"].(map[string]any); ok {
		return d
	}
	if s, ok := env["data"].(string); ok {
		if plain, err := ssoDecrypt(s); err == nil {
			var d map[string]any
			if json.Unmarshal([]byte(plain), &d) == nil {
				return d
			}
		}
		if raw, err := base64.StdEncoding.DecodeString(s); err == nil {
			var d map[string]any
			if json.Unmarshal(raw, &d) == nil {
				return d
			}
		}
	}
	return env
}

func parsePackages(qh map[string]any) []provider.PackageQuota {
	pkgs := []provider.PackageQuota{}
	for _, item := range asList(qh["packages"]) {
		p := asMap(item)
		if p == nil {
			continue
		}
		name := fmt.Sprint(firstNonEmpty(p["name"]))
		sisa := strings.TrimSpace(strings.ReplaceAll(fmt.Sprint(firstNonEmpty(p["remaining_text"], p["remaining_formatted"])), "SISA ", ""))
		if sisa == "" || sisa == "<nil>" {
			continue
		}
		unlimited, _ := p["is_unlimited"].(bool)
		if unlimited {
			sisa += " (UNLIMITED)"
		} else {
			sisa = toMB(sisa)
		}
		pkgs = append(pkgs, provider.PackageQuota{Name: name, Remaining: sisa, Unit: ""})
	}
	return pkgs
}

// toMB normalises "12.23 GB" / "410.00 MB" / "530 KB" to "N MB".
func toMB(text string) string {
	t := strings.ToUpper(strings.TrimSpace(text))
	for _, unit := range []struct {
		suffix string
		scale  float64
	}{{"GB", 1024}, {"MB", 1}, {"KB", 1.0 / 1024}} {
		if strings.HasSuffix(t, unit.suffix) {
			n, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(t, unit.suffix)), 64)
			if err == nil {
				return fmt.Sprintf("%.2f MB", n*unit.scale)
			}
		}
	}
	return text
}

func stripExp(v any) any {
	s, ok := v.(string)
	if !ok {
		return v
	}
	return strings.TrimSpace(strings.ReplaceAll(s, "Exp on ", ""))
}

// SendOTP asks AXIS to send a code. Only call on explicit user request.
func (c *Client) SendOTP(ctx context.Context, msisdn string) (string, error) {
	url, h, body := otpRequest(msisdn, "", false)
	if _, err := c.post(ctx, url, h, body); err != nil {
		return "", err
	}
	return "", nil // /otplogin/v2 is stateless, there is no transid
}

// ValidateOTP exchanges the OTP for a login token and stores an exp-less session.
func (c *Client) ValidateOTP(ctx context.Context, msisdn, otp string) (string, error) {
	url, h, body := otpRequest(msisdn, strings.TrimSpace(otp), true)
	raw, err := c.post(ctx, url, h, body)
	if err != nil {
		return "", err
	}
	var env map[string]any
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", err
	}
	data := innerData(env)
	tok, _ := data["token"].(string)
	if tok == "" {
		return "", fmt.Errorf("axisnet login has no token: %.200s", raw)
	}
	c.token = tok
	if c.v != nil {
		sessions, _ := c.v.Data["sessions"].(map[string]any)
		if sessions == nil {
			sessions = map[string]any{}
			c.v.Data["sessions"] = sessions
		}
		sessions[msisdn] = map[string]any{
			"provider": "AXISNET", "token": tok,
			"issued": time.Now().Format("2006-01-02T15:04:05-07"),
			"scheme": "Bearer", "device": "SM-A235F",
		}
		_ = c.v.Save()
	}
	return tok, nil
}

func (c *Client) post(ctx context.Context, url string, headers map[string]string, body string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header[k] = []string{v}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return nil, fmt.Errorf("axisnet HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func asMap(v any) map[string]any { m, _ := v.(map[string]any); return m }
func asList(v any) []any         { l, _ := v.([]any); return l }
func firstNonEmpty(vals ...any) any {
	for _, v := range vals {
		if v == nil {
			continue
		}
		if s, ok := v.(string); ok && (s == "" || s == "<nil>") {
			continue
		}
		return v
	}
	return nil
}

// Balance fetches both dashboard sources and normalises them. It never sends OTP.
func (c *Client) Balance(ctx context.Context, msisdn string) (provider.Balance, error) {
	if c.token == "" && c.v != nil {
		if s := c.v.Session(msisdn); s != nil {
			c.token, _ = s["token"].(string)
		}
	}
	if c.token == "" {
		return provider.Balance{}, fmt.Errorf("no token for %s; OTP required", msisdn)
	}
	get := func(url string) (map[string]any, error) {
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, err
		}
		req.Header["Authorization"] = []string{c.token}
		req.Header["x-app-version"] = []string{appVersion}
		req.Header["x-app-request-id"] = []string{requestID()}
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		var env map[string]any
		if err := json.Unmarshal(b, &env); err != nil {
			return nil, err
		}
		return env, nil
	}
	quota, err := get(c.quotaURL)
	if err != nil {
		return provider.Balance{}, err
	}
	bal, err := get(c.balanceURL)
	if err != nil {
		return provider.Balance{}, err
	}
	combined, _ := json.Marshal(map[string]any{
		"quota_home_raw": map[string]any{"status": 200, "data": quota},
		"balance_raw":    map[string]any{"status": 200, "data": bal},
	})
	return ParseDashboards(combined)
}

var _ provider.Client = (*Client)(nil)
