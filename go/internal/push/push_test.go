package push

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestVAPIDKeyReuse(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VAPID_PRIVATE_KEY", "")
	t.Setenv("VAPID_PUBLIC_KEY", "")
	priv1, pub1, err := Ensure(dir)
	if err != nil {
		t.Fatal(err)
	}
	priv2, pub2, err := Ensure(dir)
	if err != nil {
		t.Fatal(err)
	}
	if priv1 != priv2 || pub1 != pub2 {
		t.Fatal("keys changed between calls")
	}
	raw, err := base64.RawURLEncoding.DecodeString(pub1)
	if err != nil || len(raw) != 65 || raw[0] != 0x04 {
		t.Errorf("public key = %d bytes, first=%x, err=%v", len(raw), raw[0], err)
	}
	info, err := os.Stat(filepath.Join(dir, "vapid.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("vapid.json mode = %v", info.Mode().Perm())
	}
}

func TestVAPIDEnvOverride(t *testing.T) {
	t.Setenv("VAPID_PRIVATE_KEY", "priv")
	t.Setenv("VAPID_PUBLIC_KEY", "pub")
	priv, pub, err := Ensure(t.TempDir())
	if err != nil || priv != "priv" || pub != "pub" {
		t.Fatalf("env override: priv=%q pub=%q err=%v", priv, pub, err)
	}
}

func TestSubscriptionStore(t *testing.T) {
	dir := t.TempDir()
	sub := map[string]any{"endpoint": "https://example.test/push/1", "keys": map[string]string{"p256dh": "a", "auth": "b"}}
	n, total, err := AddSub(dir, sub)
	if err != nil || !n || total != 1 {
		t.Fatalf("first add: new=%v total=%d err=%v", n, total, err)
	}
	n, total, err = AddSub(dir, sub)
	if err != nil || n || total != 1 {
		t.Fatalf("duplicate add: new=%v total=%d err=%v", n, total, err)
	}
	subs, err := LoadSubs(dir)
	if err != nil || len(subs) != 1 {
		t.Fatalf("subs = %#v err=%v", subs, err)
	}
}
