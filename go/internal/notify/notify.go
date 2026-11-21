// Package notify reproduces the grace/expiry notification rules from
// scripts/push_notify.py (GRACE_WARN_DAYS = 20).
package notify

import (
	"fmt"
	"strings"
	"time"
)

const GraceWarnDays = 20

// ParseActive accepts the date shapes the providers emit.
func ParseActive(value string) *time.Time {
	s := strings.TrimSpace(strings.ReplaceAll(value, "Exp on ", ""))
	if s == "" {
		return nil
	}
	for _, layout := range []string{"02 Jan 2006", "02-01-2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return &t
		}
	}
	return nil
}

// IsGraceUrgent ports _grace_urgent: a separate grace date turns urgent 20 days
// before grace; otherwise the legacy rule (>20 days past active) applies.
func IsGraceUrgent(active time.Time, grace *time.Time, today time.Time) bool {
	if grace != nil && grace.Format("2006-01-02") != active.Format("2006-01-02") {
		return int(grace.Sub(today).Hours()/24) <= GraceWarnDays
	}
	return int(today.Sub(active).Hours()/24) > GraceWarnDays
}

type Notification struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Tag   string `json:"tag"`
}

// Mask hides the middle of a msisdn in notification text.
func Mask(msisdn string) string {
	if len(msisdn) < 8 {
		return "****"
	}
	return msisdn[:4] + "****" + msisdn[len(msisdn)-3:]
}

// Message builds the D-day or grace notification body and its dedupe tag.
func Message(label, msisdn string, daysUntilActive int, grace *time.Time, id string) Notification {
	if daysUntilActive == 0 {
		return Notification{"Digitral", fmt.Sprintf("Hari ini, masa aktif %s %s berakhir", label, Mask(msisdn)), "end-" + id}
	}
	note := ""
	if grace != nil {
		note = fmt.Sprintf(" s/d %s", grace.Format("02 Jan 2006"))
	}
	body := fmt.Sprintf("%s %s: masa aktif lewat %d hari (tenggang%s) — harus segera diisi",
		label, Mask(msisdn), -daysUntilActive, note)
	return Notification{"Digitral", body, "grace-" + id}
}
