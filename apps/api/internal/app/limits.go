package app

import (
	"context"
	"fmt"
	"slices"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// Plan limits come from the database (migration 00006): a default per limit (limits), a value per plan where it
// differs (limit_plans) and a value per workspace an operator set (limit_workspaces), which replaces the plan's.
// Stripe add-ons (ApplyEntitlements) still come on top of whichever applies. The admin panel edits all three.

// LimitSource: where a workspace's value of a limit comes from.
const (
	LimitFromDefault   = "default"
	LimitFromPlan      = "plan"
	LimitFromWorkspace = "workspace"
)

// limitField maps a limit's key to its field in PlanLimits; data_export is the one on/off limit (1 / 0).
func limitField(l *PlanLimits, key string) (*int64, bool) {
	switch key {
	case LimitPostsPerMonth:
		return &l.PostsPerMonth, true
	case LimitChannels:
		return &l.Channels, true
	case LimitAlerts:
		return &l.Alerts, true
	case LimitWebhooks:
		return &l.Webhooks, true
	case LimitAPIKeys:
		return &l.APIKeys, true
	case LimitStorage:
		return &l.StorageBytes, true
	case LimitMaxUpload:
		return &l.MaxUploadBytes, true
	case LimitXPosts:
		return &l.XPostsPerPeriod, true
	case LimitLinkedInPosts:
		return &l.LinkedInPostsPerPeriod, true
	case LimitLinkedInVideos:
		return &l.LinkedInVideosPerPeriod, true
	case LimitAIMonthlyCents:
		return &l.AIMonthlyCents, true
	}
	return nil, false
}

// setLimit puts one stored value into PlanLimits; an unknown key (a newer server's limit) is left out.
func setLimit(l *PlanLimits, key string, v int64) {
	if key == LimitDataExport {
		l.DataExport = v != 0
		return
	}
	if p, ok := limitField(l, key); ok {
		*p = v
	}
}

// effective: a limit's value and where it comes from (the workspace's, else the plan's, else the default).
func effective(r db.ResolveLimitsRow) (int64, string) {
	switch {
	case r.WorkspaceValue.Valid:
		return r.WorkspaceValue.Int64, LimitFromWorkspace
	case r.PlanValue.Valid:
		return r.PlanValue.Int64, LimitFromPlan
	}
	return r.Value, LimitFromDefault
}

// planLimits: the limits of a plan, and of a workspace on it when workspaceID is set (its own values replace the
// plan's). Every limit check reads it through planState; one query.
func (s *Service) planLimits(ctx context.Context, q *db.Queries, planID, workspaceID string) (PlanLimits, error) {
	rows, err := q.ResolveLimits(ctx, db.ResolveLimitsParams{PlanID: planID, WorkspaceID: workspaceID})
	if err != nil {
		return PlanLimits{}, err
	}
	var l PlanLimits
	for _, r := range rows {
		v, _ := effective(r)
		setLimit(&l, r.Key, v)
	}
	return l, nil
}

// withLimits: plans with their limits and the feature list made from them (GET /plans, the pricing table).
func (s *Service) withLimits(ctx context.Context, plans []Plan) ([]Plan, error) {
	for i := range plans {
		l, err := s.planLimits(ctx, s.Q, plans[i].ID, "")
		if err != nil {
			return nil, err
		}
		plans[i].Limits = l
		plans[i].Features = planFeatures(l)
	}
	return plans, nil
}

// planFeatures: the plan's selling points, the numbers from its limits so they always match what it allows.
func planFeatures(l PlanLimits) []string {
	count := func(n int64, what string) string {
		if n == Unlimited {
			return "Unlimited " + what
		}
		return fmt.Sprintf("%s %s", thousands(n), what)
	}
	ai := "Unlimited AI"
	if l.AIMonthlyCents != Unlimited {
		ai = fmt.Sprintf("$%s shared AI allowance a month", dollars(l.AIMonthlyCents))
	}
	linkedin := count(l.LinkedInPostsPerPeriod, "LinkedIn posts a month")
	if l.LinkedInPostsPerPeriod != 0 && l.LinkedInVideosPerPeriod != Unlimited {
		linkedin += fmt.Sprintf(", %s with video", thousands(l.LinkedInVideosPerPeriod))
	}
	storage := "Unlimited storage"
	if l.StorageBytes != Unlimited {
		storage = sizeText(l.StorageBytes) + " storage"
	}
	if l.MaxUploadBytes != Unlimited {
		storage += ", " + sizeText(l.MaxUploadBytes) + " per file"
	}
	export := "Data export without files"
	if l.DataExport {
		export = "Backups and full data export with files"
	}
	return []string{
		count(l.Channels, "connected channels"),
		count(l.PostsPerMonth, "new posts a month"),
		"Calendar, queues and recurring schedules",
		"Design, video and audio editors",
		"Publishing and performance analytics",
		ai,
		count(l.XPostsPerPeriod, "X credits a month"),
		linkedin,
		"MCP, REST API, SDKs and CLI",
		storage,
		export,
	}
}

func thousands(n int64) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func dollars(cents int64) string {
	if cents%100 == 0 {
		return fmt.Sprint(cents / 100)
	}
	return fmt.Sprintf("%.2f", float64(cents)/100)
}

func sizeText(b int64) string {
	if b >= gb && b%gb == 0 {
		return fmt.Sprintf("%d GB", b/gb)
	}
	return fmt.Sprintf("%d MB", b>>20)
}

// ---- admin ----

// AdminLimit: one limit, its default and each plan's own value (absent: the plan takes the default).
type AdminLimit struct {
	Key         string           `json:"key"`
	Description string           `json:"description"`
	Default     int64            `json:"default"`
	Plans       map[string]int64 `json:"plans"`
}

type AdminLimits struct {
	Plans  []string     `json:"plans"`
	Limits []AdminLimit `json:"limits"`
}

// AdminListLimits: every limit with its default and plan values (the admin panel's Limits page).
func (s *Service) AdminListLimits(ctx context.Context) (AdminLimits, error) {
	out := AdminLimits{Plans: []string{}, Limits: []AdminLimit{}}
	for _, p := range Plans {
		out.Plans = append(out.Plans, p.ID)
	}
	ls, err := s.Q.ListLimits(ctx)
	if err != nil {
		return out, err
	}
	ps, err := s.Q.ListLimitPlans(ctx)
	if err != nil {
		return out, err
	}
	for _, l := range ls {
		a := AdminLimit{Key: l.Key, Description: l.Description, Default: l.Value, Plans: map[string]int64{}}
		for _, p := range ps {
			if p.LimitKey == l.Key {
				a.Plans[p.PlanID] = p.Value
			}
		}
		out.Limits = append(out.Limits, a)
	}
	return out, nil
}

// checkLimitValue: -1 (unlimited) or more; data_export is 0 or 1.
func checkLimitValue(key string, v int64) error {
	if key == LimitDataExport && v != 0 && v != 1 {
		return invalid("value", "data_export is 1 (on) or 0 (off)")
	}
	if v < Unlimited {
		return invalid("value", "-1 (unlimited) or more")
	}
	return nil
}

func (s *Service) knownLimit(ctx context.Context, key string) error {
	ls, err := s.Q.ListLimits(ctx)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(ls, func(l db.Limit) bool { return l.Key == key }) {
		return notFound("limit")
	}
	return nil
}

// AdminSetLimit sets a limit's default (plan "") or a plan's value; a nil value removes the plan's own value, so it
// takes the default again. Every change is in the audit log with the value before.
func (s *Service) AdminSetLimit(ctx context.Context, a Admin, key, plan string, value *int64) error {
	if err := s.knownLimit(ctx, key); err != nil {
		return err
	}
	if plan != "" {
		if _, ok := PlanByID(plan); !ok {
			return invalid("plan", "unknown plan")
		}
	}
	if value != nil {
		if err := checkLimitValue(key, *value); err != nil {
			return err
		}
	}
	before, err := s.AdminListLimits(ctx)
	if err != nil {
		return err
	}
	var old any
	for _, l := range before.Limits {
		if l.Key == key {
			old = l.Default
			if plan != "" {
				if v, ok := l.Plans[plan]; ok {
					old = v
				} else {
					old = nil
				}
			}
		}
	}
	now := s.now()
	switch {
	case plan == "" && value == nil:
		return invalid("value", "a default can't be removed")
	case plan == "":
		_, err = s.W.SetLimitDefault(ctx, db.SetLimitDefaultParams{Value: *value, UpdatedAt: now, Key: key})
	case value == nil:
		_, err = s.W.DeleteLimitPlan(ctx, db.DeleteLimitPlanParams{LimitKey: key, PlanID: plan})
	default:
		err = s.W.UpsertLimitPlan(ctx, db.UpsertLimitPlanParams{LimitKey: key, PlanID: plan, Value: *value, UpdatedAt: now})
	}
	if err != nil {
		return err
	}
	return s.AdminAudit(ctx, a, "set_limit", "", key, map[string]any{"limit": key, "plan": plan, "from": old, "to": value})
}

// WorkspaceLimit: a workspace's value of one limit and where it comes from; Plan is what its plan gives.
type WorkspaceLimit struct {
	Key         string `json:"key"`
	Description string `json:"description"`
	Value       int64  `json:"value"`
	Source      string `json:"source"`
	Plan        int64  `json:"plan"`
	Note        string `json:"note"`
}

// AdminWorkspaceLimits: a workspace's limits before Stripe add-ons, each with its source.
func (s *Service) AdminWorkspaceLimits(ctx context.Context, wsID string) ([]WorkspaceLimit, error) {
	ws, err := s.adminWorkspace(ctx, wsID)
	if err != nil {
		return nil, err
	}
	rows, err := s.Q.ResolveLimits(ctx, db.ResolveLimitsParams{PlanID: ws.Plan, WorkspaceID: wsID})
	if err != nil {
		return nil, err
	}
	out := make([]WorkspaceLimit, 0, len(rows))
	for _, r := range rows {
		v, src := effective(r)
		plan := r.Value
		if r.PlanValue.Valid {
			plan = r.PlanValue.Int64
		}
		out = append(out, WorkspaceLimit{Key: r.Key, Description: r.Description, Value: v, Source: src, Plan: plan,
			Note: r.WorkspaceNote.String})
	}
	return out, nil
}

// AdminSetWorkspaceLimit gives a workspace its own value of a limit (replacing its plan's; Stripe add-ons still
// add to it), or with a nil value takes it away. note says why (a deal, a test account).
func (s *Service) AdminSetWorkspaceLimit(ctx context.Context, a Admin, wsID, key string, value *int64, note string) error {
	if _, err := s.adminWorkspace(ctx, wsID); err != nil {
		return err
	}
	if err := s.knownLimit(ctx, key); err != nil {
		return err
	}
	before, err := s.AdminWorkspaceLimits(ctx, wsID)
	if err != nil {
		return err
	}
	var old any
	for _, l := range before {
		if l.Key == key && l.Source == LimitFromWorkspace {
			old = l.Value
		}
	}
	if value == nil {
		_, err = s.W.DeleteLimitWorkspace(ctx, db.DeleteLimitWorkspaceParams{LimitKey: key, WorkspaceID: wsID})
	} else {
		if err := checkLimitValue(key, *value); err != nil {
			return err
		}
		err = s.W.UpsertLimitWorkspace(ctx, db.UpsertLimitWorkspaceParams{LimitKey: key, WorkspaceID: wsID, Value: *value,
			Note: truncateRunes(note, 500), UpdatedAt: s.now()})
	}
	if err != nil {
		return err
	}
	return s.AdminAudit(ctx, a, "set_workspace_limit", wsID, key,
		map[string]any{"limit": key, "from": old, "to": value, "note": note})
}
