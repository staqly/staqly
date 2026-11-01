package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// The admin panel's actions (SPEC §5). Each reuses the service behind the CLI and dashboard, and a successful one
// writes exactly one audit row; details hold the inputs, never tokens or link URLs.

func (s *Service) adminWorkspace(ctx context.Context, wsID string) (db.Workspace, error) {
	ws, err := s.Q.GetWorkspace(ctx, wsID)
	if isNoRows(err) {
		return ws, notFound("workspace")
	}
	return ws, err
}

func (s *Service) AdminSetPlan(ctx context.Context, a Admin, wsID, plan, until string) error {
	if _, err := s.adminWorkspace(ctx, wsID); err != nil {
		return err
	}
	// Validate here: SetPlan's errors are plain (CLI) ones, and a database error must not come back as a 422.
	if _, ok := PlanByID(plan); !ok || plan == UnlockedPlan.ID {
		return invalid("plan", "use starter, plus, pro or business")
	}
	if until != "" {
		if _, err := ParseTimeFilter("until", until); err != nil {
			return invalid("until", "RFC 3339 or YYYY-MM-DD")
		}
	}
	if err := s.SetPlan(ctx, wsID, plan, until); err != nil {
		return err
	}
	return s.AdminAudit(ctx, a, "set_plan", wsID, wsID, map[string]any{"plan": plan, "until": until})
}

func (s *Service) AdminClearTrialReview(ctx context.Context, a Admin, wsID string) error {
	if err := s.ClearTrialReview(ctx, wsID); err != nil {
		return err
	}
	return s.AdminAudit(ctx, a, "clear_trial_review", wsID, wsID, nil)
}

func (s *Service) AdminAttach(ctx context.Context, a Admin, wsID, email string) (Invitation, error) {
	inv, err := s.AttachWorkspace(ctx, wsID, email)
	if err != nil {
		return inv, err
	}
	return inv, s.AdminAudit(ctx, a, "attach", wsID, strings.ToLower(strings.TrimSpace(email)), map[string]any{"email": email})
}

type AdminLinkRequest struct {
	Email string
	Role  string
	Hours int
}

type AdminLink struct {
	URL       string `json:"url"`
	ExpiresAt string `json:"expires_at"`
}

func (s *Service) AdminSignInLink(ctx context.Context, a Admin, wsID string, r AdminLinkRequest) (AdminLink, error) {
	if _, err := s.adminWorkspace(ctx, wsID); err != nil {
		return AdminLink{}, err
	}
	var l DeviceLink
	var err error
	hours := r.Hours
	switch {
	case r.Email != "" && r.Role == "":
		l, err = s.CreateGuestLink(ctx, r.Email, wsID, GuestLink{For: 5 * time.Minute, Uses: 1})
	case r.Email != "":
		if hours == 0 {
			hours = 24
		}
		if hours < 1 || hours > 720 {
			return AdminLink{}, invalid("hours", "1 to 720")
		}
		l, err = s.CreateGuestLink(ctx, r.Email, wsID, GuestLink{For: time.Duration(hours) * time.Hour, Role: r.Role})
	default:
		l, err = s.CreateDeviceLink(ctx, Principal{WorkspaceID: wsID, Kind: PrincipalSystem, ID: "admin", Name: a.Email}, "")
	}
	if err != nil {
		return AdminLink{}, err
	}
	d := map[string]any{"email": r.Email, "role": r.Role}
	if r.Email != "" && r.Role != "" {
		d["hours"] = hours
	}
	return AdminLink{URL: l.URL, ExpiresAt: l.ExpiresAt}, s.AdminAudit(ctx, a, "sign_in_link", wsID, r.Email, d)
}

func (s *Service) AdminRevokeWorkspaceSessions(ctx context.Context, a Admin, wsID string) (int64, error) {
	if _, err := s.adminWorkspace(ctx, wsID); err != nil {
		return 0, err
	}
	n, err := s.W.RevokeWorkspaceSessions(ctx, wsID, s.now())
	if err != nil {
		return 0, err
	}
	return n, s.AdminAudit(ctx, a, "revoke_sessions", wsID, wsID, map[string]any{"revoked": n})
}

func (s *Service) adminUser(ctx context.Context, userID string) (db.User, error) {
	u, err := s.Q.GetUser(ctx, userID)
	if isNoRows(err) {
		return u, notFound("user")
	}
	return u, err
}

func (s *Service) AdminRevokeUserSessions(ctx context.Context, a Admin, userID string) (int64, error) {
	u, err := s.adminUser(ctx, userID)
	if err != nil {
		return 0, err
	}
	n, err := s.W.RevokeUserSessions(ctx, userID, s.now())
	if err != nil {
		return 0, err
	}
	return n, s.AdminAudit(ctx, a, "revoke_user_sessions", "", userID, map[string]any{"email": u.Email, "revoked": n})
}

func (s *Service) AdminSyncBilling(ctx context.Context, a Admin, wsID string) error {
	if _, err := s.adminWorkspace(ctx, wsID); err != nil {
		return err
	}
	w, err := s.Q.GetWorkspaceBilling(ctx, wsID)
	if err != nil {
		return err
	}
	if !s.BillingEnabled() || !w.StripeCustomerID.Valid || w.StripeCustomerID.String == "" {
		return conflict("this workspace has no Stripe customer to sync")
	}
	if err := s.syncSubscription(ctx, w.StripeCustomerID.String); err != nil {
		return err
	}
	return s.AdminAudit(ctx, a, "sync_billing", wsID, wsID, nil)
}

func (s *Service) AdminCancelSubscription(ctx context.Context, a Admin, wsID string) error {
	if _, err := s.adminWorkspace(ctx, wsID); err != nil {
		return err
	}
	if err := s.cancelSubscription(ctx, wsID); err != nil {
		return err
	}
	return s.AdminAudit(ctx, a, "cancel_subscription", wsID, wsID, nil)
}

func (s *Service) AdminDeleteWorkspace(ctx context.Context, a Admin, wsID, confirm string) error {
	ws, err := s.adminWorkspace(ctx, wsID)
	if err != nil {
		return err
	}
	if confirm != ws.Name {
		return &Error{Code: "confirm_mismatch", Status: 400, Message: "type the workspace name to confirm"}
	}
	// The stored id, not the one from the request: it names the workspace's files.
	if err := s.deleteWorkspace(ctx, ws.ID); err != nil {
		return err
	}
	return s.AdminAudit(ctx, a, "delete_workspace", ws.ID, ws.Name, map[string]any{"name": ws.Name})
}

func (s *Service) AdminDeleteUser(ctx context.Context, a Admin, userID, confirm string) error {
	u, err := s.adminUser(ctx, userID)
	if err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(confirm), u.Email) {
		return &Error{Code: "confirm_mismatch", Status: 400, Message: "type the person's email to confirm"}
	}
	wss, err := s.Q.ListUserWorkspaces(ctx, userID)
	if err != nil {
		return err
	}
	var alone []string // workspaces that go with the person
	for _, w := range wss {
		members, err := s.Q.CountMembers(ctx, w.ID)
		if err != nil {
			return err
		}
		if members == 1 {
			alone = append(alone, w.ID)
		}
		if w.Role != RoleOwner {
			continue
		}
		owners, err := s.Q.CountOwners(ctx, w.ID)
		if err != nil {
			return err
		}
		b, err := s.Q.GetWorkspaceBilling(ctx, w.ID)
		if err != nil {
			return err
		}
		if owners == 1 && (members > 1 || liveSubscription(b.SubscriptionStatus.String)) {
			return &Error{Code: "sole_owner", Status: 409, Message: u.Email + " is the only owner of " + w.Name + ": hand it over or delete the workspace first"}
		}
	}
	for _, id := range alone {
		if err := s.deleteWorkspace(ctx, id); err != nil {
			return err
		}
	}
	if err := s.W.DeleteUserRow(ctx, userID); err != nil {
		return err
	}
	return s.AdminAudit(ctx, a, "delete_user", "", userID, map[string]any{"email": u.Email, "workspaces_deleted": len(alone)})
}

type AdminAuditPage struct {
	Items      []AdminAuditEntry `json:"items"`
	NextCursor string            `json:"next_cursor"`
}

type AdminAuditEntry struct {
	ID          string          `json:"id"`
	Email       string          `json:"email"`
	Action      string          `json:"action"`
	WorkspaceID string          `json:"workspace_id"`
	Target      string          `json:"target"`
	Details     json.RawMessage `json:"details"`
	CreatedAt   string          `json:"created_at"`
}

func (s *Service) AdminListAudit(ctx context.Context, q, wsID, cursor string, limit int) (AdminAuditPage, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	var cAt, cID string
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		var ok bool
		cAt, cID, ok = strings.Cut(string(raw), "|")
		if err != nil || !ok {
			return AdminAuditPage{}, invalid("cursor", "not a cursor from this list")
		}
	}
	rows, err := s.Q.ListAdminAudit(ctx, db.ListAdminAuditParams{WorkspaceID: wsID, Q: q, CursorCreatedAt: cAt, CursorID: cID, Limit: int64(limit) + 1})
	if err != nil {
		return AdminAuditPage{}, err
	}
	page := AdminAuditPage{Items: []AdminAuditEntry{}}
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		page.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(last.CreatedAt + "|" + last.ID))
	}
	for _, r := range rows {
		d := json.RawMessage(r.Details)
		if !json.Valid(d) {
			d = json.RawMessage("{}")
		}
		page.Items = append(page.Items, AdminAuditEntry{ID: r.ID, Email: r.Email, Action: r.Action, WorkspaceID: r.WorkspaceID, Target: r.Target, Details: d, CreatedAt: r.CreatedAt})
	}
	return page, nil
}
