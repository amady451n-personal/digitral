// Command digitrald is the Go replacement for server.py: JSON API, static PWA,
// SSE stream, and the daily grace notification loop.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
	_ "time/tzdata" // embedded zoneinfo: the scratch image has no /usr/share/zoneinfo

	"digitral/backend/internal/httpapi"
	"digitral/backend/internal/notify"
	"digitral/backend/internal/push"
	"digitral/backend/internal/registry"
	"digitral/backend/internal/vault"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get("http://127.0.0.1:" + env("PORT", "8765") + "/health")
		if err != nil {
			os.Exit(1)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			os.Exit(1)
		}
		return
	}

	home := env("FRONTEND_HOME", filepath.Join(os.Getenv("HOME"), ".bima-frontend"))
	frontend := env("FRONTEND_DIR", "frontend")
	reg, err := registry.Load(home)
	if err != nil {
		log.Fatal(err)
	}
	factory := httpapi.Factory{Home: home}
	pollSec, _ := strconv.ParseFloat(env("POLL_SEC", "60"), 64)

	api := &httpapi.API{
		Reg: reg, Factory: factory, Home: home, Frontend: frontend,
		PollSec: time.Duration(pollSec * float64(time.Second)),
		SessionCheck: func(providerID, msisdn string) bool {
			v, err := openVault(home, providerID)
			if err != nil {
				return false
			}
			return v.Session(msisdn) != nil
		},
		PushPublic: func() (string, error) {
			_, pub, err := push.Ensure(home)
			return pub, err
		},
		PushAdd: func(sub map[string]any) (bool, int, error) {
			return push.AddSub(home, sub)
		},
	}

	go dailyLoop(context.Background(), reg, factory, home)
	addr := "0.0.0.0:" + env("PORT", "8765")
	log.Printf("digitrald listening on %s (home=%s)", addr, home)
	server := &http.Server{Addr: addr, Handler: api.Handler(), ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(server.ListenAndServe())
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// dailyLoop waits until 08:00 local time, then checks every registered number
// once per day (same schedule as push_notify.daily_loop).
func dailyLoop(ctx context.Context, reg *registry.Registry, factory httpapi.ClientFactory, home string) {
	for {
		now := time.Now()
		next := time.Date(now.Year(), now.Month(), now.Day(), 8, 0, 0, 0, now.Location())
		if !next.After(now) {
			next = next.Add(24 * time.Hour)
		}
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			checkAndNotify(ctx, reg, factory, home)
		}
	}
}

func checkAndNotify(ctx context.Context, reg *registry.Registry, factory httpapi.ClientFactory, home string) {
	today := time.Now()
	for _, n := range reg.Numbers {
		client, err := factory.Client(n.Provider, n.MSISDN)
		if err != nil {
			continue
		}
		bal, err := client.Balance(ctx, n.MSISDN)
		if err != nil {
			continue
		}
		active := notify.ParseActive(fmt.Sprint(bal.ActiveUntil))
		if active == nil {
			continue
		}
		days := int(active.Sub(today).Hours() / 24)
		grace := notify.ParseActive(fmt.Sprint(bal.GraceUntil))
		var msg notify.Notification
		switch {
		case days == 0:
			msg = notify.Message(providerLabel(n.Provider), n.MSISDN, 0, grace, n.ID)
		case days < 0 && grace != nil && notify.IsGraceUrgent(*active, grace, today):
			msg = notify.Message(providerLabel(n.Provider), n.MSISDN, days, grace, n.ID)
		default:
			continue
		}
		_, _, _ = push.Send(home, env("VAPID_MAILTO", "mailto:admin@example.com"), map[string]any{"title": msg.Title, "body": msg.Body, "tag": msg.Tag})
	}
}

func providerLabel(id string) string {
	switch id {
	case "bima":
		return "bima+"
	case "myim3":
		return "myIM3"
	case "axisnet":
		return "AXISNET"
	case "myxl":
		return "myXL"
	}
	return id
}

func openVault(home, providerID string) (*vault.Vault, error) {
	return vault.Open(filepath.Join(home, providerID))
}
