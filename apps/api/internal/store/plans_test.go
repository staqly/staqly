package store_test

import (
	"context"
	"strings"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/store"
)

// Queries run on hot paths use their index, not a scan of every workspace's rows (SQLite's query plan).
func TestHotQueriesUseTheirIndex(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, t.TempDir()+"/plans.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, c := range []struct{ query, index string }{
		// repeat_post: the copy a repeating post already made.
		{"SELECT id FROM posts WHERE repeat_of = ? AND repeat_of <> '' LIMIT 1", "posts_repeat_of"},
		// The per-link comment limit on public preview pages.
		{"SELECT COUNT(*) FROM post_comments WHERE preview_id = ? AND created_at > ?", "post_comments_preview"},
	} {
		rows, err := db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+c.query, "x", "x")
		if err != nil {
			t.Fatal(err)
		}
		var plan []string
		for rows.Next() {
			var id, parent, notused int
			var detail string
			if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
				t.Fatal(err)
			}
			plan = append(plan, detail)
		}
		rows.Close()
		if p := strings.Join(plan, "; "); !strings.Contains(p, c.index) {
			t.Errorf("%s\nplan %q, want index %s", c.query, p, c.index)
		}
	}
}
