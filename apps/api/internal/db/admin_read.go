package db

import (
	"context"
	"database/sql"
	"strings"
)

// The admin panel's read queries, written by hand: optional filters and the same SQL on SQLite and PostgreSQL
// (dates are fixed-width UTC text, cut with substr). Only fixed fragments are concatenated; values are bound.
// These are the one place that reads across workspaces.

// AdminCounts runs a "SELECT key, COUNT" query into a map.
func (q *Queries) AdminCounts(ctx context.Context, query string, args ...any) (map[string]int64, error) {
	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var k sql.NullString
		var n int64
		if err := rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		out[k.String] = n
	}
	return out, rows.Err()
}

// AdminOne reads a single number.
func (q *Queries) AdminOne(ctx context.Context, query string, args ...any) (n int64, err error) {
	err = q.db.QueryRowContext(ctx, query, args...).Scan(&n)
	return
}

// AdminDays counts rows per UTC day ("YYYY-MM-DD") from `since` on. table/col are fixed by the caller.
func (q *Queries) AdminDays(ctx context.Context, query string, since string) (map[string]int64, error) {
	return q.AdminCounts(ctx, query, since)
}

// AdminWorkspaceRow is one workspace of the list with its counts.
type AdminWorkspaceRow struct {
	ID, Name, Plan, CreatedAt, OwnerEmail               string
	PlanExpiresAt, SubscriptionStatus, StripeCustomerID sql.NullString
	Members, Channels, Posts                            int64
}

// AdminWorkspaceFilterRow: Status is one of trialing|suspended|active|canceled|"" (see app.AdminWorkspaceFilter).
type AdminWorkspaceFilterRow struct {
	Q, Plan, Status, Now, Period, CursorAt, CursorID string
	Limit                                            int
}

const adminWorkspaceSelect = `SELECT w.id, w.name, w.plan, w.created_at, w.plan_expires_at, w.subscription_status, w.stripe_customer_id,
	COALESCE((SELECT u.email FROM workspace_members m JOIN users u ON u.id = m.user_id
		WHERE m.workspace_id = w.id AND m.role = 'owner' ORDER BY m.created_at, u.id LIMIT 1), ''),
	(SELECT COUNT(*) FROM workspace_members m WHERE m.workspace_id = w.id),
	(SELECT COUNT(*) FROM channels c WHERE c.workspace_id = w.id AND c.deleted_at IS NULL),
	COALESCE((SELECT SUM(k.count) FROM usage_counters k WHERE k.workspace_id = w.id AND k.period = ? AND k.metric = 'posts_per_month'), 0)
	FROM workspaces w WHERE 1 = 1`

// AdminListWorkspaces returns up to Limit rows (the caller asks for one more to know if there is a next page).
func (q *Queries) AdminListWorkspaces(ctx context.Context, f AdminWorkspaceFilterRow) ([]AdminWorkspaceRow, error) {
	query, args := adminWorkspaceSelect, []any{f.Period}
	if f.Q != "" {
		like := "%" + f.Q + "%"
		query += ` AND (lower(w.name) LIKE lower(?) OR lower(w.id) LIKE lower(?) OR EXISTS (SELECT 1 FROM workspace_members m
			JOIN users u ON u.id = m.user_id WHERE m.workspace_id = w.id AND lower(u.email) LIKE lower(?)))`
		args = append(args, like, like, like)
	}
	if f.Plan != "" {
		query += ` AND w.plan = ?`
		args = append(args, f.Plan)
	}
	switch f.Status {
	case "suspended":
		query += ` AND w.plan_expires_at IS NOT NULL AND w.plan_expires_at <= ?`
		args = append(args, f.Now)
	case "trialing":
		query += ` AND w.plan_expires_at IS NOT NULL AND w.plan_expires_at > ?`
		args = append(args, f.Now)
	case "active", "canceled":
		query += ` AND w.subscription_status = ?`
		args = append(args, f.Status)
	}
	if f.CursorID != "" {
		query += ` AND (w.created_at < ? OR (w.created_at = ? AND w.id < ?))`
		args = append(args, f.CursorAt, f.CursorAt, f.CursorID)
	}
	query += ` ORDER BY w.created_at DESC, w.id DESC LIMIT ?`
	args = append(args, f.Limit)
	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AdminWorkspaceRow{}
	for rows.Next() {
		var r AdminWorkspaceRow
		if err := rows.Scan(&r.ID, &r.Name, &r.Plan, &r.CreatedAt, &r.PlanExpiresAt, &r.SubscriptionStatus, &r.StripeCustomerID,
			&r.OwnerEmail, &r.Members, &r.Channels, &r.Posts); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AdminWorkspaceByID is one row of the list, or sql.ErrNoRows.
func (q *Queries) AdminWorkspaceByID(ctx context.Context, id, period string) (AdminWorkspaceRow, error) {
	var r AdminWorkspaceRow
	err := q.db.QueryRowContext(ctx, strings.Replace(adminWorkspaceSelect, "WHERE 1 = 1", "WHERE w.id = ?", 1), period, id).Scan(
		&r.ID, &r.Name, &r.Plan, &r.CreatedAt, &r.PlanExpiresAt, &r.SubscriptionStatus, &r.StripeCustomerID,
		&r.OwnerEmail, &r.Members, &r.Channels, &r.Posts)
	return r, err
}

// AdminMemberRow, AdminChannelRow: a workspace's people and connections.
type AdminMemberRow struct {
	UserID, Email, Name, Role string
	LastLoginAt               sql.NullString
}

type AdminChannelRow struct {
	ID, Provider, Name, Handle, Status, LastError, CreatedAt string
}

func (q *Queries) AdminMembers(ctx context.Context, workspaceID string) ([]AdminMemberRow, error) {
	rows, err := q.db.QueryContext(ctx, `SELECT u.id, u.email, u.name, m.role, u.last_login_at FROM workspace_members m
		JOIN users u ON u.id = m.user_id WHERE m.workspace_id = ? ORDER BY m.created_at, u.id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AdminMemberRow{}
	for rows.Next() {
		var r AdminMemberRow
		if err := rows.Scan(&r.UserID, &r.Email, &r.Name, &r.Role, &r.LastLoginAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (q *Queries) AdminChannels(ctx context.Context, workspaceID string) ([]AdminChannelRow, error) {
	rows, err := q.db.QueryContext(ctx, `SELECT id, provider, name, handle, status, last_error, created_at FROM channels
		WHERE workspace_id = ? AND deleted_at IS NULL ORDER BY created_at, id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AdminChannelRow{}
	for rows.Next() {
		var r AdminChannelRow
		if err := rows.Scan(&r.ID, &r.Provider, &r.Name, &r.Handle, &r.Status, &r.LastError, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AdminUserRow is a person with the number of workspaces they belong to.
type AdminUserRow struct {
	ID, Email, Name, CreatedAt string
	LastLoginAt                sql.NullString
	Workspaces                 int64
}

const adminUserSelect = `SELECT u.id, u.email, u.name, u.created_at, u.last_login_at,
	(SELECT COUNT(*) FROM workspace_members m WHERE m.user_id = u.id) FROM users u WHERE 1 = 1`

func (q *Queries) AdminListUsers(ctx context.Context, search, cursorAt, cursorID string, limit int) ([]AdminUserRow, error) {
	query, args := adminUserSelect, []any{}
	if search != "" {
		like := "%" + search + "%"
		query += ` AND (lower(u.email) LIKE lower(?) OR lower(u.name) LIKE lower(?) OR lower(u.id) LIKE lower(?))`
		args = append(args, like, like, like)
	}
	if cursorID != "" {
		query += ` AND (u.created_at < ? OR (u.created_at = ? AND u.id < ?))`
		args = append(args, cursorAt, cursorAt, cursorID)
	}
	query += ` ORDER BY u.created_at DESC, u.id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AdminUserRow{}
	for rows.Next() {
		var r AdminUserRow
		if err := rows.Scan(&r.ID, &r.Email, &r.Name, &r.CreatedAt, &r.LastLoginAt, &r.Workspaces); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (q *Queries) AdminUserByID(ctx context.Context, id string) (AdminUserRow, error) {
	var r AdminUserRow
	err := q.db.QueryRowContext(ctx, strings.Replace(adminUserSelect, "WHERE 1 = 1", "WHERE u.id = ?", 1), id).Scan(
		&r.ID, &r.Email, &r.Name, &r.CreatedAt, &r.LastLoginAt, &r.Workspaces)
	return r, err
}

// AdminUserWorkspaceRow: a workspace a person belongs to.
type AdminUserWorkspaceRow struct{ ID, Name, Plan, Role string }

func (q *Queries) AdminUserWorkspaces(ctx context.Context, userID string) ([]AdminUserWorkspaceRow, error) {
	rows, err := q.db.QueryContext(ctx, `SELECT w.id, w.name, w.plan, m.role FROM workspace_members m
		JOIN workspaces w ON w.id = m.workspace_id WHERE m.user_id = ? ORDER BY w.created_at, w.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AdminUserWorkspaceRow{}
	for rows.Next() {
		var r AdminUserWorkspaceRow
		if err := rows.Scan(&r.ID, &r.Name, &r.Plan, &r.Role); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AdminUserIdentities: the sign-in providers of a person.
func (q *Queries) AdminUserIdentities(ctx context.Context, userID string) ([]string, error) {
	rows, err := q.db.QueryContext(ctx, `SELECT DISTINCT provider FROM user_identities WHERE user_id = ? ORDER BY provider`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
