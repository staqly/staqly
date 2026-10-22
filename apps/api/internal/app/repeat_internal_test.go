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
	// The end of a month stays in the next month (Jan 31 + 1 month is Feb 28, not Mar 3: no month is skipped).
	jan31 := time.Date(2026, 1, 31, 9, 0, 0, 0, time.UTC)
	if got := nextOccurrence(jan31, "1mo", jan31); !got.Equal(time.Date(2026, 2, 28, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("1mo from Jan 31: %v", got)
	}
	if got := nextOccurrence(time.Date(2028, 1, 31, 9, 0, 0, 0, time.UTC), "1mo", jan31); !got.Equal(time.Date(2028, 2, 29, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("1mo from Jan 31 in a leap year: %v", got)
	}
	// Late (the server was off for 20 days): whole intervals are skipped until the time is ahead.
	if got := nextOccurrence(base, "1w", base.AddDate(0, 0, 20)); !got.Equal(base.AddDate(0, 0, 21)) {
		t.Errorf("catch-up: %v", got)
	}
	if got := nextOccurrence(base, "2d", base.AddDate(0, 0, 2)); !got.Equal(base.AddDate(0, 0, 4)) {
		t.Errorf("exactly now is not ahead: %v", got)
	}
}

// Repeats keep the local time in the workspace's zone across DST: Warsaw moves to summer time on
// 2026-03-29, so a weekly 09:00 post is 08:00 UTC before and 07:00 UTC after.
func TestNextOccurrenceAcrossDST(t *testing.T) {
	warsaw, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		t.Skip("no tzdata:", err)
	}
	base := time.Date(2026, 3, 23, 9, 0, 0, 0, warsaw)
	got := nextOccurrence(base, "1w", base)
	if want := time.Date(2026, 3, 30, 7, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("1w across DST: %v, want %v", got.UTC(), want)
	}
	if got.In(warsaw).Hour() != 9 {
		t.Errorf("local hour %d, want 9", got.In(warsaw).Hour())
	}
	if got := nextOccurrence(base, "1d", base.AddDate(0, 0, 6)); !got.Equal(time.Date(2026, 3, 30, 7, 0, 0, 0, time.UTC)) {
		t.Errorf("1d catch-up across DST: %v", got.UTC())
	}
	// End of October: back to winter time, 09:00 is 08:00 UTC again; months clamp as before.
	oct31 := time.Date(2026, 10, 31, 9, 0, 0, 0, warsaw)
	if got := nextOccurrence(time.Date(2026, 9, 30, 9, 0, 0, 0, warsaw), "1mo", oct31.AddDate(0, 0, -2)); !got.Equal(time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC)) {
		t.Errorf("1mo across DST: %v", got.UTC())
	}
}

// A monthly repeat keeps the day it started on: Jan 31 goes Feb 28 and then Mar 31 (the copy from Feb 28 knows
// the chain began on the 31st), also when it catches up over several months.
func TestMonthlyRepeatKeepsItsDay(t *testing.T) {
	jan31 := time.Date(2026, 1, 31, 9, 0, 0, 0, time.UTC)
	feb28 := time.Date(2026, 2, 28, 9, 0, 0, 0, time.UTC)
	if got := nextOccurrenceOn(feb28, "1mo", feb28, 31); !got.Equal(time.Date(2026, 3, 31, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("the copy of Feb 28 from a Jan 31 chain: %v, want Mar 31", got)
	}
	if got := nextOccurrence(jan31, "1mo", time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)); !got.Equal(time.Date(2026, 3, 31, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("catching up from Jan 31: %v, want Mar 31", got)
	}
}
