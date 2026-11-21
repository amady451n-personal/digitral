// Package vault reads and writes the AES-GCM JSON store used by the Python
// scripts. The on-disk format is unchanged, so Go and Python share one vault.
//
// Layout (mirrors Vault in scripts/bima_balance.py):
//
//	vault.key : 32 raw bytes, mode 0600
//	vault.bin : iv(12) || ciphertext || tag(16)
package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Vault struct {
	Dir  string
	Key  []byte
	Data map[string]any
}

func Open(dir string) (*Vault, error) {
	key, err := os.ReadFile(filepath.Join(dir, "vault.key"))
	if err != nil {
		return nil, fmt.Errorf("vault.key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("vault.key: want 32 bytes, got %d", len(key))
	}
	data := map[string]any{}
	blob, err := os.ReadFile(filepath.Join(dir, "vault.bin"))
	switch {
	case errors.Is(err, os.ErrNotExist):
		// Fresh vault: no payload yet.
	case err != nil:
		return nil, fmt.Errorf("vault.bin: %w", err)
	default:
		if len(blob) < 28 {
			return nil, fmt.Errorf("vault.bin: %d bytes, too short for iv+tag", len(blob))
		}
		plain, err := gcmOpen(key, blob)
		if err != nil {
			return nil, fmt.Errorf("vault.bin decrypt: %w", err)
		}
		if err := json.Unmarshal(plain, &data); err != nil {
			return nil, fmt.Errorf("vault.bin json: %w", err)
		}
	}
	return &Vault{Dir: dir, Key: key, Data: data}, nil
}

func gcmOpen(key, blob []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ns := aead.NonceSize()
	if len(blob) < ns+aead.Overhead() {
		return nil, errors.New("blob shorter than nonce+tag")
	}
	return aead.Open(nil, blob[:ns], blob[ns:], nil)
}

// Save encrypts Data with a fresh IV and replaces vault.bin atomically (0600).
func (v *Vault) Save() error {
	plain, err := json.Marshal(v.Data)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(v.Key)
	if err != nil {
		return err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	iv := make([]byte, aead.NonceSize())
	if _, err := rand.Read(iv); err != nil {
		return err
	}
	blob := aead.Seal(iv, iv, plain, nil)

	tmp := filepath.Join(v.Dir, ".vault.bin.tmp")
	if err := os.WriteFile(tmp, blob, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(v.Dir, "vault.bin"))
}

func (v *Vault) Str(key string) string {
	s, _ := v.Data[key].(string)
	return s
}

func (v *Vault) DeviceProfile() map[string]any {
	p, _ := v.Data["deviceProfile"].(map[string]any)
	return p
}

func (v *Vault) profileStr(key, fallback string) string {
	if s, ok := v.DeviceProfile()[key].(string); ok && s != "" {
		return s
	}
	return fallback
}

func (v *Vault) DeviceID() string   { return v.Str("deviceId") }
func (v *Vault) DeviceName() string { return v.profileStr("deviceName", "generic") }
func (v *Vault) OEM() string        { return v.profileStr("oem", "samsung") }
func (v *Vault) Model() string      { return v.profileStr("model", "SM-A235F") }
func (v *Vault) OSVer() string      { return v.profileStr("osver", "14") }
func (v *Vault) Faaid() string      { return v.Str("faaid") }
func (v *Vault) Gaid() string       { return v.Str("gaid") }

// RawSession returns the stored session for msisdn regardless of expiry.
// myXL needs this: an expired `exp` (refresh_token lifetime) still allows a
// refresh_token grant, so the caller must see the entry to decide.
func (v *Vault) RawSession(msisdn string) map[string]any {
	sessions, _ := v.Data["sessions"].(map[string]any)
	s, _ := sessions[msisdn].(map[string]any)
	return s
}

// Session returns the stored session for msisdn, or nil when absent or expired.
// Mirrors Python Vault.session(): `exp` is seconds, checked 60s early, and a
// missing/zero `exp` means "no expiry recorded" (axisnet stores no exp at all).
func (v *Vault) Session(msisdn string) map[string]any {
	s := v.RawSession(msisdn)
	if s == nil {
		return nil
	}
	exp := num(s["exp"])
	if exp != 0 && float64(time.Now().Unix()) >= exp-60 {
		return nil
	}
	return s
}

// SessionMSISDNs lists the keys of the sessions map, sorted.
func (v *Vault) SessionMSISDNs() []string {
	sessions, _ := v.Data["sessions"].(map[string]any)
	keys := make([]string, 0, len(sessions))
	for k := range sessions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// num reads a JSON number that may arrive as float64 (json.Unmarshal) or, for
// vaults written by other tooling, as an int or string.
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
