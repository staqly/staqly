package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

func actionsTest(t *testing.T) (*Service, Admin, string) {
	t.Helper()
	s, p, _ := publishTest(t)
	s.Cfg.Mode, s.Cfg.EmailSignIn = ModeCloud, true
	s.Cfg.PublicDashboardURL = "https://app.example"
	return s, Admin{Email: "op@example.com"}, p.WorkspaceID
}

// person makes a user and, in workspace ws (created when new), a member with the role.
func person(t *testing.T, s *Service, id, ws, role string) {
	t.Helper()
	ctx, now := context.Background(), formatTime(time.Now())
	if _, err := s.Q.GetUser(ctx, id); err != nil {
		if err := s.W.CreateUser(ctx, db.CreateUserParams{ID: id, Email: id + "@x.io", CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Q.GetWorkspace(ctx, ws); err != nil {
		if err := s.W.CreateWorkspace(ctx, db.CreateWorkspaceParams{ID: ws, Name: "Name " + ws, RecoveryVerifier: ws, RecoveryRotatedAt: now, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.W.AddMember(ctx, db.AddMemberParams{WorkspaceID: ws, UserID: id, Role: role, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
}

func auditCount(t *testing.T, s *Service, action string) int {
	t.Helper()
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM admin_audit WHERE action = ? AND email = 'op@example.com'`, action).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAdminActionAuditOnceAndNoneOnFailure(t *testing.T) {
	s, a, ws := actionsTest(t)
	ctx := context.Background()
	if err := s.AdminSetPlan(ctx, a, ws, "pro", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.AdminSetPlan(ctx, a, ws, "nope", ""); !isCode(err, "validation_failed") {
		t.Fatalf("unknown plan: %v", err)
	}
	var e *Error
	if err := s.AdminSetPlan(ctx, a, ws, "pro", "soon"); !errors.As(err, &e) || e.Status != 422 || e.Fields["until"] == "" {
		t.Fatalf("bad until: %v", err)
	}
	if err := s.AdminSetPlan(ctx, a, "ws_missing", "pro", ""); !isCode(err, "not_found") {
		t.Fatalf("missing workspace: %v", err)
	}
	if got := auditCount(t, s, "set_plan"); got != 1 {
		t.Fatalf("set_plan rows: %d, want 1", got)
	}
	if err := s.AdminClearTrialReview(ctx, a, ws); err != nil {
		t.Fatal(err)
	}
	if err := s.AdminClearTrialReview(ctx, a, "ws_missing"); !isCode(err, "not_found") {
		t.Fatalf("clear missing: %v", err)
	}
	if got := auditCount(t, s, "clear_trial_review"); got != 1 {
		t.Fatalf("clear_trial_review rows: %d", got)
	}
	if err := s.AdminSyncBilling(ctx, a, ws); !isCode(err, "conflict") {
		t.Fatalf("sync without customer: %v", err)
	}
	if err := s.AdminCancelSubscription(ctx, a, ws); err != nil { // billing off: nothing to cancel
		t.Fatal(err)
	}
	if auditCount(t, s, "sync_billing") != 0 || auditCount(t, s, "cancel_subscription") != 1 {
		t.Fatal("billing audit rows")
	}
}

func TestAdminAttachAudit(t *testing.T) {
	s, a, ws := actionsTest(t)
	if _, err := s.AdminAttach(context.Background(), a, ws, "new@x.io"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AdminAttach(context.Background(), a, ws, "not an email"); err == nil {
		t.Fatal("bad email accepted")
	}
	if got := auditCount(t, s, "attach"); got != 1 {
		t.Fatalf("attach rows: %d", got)
	}
}

func TestAdminRevokeWorkspaceSessions(t *testing.T) {
	s, a, ws := actionsTest(t)
	ctx := context.Background()
	now := formatTime(time.Now())
	person(t, s, "u_r", "ws_r", RoleOwner)
	for i, w := range []string{"ws_r", "ws_r", "ws_r", ws} {
		if _, err := s.DB.ExecContext(ctx, `INSERT INTO sessions (id, workspace_id, token_hash, csrf_token, created_at, last_seen_at, expires_at, user_id) VALUES (?, ?, ?, 'c', ?, ?, ?, ?)`,
			"s"+string(rune('a'+i)), w, "h"+string(rune('a'+i)), now, now, now, nullStr("u_r")); err != nil && w == "ws_r" {
			t.Fatal(err)
		}
	}
	n, err := s.AdminRevokeWorkspaceSessions(ctx, a, "ws_r")
	if err != nil || n != 3 {
		t.Fatalf("revoked %d %v, want 3", n, err)
	}
	if n, _ = s.AdminRevokeWorkspaceSessions(ctx, a, "ws_r"); n != 0 {
		t.Fatalf("second revoke: %d", n)
	}
	if n, err = s.AdminRevokeUserSessions(ctx, a, "u_r"); err != nil || n != 1 { // the one left in the other workspace
		t.Fatalf("user revoke %d %v", n, err)
	}
	if _, err := s.AdminRevokeUserSessions(ctx, a, "u_none"); !isCode(err, "not_found") {
		t.Fatalf("missing user: %v", err)
	}
	if auditCount(t, s, "revoke_sessions") != 2 || auditCount(t, s, "revoke_user_sessions") != 1 {
		t.Fatal("revoke audit rows")
	}
}

func TestAdminSignInLinkKinds(t *testing.T) {
	s, a, ws := actionsTest(t)
	ctx := context.Background()
	l, err := s.AdminSignInLink(ctx, a, ws, AdminLinkRequest{}) // a workspace without people: a device link
	if err != nil || !strings.HasPrefix(l.URL, "https://app.example") {
		t.Fatalf("device link %+v %v", l, err)
	}
	person(t, s, "u_l", "ws_l", RoleOwner)
	l, err = s.AdminSignInLink(ctx, a, "ws_l", AdminLinkRequest{Email: "u_l@x.io"})
	if err != nil || l.URL == "" {
		t.Fatalf("own link %+v %v", l, err)
	}
	if _, err = s.AdminSignInLink(ctx, a, "ws_l", AdminLinkRequest{Email: "u_l@x.io", Role: RoleViewer, Hours: 721}); err == nil {
		t.Fatal("721 hours accepted")
	}
	if _, err = s.AdminSignInLink(ctx, a, "ws_l", AdminLinkRequest{Email: "u_l@x.io", Role: RoleViewer}); err != nil {
		t.Fatal(err) // default 24 hours
	}
	if got := auditCount(t, s, "sign_in_link"); got != 3 {
		t.Fatalf("sign_in_link rows: %d, want 3", got)
	}
	var leaked int
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM admin_audit WHERE details LIKE '%https://%' OR details LIKE '%#%'`).Scan(&leaked)
	if leaked != 0 {
		t.Fatal("a link leaked into the audit")
	}
}

func TestAdminDeleteWorkspaceConfirm(t *testing.T) {
	s, a, _ := actionsTest(t)
	ctx := context.Background()
	person(t, s, "u_d", "ws_d", RoleOwner)
	if err := s.AdminDeleteWorkspace(ctx, a, "ws_d", "wrong"); !isCode(err, "confirm_mismatch") {
		t.Fatalf("got %v", err)
	}
	if auditCount(t, s, "delete_workspace") != 0 {
		t.Fatal("audit on failure")
	}
	if err := s.AdminDeleteWorkspace(ctx, a, "ws_d", "Name ws_d"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Q.GetWorkspace(ctx, "ws_d"); !isNoRows(err) {
		t.Fatalf("workspace still there: %v", err)
	}
	if auditCount(t, s, "delete_workspace") != 1 {
		t.Fatal("audit rows")
	}
}

func TestAdminDeleteUser(t *testing.T) {
	s, a, _ := actionsTest(t)
	ctx := context.Background()
	person(t, s, "u_o", "ws_shared", RoleOwner)
	person(t, s, "u_m", "ws_shared", RoleEditor)
	person(t, s, "u_m", "ws_mine", RoleOwner)
	if err := s.AdminDeleteUser(ctx, a, "u_o", "other"); !isCode(err, "confirm_mismatch") {
		t.Fatalf("confirm: %v", err)
	}
	err := s.AdminDeleteUser(ctx, a, "u_o", "U_O@x.io")
	if e, ok := err.(*Error); !ok || e.Code != "sole_owner" || e.Status != 409 || !strings.Contains(e.Message, "Name ws_shared") {
		t.Fatalf("sole owner: %v", err)
	}
	if auditCount(t, s, "delete_user") != 0 {
		t.Fatal("audit on failure")
	}
	if err := s.AdminDeleteUser(ctx, a, "u_m", "u_m@x.io"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Q.GetWorkspace(ctx, "ws_mine"); !isNoRows(err) {
		t.Fatalf("single-member workspace kept: %v", err)
	}
	if _, err := s.Q.GetWorkspace(ctx, "ws_shared"); err != nil {
		t.Fatalf("shared workspace lost: %v", err)
	}
	if _, err := s.Q.GetUser(ctx, "u_m"); !isNoRows(err) {
		t.Fatalf("user kept: %v", err)
	}
	if auditCount(t, s, "delete_user") != 1 {
		t.Fatal("audit rows")
	}
}

func TestAdminListAuditPaging(t *testing.T) {
	s, a, ws := actionsTest(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := s.AdminAudit(ctx, a, "x", ws, "t", map[string]any{"i": i}); err != nil {
			t.Fatal(err)
		}
	}
	p1, err := s.AdminListAudit(ctx, "", ws, "", 2)
	if err != nil || len(p1.Items) != 2 || p1.NextCursor == "" {
		t.Fatalf("page 1 %+v %v", p1, err)
	}
	p2, err := s.AdminListAudit(ctx, "", ws, p1.NextCursor, 2)
	if err != nil || len(p2.Items) != 1 || p2.NextCursor != "" || p2.Items[0].ID == p1.Items[0].ID || p2.Items[0].ID == p1.Items[1].ID {
		t.Fatalf("page 2 %+v %v", p2, err)
	}
	if _, err := s.AdminListAudit(ctx, "", "", "!!", 2); err == nil {
		t.Fatal("bad cursor accepted")
	}
}
