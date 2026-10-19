package app

import (
	"context"
	"encoding/json"
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
	// LimitXPosts: posts to X this month (each post and reply; one with a live link counts XLinkPostWeight).
	LimitXPosts = "x_posts_per_period"
	// UsageSeats: the usage meter of billed seats (members); seats are billed per user, never capped.
	UsageSeats = "seats"
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
	// event exports. Without it (Starter) the workspace export is data only.
	DataExport bool `json:"data_export"`
	// XPostsPerPeriod: posts to X included per month (each post and thread reply; one with a live link counts
	// XLinkPostWeight). Sized so their cost at X is at most a quarter of the plan's per-seat price; bought
	// credits (XBundles) come on top.
	XPostsPerPeriod int64 `json:"x_posts_per_period"`
}

type Plan struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// PriceMonth: per user (seat), billed monthly, in whole units of Currency. PriceYear: per user, billed
	// yearly, twelve whole months at 20 % off or a little more (rounded down): €12, €23, €47, €79 a month.
	// Every member of the workspace is a seat, whatever the role.
	PriceMonth int    `json:"price_month"`
	PriceYear  int    `json:"price_year"`
	Currency   string `json:"currency"`
	Highlight  bool   `json:"highlight"`
	// Hidden plans can be given by the operator (`server set-plan`) but are not offered or listed.
	Hidden   bool       `json:"-"`
	Limits   PlanLimits `json:"limits"`
	Features []string   `json:"features"`
	// Entitlements: the Stripe features the workspace's subscription grants (lookup keys); a workspace's plan only.
	Entitlements []string `json:"entitlements,omitempty"`
	// BaseLimits: the plan's own limits when entitlements changed Limits; LimitBonuses: what each one added.
	BaseLimits   *PlanLimits  `json:"base_limits,omitempty"`
	LimitBonuses []LimitBonus `json:"limit_bonuses,omitempty"`
}

const gb = 1 << 30

// Currency: what every price is in (ISO 4217, lower case as Stripe has it). The Stripe prices must match;
// a price in another currency isn't shown (the built-in amount is).
const Currency = "eur"

// Billing intervals a plan is sold in.
const (
	IntervalMonth = "month"
	IntervalYear  = "year"
)

// yearly: twelve months at 20 % off, each rounded down to a whole dollar ($29 -> $23 a month, $276 a year).
func yearly(month int) int { return month * 80 / 100 * 12 }

// Plans is the single source of truth for cloud packages, cheapest first. Every new workspace gets a
// TrialDays trial of TrialPlan (Starter); afterwards it is read-only until a plan is paid for (billing
// checkout) or the operator sets one (`server set-plan <workspace> <plan>`). There is no free plan.
var Plans = []Plan{
	{ID: "starter", Name: "Starter", PriceMonth: 15, PriceYear: yearly(15), Currency: Currency,
		Limits: PlanLimits{PostsPerMonth: Unlimited, Channels: 5, Alerts: 3, Webhooks: 3, APIKeys: 2,
			StorageBytes: 10 * gb, MaxUploadBytes: 25 << 20, DataExport: false,
			XPostsPerPeriod: 250},
		Features: []string{"5 channels", "Per user: invite as many people as you need", "Unlimited posts, collections and schedules", "250 X posts a month included", "MCP, REST API and SDKs", "3 alerts, 3 webhooks, 2 API keys", "10 GB storage, 25 MB per file", "Data export without files"}},
	{ID: "plus", Name: "Plus", PriceMonth: 29, PriceYear: yearly(29), Currency: Currency, Highlight: true,
		Limits: PlanLimits{PostsPerMonth: Unlimited, Channels: 15, Alerts: 10, Webhooks: 10, APIKeys: 10,
			StorageBytes: 50 * gb, MaxUploadBytes: 100 << 20, DataExport: true,
			XPostsPerPeriod: 450},
		Features: []string{"15 channels", "Per user: invite as many people as you need", "Unlimited posts, collections and schedules", "450 X posts a month included", "MCP, REST API and SDKs", "10 alerts, 10 webhooks, 10 API keys", "50 GB storage, 100 MB per file", "Backups and full data export with files"}},
	{ID: "pro", Name: "Pro", PriceMonth: 59, PriceYear: yearly(59), Currency: Currency,
		Limits: PlanLimits{PostsPerMonth: Unlimited, Channels: 40, Alerts: Unlimited, Webhooks: Unlimited, APIKeys: Unlimited,
			StorageBytes: 200 * gb, MaxUploadBytes: 250 << 20, DataExport: true,
			XPostsPerPeriod: 950},
		Features: []string{"40 channels", "Per user: invite as many people as you need", "Unlimited posts, collections and schedules", "950 X posts a month included", "MCP, REST API and SDKs", "Unlimited alerts, webhooks and API keys", "200 GB storage, 250 MB per file", "Backups and full data export with files"}},
	{ID: "business", Name: "Business", PriceMonth: 99, PriceYear: yearly(99), Currency: Currency,
		Limits: PlanLimits{PostsPerMonth: Unlimited, Channels: Unlimited, Alerts: Unlimited, Webhooks: Unlimited, APIKeys: Unlimited,
			StorageBytes: 1024 * gb, MaxUploadBytes: 1 << 30, DataExport: true,
			XPostsPerPeriod: 1600},
		Features: []string{"Unlimited channels", "Per user: invite as many people as you need", "Unlimited posts, collections and schedules", "1,600 X posts a month included", "MCP, REST API and SDKs", "Unlimited alerts, webhooks and API keys", "1 TB storage, 1 GB per file", "Backups and full data export with files", "Priority support"}},
}

// TrialDays: the Starter trial. With billing on it is Stripe's: Starter's checkout starts a 7-day trial with a
// card required, and a new workspace has no plan (read-only) until its owner starts one. Without billing (a
// cloud server without Stripe: self-hosting, tests) a new workspace gets trialPlan() for this long, no card.
const TrialDays = 7

// TrialPlan: only Starter has a free trial; Plus, Pro and Business are paid from the first day.
const TrialPlan = "starter"

// newWorkspacePlan: the plan and its end for a new cloud workspace. With billing: Starter ending now, so the
// workspace is read-only until a subscription (Stripe's card-required trial, or a paid plan) starts; without
// billing: the server-side trial of trialPlan().
func (s *Service) newWorkspacePlan() (string, string) {
	if s.BillingEnabled() {
		return TrialPlan, s.now()
	}
	return s.trialPlan(), formatTime(s.Now().Add(TrialDays * 24 * time.Hour))
}

// trialPlan is the plan a new workspace's trial gives without billing: TrialPlan unless the server sets
// TRIAL_PLAN (test and demo servers).
func (s *Service) trialPlan() string {
	if p, ok := PlanByID(s.Cfg.TrialPlan); ok {
		return p.ID
	}
	return TrialPlan
}

// OfferedPlans: the plans people can pick (GET /plans, the plan picker, MCP).
func OfferedPlans() []Plan {
	out := make([]Plan, 0, len(Plans))
	for _, p := range Plans {
		if !p.Hidden {
			out = append(out, p)
		}
	}
	return out
}

// UnlockedPlan applies to local installs: everything is unlimited.
// UnlockedPlan is the plan of a local install, the Community Edition (staqly serve, the image).
var UnlockedPlan = Plan{ID: "unlocked", Name: "Community Edition",
	Limits: PlanLimits{PostsPerMonth: Unlimited, Channels: Unlimited, Alerts: Unlimited, Webhooks: Unlimited, APIKeys: Unlimited,
		StorageBytes: Unlimited, MaxUploadBytes: Unlimited, DataExport: true, XPostsPerPeriod: Unlimited},
	Features: []string{"Self-hosted", "No plan limits"}}

func PlanByID(id string) (Plan, bool) {
	for _, p := range Plans {
		if p.ID == id {
			return p, true
		}
	}
	return Plan{}, false
}

// ShownPlanID is the plan id to show for a workspace's stored plan: in local mode every workspace is on the
// Community Edition (unlocked), whatever the row says (a local workspace's row keeps the column's default).
func (s *Service) ShownPlanID(stored string) string {
	if s.Cfg.Mode == ModeLocal {
		return UnlockedPlan.ID
	}
	return stored
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
		// An unknown plan id (e.g. the retired free plan): the cheapest plan, read-only, never unlimited.
		return Plans[0], row.PlanExpiresAt.String, true, nil
	}
	expires := row.PlanExpiresAt.String
	// The plan as its subscription's entitlements leave it: every limit check reads this one. Without billing
	// (Stripe off) nothing grants features, so stored ones from before don't count.
	if s.BillingEnabled() {
		p = ApplyEntitlements(p, parseEntitlements(row.Entitlements))
	}
	return p, expires, p.PriceMonth > 0 && expires != "" && expires <= s.now(), nil
}

// ErrSuspended: a suspended workspace is read-only (reading and exporting
// still work) until a plan is paid for.
var ErrSuspended = &Error{Code: "plan_expired", Status: 402,
	Message: "The trial or subscription has ended, so this workspace is read-only. Choose a plan in Settings → Plan & usage to continue."}

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
// local installs have them. Starter still gets a data-only workspace export (RequestWorkspaceExport).
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

// seatsUsed: the seats billed, one per member (open invitations are billed once accepted).
func (s *Service) seatsUsed(ctx context.Context, q *db.Queries, workspaceID string) (int64, error) {
	return q.CountMembers(ctx, workspaceID)
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
	// PlanExpiresAt: end of the trial or paid period (null = no end).
	PlanExpiresAt *string `json:"plan_expires_at"`
	// TrialEndsAt: when the free trial ends (null = not in one). Stripe's trial_end for a Stripe trial, whose
	// PlanExpiresAt adds the payment grace; the end of the server-side trial without billing.
	TrialEndsAt *string `json:"trial_ends_at"`
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
	var expiresAt, trialEnds *string
	if expires != "" {
		expiresAt = &expires
	}
	if s.Cfg.Mode != ModeLocal {
		w, err := s.Q.GetWorkspaceBilling(ctx, p.WorkspaceID)
		if err != nil {
			return Usage{}, err
		}
		trialEnds = s.trialEndsAt(w)
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
	seats, err := s.seatsUsed(ctx, s.Q, p.WorkspaceID)
	if err != nil {
		return Usage{}, err
	}
	xPosts, err := s.Q.SumUnitsUsage(ctx, db.SumUnitsUsageParams{WorkspaceID: p.WorkspaceID, Provider: ProviderX, Period: period})
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
	return Usage{Plan: s.withLivePrices(ctx, plan), Period: period, PlanExpiresAt: expiresAt, TrialEndsAt: trialEnds, Suspended: suspended,
		PostsByStatus: byStatus, Deliveries: deliveries, Usage: map[string]int64{
			LimitPostsPerMonth: posts, LimitChannels: channels, UsageSeats: seats, LimitXPosts: xPosts, LimitAlerts: alerts, LimitWebhooks: hooks, LimitAPIKeys: keys, LimitStorage: storage}}, nil
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
	// Stripe tells the same state more than once (the checkout and the subscription's events): no change, no event.
	// The reason counts too: unpaid and paused leave the same plan and date but are different changes.
	if cur.Plan == planID && cur.PlanExpiresAt.String == expires {
		var last struct{ Reason string }
		if data, err := q.LastPlanChange(ctx, actor.WorkspaceID); err == nil && json.Unmarshal([]byte(data), &last) == nil &&
			last.Reason == reason {
			return nil
		}
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
		return fmt.Errorf("unknown plan %q (use starter, plus, pro or business)", planID)
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

// Stats is the operator's overview of an instance (server CLI `stats`).
type Stats struct {
	Workspaces int64            `json:"workspaces"`
	ByPlan     map[string]int64 `json:"by_plan"`
	Trialing   int64            `json:"trialing"`  // a plan with an end date still ahead (trial or paid period)
	Suspended  int64            `json:"suspended"` // a plan whose end date passed
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
		st.ByPlan[s.ShownPlanID(w.Plan)]++
		if w.PlanExpiresAt.Valid {
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
