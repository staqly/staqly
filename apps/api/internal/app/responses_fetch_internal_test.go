package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// fakeReader is a published fakeRemovable channel that also reads responses as its script says.
type fakeReader struct {
	*fakeRemovable
	rmu   sync.Mutex
	out   []providers.Response
	err   error
	since time.Time
}

func (f *fakeReader) Responses(_ context.Context, _ providers.Credentials, _ string, since time.Time) ([]providers.Response, error) {
	f.rmu.Lock()
	defer f.rmu.Unlock()
	f.since = since
	return f.out, f.err
}

// readerOn: a published post on one channel whose provider reads responses.
func readerOn(t *testing.T, id string) (*Service, Principal, Post, string, *fakeReader) {
	t.Helper()
	s, p, post, fakes, chans := publishedOn(t, id)
	r := &fakeReader{fakeRemovable: fakes[id]}
	providers.Unregister(id)
	providers.Register(r, providers.StatusAvailable)
	return s, p, post, chans[0], r
}

func storedIDs(t *testing.T, s *Service, ws, post string) []string {
	t.Helper()
	rows, err := s.Q.ListResponses(context.Background(), db.ListResponsesParams{WorkspaceID: ws, PostID: post})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	return ids
}

func TestReadResponsesSkipsOwn(t *testing.T) {
	s, p, post, ch, r := readerOn(t, "fakerd1")
	at := time.Now().UTC()
	r.out = []providers.Response{
		{ID: "9", Text: "Love it", CreatedAt: at},
		{ID: "post", Text: "the post itself", CreatedAt: at},
		{ID: "reply1", Text: "my thread part", CreatedAt: at},
		{ID: "reply2", Text: "bare id of a thread part", CreatedAt: at},
		{ID: "10", Text: "mine", Own: true, CreatedAt: at},
	}
	if err := s.readResponses(context.Background(), post.ID, ch); err != nil {
		t.Fatal(err)
	}
	if ids := storedIDs(t, s, p.WorkspaceID, post.ID); len(ids) != 1 || ids[0] != "9" {
		t.Fatalf("stored %v", ids)
	}
}

func TestReadResponsesQueuesAnalysis(t *testing.T) {
	s, p, post, ch, r := readerOn(t, "fakerd2")
	ctx := context.Background()
	key := "analyze_responses:" + post.ID
	r.out = []providers.Response{{ID: "9", Text: "a", CreatedAt: time.Now().UTC()}}
	if err := s.readResponses(ctx, post.ID, ch); err != nil {
		t.Fatal(err)
	}
	jobs := jobsByKey(t, s, key)
	if len(jobs) != 1 || jobs[0].ScheduledAt.Before(time.Now().Add(time.Minute)) {
		t.Fatalf("want one job ~2 minutes out, got %d", len(jobs))
	}
	if !r.since.IsZero() {
		t.Fatalf("first read since %v", r.since)
	}
	// Same response again: nothing new, so the queued job stays alone; since is the newest minus an hour.
	if err := s.readResponses(ctx, post.ID, ch); err != nil {
		t.Fatal(err)
	}
	if n := len(jobsByKey(t, s, key)); n != 1 || r.since.IsZero() {
		t.Fatalf("jobs %d since %v", n, r.since)
	}
	// Sentiment survives a re-read.
	if err := s.Q.SetResponseSentiment(ctx, db.SetResponseSentimentParams{Sentiment: "positive", WorkspaceID: p.WorkspaceID, PostID: post.ID, ChannelID: ch, ID: "9"}); err != nil {
		t.Fatal(err)
	}
	if err := s.readResponses(ctx, post.ID, ch); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.Q.ListResponses(ctx, db.ListResponsesParams{WorkspaceID: p.WorkspaceID, PostID: post.ID})
	if len(rows) != 1 || rows[0].Sentiment != "positive" {
		t.Fatalf("%+v", rows)
	}
}

func TestReadResponsesNothingNewNoJob(t *testing.T) {
	s, _, post, ch, r := readerOn(t, "fakerd3")
	r.out = nil
	if err := s.readResponses(context.Background(), post.ID, ch); err != nil {
		t.Fatal(err)
	}
	if n := len(jobsByKey(t, s, "analyze_responses:"+post.ID)); n != 0 {
		t.Fatalf("%d jobs", n)
	}
}

func TestReadResponsesReauth(t *testing.T) {
	s, _, post, ch, r := readerOn(t, "fakerd4")
	r.err = &providers.Failure{Kind: providers.ErrReauth, Msg: "token revoked"}
	if err := s.readResponses(context.Background(), post.ID, ch); err != nil {
		t.Fatalf("no retry wanted: %v", err)
	}
	row, _ := s.Q.GetChannelByID(context.Background(), ch)
	if row.Status != "needs_reconnect" {
		t.Fatalf("channel %s", row.Status)
	}
}

func TestReadResponsesRejectedKeepsReason(t *testing.T) {
	s, p, post, ch, r := readerOn(t, "fakerd5")
	r.err = &providers.Failure{Kind: providers.ErrRejected, Msg: "no permission to read comments"}
	if err := s.readResponses(context.Background(), post.ID, ch); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.Q.ListResponseInsights(context.Background(), db.ListResponseInsightsParams{WorkspaceID: p.WorkspaceID, PostID: post.ID})
	if len(rows) != 1 || rows[0].Status != "failed" || rows[0].ChannelID != ch || rows[0].Error == "" {
		t.Fatalf("%+v", rows)
	}
}

func TestResponsesWorkerWindow(t *testing.T) {
	s, _, post, ch, _ := readerOn(t, "fakerd6")
	ctx := context.Background()
	key := "read_responses:" + post.ID + ":" + ch
	if err := s.queueRecentResponseReads(ctx); err != nil {
		t.Fatal(err)
	}
	if n := len(jobsByKey(t, s, key)); n != 1 {
		t.Fatalf("recent post: %d jobs", n)
	}
	s.Now = func() time.Time { return time.Now().Add(8 * 24 * time.Hour) }
	if err := s.cancelJobs(ctx, s.Q, key); err != nil {
		t.Fatal(err)
	}
	if err := s.queueRecentResponseReads(ctx); err != nil {
		t.Fatal(err)
	}
	if n := len(jobsByKey(t, s, key)); n != 0 {
		t.Fatalf("old post: %d jobs", n)
	}
}

func TestRetentionResponses(t *testing.T) {
	s, p, post, ch, _ := readerOn(t, "fakerd7")
	ctx := context.Background()
	old := formatTime(time.Now().Add(-100 * 24 * time.Hour))
	for id, fetched := range map[string]string{"old": old, "new": formatTime(time.Now())} {
		if err := s.Q.UpsertResponse(ctx, db.UpsertResponseParams{WorkspaceID: p.WorkspaceID, PostID: post.ID, ChannelID: ch, ID: id, CreatedAt: fetched, FetchedAt: fetched}); err != nil {
			t.Fatal(err)
		}
	}
	rep, err := s.RunRetention(ctx)
	if err != nil || rep.Responses != 1 {
		t.Fatalf("%+v %v", rep, err)
	}
	if ids := storedIDs(t, s, p.WorkspaceID, post.ID); len(ids) != 1 || ids[0] != "new" {
		t.Fatalf("%v", ids)
	}
}

func TestReadResponsesRetriesWaitingCredits(t *testing.T) {
	s, p, post, ch, r := readerOn(t, "fakerd4")
	ctx := context.Background()
	r.out = nil
	if err := s.Q.UpsertResponseInsight(ctx, db.UpsertResponseInsightParams{WorkspaceID: p.WorkspaceID, PostID: post.ID, ChannelID: "",
		Themes: "[]", Questions: "[]", Status: "waiting_credits", AnalyzedAt: s.now()}); err != nil {
		t.Fatal(err)
	}
	if err := s.readResponses(ctx, post.ID, ch); err != nil {
		t.Fatal(err)
	}
	if n := len(jobsByKey(t, s, "analyze_responses:"+post.ID)); n != 1 {
		t.Fatalf("jobs %d", n)
	}
}
