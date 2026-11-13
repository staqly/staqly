package app

import (
	"errors"
	"testing"
	"time"
)

// The edges of "when": now itself is too late, a year and a day is too far, and any offset names the same
// instant.
func TestPublishAtBoundaries(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := t.Context()
	now := time.Date(2027, 5, 10, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	for _, c := range []struct {
		name, at string
		ok       bool
	}{
		{"a second ago", now.Add(-time.Second).Format(time.RFC3339), false},
		{"now", now.Format(time.RFC3339), false},
		{"in a millisecond", now.Add(time.Millisecond).Format(time.RFC3339Nano), true},
		{"in 366 days", now.Add(366 * 24 * time.Hour).Format(time.RFC3339), true},
		{"in 366 days and a second", now.Add(366*24*time.Hour + time.Second).Format(time.RFC3339), false},
		{"not a time", "tomorrow", false},
		{"no zone", "2027-05-11T12:00:00", false},
	} {
		got, err := s.SchedulePost(ctx, p, post.ID, c.at)
		var e *Error
		if c.ok && (err != nil || got.Status != "scheduled") {
			t.Errorf("%s (%s): %v, status %q; want scheduled", c.name, c.at, err, got.Status)
		}
		if !c.ok && (!errors.As(err, &e) || e.Status != 422) {
			t.Errorf("%s (%s): %v, want 422", c.name, c.at, err)
		}
	}
	// 14:00 in Warsaw (summer time) is 12:00 UTC the same day: stored and run as that instant.
	got, err := s.SchedulePost(ctx, p, post.ID, "2027-05-11T14:00:00+02:00")
	if err != nil || got.PublishAt == nil || *got.PublishAt != "2027-05-11T12:00:00.000000Z" {
		t.Fatalf("a time with an offset is stored as %v (%v), want 2027-05-11T12:00:00.000000Z", got.PublishAt, err)
	}
	jobs := jobsByKey(t, s, "publish_post:"+post.ID)
	if len(jobs) != 1 || !jobs[0].ScheduledAt.Equal(time.Date(2027, 5, 11, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("the post's one job: %+v, want one due at 12:00 UTC", jobs)
	}
}

// A scheduled post goes out at its time: a second earlier the worker leaves it alone, at the instant itself it
// publishes, once.
func TestScheduledPostGoesOutAtItsTimeNotBefore(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := t.Context()
	f, ch := scriptedChannelOn(t, s, p, "clockpost")
	if _, err := s.UpdatePost(ctx, p, post.ID, PostPatch{Targets: &[]PostTargetInput{{ChannelID: ch}}}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2027, 5, 10, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	due := now.Add(48 * time.Hour)
	if _, err := s.SchedulePost(ctx, p, post.ID, due.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return due.Add(-time.Second) }
	s.RunDueJobsOnce(ctx)
	if got, _ := s.GetPost(ctx, p, post.ID); got.Status != "scheduled" {
		t.Fatalf("a second before its time the post is %s", got.Status)
	}
	if calls, _ := f.counts(); calls != 0 {
		t.Fatalf("a second before its time the platform was asked %d times", calls)
	}
	s.Now = func() time.Time { return due }
	s.RunDueJobsOnce(ctx)
	s.Now = func() time.Time { return due.Add(time.Hour) }
	s.RunDueJobsOnce(ctx) // nothing is left to run it a second time
	got, _ := s.GetPost(ctx, p, post.ID)
	if _, sent := f.counts(); got.Status != "published" || sent != 1 {
		t.Fatalf("at its time the post is %s and went out %d times, want published, once", got.Status, sent)
	}
	if got.PublishedAt == nil || *got.PublishedAt != formatTime(due) {
		t.Fatalf("published_at %v, want its time %s", got.PublishedAt, formatTime(due))
	}
}

// A recurring schedule keeps its wall-clock time across a change of the clocks and runs once per day, also when
// its time falls in the hour that happens twice (autumn) or not at all (spring). The clock is moved from run
// to run through the real queue: a second before a run nothing happens, at the run's instant it runs once.
func TestRecurringScheduleAcrossClockChanges(t *testing.T) {
	for _, c := range []struct {
		name, tz, at, start string
		want                []string // the runs, wall clock in tz
		gaps                []time.Duration
	}{
		{"morning across autumn", "Europe/Warsaw", "09:00", "2027-10-30 00:00",
			[]string{"2027-10-30 09:00", "2027-10-31 09:00", "2027-11-01 09:00"}, []time.Duration{25 * time.Hour, 24 * time.Hour}},
		{"the hour that happens twice", "Europe/Warsaw", "02:30", "2027-10-30 00:00",
			[]string{"2027-10-30 02:30", "2027-10-31 02:30", "2027-11-01 02:30"}, nil}, // which 02:30 is the platform's choice; one of them
		{"morning across spring", "Europe/Warsaw", "09:00", "2027-03-27 00:00",
			[]string{"2027-03-27 09:00", "2027-03-28 09:00", "2027-03-29 09:00"}, []time.Duration{23 * time.Hour, 24 * time.Hour}},
		{"the hour that does not exist", "Europe/Warsaw", "02:30", "2027-03-27 00:00",
			[]string{"2027-03-27 02:30", "2027-03-28 03:30", "2027-03-29 02:30"}, []time.Duration{24 * time.Hour, 23 * time.Hour}},
		{"another zone, another date", "America/New_York", "09:00", "2027-11-06 00:00",
			[]string{"2027-11-06 09:00", "2027-11-07 09:00", "2027-11-08 09:00"}, []time.Duration{25 * time.Hour, 24 * time.Hour}},
	} {
		t.Run(c.name, func(t *testing.T) {
			loc, err := time.LoadLocation(c.tz)
			if err != nil {
				t.Skip("no tzdata:", err)
			}
			s, p, post := publishTest(t)
			ctx := t.Context()
			now := at(c.start, loc)
			s.Now = func() time.Time { return now }
			sc, err := s.CreateSchedule(ctx, p, ScheduleInput{Collection: &post.CollectionID, Name: ptrTo("Daily"), Kind: ptrTo("recurring"),
				Rule: &ScheduleRule{Times: []string{c.at}, TZ: c.tz}})
			if err != nil {
				t.Fatal(err)
			}
			ran := func() int { return len(eventsOfType(t, s, p, "schedule.ran")) }
			var instants []time.Time
			for i, want := range c.want {
				if sc.NextRunAt == nil {
					t.Fatalf("after %d runs the schedule has no next run", i)
				}
				next, err := time.Parse(timeFormat, *sc.NextRunAt)
				if err != nil {
					t.Fatal(err)
				}
				if got := next.In(loc).Format("2006-01-02 15:04"); got != want {
					t.Fatalf("run %d is at %s (%s), want %s", i+1, got, c.tz, want)
				}
				instants = append(instants, next)
				now = next.Add(-time.Second)
				s.RunDueJobsOnce(ctx)
				if n := ran(); n != i {
					t.Fatalf("a second before run %d the schedule ran %d times, want %d", i+1, n, i)
				}
				now = next
				s.RunDueJobsOnce(ctx)
				if n := ran(); n != i+1 {
					t.Fatalf("at run %d (%s) the schedule ran %d times, want %d", i+1, want, n, i+1)
				}
				if sc, err = s.GetSchedule(ctx, p, sc.ID); err != nil {
					t.Fatal(err)
				}
			}
			for i, gap := range c.gaps {
				if got := instants[i+1].Sub(instants[i]); got != gap {
					t.Errorf("between run %d and %d: %s, want %s", i+1, i+2, got, gap)
				}
			}
			for i := 1; i < len(instants); i++ {
				if gap := instants[i].Sub(instants[i-1]); gap < 23*time.Hour || gap > 25*time.Hour {
					t.Errorf("between run %d and %d: %s, want a day give or take the hour the clocks moved", i, i+1, gap)
				}
			}
			if jobs := jobsByKey(t, s, "run_schedule:"+sc.ID); len(jobs) == 0 || jobStatus(jobs[0].State) != "queued" {
				t.Fatalf("after the last run no next run is queued: %+v", jobs)
			}
		})
	}
}

// A run that a worker claimed for the post's old time, while the post was moved to a later one, publishes
// nothing: the post waits for its new time (whose run is queued).
func TestClaimedPublishRunHonorsANewTime(t *testing.T) {
	s, p, post := publishTest(t) // scheduled in an hour
	ctx := t.Context()
	if err := s.runPublishPost(ctx, p.WorkspaceID, post.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetPost(ctx, p, post.ID)
	if got.Status != "scheduled" || len(jobsByKey(t, s, "publish_target:"+post.ID+":"+post.Targets[0].ChannelID)) != 0 {
		t.Fatalf("a run before the post's time left it %s with a publish job; want scheduled and none", got.Status)
	}
	if jobs := jobsByKey(t, s, "publish_post:"+post.ID); len(jobs) != 1 || jobStatus(jobs[0].State) != "queued" {
		t.Fatalf("the run for its time: %+v, want one queued", jobs)
	}
}
