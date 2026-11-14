package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// benchListPosts measures the request hot path: auth middleware (API key, optionally created by a
// person, whose role is re-read on every request) plus GET /v1/posts through the real router, on a
// workspace with a few thousand posts, without a network round trip.
//
// go test -run '^$' -bench ListPosts -benchmem ./internal/httpapi
func benchListPosts(b *testing.B, personKey, parallel bool) {
	b.Setenv("RATE_LIMIT_DISABLED", "true") // measure the handler, not the per-key limit
	e := newEnv(b)
	e.paid()
	c := e.collection("Bench")
	ctx := context.Background()
	var ws string
	if err := e.svc.DB.QueryRowContext(ctx, "SELECT workspace_id FROM collections WHERE id = ?", c.ID).Scan(&ws); err != nil {
		b.Fatal(err)
	}
	h := e.srv.Config.Handler
	for i := range 3000 {
		e.post(c.ID, map[string]any{"content": fmt.Sprintf("post %d with some text #tag%d", i, i%7)})
	}
	if personKey {
		// The key belongs to a member: AuthenticateAPIKey intersects its scopes with their current role.
		for _, q := range []string{
			"INSERT INTO users (id, email, created_at, updated_at) VALUES ('usr_bench', 'b@example.com', '2026-01-01', '2026-01-01')",
			"INSERT INTO workspace_members (workspace_id, user_id, role, created_at, updated_at) VALUES ('" + ws + "', 'usr_bench', 'editor', '2026-01-01', '2026-01-01')",
			"UPDATE api_keys SET created_by_user_id = 'usr_bench' WHERE workspace_id = '" + ws + "'",
		} {
			if _, err := e.svc.DB.ExecContext(ctx, q); err != nil {
				b.Fatal(err)
			}
		}
	}
	skipContract.Store(true) // the OpenAPI response check (contract_test.go) would dominate the numbers
	b.Cleanup(func() { skipContract.Store(false) })
	b.ReportAllocs()
	b.ResetTimer()
	get := func() {
		req := httptest.NewRequest("GET", "/v1/posts?limit=50", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("Authorization", "Bearer "+e.key)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			b.Errorf("status %d: %s", rec.Code, rec.Body)
		}
	}
	if parallel {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				get()
			}
		})
		return
	}
	for b.Loop() {
		get()
	}
}

func BenchmarkListPosts(b *testing.B)          { benchListPosts(b, false, false) }
func BenchmarkListPostsPersonKey(b *testing.B) { benchListPosts(b, true, false) }
func BenchmarkListPostsParallel(b *testing.B)  { benchListPosts(b, true, true) }
