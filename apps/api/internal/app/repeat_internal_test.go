package app

import (
	"testing"
	"time"
)

func TestRepeatChoicesAndNextOccurrence(t *testing.T) {
	for _, v := range []string{"", "1d", "2d", "3d", "4d", "5d", "6d", "1w", "2w", "1mo"} {
		if got, err := checkRepeat(v); err != nil || got != v {
			t.Errorf("%q should be accepted: %v", v, err)
		}
	}
	for _, v := range []string{"7d", "1y", "weekly", " 1w"} {
		if _, err := checkRepeat(v); err == nil {
			t.Errorf("%q should be refused", v)
		}
	}
	base := time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)
	if got := nextOccurrence(base, "1w", base); !got.Equal(base.AddDate(0, 0, 7)) {
		t.Errorf("1w: %v", got)
	}
	if got := nextOccurrence(base, "1mo", base); !got.Equal(time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("1mo: %v", got)
	}
	// Late (the server was off for 20 days): whole intervals are skipped until the time is ahead.
	if got := nextOccurrence(base, "1w", base.AddDate(0, 0, 20)); !got.Equal(base.AddDate(0, 0, 21)) {
		t.Errorf("catch-up: %v", got)
	}
	if got := nextOccurrence(base, "2d", base.AddDate(0, 0, 2)); !got.Equal(base.AddDate(0, 0, 4)) {
		t.Errorf("exactly now is not ahead: %v", got)
	}
}
