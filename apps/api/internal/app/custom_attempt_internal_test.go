package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type eventRow struct{ collection, item, data string }

// eventsOfType lists the workspace's events of one type, oldest first.
func eventsOfType(t *testing.T, s *Service, p Principal, typ string) []eventRow {
	t.Helper()
	rows, err := s.DB.Query(`SELECT COALESCE(collection_id, ''), COALESCE(item_id, ''), data FROM events WHERE workspace_id = ? AND type = ? ORDER BY seq`, p.WorkspaceID, typ)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []eventRow
	for rows.Next() {
		var e eventRow
		if err := rows.Scan(&e.collection, &e.item, &e.data); err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
	return out
}

// requestedAttempts lists the attempt ids of the workspace's post.publish_requested events, oldest first.
func requestedAttempts(t *testing.T, s *Service, p Principal) []string {
	t.Helper()
	var out []string
	for _, e := range eventsOfType(t, s, p, "post.publish_requested") {
		var data struct {
			AttemptID string `json:"attempt_id"`
		}
		if err := json.Unmarshal([]byte(e.data), &data); err != nil || data.AttemptID == "" {
			t.Fatalf("post.publish_requested without attempt_id: %s", e.data)
		}
		out = append(out, data.AttemptID)
	}
	return out
}

// Attempt A of a custom target times out, the person retries (attempt B is queued). A's automation then reports
// its late failure, naming A: the report is refused and B is still asked to publish; B's own result settles it.
func TestLateResultOfATimedOutAttemptDoesNotSettleTheRetry(t *testing.T) {
	s, p, post := publishTest(t) // a custom channel without a URL: publishes through reported results
	ctx := context.Background()
	s.Cfg.ProcessingTimeout = time.Minute
	ch := post.Targets[0].ChannelID
	if _, err := s.PublishPost(ctx, p, post.ID); err != nil {
		t.Fatal(err)
	}
	s.RunDueJobsOnce(ctx) // attempt A: post.publish_requested, waiting for a result
	later := time.Now().Add(5 * time.Minute)
	s.Now = func() time.Time { return later }
	s.RunDueJobsOnce(ctx) // A times out
	got, _ := s.GetPost(ctx, p, post.ID)
	attempts := requestedAttempts(t, s, p)
	if len(attempts) != 1 || got.Targets[0].Status != "failed" || got.Targets[0].Error != "No result reported in time" {
		t.Fatalf("setup: requested %v, target %+v", attempts, got.Targets[0])
	}
	if _, err := s.RetryTarget(ctx, p, post.ID, ch); err != nil { // attempt B queued
		t.Fatal(err)
	}
	var e *Error
	if _, err := s.ReportTargetResult(ctx, p, post.ID, ch, ResultInput{Status: "failed", Error: "attempt A gave up", AttemptID: attempts[0]}); !errors.As(err, &e) || e.Status != 409 {
		t.Fatalf("A's late report: %v, want 409", err)
	}
	s.RunDueJobsOnce(ctx)
	attempts = requestedAttempts(t, s, p)
	if len(attempts) != 2 || attempts[0] == attempts[1] {
		t.Fatalf("publish requested for attempts %v, want two different ones", attempts)
	}
	// A's timeout job, had it still been running: it is about A, not B.
	if err := s.targetTimedOut(ctx, post.ID, ch, attempts[0]); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.GetPost(ctx, p, post.ID); got.Targets[0].Status != "publishing" {
		t.Fatalf("A's timeout settled attempt B: %+v", got.Targets[0])
	}
	if got, err := s.ReportTargetResult(ctx, p, post.ID, ch, ResultInput{Status: "published", AttemptID: attempts[1]}); err != nil || got.Status != "published" {
		t.Fatalf("B's result: %v %s", err, got.Status)
	}
}

// A custom target's timeout whose write fails once is tried again; with one attempt the target stayed
// publishing for good (nothing else settles it).
func TestTargetTimeoutIsRetried(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := context.Background()
	s.Cfg.ProcessingTimeout = time.Minute
	if _, err := s.PublishPost(ctx, p, post.ID); err != nil {
		t.Fatal(err)
	}
	s.RunDueJobsOnce(ctx)
	lift := refuseWrites(t, s, `UPDATE ON post_targets WHEN NEW.status = 'failed'`)
	later := time.Now().Add(5 * time.Minute)
	s.Now = func() time.Time { return later }
	s.RunDueJobsOnce(ctx) // the timeout's first try fails
	lift()
	later = later.Add(time.Hour)
	s.RunDueJobsOnce(ctx)
	if got, _ := s.GetPost(ctx, p, post.ID); got.Targets[0].Status != "failed" || got.Status != "failed" {
		t.Fatalf("after the timeout's retry: target %s, post %s; want both failed", got.Targets[0].Status, got.Status)
	}
}

// A repeating post whose next copy is refused (here: its collection was archived) ends its chain with an event
// people can be alerted on, not with a log line only.
func TestRepeatThatStopsSaysSo(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := context.Background()
	if _, err := s.UpdatePost(ctx, p, post.ID, PostPatch{RepeatEvery: json.RawMessage(`"1d"`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PublishPost(ctx, p, post.ID); err != nil {
		t.Fatal(err)
	}
	s.RunDueJobsOnce(ctx)
	if _, err := s.UpdateCollection(ctx, p, post.CollectionID, CollectionInput{Archived: ptrTo(true)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReportTargetResult(ctx, p, post.ID, post.Targets[0].ChannelID, ResultInput{Status: "published"}); err != nil {
		t.Fatal(err)
	}
	s.RunDueJobsOnce(ctx) // repeat_post: the copy is refused
	if rows := eventsOfType(t, s, p, "post.repeat_stopped"); len(rows) != 1 || rows[0].item != post.ID || rows[0].collection != post.CollectionID {
		t.Fatalf("post.repeat_stopped events: %+v", rows)
	}
}

// post.scheduled carries its post's collection like every other post event; webhooks, alerts and filters
// limited to that collection match on it.
func TestPostScheduledEventNamesItsCollection(t *testing.T) {
	s, p, post := publishTest(t) // creates a scheduled post
	rows := eventsOfType(t, s, p, "post.scheduled")
	if len(rows) != 1 || rows[0].collection != post.CollectionID {
		t.Fatalf("post.scheduled: %+v, want collection %s", rows, post.CollectionID)
	}
}
