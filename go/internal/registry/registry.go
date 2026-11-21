// Package registry persists the dashboard's number list and in-memory pending
// OTP entries. The file format is identical to server.py's registry.json.
package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Number struct {
	ID       string  `json:"id"`
	Provider string  `json:"provider"`
	MSISDN   string  `json:"msisdn"`
	AddedAt  float64 `json:"added_at"`
}

type Pending struct {
	Provider  string
	MSISDN    string
	TransID   string
	CreatedAt time.Time
}

type Registry struct {
	Dir     string
	Numbers []Number
	Pending map[string]*Pending
	mu      sync.Mutex
}

func Load(dir string) (*Registry, error) {
	r := &Registry{Dir: dir, Pending: map[string]*Pending{}}
	b, err := os.ReadFile(filepath.Join(dir, "registry.json"))
	if os.IsNotExist(err) {
		return r, nil
	}
	if err != nil {
		return nil, err
	}
	var doc struct {
		Numbers []Number `json:"numbers"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	r.Numbers = doc.Numbers
	return r, nil
}

func (r *Registry) Save() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.saveLocked()
}

func (r *Registry) saveLocked() error {
	b, err := json.MarshalIndent(struct {
		Numbers []Number `json:"numbers"`
	}{r.Numbers}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(r.Dir, 0o700); err != nil {
		return err
	}
	tmp := filepath.Join(r.Dir, ".registry.json.tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(r.Dir, "registry.json"))
}

// Normalize matches bima_balance.normalize_msisdn for the supported inputs.
func Normalize(msisdn string) string {
	d := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, msisdn)
	switch {
	case strings.HasPrefix(d, "62"):
		return d
	case strings.HasPrefix(d, "8"):
		return "62" + d
	case strings.HasPrefix(d, "0"):
		return "62" + d[1:]
	}
	return d
}

func (r *Registry) Add(provider, msisdn string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	ms := Normalize(msisdn)
	for _, n := range r.Numbers {
		if n.Provider == provider && n.MSISDN == ms {
			return fmt.Errorf("number already registered")
		}
	}
	r.Numbers = append(r.Numbers, Number{
		ID: provider + "-" + ms, Provider: provider, MSISDN: ms,
		AddedAt: float64(time.Now().Unix()),
	})
	return r.saveLocked()
}

func (r *Registry) Remove(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, n := range r.Numbers {
		if n.ID == id {
			r.Numbers = append(r.Numbers[:i], r.Numbers[i+1:]...)
			return r.saveLocked()
		}
	}
	return fmt.Errorf("number not found")
}

func (r *Registry) Find(id string) (Number, bool) {
	for _, n := range r.Numbers {
		if n.ID == id {
			return n, true
		}
	}
	return Number{}, false
}
