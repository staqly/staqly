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
		// Analytics: what waits now, and the preview feedback threads.
		{`SELECT SUM(p.status = 'scheduled'), SUM(p.status = 'draft'), MIN(CASE WHEN p.status = 'scheduled' THEN p.publish_at END)
			FROM posts p WHERE p.workspace_id = ? AND p.status IN ('scheduled', 'draft')`, "posts_status"},
		{`SELECT COUNT(*) FROM post_comments c JOIN posts p ON p.id = c.post_id WHERE c.workspace_id = ? AND c.parent_id = ''`,
			"post_comments_workspace"},
		// Analytics: a workspace's finished targets go through its posts, never every workspace's targets.
		{`SELECT COUNT(*) FROM posts p JOIN post_targets t ON t.post_id = p.id
			WHERE p.workspace_id = ? AND t.status IN ('published', 'failed') AND COALESCE(t.published_at, t.updated_at) >= ?`,
			"SEARCH t USING INDEX sqlite_autoindex_post_targets_1"},
	} {
		args := make([]any, strings.Count(c.query, "?"))
		for i := range args {
			args[i] = "x"
		}
		rows, err := db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+c.query, args...)
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
