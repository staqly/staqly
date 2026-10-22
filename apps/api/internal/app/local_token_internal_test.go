package app

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/store/storetest"
)

// Was a bug: changing STAQLY_LOCAL_TOKEN and restarting signed no browser out. Now a start with another token
// signs every browser out; the same token, or a rotation from the dashboard (whose session stays), doesn't.
func TestChangedLocalTokenSignsBrowsersOut(t *testing.T) {
	dir := t.TempDir()
	database, err := storetest.Open(t, dir+"/w.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	ctx := context.Background()
	start := func(token string) (*Service, int64) {
		t.Helper()
		s := New(database, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{UploadDir: dir + "/uploads", LocalToken: token,
			LocalTokenPath: dir + "/access_token"})
		if err := s.EnsureLocalWorkspace(ctx); err != nil {
			t.Fatal(err)
		}
		n, err := s.SignOutIfTokenChanged(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return s, n
	}
	signedIn := func(s *Service, session NewSession) bool {
		_, _, err := s.AuthenticateSession(ctx, session.Token)
		return err == nil
	}
	const a, b = "stk_local_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "stk_local_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	s, n := start(a) // the first start records the token
	if n != 0 {
		t.Fatalf("first start signed %d out", n)
	}
	session, err := s.SignInWithLocalToken(ctx, a, "test")
	if err != nil {
		t.Fatal(err)
	}
	if s, n = start(a); n != 0 || !signedIn(s, session) {
		t.Fatalf("a restart with the same token: %d signed out, still signed in %v", n, signedIn(s, session))
	}
	if s, n = start(b); n != 1 || signedIn(s, session) {
		t.Fatalf("a restart with another token: %d signed out, still signed in %v", n, signedIn(s, session))
	}

	// A rotation from the dashboard (the file token) keeps the browser that did it, also after a restart.
	s, _ = start("")
	tok, err := s.EnsureLocalToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SignOutIfTokenChanged(ctx); err != nil { // the file token is now the one browsers use
		t.Fatal(err)
	}
	session, err = s.SignInWithLocalToken(ctx, tok, "test")
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := s.AuthenticateSession(ctx, session.Token)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.RotateLocalToken(ctx, p, p.ID); err != nil { // the dashboard keeps its own session
		t.Fatal(err)
	}
	if s, n = start(""); n != 0 || !signedIn(s, session) {
		t.Fatalf("a restart after a rotation: %d signed out, the rotating browser signed in %v", n, signedIn(s, session))
	}
}
