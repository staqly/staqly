package app

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
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

// Was a bug: a refused Stripe request was logged with its query string (customer ids, the promotion code someone
// typed) and Stripe's message, which may quote them.
func TestStripeFailureLogHasNoQueryOrMessage(t *testing.T) {
	s, _, _ := publishTest(t)
	stripe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(400)
		fmt.Fprint(w, `{"error":{"type":"invalid_request_error","code":"resource_missing","message":"No such promotion code: 'SECRET20' for ana@example.com"}}`)
	}))
	defer stripe.Close()
	var logged bytes.Buffer
	s.Log = slog.New(slog.NewTextHandler(&logged, nil))
	s.Cfg.Billing = BillingConfig{SecretKey: "sk_test_log", WebhookSecret: "whsec_log", APIBase: stripe.URL}
	if err := s.stripe(context.Background(), "GET", "/v1/promotion_codes?code=SECRET20&customer=cus_9", nil, nil); err == nil {
		t.Fatal("the request should fail")
	}
	got := logged.String()
	for _, secret := range []string{"SECRET20", "cus_9", "ana@example.com"} {
		if strings.Contains(got, secret) {
			t.Errorf("the log has %q: %s", secret, got)
		}
	}
	for _, want := range []string{"path=/v1/promotion_codes ", "code=resource_missing", "type=invalid_request_error", "status=400"} {
		if !strings.Contains(got, want) {
			t.Errorf("the log lacks %q: %s", want, got)
		}
	}
}

// Was a bug: a storage limit in megabytes read "0.0 GB of storage".
func TestDescribeLimitStorageUnit(t *testing.T) {
	for max, want := range map[int64]string{5 << 20: "5 MB of storage", 3 * gb / 2: "1.5 GB of storage", 100 * gb: "100.0 GB of storage"} {
		if got := describeLimit(LimitStorage, max); got != want {
			t.Errorf("%d bytes: %q, want %q", max, got, want)
		}
	}
}
