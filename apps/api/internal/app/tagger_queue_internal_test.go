package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/store"
)

// A model that keeps failing gets exactly summaryMaxAttempts tries per item (the lease counts each one), then
// the item leaves the queue.
func TestSummaryQueueGivesUpAfterMaxAttempts(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	database, err := store.Open(ctx, dir+"/workspace.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "down", 503)
	}))
	t.Cleanup(model.Close)
	s := New(database, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{Mode: ModeLocal, UploadDir: dir + "/uploads",
		Tagger: TaggerConfig{URL: model.URL, Model: "m"}})
	if err := s.EnsureLocalWorkspace(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureDefaultCollections(ctx); err != nil {
		t.Fatal(err)
	}
	p, err := s.LocalPrincipal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cols, err := s.ListCollections(ctx, p, false)
	if err != nil || len(cols) == 0 {
		t.Fatalf("collections: %v %v", cols, err)
	}
	title := "Post"
	it, err := s.CreateItem(ctx, p, cols[0].ID, CreateItemInput{itemFields: itemFields{Title: &title}})
	if err != nil {
		t.Fatal(err)
	}
	// One poll of RunSummaries per round, with the backoff skipped.
	for range 2 * summaryMaxAttempts {
		if _, err := database.Exec(`UPDATE items SET summary_next_at = '2000-01-01T00:00:00.000000Z'
			WHERE id = ? AND summary_next_at IS NOT NULL`, it.ID); err != nil {
			t.Fatal(err)
		}
		due, err := s.Q.DueSummaries(ctx, db.DueSummariesParams{Now: nullStr(s.now()), Lim: 10})
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range due {
			n, err := s.W.LeaseSummary(ctx, db.LeaseSummaryParams{LeaseUntil: nullStr(formatTime(s.Now().Add(summaryLease))),
				WorkspaceID: d.WorkspaceID, ID: d.ID, Expected: d.SummaryNextAt})
			if err != nil || n == 0 {
				t.Fatalf("lease: %v %d", err, n)
			}
			s.summarizeQueued(ctx, d.ID, d.WorkspaceID)
		}
	}
	if n := calls.Load(); n != summaryMaxAttempts {
		t.Fatalf("model called %d times, want %d", n, summaryMaxAttempts)
	}
	var queued bool
	if err := database.QueryRow(`SELECT summary_next_at IS NOT NULL FROM items WHERE id = ?`, it.ID).Scan(&queued); err != nil || queued {
		t.Fatalf("still queued: %v %v", queued, err)
	}
}
