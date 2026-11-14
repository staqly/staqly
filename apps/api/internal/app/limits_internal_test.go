package app

import (
	"context"
	"slices"
	"strings"
	"testing"
)

// Limits come from the database: the default, a plan's own value over it, a workspace's over that; Stripe add-ons
// still add to the workspace's value. The pricing text follows the numbers.
func TestLimitsResolveDefaultPlanWorkspace(t *testing.T) {
	s, p, _ := publishTest(t)
	s.Cfg.Mode = ModeCloud // local installs are unlimited
	ctx := context.Background()
	a := Admin{Email: "ops@staqly.ai"}
	if err := s.SetPlan(ctx, p.WorkspaceID, "plus", ""); err != nil {
		t.Fatal(err)
	}
	plan := func() Plan {
		got, err := s.planFor(ctx, s.Q, p.WorkspaceID)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if l := plan().Limits; l.Channels != 15 || l.Webhooks != 10 || !l.DataExport || l.StorageBytes != 10*gb {
		t.Fatalf("plus as seeded: %+v", l)
	}
	n := int64(7)
	if err := s.AdminSetLimit(ctx, a, LimitChannels, "plus", &n); err != nil {
		t.Fatal(err)
	}
	if plan().Limits.Channels != 7 {
		t.Fatal("the plan's value applies")
	}
	if err := s.AdminSetLimit(ctx, a, LimitChannels, "plus", nil); err != nil {
		t.Fatal(err)
	}
	def := int64(4)
	if err := s.AdminSetLimit(ctx, a, LimitChannels, "", &def); err != nil {
		t.Fatal(err)
	}
	if plan().Limits.Channels != 4 {
		t.Fatal("without the plan's own value, the default applies")
	}
	w := int64(Unlimited)
	if err := s.AdminSetWorkspaceLimit(ctx, a, p.WorkspaceID, LimitChannels, &w, "deal"); err != nil {
		t.Fatal(err)
	}
	got := plan()
	if got.Limits.Channels != Unlimited || !slices.Contains(got.Features, "Unlimited connected channels") {
		t.Fatalf("the workspace's value wins: %+v %v", got.Limits, got.Features)
	}
	rows, err := s.AdminWorkspaceLimits(ctx, p.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(rows, func(r WorkspaceLimit) bool { return r.Key == LimitChannels })
	if rows[i].Source != LimitFromWorkspace || rows[i].Plan != 4 || rows[i].Note != "deal" {
		t.Fatalf("admin view: %+v", rows[i])
	}
	bad := int64(-2)
	if s.AdminSetLimit(ctx, a, LimitChannels, "", &bad) == nil || s.AdminSetLimit(ctx, a, "nope", "", &def) == nil {
		t.Fatal("invalid values and unknown limits are refused")
	}
	if !hasAudit(t, s, "set_limit") || !hasAudit(t, s, "set_workspace_limit") {
		t.Fatal("changes are in the audit log")
	}
}

func TestPlanFeaturesFollowTheLimits(t *testing.T) {
	f := strings.Join(planFeatures(PlanLimits{Channels: 15, PostsPerMonth: 1500, AIMonthlyCents: 250, XPostsPerPeriod: 100,
		LinkedInPostsPerPeriod: 300, LinkedInVideosPerPeriod: 15, StorageBytes: 10 * gb, MaxUploadBytes: 100 << 20, DataExport: true}), "|")
	for _, want := range []string{"15 connected channels", "1,500 new posts a month", "$2.50 shared AI allowance a month",
		"300 LinkedIn posts a month, 15 with video", "10 GB storage, 100 MB per file", "Backups and full data export with files"} {
		if !strings.Contains(f, want) {
			t.Errorf("missing %q in %s", want, f)
		}
	}
}

func hasAudit(t *testing.T, s *Service, action string) bool {
	t.Helper()
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM admin_audit WHERE action = ?`, action).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}
