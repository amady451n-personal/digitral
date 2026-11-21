package main

// parse.go mirrors parse_dashboard() + fmt_quota() from scripts/bima_balance.py.

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type PackageQuota struct {
	Name      string `json:"name"`
	Remaining string `json:"remaining"`
	Unit      string `json:"unit"`
}

type Balance struct {
	Balance       any            `json:"balance"`
	Quota         any            `json:"quota"`
	Unit          string         `json:"unit"`
	Used          any            `json:"used"`
	Unlimited     bool           `json:"unlimited"`
	ActiveUntil   any            `json:"active_until"`
	GraceUntil    any            `json:"grace_until"`
	PackageQuotas []PackageQuota `json:"package_quotas"`
	Bonstri       any            `json:"bonstri"`
}

var xmlDataRe = regexp.MustCompile(`(?s)<data>(.*)</data>`)

// unwrap accepts plain JSON or the XML envelope <Response>...<data>{JSON}</data>.
func unwrap(text string) map[string]any {
	var d map[string]any
	if json.Unmarshal([]byte(text), &d) != nil {
		if m := xmlDataRe.FindStringSubmatch(text); m != nil {
			d = nil
			if json.Unmarshal([]byte(htmlUnescape(m[1])), &d) != nil {
				return nil
			}
		} else {
			return nil
		}
	}
	// JSON transport wraps the payload in an envelope ({"status","code","data":{...}}),
	// while the XML form's <data> already holds the payload itself. Normalise both.
	if _, hasPayload := d["prepaidinfo"]; !hasPayload {
		if inner, ok := d["data"].(map[string]any); ok {
			return inner
		}
	}
	return d
}

func htmlUnescape(s string) string {
	r := strings.NewReplacer(
		"&quot;", `"`, "&apos;", "'", "&lt;", "<", "&gt;", ">", "&amp;", "&",
	)
	return r.Replace(s)
}

func asMap(v any) map[string]any { m, _ := v.(map[string]any); return m }
func asList(v any) []any         { l, _ := v.([]any); return l }

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(n))
		return i, err == nil
	}
	return 0, false
}

// parseDashboard extracts the same human fields as parse_dashboard().
func parseDashboard(dash map[string]any) Balance {
	data := asMap(asMap(dash["summary"])["data"])
	prep := asMap(dash["prepaidinfo"])

	today := time.Now()
	var pkgs []PackageQuota
	for _, raw := range asList(asMap(dash["packdata"])["packageslist"]) {
		p := asMap(raw)
		if end, err := time.Parse("02.01.2006", fmt.Sprint(p["EndDate"])); err == nil {
			if end.Before(today.Truncate(24 * time.Hour)) {
				continue
			}
		}
		for _, qraw := range asList(p["Quotas"]) {
			q := asMap(qraw)
			if q == nil || strings.ToUpper(fmt.Sprint(q["quotaUnit"])) != "MB" {
				continue
			}
			rem, _ := asInt(q["remainingQuota"])
			name := strings.TrimSpace(fmt.Sprint(firstNonEmpty(p["PackageName"], q["name"])))
			if name == "" || name == "<nil>" {
				continue
			}
			pkgs = append(pkgs, PackageQuota{Name: name, Remaining: fmt.Sprintf("%d MB", rem)})
		}
	}

	unit := strings.ToUpper(fmt.Sprint(firstNonEmpty(data["quotaunit"], "KB")))
	used := data["usedquota"]
	if n, ok := asInt(used); !ok || n < 0 {
		used = nil
	}
	unlimited, _ := data["isunlimited"].(bool)

	return Balance{
		Balance:       firstNonEmpty(prep["balance"], asMap(dash["packdata"])["lastbalance"]),
		Quota:         data["remainingquota"],
		Unit:          unit,
		Used:          used,
		Unlimited:     unlimited,
		ActiveUntil:   prep["cardactiveuntil"],
		GraceUntil:    prep["graceperioduntil"],
		PackageQuotas: pkgs,
		Bonstri:       asMap(dash["bonstriDetails"])["totalPoints"],
	}
}

func firstNonEmpty(vals ...any) any {
	for _, v := range vals {
		if v == nil {
			continue
		}
		if s, ok := v.(string); ok && s == "" {
			continue
		}
		return v
	}
	return nil
}

// fmtQuota mirrors fmt_quota(dict) in server.py / bima_balance.py.
func fmtQuota(b Balance) string {
	if b.Unlimited {
		return "UNLIMITED"
	}
	kb, _ := asInt(b.Quota)
	switch b.Unit {
	case "KB":
		switch {
		case kb >= 1024*1024:
			return fmt.Sprintf("%.1f GB", float64(kb)/(1024*1024))
		case kb >= 1024:
			return fmt.Sprintf("%.0f MB", float64(kb)/1024)
		}
		return fmt.Sprintf("%d KB", kb)
	}
	return fmt.Sprintf("%d %s", kb, b.Unit)
}
