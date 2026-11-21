// Package push stores Web Push subscriptions and VAPID keys in the same files
// the Python backend used (vapid.json, subscriptions.json under FRONTEND_HOME).
package push

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
)

type vapidFile struct {
	PrivateKey string `json:"private_key"`
	PublicKey  string `json:"public_key"`
}

func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// Generate creates a fresh P-256 VAPID pair. It never reads env or files.
func Generate() (string, string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	priv := make([]byte, 32)
	key.D.FillBytes(priv)
	pub := elliptic.Marshal(elliptic.P256(), key.X, key.Y)
	return b64url(priv), b64url(pub), nil
}

// Ensure returns env keys when set, otherwise the file keys, otherwise a new
// pair saved to vapid.json (0600).
func Ensure(dir string) (string, string, error) {
	priv, pub := os.Getenv("VAPID_PRIVATE_KEY"), os.Getenv("VAPID_PUBLIC_KEY")
	if priv != "" && pub != "" {
		return priv, pub, nil
	}
	path := filepath.Join(dir, "vapid.json")
	if b, err := os.ReadFile(path); err == nil {
		var v vapidFile
		if err := json.Unmarshal(b, &v); err == nil && v.PrivateKey != "" && v.PublicKey != "" {
			return v.PrivateKey, v.PublicKey, nil
		}
	} else if !os.IsNotExist(err) {
		return "", "", err
	}
	priv, pub, err := Generate()
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", err
	}
	b, _ := json.Marshal(vapidFile{priv, pub})
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return "", "", err
	}
	return priv, pub, nil
}

// PrivateKey converts a base64url scalar back to an ECDSA P-256 key (for webpush).
func PrivateKey(privB64 string) (*ecdsa.PrivateKey, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(privB64, "="))
	if err != nil || len(raw) != 32 {
		return nil, fmt.Errorf("bad VAPID private key: %v", err)
	}
	curve := elliptic.P256()
	d := new(big.Int).SetBytes(raw)
	x, y := curve.ScalarBaseMult(raw)
	return &ecdsa.PrivateKey{PublicKey: ecdsa.PublicKey{Curve: curve, X: x, Y: y}, D: d}, nil
}

func LoadSubs(dir string) ([]map[string]any, error) {
	b, err := os.ReadFile(filepath.Join(dir, "subscriptions.json"))
	if os.IsNotExist(err) {
		return []map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	var subs []map[string]any
	if err := json.Unmarshal(b, &subs); err != nil {
		return []map[string]any{}, nil // corrupted file: start fresh, like Python
	}
	return subs, nil
}

func AddSub(dir string, sub map[string]any) (bool, int, error) {
	subs, err := LoadSubs(dir)
	if err != nil {
		return false, 0, err
	}
	endpoint := fmt.Sprint(sub["endpoint"])
	isNew := true
	kept := make([]map[string]any, 0, len(subs)+1)
	for _, s := range subs {
		if fmt.Sprint(s["endpoint"]) == endpoint {
			isNew = false
			continue
		}
		kept = append(kept, s)
	}
	kept = append(kept, sub)
	b, err := json.MarshalIndent(kept, "", "  ")
	if err != nil {
		return false, 0, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, 0, err
	}
	if err := os.WriteFile(filepath.Join(dir, "subscriptions.json"), b, 0o600); err != nil {
		return false, 0, err
	}
	return isNew, len(kept), nil
}
