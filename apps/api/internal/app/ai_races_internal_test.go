package app

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/blob"
	"github.com/staqly/staqly/apps/api/internal/db"
)

// Both requests have passed the initial duplicate lookup before either enters the write transaction.
type aiBarrierStore struct {
	blob.Store
	arrivals atomic.Int32
	ready    chan struct{}
}

func (b *aiBarrierStore) Put(ctx context.Context, key string, r io.Reader, size int64, ct string) error {
	if b.arrivals.Add(1) == 2 {
		close(b.ready)
	}
	select {
	case <-b.ready:
	case <-ctx.Done():
		return ctx.Err()
	}
	return b.Store.Put(ctx, key, r, size, ct)
}

func TestAIConcurrentIdenticalRequestsCreateOneJob(t *testing.T) {
	s, p, _ := publishTest(t)
	s.Cfg.AI.FalKey = "test-key"
	s.Cfg.Blob = &aiBarrierStore{Store: s.Cfg.Blob, ready: make(chan struct{})}
	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 32, 32))); err != nil {
		t.Fatal(err)
	}
	in := AIJobInput{Operation: AIRemoveBackground, Image: picture.Bytes()}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	type result struct {
		job     AIJob
		created bool
		err     error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() { j, c, e := s.CreateAIJob(ctx, p, in); results <- result{j, c, e} }()
	}
	a, b := <-results, <-results
	if a.err != nil || b.err != nil {
		t.Fatalf("errors: %v %v", a.err, b.err)
	}
	if a.job.ID != b.job.ID || a.created == b.created {
		t.Fatalf("duplicate jobs: %+v %+v", a, b)
	}
	var n int
	if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM ai_jobs WHERE workspace_id = ?", p.WorkspaceID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("jobs %d: %v", n, err)
	}
	files := 0
	if err := s.Cfg.Blob.Walk(ctx, workspaceKey(p.WorkspaceID)+"ai/", func(string, int64) error { files++; return nil }); err != nil || files != 1 {
		t.Fatalf("input files %d: %v", files, err)
	}
}

func TestAIConcurrentIdempotencyKeyRejectsDifferentInput(t *testing.T) {
	s, p, _ := publishTest(t)
	s.Cfg.AI.FalKey = "test-key"
	s.Cfg.Blob = &aiBarrierStore{Store: s.Cfg.Blob, ready: make(chan struct{})}
	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 32, 32))); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	results := make(chan error, 2)
	for _, prompt := range []string{"one", "two"} {
		go func() {
			_, _, err := s.CreateAIJob(ctx, p, AIJobInput{Operation: AIEdit, Prompt: prompt, Image: picture.Bytes(), IdempotencyKey: "same-key"})
			results <- err
		}()
	}
	a, b := <-results, <-results
	if a == nil {
		a = b
	} else if b != nil {
		t.Fatalf("both requests failed: %v %v", a, b)
	}
	ae, ok := a.(*Error)
	if !ok || ae.Status != 422 || ae.Code != "idempotency_mismatch" {
		t.Fatalf("expected idempotency mismatch, got %v", a)
	}
}

func TestAIAbandonedTextReservationsStopBlockingMedia(t *testing.T) {
	s, p, _ := publishTest(t)
	s.Cfg.AI.FalKey = "test-key"
	ctx := t.Context()
	now := s.Now()
	var old []string
	for range aiMaxLive {
		id, err := s.reserveAI(ctx, p, AIAssistant, "", nil, 1234)
		if err != nil {
			t.Fatal(err)
		}
		old = append(old, id)
	}
	// A genuine queued media job may be much older, and must stay resumable.
	if err := s.Q.CreateAIJob(ctx, db.CreateAIJobParams{ID: "old-media", WorkspaceID: p.WorkspaceID, CreatedBy: p.actor(), Operation: AIGenerate, Kind: "image", InputHash: "old-media", CreatedAt: s.now()}); err != nil {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return now.Add(assistantCallTimeout + 2*time.Minute) }
	recent, err := s.reserveAI(ctx, p, AIAssistant, "", nil, 4321)
	if err != nil {
		t.Fatal(err)
	}
	s.sweepAIFiles(ctx)
	for _, id := range old {
		row, err := s.aiJob(ctx, p, id)
		if err != nil || row.Status != "failed" || row.CostMicros != 1234 || row.FinishedAt == "" {
			t.Fatalf("abandoned reservation: %+v %v", row, err)
		}
	}
	for _, id := range []string{recent, "old-media"} {
		row, err := s.aiJob(ctx, p, id)
		if err != nil || row.Status != "queued" {
			t.Fatalf("live job changed: %+v %v", row, err)
		}
	}
	if _, _, err := s.CreateAIJob(ctx, p, AIJobInput{Operation: AIGenerate, Prompt: "after restart"}); err != nil {
		t.Fatalf("abandoned reservations still block media: %v", err)
	}
}

func TestAIConcurrentAcceptKeepsOneMedia(t *testing.T) {
	s, p, _ := publishTest(t)
	s.Cfg.AI.FalKey = "test-key"
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	job, _, err := s.CreateAIJob(ctx, p, AIJobInput{Operation: AIGenerate, Prompt: "one result"})
	if err != nil {
		t.Fatal(err)
	}
	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 32, 32))); err != nil {
		t.Fatal(err)
	}
	key := workspaceKey(p.WorkspaceID) + "ai/" + job.ID + "/result.png"
	if err := s.Cfg.Blob.Put(ctx, key, bytes.NewReader(picture.Bytes()), int64(picture.Len()), "image/png"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.W.FinishAIJob(ctx, db.FinishAIJobParams{ID: job.ID, Status: "succeeded", ResultKey: key, ResultType: "image/png", FinishedAt: s.now()}); err != nil {
		t.Fatal(err)
	}
	// Both requests have opened the same result and staged their uploads before either commits.
	s.Cfg.Blob = &aiBarrierStore{Store: s.Cfg.Blob, ready: make(chan struct{})}
	type result struct {
		media Media
		err   error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() { m, err := s.AcceptAIJob(ctx, p, job.ID); results <- result{m, err} }()
	}
	a, b := <-results, <-results
	if a.err != nil || b.err != nil {
		t.Fatalf("errors: %v %v", a.err, b.err)
	}
	if a.media.ID != b.media.ID {
		t.Fatalf("accepted twice: %s %s", a.media.ID, b.media.ID)
	}
	var n int
	if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM media WHERE workspace_id = ?", p.WorkspaceID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("library files %d: %v", n, err)
	}
	files := 0
	if err := s.Cfg.Blob.Walk(ctx, workspaceKey(p.WorkspaceID)+"media/", func(string, int64) error { files++; return nil }); err != nil || files != 1 {
		t.Fatalf("stored files %d: %v", files, err)
	}
	row, err := s.aiJob(ctx, p, job.ID)
	if err != nil || row.MediaID != a.media.ID || row.ResultKey != "" {
		t.Fatalf("kept result: %+v %v", row, err)
	}
}

// A cancel that read the job before the worker began to hand it to a provider must not record it as free: it
// changes nothing, and the cancel decided on the job as it is now keeps the reserved cost.
func TestAICancelLosesToSubmission(t *testing.T) {
	s, p, _ := publishTest(t)
	s.Cfg.AI.FalKey = "test-key"
	ctx := t.Context()
	job, _, err := s.CreateAIJob(ctx, p, AIJobInput{Operation: AIGenerate, Prompt: "raced"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.aiJob(ctx, p, job.ID) // what the cancel read
	if err != nil {
		t.Fatal(err)
	}
	// The worker, between that read and the cancel's write.
	if _, err := s.W.StartAIJob(ctx, db.StartAIJobParams{StartedAt: s.now(), ID: job.ID}); err != nil {
		t.Fatal(err)
	}
	if n, err := s.W.SetAIJobSubmitting(ctx, db.SetAIJobSubmittingParams{Provider: AIProviderFal, Model: "m", CostMicros: 5000, ID: job.ID}); err != nil || n != 1 {
		t.Fatalf("submitting: %d %v", n, err)
	}
	if done, err := s.cancelAIJob(ctx, before); err != nil || done {
		t.Fatalf("a cancel decided before the submission was applied: %v %v", done, err)
	}
	if row, _ := s.aiJob(ctx, p, job.ID); row.Status != "running" || row.CostMicros != 5000 {
		t.Fatalf("the lost cancel changed the job: %+v", row)
	}
	if err := s.DiscardAIJob(ctx, p, job.ID); err != nil {
		t.Fatal(err)
	}
	if row, _ := s.aiJob(ctx, p, job.ID); row.Status != "canceled" || row.CostMicros != 5000 {
		t.Fatalf("cancelled while a provider may have taken it: %+v", row)
	}
	// The worker's answer arrives after the cancel: no row, so it tells the provider itself (runAIJob).
	if n, err := s.W.SetAIJobSubmitted(ctx, db.SetAIJobSubmittedParams{ProviderRequest: "{}", ID: job.ID}); err != nil || n != 0 {
		t.Fatalf("a cancelled job was marked submitted: %d %v", n, err)
	}
}
