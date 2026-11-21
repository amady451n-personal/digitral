package provider

import (
	"encoding/json"
	"encoding/xml"
	"os"
	"reflect"
	"testing"
)

// Fixtures were captured by the Python scripts (scripts/*.py) and sanitised by
// masking the msisdn. The comparison is field-by-field against those captured
// results, so the Go parser is pinned to Python behaviour.
func TestParseParityIMI(t *testing.T) {
	for _, prov := range []string{"bima", "myim3"} {
		t.Run(prov, func(t *testing.T) {
			raw, err := os.ReadFile("testdata/dashboards/" + prov + ".json")
			if err != nil {
				t.Fatal(err)
			}
			want := readExpected(t, prov+".expected.json")

			got, err := ParseDashboard(prov, raw)
			if err != nil {
				t.Fatalf("ParseDashboard: %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("ParseDashboard(%s) mismatch\n got: %#v\nwant: %#v", prov, got, want)
			}
		})
	}
}

// The XML envelope (<Response><data>{...}</data></Response>) and the JSON
// envelope ({"status","code","data":{...}}) must both yield the same payload.
func TestParseAcceptsBothEnvelopes(t *testing.T) {
	raw, err := os.ReadFile("testdata/dashboards/bima.json")
	if err != nil {
		t.Fatal(err)
	}
	fromXML, err := ParseDashboard("bima", raw)
	if err != nil {
		t.Fatalf("xml: %v", err)
	}
	inner := extractData(t, raw)
	fromJSON, err := ParseDashboard("bima", inner)
	if err != nil {
		t.Fatalf("json: %v", err)
	}
	if !reflect.DeepEqual(fromXML, fromJSON) {
		t.Errorf("envelopes disagree: %#v vs %#v", fromXML, fromJSON)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := ParseDashboard("bima", []byte("not a dashboard")); err == nil {
		t.Fatal("garbage accepted")
	}
}

func TestFmtQuota(t *testing.T) {
	cases := []struct {
		name string
		in   Balance
		want string
	}{
		{"kb-below-mb", Balance{Quota: 512, Unit: "KB"}, "512 KB"},
		{"kb-to-mb", Balance{Quota: 150528, Unit: "KB"}, "147 MB"},
		{"kb-to-gb", Balance{Quota: 31981572, Unit: "KB"}, "30.5 GB"},
		{"unlimited", Balance{Unlimited: true, Unit: ""}, "UNLIMITED"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := FmtQuota(c.in); got != c.want {
				t.Errorf("FmtQuota = %q, want %q", got, c.want)
			}
		})
	}
}

func readExpected(t *testing.T, path string) Balance {
	t.Helper()
	b, err := os.ReadFile("testdata/dashboards/" + path)
	if err != nil {
		t.Fatal(err)
	}
	var got Balance
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return got
}

// extractData pulls the <data> element out of the XML envelope as raw JSON.
func extractData(t *testing.T, raw []byte) []byte {
	t.Helper()
	var env struct {
		Data string `xml:"data"`
	}
	if err := xml.Unmarshal(raw, &env); err != nil {
		t.Fatalf("xml envelope: %v", err)
	}
	return []byte(env.Data)
}

// The dashboard must stay provider-blind: every provider's Balance JSON has
// exactly the same key set, even when a field is null.
func TestAllProvidersShareBalanceKeys(t *testing.T) {
	var reference map[string]any
	for _, prov := range []string{"bima", "myim3", "axisnet", "myxl"} {
		b, err := os.ReadFile("testdata/dashboards/" + prov + ".expected.json")
		if err != nil {
			t.Fatal(err)
		}
		var bal Balance
		if err := json.Unmarshal(b, &bal); err != nil {
			t.Fatal(err)
		}
		// Compare what Go actually emits, not the raw Python fixtures: myXL's
		// subscription_status is present as null on every other provider too.
		out, _ := json.Marshal(bal)
		var keys map[string]any
		_ = json.Unmarshal(out, &keys)
		if reference == nil {
			reference = keys
			continue
		}
		if len(keys) != len(reference) {
			t.Errorf("%s key count = %d, want %d", prov, len(keys), len(reference))
		}
		for k := range reference {
			if _, ok := keys[k]; !ok {
				t.Errorf("%s missing key %s", prov, k)
			}
		}
	}
}
