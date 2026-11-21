package imi

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

type headerCase struct {
	Name    string            `json:"name"`
	Path    string            `json:"path"`
	Body    string            `json:"body"`
	Token   string            `json:"token"`
	ReqTime int64             `json:"req_time"`
	Headers map[string]string `json:"headers"`
}

type headerVector struct {
	Provider string       `json:"provider"`
	Cases    []headerCase `json:"cases"`
}

// The vectors were produced by scripts/bima_balance.py's build_headers with a
// synthetic device identity, so this compares Go against Python byte-for-byte.
func TestHeadersParity(t *testing.T) {
	for _, prov := range []string{"bima", "myim3"} {
		t.Run(prov, func(t *testing.T) {
			b, err := os.ReadFile("testdata/headers_" + prov + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var v headerVector
			if err := json.Unmarshal(b, &v); err != nil {
				t.Fatal(err)
			}
			cfg := configFor(prov)
			for _, c := range v.Cases {
				got := headers(cfg, testDevice(), c.Path, c.Body, c.ReqTime, c.Token)
				if !reflect.DeepEqual(got, c.Headers) {
					t.Errorf("%s/%s header mismatch:\n got: %#v\nwant: %#v", prov, c.Name, got, c.Headers)
				}
			}
		})
	}
}

func testDevice() Device {
	return Device{
		ID: "0123456789abcdef", Name: "generic", OEM: "samsung",
		Model: "SM-A235F", OSVer: "14",
		Faaid: "fa000000000000000000000000000000",
		Gaid:  "00000000-0000-0000-0000-000000000001",
	}
}
