package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func adminTest(t *testing.T) (*Service, *time.Time) {
	t.Helper()
	s, _, _ := publishTest(t)
	s.Cfg.Mode, s.Cfg.DevSignIn = ModeCloud, true
	s.Cfg.AdminEnabled, s.Cfg.AdminEmails = true, []string{"op@example.com"}
	if err := s.EnsureAdmins(context.Background()); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	s.Now = func() time.Time { return now }
	return s, &now
}

func adminCode(t *testing.T, s *Service) string {
	t.Helper()
	if err := s.StartAdminSignIn(context.Background(), "OP@example.com "); err != nil {
		t.Fatal(err)
	}
	c, ok := s.DevAdminCode("op@example.com")
	if !ok {
		t.Fatal("no dev code")
	}
	return c
}

func isCode(err error, code string) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == code
}

func adminLogin(t *testing.T, s *Service) (Admin, string) {
	t.Helper()
	a, tok, _, err := s.VerifyAdminSignIn(context.Background(), "op@example.com", adminCode(t, s), "ua")
	if err != nil {
		t.Fatal(err)
	}
	return a, tok
}

func TestAdminCodeNotOnList(t *testing.T) {
	s, _ := adminTest(t)
	if err := s.StartAdminSignIn(context.Background(), "x@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.DevAdminCode("x@example.com"); ok {
		t.Fatal("code made for a stranger")
	}
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM admin_codes`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("rows %d %v", n, err)
	}
	if _, _, _, err := s.VerifyAdminSignIn(context.Background(), "x@example.com", "123456", ""); !isCode(err, "invalid_code") {
		t.Fatal(err)
	}
}

func TestAdminCodeFiveAttempts(t *testing.T) {
	s, _ := adminTest(t)
	ctx := context.Background()
	code := adminCode(t, s)
	for i := 0; i < 5; i++ {
		if _, _, _, err := s.VerifyAdminSignIn(ctx, "op@example.com", "000000x", ""); !isCode(err, "invalid_code") {
			t.Fatal(err)
		}
	}
	if _, _, _, err := s.VerifyAdminSignIn(ctx, "op@example.com", code, ""); !isCode(err, "invalid_code") {
		t.Fatalf("6th try with the right code: %v", err)
	}
}

func TestAdminCodeExpired(t *testing.T) {
	s, now := adminTest(t)
	code := adminCode(t, s)
	*now = now.Add(11 * time.Minute)
	if _, _, _, err := s.VerifyAdminSignIn(context.Background(), "op@example.com", code, ""); !isCode(err, "invalid_code") {
		t.Fatal(err)
	}
}

func TestAdminCodeSingleUse(t *testing.T) {
	s, _ := adminTest(t)
	code := adminCode(t, s)
	a, tok, exp, err := s.VerifyAdminSignIn(context.Background(), "op@example.com", code, "ua")
	if err != nil || tok == "" || len(a.CSRF) != 20 || a.SessionID == "" || exp.Sub(s.Now()) != 12*time.Hour {
		t.Fatalf("%+v %v", a, err)
	}
	if _, _, _, err := s.VerifyAdminSignIn(context.Background(), "op@example.com", code, ""); !isCode(err, "invalid_code") {
		t.Fatalf("second use: %v", err)
	}
}

func TestAdminSessionRemovedFromList(t *testing.T) {
	s, _ := adminTest(t)
	a, tok := adminLogin(t, s)
	got, err := s.AuthenticateAdmin(context.Background(), tok)
	if err != nil || got.Email != a.Email || got.CSRF != a.CSRF {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := s.DB.Exec(`DELETE FROM admins`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthenticateAdmin(context.Background(), tok); !isCode(err, "unauthorized") {
		t.Fatal(err)
	}
	if err := s.EnsureAdmins(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.SignOutAdmin(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthenticateAdmin(context.Background(), tok); !isCode(err, "unauthorized") {
		t.Fatalf("revoked: %v", err)
	}
}

func TestAdminSessionExpired12h(t *testing.T) {
	s, now := adminTest(t)
	_, tok := adminLogin(t, s)
	*now = now.Add(11*time.Hour + 59*time.Minute)
	if _, err := s.AuthenticateAdmin(context.Background(), tok); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(2 * time.Minute)
	if _, err := s.AuthenticateAdmin(context.Background(), tok); !isCode(err, "unauthorized") {
		t.Fatal(err)
	}
}

func TestAdminCodeCooldown30s(t *testing.T) {
	s, now := adminTest(t)
	first := adminCode(t, s)
	// Same answer as for an address off the list (no enumeration), and no second code.
	if err := s.StartAdminSignIn(context.Background(), "op@example.com"); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.DevAdminCode("op@example.com"); c != first {
		t.Fatal("a code was sent during the cooldown")
	}
	*now = now.Add(31 * time.Second)
	if err := s.StartAdminSignIn(context.Background(), "op@example.com"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM admin_codes`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("rows %d %v", n, err)
	}
}

func TestAdminCodeHourlyCapIsQuiet(t *testing.T) {
	s, now := adminTest(t)
	for range adminCodesPerHour {
		adminCode(t, s)
		*now = now.Add(31 * time.Second)
	}
	if err := s.StartAdminSignIn(context.Background(), "op@example.com"); err != nil {
		t.Fatalf("the cap must not tell an admin address apart: %v", err)
	}
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM admin_codes`).Scan(&n); err != nil || n != adminCodesPerHour {
		t.Fatalf("rows %d %v", n, err)
	}
}

func TestAdminCodeParallelGuessesStayWithinLimit(t *testing.T) {
	s, _ := adminTest(t)
	code := adminCode(t, s)
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _, _ = s.VerifyAdminSignIn(context.Background(), "op@example.com", wrong, "")
		}()
	}
	wg.Wait()
	var attempts int
	if err := s.DB.QueryRow(`SELECT attempts FROM admin_codes`).Scan(&attempts); err != nil || attempts != adminCodeMaxAttempt {
		t.Fatalf("attempts %d %v", attempts, err)
	}
	if _, _, _, err := s.VerifyAdminSignIn(context.Background(), "op@example.com", code, ""); !isCode(err, "invalid_code") {
		t.Fatalf("the right code after the limit: %v", err)
	}
}

func TestAdminAuditRow(t *testing.T) {
	s, _ := adminTest(t)
	a := Admin{Email: "op@example.com"}
	if err := s.AdminAudit(context.Background(), a, "x.y", "ws1", "t", nil); err != nil {
		t.Fatal(err)
	}
	var email, details, ws string
	if err := s.DB.QueryRow(`SELECT email, details, workspace_id FROM admin_audit`).Scan(&email, &details, &ws); err != nil ||
		email != a.Email || details != "{}" || ws != "ws1" {
		t.Fatalf("%q %q %q %v", email, details, ws, err)
	}
}
