package app

import (
	"context"
	"fmt"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// Unlimited marks a limit that does not apply.
const Unlimited = -1

const (
	LimitPostsPerMonth = "posts_per_month"
	LimitChannels      = "channels"
	LimitAlerts        = "alerts"
	LimitWebhooks      = "webhooks"
	LimitAPIKeys       = "api_keys"
	LimitStorage       = "storage_bytes"
	LimitMaxUpload     = "max_upload_bytes"
	LimitDataExport    = "data_export"
)

type PlanLimits struct {
	// PostsPerMonth: posts created per calendar month (by people, agents and schedules).
	PostsPerMonth int64 `json:"posts_per_month"`
	// Channels: connected accounts.
	Channels       int64 `json:"channels"`
	Alerts         int64 `json:"alerts"`
	Webhooks       int64 `json:"webhooks"`
	APIKeys        int64 `json:"api_keys"`
	StorageBytes   int64 `json:"storage_bytes"`
	MaxUploadBytes int64 `json:"max_upload_bytes"`
	// DataExport: full workspace ZIP exports with media files (backups) and
	// event exports. Without it (Free) the workspace export is data only.
	DataExport bool `json:"data_export"`
}

type Plan struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	PriceUSDMonth int        `json:"price_usd_month"`
	Highlight     bool       `json:"highlight"`
	Limits        PlanLimits `json:"limits"`
	Features      []string   `json:"features"`
}

const gb = 1 << 30

// Plans is the single source of truth for cloud packages. With Stripe
// configured, Pro is bought through billing checkout; the operator can also
// change a plan (`server set-plan <workspace> <plan>`).
var Plans = []Plan{
	{ID: "free", Name: "Free", PriceUSDMonth: 0,
		Limits:   PlanLimits{PostsPerMonth: 50, Channels: 3, Alerts: 2, Webhooks: 3, APIKeys: 1, StorageBytes: 1 * gb, MaxUploadBytes: 5 << 20, DataExport: false},
		Features: []string{"50 posts / month", "3 channels", "Unlimited collections and schedules", "2 alerts", "3 webhooks", "1 API key", "1 GB storage, 5 MB per file", "Data export without files", "No card, no email"}},
	{ID: "pro", Name: "Pro", PriceUSDMonth: 30, Highlight: true,
		Limits:   PlanLimits{PostsPerMonth: Unlimited, Channels: Unlimited, Alerts: Unlimited, Webhooks: Unlimited, APIKeys: Unlimited, StorageBytes: 100 * gb, MaxUploadBytes: 100 << 20, DataExport: true},
		Features: []string{"Unlimited posts", "Unlimited channels", "Unlimited collections and schedules", "Unlimited alerts", "Unlimited webhooks", "Unlimited API keys", "100 GB storage, 100 MB per file", "Backups and full data export with files", "7-day free trial"}},
}

// TrialDays: new cloud workspaces start on Pro until then.
const TrialDays = 7

// UnlockedPlan applies to local installs: everything is unlimited.
var UnlockedPlan = Plan{ID: "unlocked", Name: "Unlocked",
	Limits:   PlanLimits{PostsPerMonth: Unlimited, Channels: Unlimited, Alerts: Unlimited, Webhooks: Unlimited, APIKeys: Unlimited, StorageBytes: Unlimited, MaxUploadBytes: Unlimited, DataExport: true},
	Features: []string{"Local install", "No limits"}}

func PlanByID(id string) (Plan, bool) {
	for _, p := range Plans {
		if p.ID == id {
			return p, true
		}
	}
	return Plan{}, false
}

// planFor returns the effective plan of a workspace.
func (s *Service) planFor(ctx context.Context, q *db.Queries, workspaceID string) (Plan, error) {
	p, _, _, err := s.planState(ctx, q, workspaceID)
	return p, err
}

// planState returns the plan, when it ends ("" = no end) and whether the
// workspace is suspended: a paid plan whose trial or paid period ended.
func (s *Service) planState(ctx context.Context, q *db.Queries, workspaceID string) (Plan, string, bool, error) {
	if s.Cfg.Mode == ModeLocal {
		return UnlockedPlan, "", false, nil
	}
	row, err := q.GetWorkspacePlan(ctx, workspaceID)
	if err != nil {
		return Plan{}, "", false, err
	}
	p, ok := PlanByID(row.Plan)
	if !ok {
		return Plans[0], "", false, nil // unknown plan id: fall back to free, never to unlimited
	}
	expires := row.PlanExpiresAt.String
	return p, expires, p.PriceUSDMonth > 0 && expires != "" && expires <= s.now(), nil
}

// ErrSuspended: a suspended workspace is read-only (reading and exporting
// still work) until it is paid or switched to Free.
var ErrSuspended = &Error{Code: "plan_expired", Status: 402,
	Message: "The Pro trial or subscription has ended, so this workspace is read-only. Upgrade, or switch to the Free plan in Settings → Plan & usage."}

// CheckWritable returns ErrSuspended when the workspace may not change.
func (s *Service) CheckWritable(ctx context.Context, workspaceID string) error {
	if workspaceID == "" {
		return nil
	}
	_, _, suspended, err := s.planState(ctx, s.Q, workspaceID)
	if err != nil {
		return err
	}
	if suspended {
		return ErrSuspended
	}
	return nil
}

func (s *Service) period() string { return s.Now().UTC().Format("2006-01") }

func limitErr(plan Plan, limit string, max, used int64) *Error {
	return &Error{Code: "plan_limit_exceeded", Status: 402,
		Message: fmt.Sprintf("The %s plan allows %s; upgrade the workspace plan to continue.", plan.Name, describeLimit(limit, max)),
		Details: map[string]any{"limit": limit, "max": max, "used": used, "plan": plan.ID}}
}

func describeLimit(limit string, max int64) string {
	switch limit {
	case LimitPostsPerMonth:
		return fmt.Sprintf("%d posts per month", max)
	case LimitChannels:
		return fmt.Sprintf("%d channels", max)
	case LimitAlerts:
		return fmt.Sprintf("%d alerts", max)
	case LimitStorage:
		return fmt.Sprintf("%.1f GB of storage", float64(max)/gb)
	case LimitMaxUpload:
		return fmt.Sprintf("files up to %d MB", max>>20)
	}
	return fmt.Sprintf("%d %s", max, limit)
}

// requireDataExport: full exports (workspace backups with media files, event exports) are a paid feature;
// local installs have them. Free still gets a data-only workspace export (RequestWorkspaceExport).
func (s *Service) requireDataExport(ctx context.Context, workspaceID string) error {
	plan, _, _, err := s.planState(ctx, s.Q, workspaceID)
	if err != nil {
		return err
	}
	if !plan.Limits.DataExport {
		return &Error{Code: "plan_limit_exceeded", Status: 402,
			Message: fmt.Sprintf("The %s plan has no activity log export or backups; upgrade the workspace plan to export them. The workspace export (data without files) works on every plan.", plan.Name),
			Details: map[string]any{"limit": LimitDataExport, "plan": plan.ID}}
	}
	return nil
}

// checkCount fails when adding one more would exceed max.
func checkCount(plan Plan, limit string, max, used int64) error {
	if max != Unlimited && used >= max {
		return limitErr(plan, limit, max, used)
	}
	return nil
}

// usePostQuota counts one created post against this month's quota, inside the
// caller's transaction (so a rolled-back create does not consume quota).
func (s *Service) usePostQuota(ctx context.Context, q *db.Queries, workspaceID string) error {
	return s.useMonthlyQuota(ctx, q, workspaceID, LimitPostsPerMonth, func(l PlanLimits) int64 { return l.PostsPerMonth })
}

// useChannelQuota checks the connected-channels limit before one more is connected.
func (s *Service) useChannelQuota(ctx context.Context, q *db.Queries, workspaceID string) error {
	plan, err := s.planFor(ctx, q, workspaceID)
	if err != nil {
		return err
	}
	n, err := q.CountChannels(ctx, workspaceID)
	if err != nil {
		return err
	}
	return checkCount(plan, LimitChannels, plan.Limits.Channels, n)
}

// useScheduleQuota: schedules are unlimited on every plan; the hook stays so a limit is one line away.
func (s *Service) useScheduleQuota(context.Context, *db.Queries, string) error { return nil }

func (s *Service) useMonthlyQuota(ctx context.Context, q *db.Queries, workspaceID, metric string, max func(PlanLimits) int64) error {
	plan, err := s.planFor(ctx, q, workspaceID)
	if err != nil {
		return err
	}
	if limit := max(plan.Limits); limit != Unlimited {
		used, err := q.GetUsage(ctx, db.GetUsageParams{WorkspaceID: workspaceID, Period: s.period(), Metric: metric})
		if err != nil && !isNoRows(err) {
			return err
		}
		if used >= limit {
			return limitErr(plan, metric, limit, used)
		}
	}
	_, err = q.IncrementUsage(ctx, db.IncrementUsageParams{WorkspaceID: workspaceID, Period: s.period(), Metric: metric})
	return err
}

// isDecision: the status records a decision.
type Usage struct {
	Plan   Plan             `json:"plan"`
	Period string           `json:"period"`
	Usage  map[string]int64 `json:"usage"`
	// PlanExpiresAt: end of the Pro trial or paid period (null = no end).
	PlanExpiresAt *string `json:"plan_expires_at"`
	// Suspended: the period ended unpaid; the workspace is read-only.
	Suspended bool `json:"suspended"`
	// ItemsByStatus: items created this period, by their current status.
	PostsByStatus map[string]int64 `json:"posts_by_status"`
	// Deliveries: webhook deliveries waiting (queued, retrying) or given up (failed).
	Deliveries db.WebhookDeliveryBacklogRow `json:"webhook_deliveries"`
}

func (s *Service) GetUsage(ctx context.Context, p Principal) (Usage, error) {
	if p.WorkspaceID == "" {
		return Usage{}, ErrUnauthorized
	}
	plan, expires, suspended, err := s.planState(ctx, s.Q, p.WorkspaceID)
	if err != nil {
		return Usage{}, err
	}
	var expiresAt *string
	if expires != "" {
		expiresAt = &expires
	}
	period := s.period()
	posts, err := s.Q.GetUsage(ctx, db.GetUsageParams{WorkspaceID: p.WorkspaceID, Period: period, Metric: LimitPostsPerMonth})
	if err != nil && !isNoRows(err) {
		return Usage{}, err
	}
	hooks, err := s.Q.CountWebhooks(ctx, p.WorkspaceID)
	if err != nil {
		return Usage{}, err
	}
	keys, err := s.Q.CountActiveAPIKeys(ctx, db.CountActiveAPIKeysParams{WorkspaceID: p.WorkspaceID, Now: nullStr(s.now())})
	if err != nil {
		return Usage{}, err
	}
	storage, err := s.Q.StorageUsed(ctx, p.WorkspaceID)
	if err != nil {
		return Usage{}, err
	}
	channels, err := s.Q.CountChannels(ctx, p.WorkspaceID)
	if err != nil {
		return Usage{}, err
	}
	alerts, err := s.Q.CountNotifyChannels(ctx, p.WorkspaceID)
	if err != nil {
		return Usage{}, err
	}
	rows, err := s.Q.PostStatusCountsSince(ctx, db.PostStatusCountsSinceParams{WorkspaceID: p.WorkspaceID, CreatedAt: period})
	if err != nil {
		return Usage{}, err
	}
	byStatus := map[string]int64{}
	for _, r := range rows {
		byStatus[r.Status] = r.N
	}
	deliveries, err := s.Q.WebhookDeliveryBacklog(ctx, p.WorkspaceID)
	if err != nil {
		return Usage{}, err
	}
	return Usage{Plan: plan, Period: period, PlanExpiresAt: expiresAt, Suspended: suspended,
		PostsByStatus: byStatus, Deliveries: deliveries, Usage: map[string]int64{
			LimitPostsPerMonth: posts, LimitChannels: channels, LimitAlerts: alerts, LimitWebhooks: hooks, LimitAPIKeys: keys, LimitStorage: storage}}, nil
}

// changePlan sets a workspace's plan (expires "" = no end date, which also
// ends a trial or a suspension) and records workspace.plan_changed.
func (s *Service) changePlan(ctx context.Context, q *db.Queries, actor Principal, planID, expires, reason string) error {
	cur, err := q.GetWorkspacePlan(ctx, actor.WorkspaceID)
	if isNoRows(err) {
		return notFound("workspace")
	}
	if err != nil {
		return err
	}
	if _, err := q.SetWorkspacePlan(ctx, db.SetWorkspacePlanParams{Plan: planID, PlanExpiresAt: nullStr(expires),
		UpdatedAt: s.now(), ID: actor.WorkspaceID}); err != nil {
		return err
	}
	var exp any
	if expires != "" {
		exp = expires
	}
	_, err = s.emit(ctx, q, actor, "workspace.plan_changed", "", "", map[string]any{"from": cur.Plan, "to": planID,
		"expires_at": exp, "reason": reason})
	return err
}

// SetPlan is the operator's switch (server CLI `set-plan`): until "" applies
// the plan with no end date; a date (RFC 3339 or YYYY-MM-DD) comps it until
// then, after which the workspace is suspended like an unpaid one.
func (s *Service) SetPlan(ctx context.Context, workspaceID, planID, until string) error {
	if _, ok := PlanByID(planID); !ok || planID == UnlockedPlan.ID {
		return fmt.Errorf("unknown plan %q (use free or pro)", planID)
	}
	exp := ""
	if until != "" {
		v, err := ParseTimeFilter("until", until)
		if err != nil {
			return fmt.Errorf("--until must be RFC 3339 or YYYY-MM-DD")
		}
		exp = v.(string)
	}
	reason := "operator"
	if exp != "" {
		reason = "operator_comp"
	}
	actor := Principal{WorkspaceID: workspaceID, Kind: PrincipalSystem, ID: "operator", Name: "Operator"}
	err := s.tx(ctx, func(q *db.Queries) error { return s.changePlan(ctx, q, actor, planID, exp, reason) })
	if isNotFound(err) {
		return fmt.Errorf("workspace %s not found", workspaceID)
	}
	return err
}

// ChooseFreePlan lets a workspace admin move to Free themselves, e.g. when
// the Pro trial ends; it lifts a suspension. Upgrades go through Stripe
// (billing.go) or the operator (SetPlan).
func (s *Service) ChooseFreePlan(ctx context.Context, p Principal) (Usage, error) {
	if err := p.require(ScopeAdmin); err != nil {
		return Usage{}, err
	}
	if s.Cfg.Mode == ModeLocal {
		return Usage{}, &Error{Code: "not_available", Status: 409, Message: "local installs have no plans"}
	}
	if b, err := s.Q.GetWorkspaceBilling(ctx, p.WorkspaceID); err == nil && b.SubscriptionStatus.Valid &&
		liveSubscription(b.SubscriptionStatus.String) {
		return Usage{}, &Error{Code: "conflict", Status: 409,
			Message: "cancel the Pro subscription in the billing portal first; the workspace moves to Free when it ends"}
	}
	if cur, err := s.Q.GetWorkspacePlan(ctx, p.WorkspaceID); err == nil && cur.Plan == "free" && !cur.PlanExpiresAt.Valid {
		return Usage{}, alreadyErr("workspace", "on the Free plan")
	}
	if err := s.tx(ctx, func(q *db.Queries) error { return s.changePlan(ctx, q, p, "free", "", "self_service") }); err != nil {
		return Usage{}, err
	}
	return s.GetUsage(ctx, p)
}

// Stats is the operator's overview of an instance (server CLI `stats`).
type Stats struct {
	Workspaces int64            `json:"workspaces"`
	ByPlan     map[string]int64 `json:"by_plan"`
	Trialing   int64            `json:"trialing"`  // paid plan with an end date still ahead
	Suspended  int64            `json:"suspended"` // paid plan whose end date passed
	Signups7d  int64            `json:"signups_7d"`
	Signups30d int64            `json:"signups_30d"`
	Period     string           `json:"period"`
	// Usage this month per metric: total and how many workspaces used it.
	Usage map[string]UsageTotal `json:"usage"`
}

type UsageTotal struct {
	Total      int64 `json:"total"`
	Workspaces int64 `json:"workspaces"`
}

func (s *Service) OperatorStats(ctx context.Context) (Stats, error) {
	st := Stats{ByPlan: map[string]int64{}, Usage: map[string]UsageTotal{}, Period: s.period()}
	rows, err := s.Q.ListWorkspaces(ctx)
	if err != nil {
		return st, err
	}
	now := s.Now()
	for _, w := range rows {
		st.Workspaces++
		st.ByPlan[w.Plan]++
		if w.Plan != "free" && w.PlanExpiresAt.Valid {
			if t, err := time.Parse(timeFormat, w.PlanExpiresAt.String); err == nil && t.After(now) {
				st.Trialing++
			} else {
				st.Suspended++
			}
		}
		if t, err := time.Parse(timeFormat, w.CreatedAt); err == nil {
			if now.Sub(t) <= 7*24*time.Hour {
				st.Signups7d++
			}
			if now.Sub(t) <= 30*24*time.Hour {
				st.Signups30d++
			}
		}
	}
	totals, err := s.Q.UsageTotals(ctx, st.Period)
	for _, u := range totals {
		st.Usage[u.Metric] = UsageTotal{Total: u.Total, Workspaces: u.Workspaces}
	}
	return st, err
}
