package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func TestLoadFixture(t *testing.T) {
	r, err := Load(fixtureDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Numbers) != 5 {
		t.Fatalf("numbers = %d, want 5", len(r.Numbers))
	}
	if r.Numbers[0].ID != "bima-6280000000000" || r.Numbers[0].Provider != "bima" {
		t.Errorf("first = %#v", r.Numbers[0])
	}
}

func TestAddDuplicateAndRemove(t *testing.T) {
	dir := t.TempDir()
	r, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Add("bima", "081234567890"); err != nil {
		t.Fatal(err)
	}
	if len(r.Numbers) != 1 || r.Numbers[0].MSISDN != "6281234567890" {
		t.Fatalf("after Add: %#v", r.Numbers)
	}
	if err := r.Add("bima", "+6281234567890"); err == nil {
		t.Fatal("duplicate accepted")
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Numbers, r.Numbers) {
		t.Fatalf("round trip mismatch: %#v vs %#v", got.Numbers, r.Numbers)
	}
	if err := r.Remove(r.Numbers[0].ID); err != nil {
		t.Fatal(err)
	}
	if len(r.Numbers) != 0 {
		t.Fatalf("after Remove: %#v", r.Numbers)
	}
}

func TestSaveCreatesAtomicFile(t *testing.T) {
	dir := t.TempDir()
	r, _ := Load(dir)
	if err := r.Add("myxl", "6287777313358"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Fatal("empty registry.json")
	}
	if _, err := os.Stat(filepath.Join(dir, ".registry.json.tmp")); !os.IsNotExist(err) {
		t.Fatal("temporary file left behind")
	}
}

func fixtureDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	b, err := os.ReadFile("testdata/registry.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "registry.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestConcurrentRegistryAccess(t *testing.T) {
	r, _ := Load(t.TempDir())
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("bima-%d", i)
			r.PendingSet(id, &Pending{Provider: "bima"})
			_, _ = r.PendingGet(id)
			_ = r.Snapshot()
			r.PendingDelete(id)
		}(i)
	}
	wg.Wait()
	if len(r.PendingList()) != 0 {
		t.Fatal("pending map not empty")
	}
}
