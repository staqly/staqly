package app

import (
	"context"
	"errors"
	"testing"
	"time"
)

// The send limits count per address and asker: someone asking for another person's codes uses up only their
// own allowance, never the person's; all askers together stay under the per-address cap that bounds guessing.
func TestEmailCodeLimitsPerAsker(t *testing.T) {
	s, _, _ := publishTest(t)
	s.Cfg.Mode, s.Cfg.EmailSignIn, s.Cfg.DevSignIn = ModeCloud, true, true
	s.Cfg.SecretKey = []byte("k")
	now := time.Now()
	s.Now = func() time.Time { return now }
	send := func(ip string) error {
		now = now.Add(31 * time.Second) // past the cooldown
		_, err := s.StartEmailSignIn(WithClientIP(context.Background(), ip), "victim@example.com")
		return err
	}
	limited := func(err error) bool {
		var e *Error
		return errors.As(err, &e) && e.Code == "rate_limited"
	}
	// An attacker uses up a day's codes for the address from one place.
	for i := 0; i < emailCodesPerDay; i++ {
		if i > 0 && i%emailCodesPerHour == 0 {
			now = now.Add(time.Hour)
		}
		if err := send("203.0.113.9"); err != nil {
			t.Fatalf("code %d: %v", i+1, err)
		}
	}
	if err := send("203.0.113.9"); !limited(err) {
		t.Fatalf("the asker's day is over: %v", err)
	}
	// The person, asking from elsewhere, still gets a code.
	if err := send("198.51.100.7"); err != nil {
		t.Fatalf("the person is locked out: %v", err)
	}
	// All askers together: at most emailCodesPerAddressDay a day.
	sent := emailCodesPerDay + 1
	for i := 0; sent < emailCodesPerAddressDay; i++ {
		ip := "192.0.2." + itoaTest(i)
		for j := 0; j < emailCodesPerHour && sent < emailCodesPerAddressDay; j++ {
			if err := send(ip); err != nil {
				t.Fatalf("code %d: %v", sent+1, err)
			}
			sent++
		}
	}
	if err := send("192.0.2.250"); !limited(err) {
		t.Fatalf("over the per-address cap: %v", err)
	}
}

func itoaTest(i int) string { return string(rune('a'+i/26)) + string(rune('a'+i%26)) }
