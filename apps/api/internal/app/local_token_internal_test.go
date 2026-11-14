package app

import (
	"context"
	"io"
	"log/slog"
	"strings"
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

// Was a bug: a changed access token signed browsers out and left the other ways in that the old token had
// opened: a connect link not used yet, and an admin panel session (12 hours). Both end with the token, on a
// rotation and on a start with another token.
func TestChangedLocalTokenClosesLinksAndAdminSessions(t *testing.T) {
	dir := t.TempDir()
	database, err := storetest.Open(t, dir+"/w.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	ctx := context.Background()
	start := func(token string) *Service {
		t.Helper()
		s := New(database, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{UploadDir: dir + "/uploads", LocalToken: token,
			LocalTokenPath: dir + "/access_token", AdminEnabled: true})
		if err := s.EnsureLocalWorkspace(ctx); err != nil {
			t.Fatal(err)
		}
		if err := s.EnsureAdmins(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SignOutIfTokenChanged(ctx); err != nil {
			t.Fatal(err)
		}
		return s
	}
	// open returns a connect link and an admin session made with the token; check tells what still works.
	open := func(s *Service, token string) (link, admin string) {
		t.Helper()
		p, err := s.LocalPrincipal(ctx)
		if err != nil {
			t.Fatal(err)
		}
		l, err := s.CreateDeviceLink(ctx, p, "http://localhost:8080")
		if err != nil {
			t.Fatal(err)
		}
		_, link, _ = strings.Cut(l.URL, "#")
		if _, admin, _, err = s.VerifyAdminLocalToken(ctx, token, "test"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.AuthenticateAdmin(ctx, admin); err != nil {
			t.Fatal(err)
		}
		return link, admin
	}
	check := func(s *Service, when, link, admin string) {
		t.Helper()
		if _, err := s.AuthenticateAdmin(ctx, admin); err == nil {
			t.Errorf("%s: the admin session made with the old token still works", when)
		}
		if _, err := s.RedeemDeviceLink(ctx, link, "test", ""); err == nil {
			t.Errorf("%s: the connect link made with the old token still signs a browser in", when)
		}
	}

	s := start("")
	tok, err := s.EnsureLocalToken()
	if err != nil {
		t.Fatal(err)
	}
	link, admin := open(s, tok)
	p, _ := s.LocalPrincipal(ctx)
	if _, _, err := s.RotateLocalToken(ctx, Principal{WorkspaceID: p.WorkspaceID, Kind: PrincipalSystem, ID: "cli"}, ""); err != nil {
		t.Fatal(err)
	}
	check(s, "after a rotation", link, admin)

	const a, b = "stk_local_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "stk_local_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	s = start(a)
	link, admin = open(s, a)
	check(start(b), "after a start with another token", link, admin)
	// The new token opens both again.
	open(start(b), b)
}
