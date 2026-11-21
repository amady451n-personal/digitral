package main

// runVaultMode reads an existing Python-written vault, reuses its saved session,
// and prints the parsed balance. Read-only: no OTP, no vault write.

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"digitral/backend/internal/provider"
	"digitral/backend/internal/vault"
)

func runVaultMode(dir string) {
	v, err := vault.Open(dir)
	if err != nil {
		fmt.Println("VAULT ERR:", err)
		os.Exit(1)
	}
	fmt.Printf("VAULT %s: deviceId=%s… profile=%v sessions=%d\n",
		dir, trunc(v.DeviceID(), 6), v.DeviceProfile() != nil, len(v.SessionMSISDNs()))

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
		msisdn = onlySession(v)
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

	dev := deviceFromVault(v, os.Getenv("POC_DEVICE_PROFILE") != "")
	status, text, proto, err := post(dev, "/dashboard/get/v7", "{}", token, false, false)
	if err != nil {
		fmt.Println("ERR dashboard:", err)
		os.Exit(1)
	}
	bal, err := provider.ParseDashboard("bima", []byte(text))
	if err != nil {
		fmt.Printf("DASHBOARD status=%d proto=%s unparsable: %v raw=%.200s\n", status, proto, err, text)
		os.Exit(1)
	}
	fmt.Printf("DASHBOARD status=%d proto=%s\n", status, proto)
	out, _ := json.MarshalIndent(bal, "", "  ")
	fmt.Println("PARSED:", string(out))
	fmt.Println("QUOTA_TEXT:", provider.FmtQuota(bal))
}

func onlySession(v *vault.Vault) string {
	keys := v.SessionMSISDNs()
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
	d := (exp - float64(time.Now().Unix())) / 86400
	if d <= 0 {
		return "expired"
	}
	return fmt.Sprintf("%.1fd", d)
}

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

func sortedKeys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
