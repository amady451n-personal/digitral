package notify

import (
	"testing"
	"time"
)

func d(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestGraceUrgentSeparateGrace(t *testing.T) {
	active := d("2026-11-01")
	today := d("2026-12-12") // 20 days before grace
	grace := d("2027-01-01")
	if !IsGraceUrgent(active, &grace, today) {
		t.Fatal("separate grace, 20 days before: should be urgent")
	}
	if IsGraceUrgent(active, &grace, d("2026-12-11")) {
		t.Fatal("separate grace, 21 days before: should not be urgent")
	}
}

func TestGraceUrgentLegacy(t *testing.T) {
	active := d("2026-10-01")
	today21 := d("2026-10-22")
	same := active
	if !IsGraceUrgent(active, &same, today21) {
		t.Fatal("grace == active, 21 days past: should be urgent")
	}
	if IsGraceUrgent(active, nil, d("2026-10-21")) {
		t.Fatal("no grace, 20 days past: should not be urgent")
	}
	if !IsGraceUrgent(active, nil, today21) {
		t.Fatal("no grace, 21 days past: should be urgent")
	}
}

func TestParseActiveFormats(t *testing.T) {
	for in, want := range map[string]string{
		"16 Nov 2027":        "2027-11-16",
		"Exp on 29 Dec 2026": "2026-12-29",
		"29-12-2026":         "2026-12-29",
	} {
		got := ParseActive(in)
		if got == nil || got.Format("2006-01-02") != want {
			t.Errorf("ParseActive(%q) = %v, want %s", in, got, want)
		}
	}
	if ParseActive("") != nil {
		t.Fatal("empty date must return nil")
	}
}

func TestNotificationMessage(t *testing.T) {
	got := Message("bima+", "628123456789", 0, nil, "bima-1")
	if got.Title != "Digitral" || got.Tag != "end-bima-1" || got.Body == "" {
		t.Errorf("message = %#v", got)
	}
	grace := d("2027-01-01")
	got = Message("bima+", "628123456789", -1, &grace, "bima-1")
	if got.Tag != "grace-bima-1" || !contains(got.Body, "harus segera diisi") {
		t.Errorf("message = %#v", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
