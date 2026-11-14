package app

import (
	"slices"
	"testing"
	"time"
)

func at(s string, loc *time.Location) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
	if err != nil {
		panic(err)
	}
	return t
}

func TestNextRunTimesAndWeekdays(t *testing.T) {
	warsaw, _ := time.LoadLocation("Europe/Warsaw")
	r := ScheduleRule{Times: []string{"09:00", "17:30"}, Weekdays: []int{1, 2, 3, 4, 5}, TZ: "Europe/Warsaw"}
	if err := validateRule(&r); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ after, want string }{
		{"2026-10-19 08:59", "2026-10-19 09:00"}, // Monday morning
		{"2026-10-19 09:00", "2026-10-19 17:30"}, // strictly after
		{"2026-10-19 17:30", "2026-10-20 09:00"},
		{"2026-10-23 18:00", "2026-10-26 09:00"}, // Friday evening skips the weekend
		{"2026-10-24 12:00", "2026-10-26 09:00"}, // Saturday
	} {
		got, ok := nextRun(r, at(c.after, warsaw))
		if !ok || !got.Equal(at(c.want, warsaw)) {
			t.Errorf("after %s: got %s, want %s", c.after, got.In(warsaw), c.want)
		}
	}
	// Local wall-clock time holds across the DST change (Warsaw leaves summer time on 2026-10-25).
	got, _ := nextRun(ScheduleRule{Times: []string{"09:00"}, TZ: "Europe/Warsaw"}, at("2026-10-24 10:00", warsaw))
	if got.In(warsaw).Hour() != 9 || got.In(warsaw).Day() != 25 || got.UTC().Hour() != 8 {
		t.Errorf("DST: %s (%s)", got.In(warsaw), got.UTC())
	}
	got, _ = nextRun(ScheduleRule{Times: []string{"09:00"}, TZ: "Europe/Warsaw"}, at("2026-10-25 10:00", warsaw))
	if got.In(warsaw).Hour() != 9 || got.In(warsaw).Day() != 26 || got.UTC().Hour() != 8 {
		t.Errorf("after DST: %s (%s)", got.In(warsaw), got.UTC())
	}
	// 02:30 does not exist on the spring change day (2026-03-29): the run still lands that day, once.
	got, ok := nextRun(ScheduleRule{Times: []string{"02:30"}, TZ: "Europe/Warsaw"}, at("2026-03-29 01:00", warsaw))
	if !ok || got.In(warsaw).Day() != 29 {
		t.Errorf("spring forward: %s", got.In(warsaw))
	}
	// Every N minutes counts real minutes, in any zone.
	every := ScheduleRule{EveryMinutes: 90}
	if err := validateRule(&every); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC)
	if got, _ := nextRun(every, base); !got.Equal(base.Add(90 * time.Minute)) {
		t.Errorf("every 90 min: %s", got)
	}
	// UTC when no zone is given; a rule without times runs never.
	if got, _ := nextRun(ScheduleRule{Times: []string{"23:00"}}, time.Date(2026, 1, 1, 23, 30, 0, 0, time.UTC)); !got.Equal(time.Date(2026, 1, 2, 23, 0, 0, 0, time.UTC)) {
		t.Errorf("utc: %s", got)
	}
	if _, ok := nextRun(ScheduleRule{}, time.Now()); ok {
		t.Error("empty rule has a next run")
	}
}

// Europe/Warsaw, 2026-03-29: clocks jump from 02:00 to 03:00, so 02:30 does not exist (Go reads it as 03:30).
// With times 02:30 and 03:00 the schedule still runs at 03:00 that day, then at 03:30.
func TestNextRunSpringForwardKeepsTheLaterSlot(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		t.Fatal(err)
	}
	rule := ScheduleRule{Times: []string{"02:30", "03:00"}, TZ: "Europe/Warsaw"}
	after := time.Date(2026, 3, 29, 0, 0, 0, 0, loc)
	var runs []string
	for range 3 {
		next, ok := nextRun(rule, after)
		if !ok || !next.After(after) {
			t.Fatalf("next run %s after %s (ok %v)", next.In(loc), after.In(loc), ok)
		}
		runs = append(runs, next.In(loc).Format("01-02 15:04"))
		after = next
	}
	if want := []string{"03-29 03:00", "03-29 03:30", "03-30 02:30"}; !slices.Equal(runs, want) {
		t.Fatalf("runs %v, want %v", runs, want)
	}
}

func TestValidateRule(t *testing.T) {
	for _, bad := range []ScheduleRule{
		{}, {Times: []string{"9:00"}}, {Times: []string{"24:00"}}, {Times: []string{"09:00"}, Weekdays: []int{7}},
		{Times: []string{"09:00"}, TZ: "Mars/Olympus"}, {EveryMinutes: 4}, {EveryMinutes: 10081}, {EveryMinutes: 60, Times: []string{"09:00"}},
	} {
		r := bad
		if err := validateRule(&r); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
	r := ScheduleRule{Times: []string{"17:30", "09:00", "09:00"}, Weekdays: []int{5, 1, 1}}
	if err := validateRule(&r); err != nil || len(r.Times) != 2 || r.Times[0] != "09:00" || len(r.Weekdays) != 2 || r.Weekdays[0] != 1 || r.TZ != "UTC" {
		t.Errorf("normalized: %+v %v", r, err)
	}
}
