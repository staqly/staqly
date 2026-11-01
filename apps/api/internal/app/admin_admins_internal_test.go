package app

import (
	"context"
	"testing"
)

func TestAdminRemoveRevokesAtOnce(t *testing.T) {
	s, _ := adminTest(t)
	ctx := context.Background()
	_, tok := adminLogin(t, s) // op
	other := Admin{Email: "other@example.com"}
	if _, err := s.AdminAddAdmin(ctx, other, "Other@Example.com", "O"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AdminAddAdmin(ctx, other, "other@example.com", ""); !isCode(err, "admin_exists") {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := s.AdminAddAdmin(ctx, other, "nope", ""); !isCode(err, "validation_failed") {
		t.Fatalf("invalid: %v", err)
	}
	if err := s.AdminRemoveAdmin(ctx, other, "other@example.com"); !isCode(err, "cannot_remove_self") {
		t.Fatalf("self: %v", err)
	}
	if err := s.AdminRemoveAdmin(ctx, other, "op@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthenticateAdmin(ctx, tok); !isCode(err, "unauthorized") {
		t.Fatalf("removed admin's session: %v", err)
	}
	if err := s.AdminRemoveAdmin(ctx, Admin{}, "other@example.com"); !isCode(err, "last_admin") {
		t.Fatalf("last: %v", err)
	}
	if err := s.AdminRemoveAdmin(ctx, other, "ghost@example.com"); !isCode(err, "not_found") {
		t.Fatalf("ghost: %v", err)
	}
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM admin_audit WHERE action IN ('add_admin','remove_admin')`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("audit %d %v", n, err)
	}
}

func TestEnsureAdmins(t *testing.T) {
	s, _ := adminTest(t)
	ctx := context.Background()
	s.Cfg.AdminEmails = []string{"new@example.com", "op@example.com"}
	if err := s.EnsureAdmins(ctx); err != nil {
		t.Fatal(err)
	}
	s.Cfg.AdminEmails = nil // never deleted
	if err := s.EnsureAdmins(ctx); err != nil {
		t.Fatal(err)
	}
	if l, _ := s.AdminListAdmins(ctx); len(l) != 2 || l[0].CreatedBy != "ADMIN_EMAILS" {
		t.Fatalf("%+v", l)
	}
	// Cloud with none: nothing made. Local with none: the default, once.
	if _, err := s.DB.Exec(`DELETE FROM admins`); err != nil {
		t.Fatal(err)
	}
	s.Cfg.Mode = ModeCloud
	if err := s.EnsureAdmins(ctx); err != nil {
		t.Fatal(err)
	}
	if l, _ := s.AdminListAdmins(ctx); len(l) != 0 {
		t.Fatalf("cloud default: %+v", l)
	}
	s.Cfg.Mode = ModeLocal
	for range 2 {
		if err := s.EnsureAdmins(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if l, _ := s.AdminListAdmins(ctx); len(l) != 1 || l[0].Email != "admin@localhost" || l[0].CreatedBy != "install" {
		t.Fatalf("local default: %+v", l)
	}
}

func TestVerifyAdminLocalToken(t *testing.T) {
	s, _ := adminTest(t)
	ctx := context.Background()
	s.Cfg.LocalToken = LocalTokenPrefix + "secret"
	if _, _, _, err := s.VerifyAdminLocalToken(ctx, s.Cfg.LocalToken, ""); !isCode(err, "not_found") {
		t.Fatalf("cloud: %v", err)
	}
	s.Cfg.Mode = ModeLocal
	if _, _, _, err := s.VerifyAdminLocalToken(ctx, "wrong", ""); !isCode(err, "invalid_token") {
		t.Fatalf("wrong: %v", err)
	}
	a, tok, _, err := s.VerifyAdminLocalToken(ctx, " "+s.Cfg.LocalToken+"\n", "ua")
	if err != nil || a.Email != "op@example.com" {
		t.Fatalf("%+v %v", a, err)
	}
	if got, err := s.AuthenticateAdmin(ctx, tok); err != nil || got.Email != a.Email {
		t.Fatalf("%+v %v", got, err)
	}
	if l, _ := s.AdminListAdmins(ctx); l[0].LastLoginAt == nil {
		t.Fatal("last_login_at not set")
	}
}
