package app

import (
	"context"
	"fmt"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// Unlimited marks a limit that does not apply.
const Unlimited = -1

const (
	LimitItemsPerMonth = "items_per_month"
	LimitWebhooks      = "webhooks"
	LimitAPIKeys       = "api_keys"
	LimitStorage       = "storage_bytes"
	LimitMaxUpload     = "max_upload_bytes"
	LimitRules         = "rules"
	LimitWatchers      = "watchers"
	LimitWatcherEvery  = "min_watcher_interval_seconds"
	LimitChannels      = "notification_channels"
)

type PlanLimits struct {
	ItemsPerMonth  int64 `json:"items_per_month"`
	Webhooks       int64 `json:"webhooks"`
	APIKeys        int64 `json:"api_keys"`
	StorageBytes   int64 `json:"storage_bytes"`
	MaxUploadBytes int64 `json:"max_upload_bytes"`
	Rules          int64 `json:"rules"`
	Watchers       int64 `json:"watchers"`
	// MinWatcherIntervalSeconds is how often a watcher may check its source.
	MinWatcherIntervalSeconds int64 `json:"min_watcher_interval_seconds"`
	NotificationChannels      int64 `json:"notification_channels"`
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

// Plans is the single source of truth for cloud packages. Payments are not
// integrated yet: a workspace's plan is changed by the operator
// (`server set-plan <workspace> <plan>`).
var Plans = []Plan{
	{ID: "free", Name: "Free", PriceUSDMonth: 0,
		Limits: PlanLimits{ItemsPerMonth: 100, Webhooks: 3, APIKeys: 3, StorageBytes: 1 * gb, MaxUploadBytes: 25 << 20,
			Rules: 5, Watchers: 3, MinWatcherIntervalSeconds: 15 * 60, NotificationChannels: 2},
		Features: []string{"100 items / month", "3 watchers (every 15 min)", "5 rules", "2 notification channels", "3 webhooks", "3 API keys", "1 GB storage", "No card, no email"}},
	{ID: "indie", Name: "Indie", PriceUSDMonth: 9, Highlight: true,
		Limits: PlanLimits{ItemsPerMonth: 2000, Webhooks: 20, APIKeys: 20, StorageBytes: 20 * gb, MaxUploadBytes: 100 << 20,
			Rules: 50, Watchers: 20, MinWatcherIntervalSeconds: 5 * 60, NotificationChannels: 10},
		Features: []string{"2,000 items / month", "20 watchers (every 5 min)", "50 rules", "10 notification channels", "20 webhooks", "20 API keys", "20 GB storage"}},
	{ID: "team", Name: "Team", PriceUSDMonth: 29,
		Limits: PlanLimits{ItemsPerMonth: Unlimited, Webhooks: Unlimited, APIKeys: Unlimited, StorageBytes: 200 * gb, MaxUploadBytes: 500 << 20,
			Rules: Unlimited, Watchers: Unlimited, MinWatcherIntervalSeconds: 60, NotificationChannels: Unlimited},
		Features: []string{"Unlimited items", "Unlimited watchers (every minute)", "Unlimited rules", "Unlimited notification channels", "Unlimited webhooks", "Unlimited API keys", "200 GB storage"}},
}

// UnlockedPlan applies to local installs: everything is unlimited.
var UnlockedPlan = Plan{ID: "unlocked", Name: "Unlocked",
	Limits: PlanLimits{ItemsPerMonth: Unlimited, Webhooks: Unlimited, APIKeys: Unlimited, StorageBytes: Unlimited, MaxUploadBytes: Unlimited,
		Rules: Unlimited, Watchers: Unlimited, MinWatcherIntervalSeconds: 60, NotificationChannels: Unlimited},
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
	if s.Cfg.Mode == ModeLocal {
		return UnlockedPlan, nil
	}
	id, err := q.GetWorkspacePlan(ctx, workspaceID)
	if err != nil {
		return Plan{}, err
	}
	if p, ok := PlanByID(id); ok {
		return p, nil
	}
	return Plans[0], nil // unknown plan id: fall back to free, never to unlimited
}

func (s *Service) period() string { return s.Now().UTC().Format("2006-01") }

func limitErr(plan Plan, limit string, max, used int64) *Error {
	return &Error{Code: "plan_limit_exceeded", Status: 402,
		Message: fmt.Sprintf("The %s plan allows %s; upgrade the workspace plan to continue.", plan.Name, describeLimit(limit, max)),
		Details: map[string]any{"limit": limit, "max": max, "used": used, "plan": plan.ID}}
}

func describeLimit(limit string, max int64) string {
	switch limit {
	case LimitItemsPerMonth:
		return fmt.Sprintf("%d items per month", max)
	case LimitStorage:
		return fmt.Sprintf("%.1f GB of storage", float64(max)/gb)
	case LimitMaxUpload:
		return fmt.Sprintf("files up to %d MB", max>>20)
	}
	return fmt.Sprintf("%d %s", max, limit)
}

// checkCount fails when adding one more would exceed max.
func checkCount(plan Plan, limit string, max, used int64) error {
	if max != Unlimited && used >= max {
		return limitErr(plan, limit, max, used)
	}
	return nil
}

// useItemQuota counts one created item against this month's quota, inside the
// caller's transaction (so a rolled-back create does not consume quota).
func (s *Service) useItemQuota(ctx context.Context, q *db.Queries, workspaceID string) error {
	plan, err := s.planFor(ctx, q, workspaceID)
	if err != nil {
		return err
	}
	n, err := q.IncrementUsage(ctx, db.IncrementUsageParams{WorkspaceID: workspaceID, Period: s.period(), Metric: LimitItemsPerMonth})
	if err != nil {
		return err
	}
	if plan.Limits.ItemsPerMonth != Unlimited && n > plan.Limits.ItemsPerMonth {
		return limitErr(plan, LimitItemsPerMonth, plan.Limits.ItemsPerMonth, n-1)
	}
	return nil
}

type Usage struct {
	Plan   Plan             `json:"plan"`
	Period string           `json:"period"`
	Usage  map[string]int64 `json:"usage"`
}

func (s *Service) GetUsage(ctx context.Context, p Principal) (Usage, error) {
	if p.WorkspaceID == "" {
		return Usage{}, ErrUnauthorized
	}
	plan, err := s.planFor(ctx, s.Q, p.WorkspaceID)
	if err != nil {
		return Usage{}, err
	}
	period := s.period()
	items, err := s.Q.GetUsage(ctx, db.GetUsageParams{WorkspaceID: p.WorkspaceID, Period: period, Metric: LimitItemsPerMonth})
	if err != nil && !isNoRows(err) {
		return Usage{}, err
	}
	hooks, err := s.Q.CountWebhooks(ctx, p.WorkspaceID)
	if err != nil {
		return Usage{}, err
	}
	keys, err := s.Q.CountActiveAPIKeys(ctx, p.WorkspaceID)
	if err != nil {
		return Usage{}, err
	}
	storage, err := s.Q.StorageUsed(ctx, p.WorkspaceID)
	if err != nil {
		return Usage{}, err
	}
	rules, err := s.Q.CountRules(ctx, p.WorkspaceID)
	if err != nil {
		return Usage{}, err
	}
	watchers, err := s.Q.CountActiveWatchers(ctx, p.WorkspaceID)
	if err != nil {
		return Usage{}, err
	}
	channels, err := s.Q.CountNotifyChannels(ctx, p.WorkspaceID)
	if err != nil {
		return Usage{}, err
	}
	return Usage{Plan: plan, Period: period, Usage: map[string]int64{
		LimitItemsPerMonth: items, LimitWebhooks: hooks, LimitAPIKeys: keys, LimitStorage: storage,
		LimitRules: rules, LimitWatchers: watchers, LimitChannels: channels}}, nil
}

// SetPlan changes a workspace's plan. Operator-only (server CLI); there is
// deliberately no HTTP endpoint for it until payments exist.
func (s *Service) SetPlan(ctx context.Context, workspaceID, planID string) error {
	if _, ok := PlanByID(planID); !ok {
		return fmt.Errorf("unknown plan %q", planID)
	}
	n, err := s.Q.SetWorkspacePlan(ctx, db.SetWorkspacePlanParams{Plan: planID, UpdatedAt: s.now(), ID: workspaceID})
	if err == nil && n == 0 {
		return fmt.Errorf("workspace %s not found", workspaceID)
	}
	return err
}
