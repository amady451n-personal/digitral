package provider

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var xmlDataRe = regexp.MustCompile(`(?s)<data>(.*)</data>`)

// ParseDashboard parses the shared Digitral (imi) dashboard payload used by
// bima+ and myIM3. axisnet and myXL have their own normalisers.
func ParseDashboard(provider string, raw []byte) (Balance, error) {
	switch provider {
	case "bima", "myim3", "":
	default:
		return Balance{}, fmt.Errorf("provider %q has its own parser", provider)
	}
	dash := unwrap(string(raw))
	if dash == nil {
		return Balance{}, fmt.Errorf("unparsable dashboard payload")
	}
	return parseIMI(dash), nil
}

// unwrap accepts the JSON envelope {"status","code","data":{...}} and the XML
// envelope <Response>...<data>{...}</data></Response>, returning the payload
// object in both cases.
func unwrap(text string) map[string]any {
	var d map[string]any
	if json.Unmarshal([]byte(text), &d) == nil {
		return descend(d)
	}
	if m := xmlDataRe.FindStringSubmatch(text); m != nil {
		var env struct {
			Data string `xml:"data"`
		}
		if xml.Unmarshal([]byte(text), &env) == nil && env.Data != "" {
			if json.Unmarshal([]byte(html.UnescapeString(env.Data)), &d) == nil {
				return descend(d)
			}
		}
		if json.Unmarshal([]byte(html.UnescapeString(m[1])), &d) == nil {
			return descend(d)
		}
	}
	return nil
}

// descend moves into the envelope's data object when the payload is not already
// at the top level (no prepaidinfo key).
func descend(d map[string]any) map[string]any {
	if d == nil {
		return nil
	}
	if _, ok := d["prepaidinfo"]; ok {
		return d
	}
	if inner, ok := d["data"].(map[string]any); ok {
		return inner
	}
	return d
}

func parseIMI(dash map[string]any) Balance {
	data := asMap(asMap(dash["summary"])["data"])
	prep := asMap(dash["prepaidinfo"])
	packdata := asMap(dash["packdata"])

	today := time.Now()
	pkgs := []PackageQuota{} // Python always returns a list, never null
	for _, raw := range asList(packdata["packageslist"]) {
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
		Balance:       firstNonEmpty(prep["balance"], packdata["lastbalance"]),
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

func asMap(v any) map[string]any { m, _ := v.(map[string]any); return m }
func asList(v any) []any         { l, _ := v.([]any); return l }

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(n))
		return i, err == nil
	}
	return 0, false
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

// FmtQuota renders the remaining quota the way the dashboard displays it.
func FmtQuota(b Balance) string {
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
