package app

import (
	"context"
	"errors"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// Repeat a post: once a post with repeat_every is published, the repeat_post job schedules a copy one
// interval after it (the copy repeats too). Clearing repeat_every on the next copy, or deleting it, stops
// the chain.

var repeatIntervals = map[string]func(time.Time) time.Time{
	"1d":  func(t time.Time) time.Time { return t.AddDate(0, 0, 1) },
	"2d":  func(t time.Time) time.Time { return t.AddDate(0, 0, 2) },
	"3d":  func(t time.Time) time.Time { return t.AddDate(0, 0, 3) },
	"4d":  func(t time.Time) time.Time { return t.AddDate(0, 0, 4) },
	"5d":  func(t time.Time) time.Time { return t.AddDate(0, 0, 5) },
	"6d":  func(t time.Time) time.Time { return t.AddDate(0, 0, 6) },
	"1w":  func(t time.Time) time.Time { return t.AddDate(0, 0, 7) },
	"2w":  func(t time.Time) time.Time { return t.AddDate(0, 0, 14) },
	"1mo": addMonth,
}

// addMonth is the same day next month, or that month's last day when it is shorter (Jan 31 -> Feb 28):
// AddDate would normalize to Mar 3 and skip February.
func addMonth(t time.Time) time.Time { return addMonthOn(t, t.Day()) }

// addMonthOn is day of the next month, or its last day when the month is shorter.
func addMonthOn(t time.Time, day int) time.Time {
	y, m, _ := t.Date()
	last := time.Date(y, m+2, 0, 0, 0, 0, 0, t.Location()).Day() // the last day of the next month
	return time.Date(y, m+1, min(day, last), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
}

const repeatChoices = "one of 1d, 2d, 3d, 4d, 5d, 6d (every 1 to 6 days), 1w, 2w (every 1 or 2 weeks), 1mo (every month), or empty for no repeat"

func checkRepeat(v string) (string, error) {
	if v == "" {
		return "", nil
	}
	if _, ok := repeatIntervals[v]; !ok {
		return "", invalid("repeat_every", "must be "+repeatChoices)
	}
	return v, nil
}

// nextOccurrence is base plus the interval, moved on by whole intervals until it is after now. Days and
// months are added on base's wall clock (its location), so the local time of day survives DST.
func nextOccurrence(base time.Time, every string, now time.Time) time.Time {
	return nextOccurrenceOn(base, every, now, base.Day())
}

// nextOccurrenceOn is nextOccurrence for a monthly repeat that started on day (of month): every month lands on
// that day or the month's last, so Jan 31 goes Feb 28, Mar 31, not on the 28th from then on.
func nextOccurrenceOn(base time.Time, every string, now time.Time, day int) time.Time {
	step := repeatIntervals[every]
	if day <= 0 {
		day = base.Day()
	}
	if every == "1mo" {
		step = func(t time.Time) time.Time { return addMonthOn(t, day) }
	}
	next := step(base)
	for !next.After(now) {
		next = step(next)
	}
	return next
}

// runRepeatPost is the repeat_post job: the next occurrence of a published post, made once.
func (s *Service) runRepeatPost(ctx context.Context, workspaceID, postID string) error {
	row, err := s.Q.GetPostByID(ctx, postID)
	if isNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if row.Status != "published" || row.RepeatEvery == "" {
		return nil
	}
	if _, err := s.Q.RepeatOfPost(ctx, postID); err == nil {
		return nil // already repeated (the post settled twice)
	} else if !isNoRows(err) {
		return err
	}
	base := s.Now()
	for _, v := range []string{row.PublishAt.String, row.PublishedAt.String} {
		if t, err := time.Parse(timeFormat, v); err == nil {
			base = t
			break
		}
	}
	// In the workspace's zone: a weekly 09:00 post stays 09:00 there across a DST change.
	loc := s.workspaceLocation(ctx, s.Q, workspaceID)
	base = base.In(loc)
	next := nextOccurrenceOn(base, row.RepeatEvery, s.Now(), s.repeatDay(ctx, row, loc))
	src, _, err := s.loadPost(ctx, s.Q, workspaceID, postID)
	if err != nil {
		return err
	}
	at := next.UTC().Format(time.RFC3339Nano)
	in := PostInput{Collection: row.CollectionID, Content: src.Content, Media: src.Media, Tags: src.Tags, Metadata: src.Metadata,
		ExternalRef: src.ExternalRef, Status: "scheduled", PublishAt: &at, Source: "repeat", RepeatEvery: row.RepeatEvery, Thread: parseThread(row.Thread), repeatOf: postID}
	for _, t := range src.Targets {
		in.Targets = append(in.Targets, PostTargetInput{ChannelID: t.ChannelID, Content: t.Content, Settings: t.Settings})
	}
	sys := Principal{WorkspaceID: workspaceID, Kind: PrincipalSystem, ID: "repeater", Name: "Repeater", Scopes: []string{ScopeAdmin}}
	_, err = s.CreatePost(ctx, sys, in)
	var e *Error
	if errors.As(err, &e) && e.Status >= 400 && e.Status < 500 {
		// A plan limit, an archived collection, channels that are gone: the chain ends here, retrying won't help.
		s.logger(ctx).Warn("repeat stopped", "post_id", postID, "reason", e.Message)
		return nil
	}
	return err
}

// repeatDay is the day of month the chain started on: the first post's (copies point back with repeat_of).
// ponytail: one read per earlier copy (a year of monthly copies is 12); a stored anchor would make it one.
func (s *Service) repeatDay(ctx context.Context, row db.Post, loc *time.Location) int {
	day := 0
	for i := 0; i < 1000; i++ {
		if t, err := time.Parse(timeFormat, row.PublishAt.String); err == nil {
			day = t.In(loc).Day()
		}
		if row.RepeatOf == "" {
			break
		}
		prev, err := s.Q.GetPostByID(ctx, row.RepeatOf)
		if err != nil {
			break // the first ones were deleted: the earliest one left decides
		}
		row = prev
	}
	return day
}
