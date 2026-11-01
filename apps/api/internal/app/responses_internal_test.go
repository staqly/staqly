package app

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/db"
)

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

func TestPostResponsesScope(t *testing.T) {
	s, p, post, _, chans := publishedOn(t, "fakers1")
	ctx := context.Background()
	if err := s.Q.UpsertResponse(ctx, db.UpsertResponseParams{WorkspaceID: p.WorkspaceID, PostID: post.ID, ChannelID: chans[0], ID: "9",
		Author: "Ann", Text: "Love it", Likes: sql.NullInt64{Int64: 3, Valid: true}, CreatedAt: "2026-10-30T00:00:00Z", FetchedAt: "2026-10-30T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.PostResponses(ctx, p, post.ID, "")
	if err != nil || got.Overall != nil || len(got.Channels) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	c := got.Channels[0]
	if c.Available || c.Insight != nil || len(c.Responses) != 1 || *c.Responses[0].Likes != 3 {
		t.Fatalf("%+v", c) // a fake without ResponseReader is not available
	}
	_, err = s.PostResponses(ctx, p, post.ID, "chn_nope")
	wantCode(t, err, "validation_failed")
	q := p
	q.Kind, q.UserID, q.Scopes = PrincipalAPIKey, "", []string{ScopeMediaRead}
	_, err = s.PostResponses(ctx, q, post.ID, "")
	if err == nil {
		t.Fatal("no scope, no read")
	}
}

func TestPostResponsesOtherWorkspace(t *testing.T) {
	s, p, _ := publishTest(t)
	_, err := s.PostResponses(context.Background(), p, "pst_unknown", "")
	wantCode(t, err, "not_found")
}

func TestRefreshNeedsWrite(t *testing.T) {
	s, p, post, _, _ := publishedOn(t, "fakers2")
	ro := p
	ro.Kind, ro.UserID, ro.Scopes = PrincipalAPIKey, "", []string{ScopePostsRead}
	if _, err := s.RefreshPostResponses(context.Background(), ro, post.ID); err == nil {
		t.Fatal("read-only key refreshed")
	}
}

func TestRefreshNotPublished(t *testing.T) {
	s, p, post := publishTest(t)
	_, err := s.RefreshPostResponses(context.Background(), p, post.ID)
	wantCode(t, err, "not_published")
}
