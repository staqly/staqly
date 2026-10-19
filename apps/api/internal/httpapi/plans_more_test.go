package httpapi_test

import (
	"context"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// The operator's overview counts workspaces per plan, trials vs suspended
// paid plans, recent signups and this month's usage.
func TestOperatorStats(t *testing.T) {
	e := newEnv(t) // Pro trial
	c := e.collection("Stats")
	e.post(c.ID, map[string]any{"content": "a"})
	e.post(c.ID, map[string]any{"content": "b"})
	free := newWorkspace(e, "Free one")
	free.free()
	suspended := newWorkspace(e, "Lapsed")
	if err := e.svc.SetPlan(context.Background(), workspaceID(suspended), "pro", "2020-01-01"); err != nil {
		t.Fatal(err)
	}
	comped := newWorkspace(e, "Comped")
	comped.paid() // Pro with no end date: neither trialing nor suspended
	fc := free.collection("F")
	free.post(fc.ID, map[string]any{"content": "c"})

	st, err := e.svc.OperatorStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Workspaces != 4 || st.ByPlan["pro"] != 3 || st.ByPlan["free"] != 1 {
		t.Fatalf("by plan: %+v", st)
	}
	if st.Trialing != 1 || st.Suspended != 1 {
		t.Fatalf("trialing/suspended: %+v", st)
	}
	if st.Signups7d != 4 || st.Signups30d != 4 || st.Period != time.Now().UTC().Format("2006-01") {
		t.Fatalf("signups/period: %+v", st)
	}
	if u := st.Usage[app.LimitPostsPerMonth]; u.Total != 3 || u.Workspaces != 2 {
		t.Fatalf("posts usage: %+v", st.Usage)
	}

	// Signups age out of the windows.
	e.svc.Now = func() time.Time { return time.Now().Add(10 * 24 * time.Hour) }
	defer func() { e.svc.Now = time.Now }()
	st, _ = e.svc.OperatorStats(context.Background())
	if st.Signups7d != 0 || st.Signups30d != 4 || st.Trialing != 0 || st.Suspended != 2 {
		t.Fatalf("10 days later: %+v", st)
	}
}

// SetPlan (operator CLI) validates plan ids and dates and reports unknown workspaces.
func TestSetPlanValidation(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	ws := workspaceID(e)
	for _, bad := range [][2]string{{"enterprise", ""}, {app.UnlockedPlan.ID, ""}, {"pro", "next week"}} {
		if err := e.svc.SetPlan(ctx, ws, bad[0], bad[1]); err == nil {
			t.Errorf("SetPlan(%q, %q) accepted", bad[0], bad[1])
		}
	}
	if err := e.svc.SetPlan(ctx, "ws_missing", "free", ""); err == nil {
		t.Error("unknown workspace accepted")
	}
	if err := e.svc.SetPlan(ctx, ws, "pro", "2999-01-01"); err != nil {
		t.Fatal(err)
	}
	var u app.Usage
	e.do(nil, "GET", "/v1/workspace/usage", e.key, nil, 200, &u)
	if u.Plan.ID != "pro" || u.Suspended || u.PlanExpiresAt == nil || (*u.PlanExpiresAt)[:4] != "2999" {
		t.Fatalf("comped until 2999: %+v", u)
	}
}
