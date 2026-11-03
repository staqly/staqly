package app

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"strings"
	"sync"
	"testing"
	"time"
)

func feedbackTest(t *testing.T) (*Service, Principal, Admin) {
	t.Helper()
	s := adminReadTest(t)
	seedWS(t, s, "feedback", s.Now())
	s.Cfg.AdminEnabled = true
	s.Cfg.AdminEmails = []string{"operator@example.com"}
	if err := s.EnsureAdmins(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s, Principal{Kind: PrincipalSession, WorkspaceID: "feedback", UserID: "u_feedback", Role: RoleViewer}, Admin{Email: "operator@example.com"}
}
func feedbackStatus(t *testing.T, err error, want int) {
	t.Helper()
	var ae *Error
	if !errors.As(err, &ae) || ae.Status != want {
		t.Fatalf("error = %v, want %d", err, want)
	}
}
func feedbackPNG(t *testing.T) *bytes.Reader {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(b.Bytes())
}
func TestProductFeedbackPrivateAttachmentAndStatus(t *testing.T) {
	s, p, a := feedbackTest(t)
	ctx := context.Background()
	id, err := s.CreateProductFeedback(ctx, p, ProductFeedbackInput{"problem", "  Something failed  ", "https://app.example/posts?token=secret#private"}, feedbackPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.AdminListProductFeedback(ctx, a, "")
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("page: %+v %v", page, err)
	}
	r := page.Items[0]
	if r.ID != id || r.PagePath != "/posts" || r.ReporterID != p.UserID || r.WorkspaceID != p.WorkspaceID || !r.Attachment || r.Status != "new" || r.Description != "Something failed" || r.ReporterEmail == "" {
		t.Fatalf("feedback: %+v", r)
	}
	f, ct, err := s.AdminOpenProductFeedbackAttachment(ctx, a, id)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if ct != "image/png" {
		t.Fatalf("type: %s", ct)
	}
	for _, status := range []string{"in_progress", "closed", "new"} {
		if err := s.AdminSetProductFeedbackStatus(ctx, a, id, status); err != nil {
			t.Fatal(err)
		}
	}
	feedbackStatus(t, s.AdminSetProductFeedbackStatus(ctx, a, id, "bogus"), 422)
	feedbackStatus(t, s.AdminSetProductFeedbackStatus(ctx, a, "missing", "closed"), 404)
	_, err = s.AdminListProductFeedback(ctx, Admin{Email: "feedback@Acme.io"}, "")
	feedbackStatus(t, err, 403)
	_, _, err = s.AdminOpenProductFeedbackAttachment(ctx, Admin{}, id)
	feedbackStatus(t, err, 403)
	feedbackStatus(t, s.AdminSetProductFeedbackStatus(ctx, Admin{}, id, "closed"), 403)
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM media`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("screenshots in media: %d %v", n, err)
	}
}
func TestProductFeedbackValidation(t *testing.T) {
	s, p, a := feedbackTest(t)
	ctx := context.Background()
	valid := ProductFeedbackInput{"feature", "Useful idea", "/schedule?secret=yes#token"}
	for _, in := range []ProductFeedbackInput{
		{"bogus", "text", "/"}, {"problem", " \n ", "/"}, {"problem", strings.Repeat("a", 5001), "/"},
		{"problem", "text", "not/a/path"}, {"problem", "text", "/bad%00path"}, {"problem", "text", "/" + strings.Repeat("a", 2048)},
	} {
		_, err := s.CreateProductFeedback(ctx, p, in, nil)
		feedbackStatus(t, err, 422)
	}
	for _, data := range [][]byte{[]byte("<svg><script>bad()</script></svg>"), bytes.Repeat([]byte("a"), MaxFeedbackAttachmentBytes+1), {}} {
		_, err := s.CreateProductFeedback(ctx, p, valid, bytes.NewReader(data))
		feedbackStatus(t, err, 422)
	}
	key := p
	key.Kind = PrincipalAPIKey
	key.Scopes = AllScopes
	_, err := s.CreateProductFeedback(ctx, key, valid, nil)
	feedbackStatus(t, err, 403)
	_, err = s.CreateProductFeedback(ctx, Principal{}, valid, nil)
	feedbackStatus(t, err, 401)
	_, err = s.AdminListProductFeedback(ctx, a, "bad cursor")
	feedbackStatus(t, err, 422)
}
func TestProductFeedbackRateLimitConcurrent(t *testing.T) {
	s, p, _ := feedbackTest(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.CreateProductFeedback(ctx, p, ProductFeedbackInput{"feature", "Idea", "/"}, nil)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
		} else {
			feedbackStatus(t, err, 429)
		}
	}
	if accepted != 5 {
		t.Fatalf("accepted %d, want 5", accepted)
	}
	now := s.Now()
	s.Now = func() time.Time { return now.Add(2 * time.Hour) }
	if _, err := s.CreateProductFeedback(ctx, p, ProductFeedbackInput{"feature", "Later idea", "/"}, nil); err != nil {
		t.Fatal(err)
	}
}
func TestProductFeedbackCursor(t *testing.T) {
	s, p, a := feedbackTest(t)
	ctx := context.Background()
	now := s.Now()
	s.Now = func() time.Time { return now }
	// Multiple reporters at an identical time exercise the cursor's ID tie breaker.
	for i := range 53 {
		p.UserID = "reporter" + strings.Repeat("x", i)
		if _, err := s.CreateProductFeedback(ctx, p, ProductFeedbackInput{"problem", "Description", "/"}, nil); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.AdminListProductFeedback(ctx, a, "")
	if err != nil || len(page.Items) != 50 || page.NextCursor == "" {
		t.Fatalf("page: %d %v", len(page.Items), err)
	}
	next, err := s.AdminListProductFeedback(ctx, a, page.NextCursor)
	if err != nil || len(next.Items) != 3 || next.NextCursor != "" {
		t.Fatalf("next: %d %v", len(next.Items), err)
	}
	seen := map[string]bool{}
	for _, row := range append(page.Items, next.Items...) {
		if seen[row.ID] {
			t.Fatal("duplicate")
		}
		seen[row.ID] = true
	}
}
