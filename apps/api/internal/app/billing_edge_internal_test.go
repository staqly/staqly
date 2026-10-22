package app

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// Was a bug: a suspended (unpaid) cloud workspace still publishes its scheduled posts.
func TestSuspendedWorkspaceDoesNotPublish(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := context.Background()
	s.Cfg.Mode = ModeCloud
	past := formatTime(time.Now().Add(-24 * time.Hour))
	if _, err := s.W.SetWorkspacePlan(ctx, db.SetWorkspacePlanParams{Plan: "starter", PlanExpiresAt: nullStr(past), UpdatedAt: s.now(), ID: p.WorkspaceID}); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckWritable(ctx, p.WorkspaceID); err != ErrSuspended {
		t.Fatalf("expected suspended, got %v", err)
	}
	later := time.Now().Add(2 * time.Hour)
	s.Now = func() time.Time { return later }
	if err := s.runPublishPost(ctx, p.WorkspaceID, post.ID); err != nil {
		t.Fatal(err)
	}
	row, _ := s.Q.GetPostByID(ctx, post.ID)
	t.Logf("post status after the scheduled run in a suspended workspace: %s", row.Status)
	if row.Status != "scheduled" && row.Status != "failed" {
		t.Errorf("suspended workspace publishing: post is %q", row.Status)
	}
}

// Was a bug: billing reconcile always takes the same first 500 ids; churned workspaces never leave that set.
func TestReconcileReachesLiveSubscriptionsFirst(t *testing.T) {
	s, _, _ := publishTest(t)
	ctx := context.Background()
	past := formatTime(time.Now().Add(-48 * time.Hour))
	mk := func(id, status string) {
		if err := s.W.CreateWorkspace(ctx, db.CreateWorkspaceParams{ID: id, Name: id, RecoveryVerifier: id, RecoveryRotatedAt: past, CreatedAt: past, UpdatedAt: past}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.DB.ExecContext(ctx, `UPDATE workspaces SET stripe_customer_id = ?, subscription_status = ?, plan_expires_at = ? WHERE id = ?`,
			"cus_"+id, status, past, id); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 500; i++ {
		mk(fmt.Sprintf("ws_a%04d", i), "canceled") // churned long ago: canceled, period over
	}
	mk("ws_zlost", "active") // a paying workspace whose renewal webhook was lost: plan_expires_at passed
	rows, err := s.Q.ListBillingToReconcile(ctx, nullStr(s.now()))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ID == "ws_zlost" {
			return
		}
	}
	t.Errorf("the workspace with a lost renewal is never reconciled: %d rows, all churned", len(rows))
}
