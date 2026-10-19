package httpapi_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// benchDeliveryBacklog fills one workspace with n due webhook deliveries (a receiver that has been down
// for a while), next to idleWorkspaces with nothing due, and measures the worker's poll query, which runs
// every PollInterval and after every finished delivery.
// go test -run '^$' -bench DueDeliveries -benchmem ./internal/httpapi
func benchDeliveryBacklog(b *testing.B, n, idleWorkspaces int) {
	e := newEnv(b)
	var hook app.Webhook
	e.do(nil, "POST", "/v1/webhooks", e.admin, map[string]any{"url": "http://127.0.0.1:1/hook"}, 201, &hook)
	ws := workspaceID(e)
	ctx := context.Background()
	tx, err := e.svc.DB.BeginTx(ctx, nil)
	if err != nil {
		b.Fatal(err)
	}
	now := time.Now().UTC()
	for i := range idleWorkspaces { // workspaces with nothing due (most of a cloud instance)
		if _, err := tx.ExecContext(ctx, `INSERT INTO workspaces (id, name, recovery_verifier, recovery_rotated_at, created_at, updated_at)
			VALUES (?, 'idle', '', '', '', '')`, fmt.Sprintf("ws_idle%06d", i)); err != nil {
			b.Fatal(err)
		}
	}
	for i := range n {
		ts := now.Add(-time.Duration(n-i) * time.Second).Format("2006-01-02T15:04:05.000000Z")
		ev := fmt.Sprintf("evt_b%07d", i)
		if _, err := tx.ExecContext(ctx, `INSERT INTO events (id, workspace_id, type, actor_type, actor_id, actor_name, data, created_at)
			VALUES (?, ?, 'post.created', 'system', 'bench', 'bench', '{}', ?)`, ev, ws, ts); err != nil {
			b.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO webhook_deliveries (id, workspace_id, webhook_id, event_id, event_type, next_attempt_at, created_at)
			VALUES (?, ?, ?, ?, 'post.created', ?, ?)`, fmt.Sprintf("dlv_b%07d", i), ws, hook.ID, ev, ts, ts); err != nil {
			b.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	pollAt := now.Add(time.Hour).Format("2006-01-02T15:04:05.000000Z")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		due, err := e.svc.Q.DueDeliveries(ctx, pollAt, 2, 50)
		if err != nil || len(due) != 2 {
			b.Fatalf("due = %d, %v", len(due), err)
		}
	}
}

func BenchmarkDueDeliveries1k(b *testing.B)  { benchDeliveryBacklog(b, 1_000, 0) }
func BenchmarkDueDeliveries50k(b *testing.B) { benchDeliveryBacklog(b, 50_000, 0) }

// Many workspaces, a few of them with something due.
func BenchmarkDueDeliveries1kWorkspaces10k(b *testing.B) { benchDeliveryBacklog(b, 1_000, 10_000) }
