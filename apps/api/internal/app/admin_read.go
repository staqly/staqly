package app

import (
	"context"
	"database/sql"
	"encoding/base64"
	"strings"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// The admin panel's read side (GET /v1/admin/overview, workspaces, users). Callers are the admin handlers, which
// check the operator's session; nothing here is reachable with a workspace principal.

type DayCount struct {
	Day   string `json:"day"`
	Count int64  `json:"count"`
}

type AdminChannelStats struct {
	Total      int64            `json:"total"`
	ByProvider map[string]int64 `json:"by_provider"`
	ByStatus   map[string]int64 `json:"by_status"`
}

type AdminPostStats struct {
	Period   string           `json:"period"`
	ByStatus map[string]int64 `json:"by_status"`
}

type AdminOverview struct {
	Workspaces      int64                 `json:"workspaces"`
	Users           int64                 `json:"users"`
	ActiveUsers7d   int64                 `json:"active_users_7d"`
	ActiveUsers30d  int64                 `json:"active_users_30d"`
	Signups7d       int64                 `json:"signups_7d"`
	Signups30d      int64                 `json:"signups_30d"`
	ByPlan          map[string]int64      `json:"by_plan"`
	Subscriptions   map[string]int64      `json:"subscriptions"`
	Trialing        int64                 `json:"trialing"`
	Suspended       int64                 `json:"suspended"`
	Channels        AdminChannelStats     `json:"channels"`
	Posts           AdminPostStats        `json:"posts"`
	FailedTargets7d int64                 `json:"failed_targets_7d"`
	Usage           map[string]UsageTotal `json:"usage"`
	SignupsDaily    []DayCount            `json:"signups_daily"`
	PublishedDaily  []DayCount            `json:"published_daily"`
}

// days zero-fills the last 30 UTC days (oldest first) from per-day counts.
func (s *Service) days(counts map[string]int64) []DayCount {
	out := make([]DayCount, 30)
	today := s.Now().UTC()
	for i := range out {
		d := today.AddDate(0, 0, i-29).Format("2006-01-02")
		out[i] = DayCount{Day: d, Count: counts[d]}
	}
	return out
}

func (s *Service) AdminOverview(ctx context.Context) (AdminOverview, error) {
	st, err := s.OperatorStats(ctx)
	if err != nil {
		return AdminOverview{}, err
	}
	o := AdminOverview{Workspaces: st.Workspaces, ByPlan: st.ByPlan, Trialing: st.Trialing, Suspended: st.Suspended,
		Signups7d: st.Signups7d, Signups30d: st.Signups30d, Usage: st.Usage,
		Posts: AdminPostStats{Period: st.Period}}
	q := s.Q
	now := s.Now().UTC()
	ago := func(d int) string { return formatTime(now.AddDate(0, 0, -d)) }
	var e error
	one := func(dst *int64, query string, args ...any) {
		if e == nil {
			*dst, e = q.AdminOne(ctx, query, args...)
		}
	}
	group := func(dst *map[string]int64, query string, args ...any) {
		if e == nil {
			*dst, e = q.AdminCounts(ctx, query, args...)
		}
	}
	one(&o.Users, `SELECT COUNT(*) FROM users`)
	one(&o.ActiveUsers7d, `SELECT COUNT(*) FROM users WHERE last_login_at >= ?`, ago(7))
	one(&o.ActiveUsers30d, `SELECT COUNT(*) FROM users WHERE last_login_at >= ?`, ago(30))
	group(&o.Subscriptions, `SELECT subscription_status, COUNT(*) FROM workspaces WHERE subscription_status IS NOT NULL GROUP BY 1`)
	one(&o.Channels.Total, `SELECT COUNT(*) FROM channels WHERE deleted_at IS NULL`)
	group(&o.Channels.ByProvider, `SELECT provider, COUNT(*) FROM channels WHERE deleted_at IS NULL GROUP BY 1`)
	group(&o.Channels.ByStatus, `SELECT status, COUNT(*) FROM channels WHERE deleted_at IS NULL GROUP BY 1`)
	group(&o.Posts.ByStatus, `SELECT status, COUNT(*) FROM posts WHERE substr(created_at, 1, 7) = ? GROUP BY 1`, st.Period)
	// A failed target keeps its updated_at until it is retried, so this is when it failed.
	one(&o.FailedTargets7d, `SELECT COUNT(*) FROM post_targets WHERE status = 'failed' AND updated_at >= ?`, ago(7))
	var signups, published map[string]int64
	group(&signups, `SELECT substr(created_at, 1, 10), COUNT(*) FROM workspaces WHERE created_at >= ? GROUP BY 1`, ago(30)[:10])
	group(&published, `SELECT substr(published_at, 1, 10), COUNT(*) FROM post_targets WHERE status = 'published' AND published_at >= ? GROUP BY 1`, ago(30)[:10])
	if e != nil {
		return AdminOverview{}, e
	}
	o.SignupsDaily, o.PublishedDaily = s.days(signups), s.days(published)
	return o, nil
}

// AdminWorkspaceFilter narrows the list. Status maps onto the plan end date and the Stripe status: "suspended" =
// plan_expires_at passed, "trialing" = plan_expires_at still ahead (a trial or a paid period, as OperatorStats
// counts), "active" / "canceled" = subscription_status. "" = all. Plan is the stored plan id.
type AdminWorkspaceFilter struct {
	Q, Plan, Status, Cursor string
	Limit                   int
}

type AdminWorkspace struct {
	ID                 string  `json:"id"`
	Name               string  `json:"name"`
	Plan               string  `json:"plan"`
	PlanExpiresAt      *string `json:"plan_expires_at"`
	SubscriptionStatus string  `json:"subscription_status"`
	StripeCustomerID   string  `json:"stripe_customer_id"`
	OwnerEmail         string  `json:"owner_email"`
	Members            int64   `json:"members"`
	Channels           int64   `json:"channels"`
	PostsThisPeriod    int64   `json:"posts_this_period"`
	CreatedAt          string  `json:"created_at"`
	Suspended          bool    `json:"suspended"`
}

type AdminWorkspacePage struct {
	Items      []AdminWorkspace `json:"items"`
	NextCursor string           `json:"next_cursor"`
}

func (s *Service) adminWorkspaceItem(r db.AdminWorkspaceRow) AdminWorkspace {
	w := AdminWorkspace{ID: r.ID, Name: r.Name, Plan: s.ShownPlanID(r.Plan), SubscriptionStatus: r.SubscriptionStatus.String,
		StripeCustomerID: r.StripeCustomerID.String, OwnerEmail: r.OwnerEmail, Members: r.Members, Channels: r.Channels,
		PostsThisPeriod: r.Posts, CreatedAt: r.CreatedAt}
	if r.PlanExpiresAt.Valid && r.PlanExpiresAt.String != "" {
		w.PlanExpiresAt = &r.PlanExpiresAt.String
		w.Suspended = r.PlanExpiresAt.String <= s.now()
	}
	return w
}

func adminLimit(n int) int {
	if n <= 0 {
		return 50
	}
	return min(n, 200)
}

func encodeCursor(createdAt, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(createdAt + "|" + id))
}

func decodeCursor(c string) (createdAt, id string, err error) {
	if c == "" {
		return "", "", nil
	}
	b, err := base64.RawURLEncoding.DecodeString(c)
	createdAt, id, ok := strings.Cut(string(b), "|")
	if err != nil || !ok || id == "" {
		return "", "", invalid("cursor", "is not valid")
	}
	return createdAt, id, nil
}

func (s *Service) AdminListWorkspaces(ctx context.Context, f AdminWorkspaceFilter) (AdminWorkspacePage, error) {
	at, id, err := decodeCursor(f.Cursor)
	if err != nil {
		return AdminWorkspacePage{}, err
	}
	limit := adminLimit(f.Limit)
	rows, err := s.Q.AdminListWorkspaces(ctx, db.AdminWorkspaceFilterRow{Q: f.Q, Plan: f.Plan, Status: f.Status, Now: s.now(),
		Period: s.period(), CursorAt: at, CursorID: id, Limit: limit + 1})
	if err != nil {
		return AdminWorkspacePage{}, err
	}
	page := AdminWorkspacePage{Items: []AdminWorkspace{}}
	if len(rows) > limit {
		rows = rows[:limit]
		page.NextCursor = encodeCursor(rows[limit-1].CreatedAt, rows[limit-1].ID)
	}
	for _, r := range rows {
		page.Items = append(page.Items, s.adminWorkspaceItem(r))
	}
	return page, nil
}

type AdminMember struct {
	UserID      string  `json:"user_id"`
	Email       string  `json:"email"`
	Name        string  `json:"name"`
	Role        string  `json:"role"`
	LastLoginAt *string `json:"last_login_at"`
}

type AdminChannel struct {
	ID        string `json:"id"`
	Provider  string `json:"provider"`
	Name      string `json:"name"`
	Handle    string `json:"handle"`
	Status    string `json:"status"`
	LastError string `json:"last_error"`
	CreatedAt string `json:"created_at"`
}

type AdminTrial struct {
	EndsAt *string `json:"ends_at"`
	Review string  `json:"review"` // why the trial waits for review ("channels"), "" when it does not
}

type AdminWorkspaceDetail struct {
	Workspace          AdminWorkspace `json:"workspace"`
	Usage              Usage          `json:"usage"`
	Members            []AdminMember  `json:"members"`
	Channels           []AdminChannel `json:"channels"`
	InvitationsPending int64          `json:"invitations_pending"`
	SessionsActive     int64          `json:"sessions_active"`
	Trial              AdminTrial     `json:"trial"`
}

func nullPtr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	return &n.String
}

func (s *Service) AdminWorkspace(ctx context.Context, id string) (AdminWorkspaceDetail, error) {
	row, err := s.Q.AdminWorkspaceByID(ctx, id, s.period())
	if isNoRows(err) {
		return AdminWorkspaceDetail{}, notFound("workspace")
	}
	if err != nil {
		return AdminWorkspaceDetail{}, err
	}
	d := AdminWorkspaceDetail{Workspace: s.adminWorkspaceItem(row)}
	if d.Usage, err = s.GetUsage(ctx, Principal{WorkspaceID: id, Kind: PrincipalSystem, ID: "admin"}); err != nil {
		return d, err
	}
	members, err := s.Q.AdminMembers(ctx, id)
	if err != nil {
		return d, err
	}
	d.Members = []AdminMember{}
	for _, m := range members {
		d.Members = append(d.Members, AdminMember{UserID: m.UserID, Email: m.Email, Name: m.Name, Role: m.Role, LastLoginAt: nullPtr(m.LastLoginAt)})
	}
	channels, err := s.Q.AdminChannels(ctx, id)
	if err != nil {
		return d, err
	}
	d.Channels = []AdminChannel{}
	for _, c := range channels {
		d.Channels = append(d.Channels, AdminChannel(c))
	}
	now := s.now()
	if d.InvitationsPending, err = s.Q.AdminOne(ctx, `SELECT COUNT(*) FROM workspace_invitations WHERE workspace_id = ?
		AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > ?`, id, now); err != nil {
		return d, err
	}
	if d.SessionsActive, err = s.Q.AdminOne(ctx, `SELECT COUNT(*) FROM sessions WHERE workspace_id = ? AND revoked_at IS NULL AND expires_at > ?`, id, now); err != nil {
		return d, err
	}
	d.Trial.EndsAt = d.Usage.TrialEndsAt
	t, err := workspaceTrial(ctx, s.Q, id)
	if err != nil {
		return d, err
	}
	if trialPaused(t) {
		d.Trial.Review = t.ReviewReason.String
	}
	return d, nil
}

type AdminUser struct {
	ID          string  `json:"id"`
	Email       string  `json:"email"`
	Name        string  `json:"name"`
	CreatedAt   string  `json:"created_at"`
	LastLoginAt *string `json:"last_login_at"`
	Workspaces  int64   `json:"workspaces"`
}

type AdminUserPage struct {
	Items      []AdminUser `json:"items"`
	NextCursor string      `json:"next_cursor"`
}

func adminUser(r db.AdminUserRow) AdminUser {
	return AdminUser{ID: r.ID, Email: r.Email, Name: r.Name, CreatedAt: r.CreatedAt, LastLoginAt: nullPtr(r.LastLoginAt), Workspaces: r.Workspaces}
}

func (s *Service) AdminListUsers(ctx context.Context, q, cursor string, limit int) (AdminUserPage, error) {
	at, id, err := decodeCursor(cursor)
	if err != nil {
		return AdminUserPage{}, err
	}
	limit = adminLimit(limit)
	rows, err := s.Q.AdminListUsers(ctx, q, at, id, limit+1)
	if err != nil {
		return AdminUserPage{}, err
	}
	page := AdminUserPage{Items: []AdminUser{}}
	if len(rows) > limit {
		rows = rows[:limit]
		page.NextCursor = encodeCursor(rows[limit-1].CreatedAt, rows[limit-1].ID)
	}
	for _, r := range rows {
		page.Items = append(page.Items, adminUser(r))
	}
	return page, nil
}

type AdminUserWorkspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Plan string `json:"plan"`
	Role string `json:"role"`
}

type AdminIdentity struct {
	Provider string `json:"provider"`
}

type AdminUserDetail struct {
	User           AdminUser            `json:"user"`
	Workspaces     []AdminUserWorkspace `json:"workspaces"`
	Identities     []AdminIdentity      `json:"identities"`
	SessionsActive int64                `json:"sessions_active"`
}

func (s *Service) AdminUser(ctx context.Context, id string) (AdminUserDetail, error) {
	row, err := s.Q.AdminUserByID(ctx, id)
	if isNoRows(err) {
		return AdminUserDetail{}, notFound("user")
	}
	if err != nil {
		return AdminUserDetail{}, err
	}
	d := AdminUserDetail{User: adminUser(row), Workspaces: []AdminUserWorkspace{}, Identities: []AdminIdentity{}}
	ws, err := s.Q.AdminUserWorkspaces(ctx, id)
	if err != nil {
		return d, err
	}
	for _, w := range ws {
		d.Workspaces = append(d.Workspaces, AdminUserWorkspace{ID: w.ID, Name: w.Name, Plan: s.ShownPlanID(w.Plan), Role: w.Role})
	}
	ids, err := s.Q.AdminUserIdentities(ctx, id)
	if err != nil {
		return d, err
	}
	for _, p := range ids {
		d.Identities = append(d.Identities, AdminIdentity{Provider: p})
	}
	d.SessionsActive, err = s.Q.AdminOne(ctx, `SELECT COUNT(*) FROM sessions WHERE user_id = ? AND revoked_at IS NULL AND expires_at > ?`, id, s.now())
	return d, err
}
