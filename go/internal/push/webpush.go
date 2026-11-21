package push

import (
	"encoding/json"
	"fmt"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// Send sends one payload to every stored subscription. It uses the same VAPID
// keys the frontend subscribed with, so the public key must not be regenerated.
func Send(dir, subject string, payload map[string]any) (sent, failed int, err error) {
	priv, pub, err := Ensure(dir)
	if err != nil {
		return 0, 0, err
	}
	subs, err := LoadSubs(dir)
	if err != nil {
		return 0, 0, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, 0, err
	}
	for _, s := range subs {
		keys, _ := s["keys"].(map[string]any)
		sub := webpush.Subscription{
			Endpoint: fmt.Sprint(s["endpoint"]),
			Keys: webpush.Keys{
				P256dh: fmt.Sprint(keys["p256dh"]),
				Auth:   fmt.Sprint(keys["auth"]),
			},
		}
		resp, err := webpush.SendNotification(body, &sub, &webpush.Options{
			Subscriber: subject, VAPIDPublicKey: pub, VAPIDPrivateKey: priv, TTL: 60,
		})
		if err != nil {
			failed++
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			failed++
		} else {
			sent++
		}
		resp.Body.Close()
	}
	return sent, failed, nil
}
