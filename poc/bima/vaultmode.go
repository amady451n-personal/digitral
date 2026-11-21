package main

// runVaultMode reads an existing Python-written vault, reuses its saved session,
// and prints the parsed balance. Read-only: no OTP, no vault write.

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"
)

func runVaultMode(dir string) {
	v, err := OpenVault(dir)
	if err != nil {
		fmt.Println("VAULT ERR:", err)
		os.Exit(1)
	}
	fmt.Printf("VAULT %s: deviceId=%s… profile=%v sessions=%d\n",
		dir, trunc(v.DeviceID(), 6), v.DeviceProfile() != nil, v.sessionCount())

	if os.Getenv("POC_VAULT_SAVE") != "" {
		if err := v.Save(); err != nil {
			fmt.Println("SAVE ERR:", err)
			os.Exit(1)
		}
		fmt.Println("SAVE OK: vault.bin rewritten by Go")
		return
	}

	msisdn := os.Getenv("POC_MSISDN")
	if msisdn == "" {
		msisdn = v.onlySessionMSISDN()
	}
	if msisdn == "" {
		fmt.Println("NO MSISDN: set POC_MSISDN (vault has no usable session)")
		os.Exit(2)
	}
	sess := v.Session(msisdn)
	if sess == nil {
		fmt.Printf("NO VALID SESSION for %s (expired or absent); refusing to send OTP\n", mask(msisdn))
		os.Exit(2)
	}
	token, _ := sess["token"].(string)
	fmt.Printf("SESSION %s: token_len=%d exp_left=%s\n", mask(msisdn), len(token), expLeft(sess))

	useProfile := os.Getenv("POC_DEVICE_PROFILE") != ""
	dev := deviceFromVault(v, useProfile)
	status, text, proto, err := post(dev, "/dashboard/get/v7", "{}", token, false, false)
	if err != nil {
		fmt.Println("ERR dashboard:", err)
		os.Exit(1)
	}
	dash := unwrap(text)
	if dash == nil {
		fmt.Printf("DASHBOARD status=%d unparsable, raw=%.200s\n", status, text)
		os.Exit(1)
	}
	fmt.Printf("DASHBOARD status=%d proto=%s payload_keys=%d\n", status, proto, len(dash))
	if os.Getenv("POC_DEBUG") != "" {
		fmt.Println("TOP_KEYS:", sortedKeys(dash))
		for _, k := range []string{"data", "summary", "prepaidinfo", "packdata", "bonstriDetails"} {
			if m, ok := dash[k].(map[string]any); ok {
				fmt.Printf("  %s keys: %v\n", k, sortedKeys(m))
			}
		}
	}

	bal := parseDashboard(dash)
	out, _ := json.MarshalIndent(bal, "", "  ")
	fmt.Println("PARSED:", string(out))
	fmt.Println("QUOTA_TEXT:", fmtQuota(bal))
}

func (v *Vault) sessionCount() int {
	s, _ := v.Data["sessions"].(map[string]any)
	return len(s)
}

func (v *Vault) onlySessionMSISDN() string {
	s, _ := v.Data["sessions"].(map[string]any)
	var keys []string
	for k := range s {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 1 {
		return keys[0]
	}
	return ""
}

func expLeft(sess map[string]any) string {
	exp, _ := sess["exp"].(float64)
	if exp == 0 {
		return "unknown"
	}
	d := (exp - nowUnix()) / 86400
	if d <= 0 {
		return "expired"
	}
	return fmt.Sprintf("%.1fd", d)
}

func sortedKeys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func nowUnix() float64 { return float64(time.Now().Unix()) }

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func mask(msisdn string) string {
	if len(msisdn) < 8 {
		return "****"
	}
	return msisdn[:4] + "****" + msisdn[len(msisdn)-3:]
}
