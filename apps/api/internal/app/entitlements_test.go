package app

import (
	"reflect"
	"testing"
)

func TestApplyEntitlements(t *testing.T) {
	plus, _ := PlanByID("plus")
	business := UnlockedPlan
	starter, _ := PlanByID("starter")
	pack := func(key, name string, md map[string]string) Entitlement {
		return Entitlement{LookupKey: key, Name: name, Metadata: md}
	}
	cases := []struct {
		name    string
		plan    Plan
		ents    []Entitlement
		check   func(Plan) bool
		bonuses int
	}{
		{"none: the plan as it is", plus, nil, func(p Plan) bool { return reflect.DeepEqual(p, plus) }, 0},
		{"a delta adds", plus, []Entitlement{pack("channels_extra_10", "Channels pack", map[string]string{"staqly_limit_channels": "+10"})},
			func(p Plan) bool { return p.Limits.Channels == 25 && p.BaseLimits.Channels == 15 }, 1},
		{"deltas add up across features", plus, []Entitlement{
			pack("channels_extra_10", "Channels pack", map[string]string{"staqly_limit_channels": "+10"}),
			pack("deal_acme", "ACME deal", map[string]string{"staqly_limit_channels": "5", "staqly_limit_storage_gb": "+100"})},
			func(p Plan) bool { return p.Limits.Channels == 30 && p.Limits.StorageBytes == 110*gb }, 3},
		{"unlimited lifts a limit", starter, []Entitlement{pack("x_unlimited", "X unlimited", map[string]string{"staqly_limit_x_posts_per_period": "unlimited"})},
			func(p Plan) bool { return p.Limits.XPostsPerPeriod == Unlimited }, 1},
		{"LinkedIn allowances rise", starter, []Entitlement{pack("linkedin_extra", "LinkedIn pack", map[string]string{
			"staqly_limit_linkedin_posts_per_period": "+100", "staqly_limit_linkedin_videos_per_period": "+20"})},
			func(p Plan) bool {
				return p.Limits.LinkedInPostsPerPeriod == 200 && p.Limits.LinkedInVideosPerPeriod == 25 && p.BaseLimits.LinkedInPostsPerPeriod == 100
			}, 2},
		{"an unlimited limit stays unlimited", business, []Entitlement{pack("channels_extra_10", "Channels pack", map[string]string{"staqly_limit_channels": "+10"})},
			func(p Plan) bool { return p.Limits.Channels == Unlimited && p.BaseLimits == nil }, 0},
		{"a capability switches on", starter, []Entitlement{pack("backups", "Backups", map[string]string{"staqly_enable": "data_export"})},
			func(p Plan) bool { return p.Limits.DataExport && !p.BaseLimits.DataExport }, 1},
		{"malformed and unknown metadata is ignored", plus, []Entitlement{pack("odd", "Odd", map[string]string{
			"staqly_limit_channels": "lots", "staqly_limit_seats": "+3", "staqly_limit_alerts": "-5", "staqly_enable": "teleport"})},
			func(p Plan) bool { return p.Limits == plus.Limits && p.BaseLimits == nil }, 0},
		{"a feature without metadata is listed, nothing changes", plus, []Entitlement{pack("ai_pack", "AI pack", nil)},
			func(p Plan) bool { return p.HasEntitlement("ai_pack") && p.Limits == plus.Limits }, 0},
		{"upload size in MB", starter, []Entitlement{pack("big_files", "Big files", map[string]string{"staqly_limit_max_upload_mb": "+75"})},
			func(p Plan) bool { return p.Limits.MaxUploadBytes == 100<<20 }, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ApplyEntitlements(c.plan, c.ents)
			if !c.check(got) || len(got.LimitBonuses) != c.bonuses {
				t.Fatalf("%+v (bonuses %+v)", got.Limits, got.LimitBonuses)
			}
			if c.plan.ID != UnlockedPlan.ID && c.plan.Limits != plansByID(c.plan.ID).Limits {
				t.Fatal("the shared plan must not change")
			}
		})
	}
	if got := ApplyEntitlements(plus, []Entitlement{pack("channels_extra_10", "Channels pack", map[string]string{"staqly_limit_channels": "+10"})}); got.LimitBonuses[0] != (LimitBonus{Limit: LimitChannels, Add: 10, From: "Channels pack"}) {
		t.Fatalf("bonus: %+v", got.LimitBonuses)
	}
	if parseEntitlements("not json") != nil || parseEntitlements("") != nil || len(parseEntitlements(`[{"lookup_key":"a"}]`)) != 1 {
		t.Fatal("parseEntitlements")
	}
}

func plansByID(id string) Plan {
	p, _ := PlanByID(id)
	return p
}
