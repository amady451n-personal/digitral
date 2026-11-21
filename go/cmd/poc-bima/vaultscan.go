package main

// vaultscan.go: cross-provider vault inspection.
//
// The container format is one implementation for every provider; what differs is
// the `sessions` payload. This mode prints the shape of each provider vault so
// the per-provider clients can be written against real data instead of guesses.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"digitral/backend/internal/vault"
)

type providerShape struct {
	sessionKeys string
	expiry      string
	extra       string
}

var providerShapes = map[string]providerShape{
	"bima":    {"action deviceId exp token", "exp (JWT, seconds)", ""},
	"myim3":   {"action deviceId exp token", "exp (JWT, seconds)", ""},
	"myxl":    {"access_token exp id_token refresh_token token token_exp", "exp=refresh_token, token_exp=access_token", "refresh_token grant keeps the session alive"},
	"axisnet": {"device issued provider scheme token", "none stored (issued is a timestamp)", "token is long-lived; no exp to gate on"},
}

func runVaultScan(base string) {
	entries, err := os.ReadDir(base)
	if err != nil {
		fmt.Println("SCAN ERR:", err)
		os.Exit(1)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	sort.Strings(dirs)

	fail := 0
	for _, name := range dirs {
		dir := filepath.Join(base, name)
		if _, err := os.Stat(filepath.Join(dir, "vault.key")); err != nil {
			continue
		}
		v, err := vault.Open(dir)
		if err != nil {
			fmt.Printf("%-8s OPEN ERR: %v\n", name, err)
			fail++
			continue
		}
		fmt.Printf("%-8s deviceId=%s… faaid=%v gaid=%v profile=%v sessions=%d\n",
			name, trunc(v.DeviceID(), 6), v.Faaid() != "", v.Gaid() != "", v.DeviceProfile() != nil, len(v.SessionMSISDNs()))

		for _, ms := range v.SessionMSISDNs() {
			raw := v.RawSession(ms)
			state := "no-exp"
			switch {
			case raw["exp"] != nil && num(raw["exp"]) > 0:
				if v.Session(ms) != nil {
					state = "valid(exp)"
				} else {
					state = "expired(exp)"
				}
			case raw["token_exp"] != nil:
				state = fmt.Sprintf("access_exp_left=%.1fd", (num(raw["token_exp"])-float64(nowUnix()))/86400)
			case raw["issued"] != nil:
				state = "issued=" + fmt.Sprint(raw["issued"])
			}
			fmt.Printf("         %s keys=[%s] state=%s\n", mask(ms), strings.Join(sortedKeys(raw), " "), state)
		}

		if want, ok := providerShapes[name]; ok {
			extra := ""
			if want.extra != "" {
				extra = " | " + want.extra
			}
			fmt.Printf("         expected: keys=[%s] expiry=%s%s\n", want.sessionKeys, want.expiry, extra)
			if msg, ok := checkShape(v, want); !ok {
				fmt.Printf("         MISMATCH: %s\n", msg)
				fail++
			}
		}
	}
	if fail > 0 {
		os.Exit(1)
	}
}

func num(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f
	}
	return 0
}

func nowUnix() int64 { return time.Now().Unix() }

// checkShape compares every stored session against the documented provider shape.
func checkShape(v *vault.Vault, want providerShape) (string, bool) {
	wantKeys := strings.Fields(want.sessionKeys)
	for _, ms := range v.SessionMSISDNs() {
		got := sortedKeys(v.RawSession(ms))
		if strings.Join(got, " ") != strings.Join(wantKeys, " ") {
			return fmt.Sprintf("%s keys=[%s]", mask(ms), strings.Join(got, " ")), false
		}
	}
	return "", true
}
