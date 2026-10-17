package db

import (
	"context"
	"strings"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/store"
)

// The worker polls run on every pass and every commit: they must search an
// index, not scan the items/events tables.
func TestHotQueriesUseIndexes(t *testing.T) {
	ctx := context.Background()
	database, err := store.Open(ctx, t.TempDir()+"/plan.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	plan := func(query string, args ...any) string {
		t.Helper()
		rows, err := database.QueryContext(ctx, "EXPLAIN QUERY PLAN "+query, args...)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			out = append(out, detail)
		}

		return strings.Join(out, "\n")
	}

	stale := plan(staleProcessingItems, "2026-01-01")
	if !strings.Contains(stale, "items_processing") || strings.Contains(stale, "SCAN i") {
		t.Errorf("StaleProcessingItems plan:\n%s", stale)
	}
	dlv := plan(listDeliveries, "ws", nil, nil, "itm", 50)
	if strings.Contains(dlv, "SCAN ev") {
		t.Errorf("ListDeliveries item filter scans events:\n%s", dlv)
	}

	// Lists, the tag cloud and the flow view read an index, never the items table; sorted lists walk it in order
	// (the counts still group the few distinct values in a small temporary B-tree).
	for name, q := range map[string]string{
		"decided_desc": "SELECT i.* FROM items i WHERE i.workspace_id = 'ws' AND (COALESCE(i.decided_at, ''), i.id) < ('x', 'y') ORDER BY COALESCE(i.decided_at, '') DESC, i.id DESC LIMIT 51",
		"title_asc":    "SELECT i.* FROM items i WHERE i.workspace_id = 'ws' ORDER BY i.title COLLATE NOCASE ASC, i.id ASC LIMIT 51",
		"tag filter":   "SELECT i.* FROM items i WHERE i.workspace_id = 'ws' AND i.id IN (SELECT item_id FROM item_tags WHERE workspace_id = 'ws' AND tag IN ('a')) ORDER BY i.created_at DESC, i.id DESC LIMIT 51",
		"ListTags":     listTags,
		"FlowStates":   flowStates,
		"FlowSources":  flowSources,
	} {
		p := plan(strings.NewReplacer("?1", "'ws'", "?2", "''", "?", "'ws'").Replace(q))
		if strings.Contains(p, "SCAN i\n") || strings.HasSuffix(p, "SCAN i") || strings.Contains(p, "SCAN items") ||
			(strings.HasSuffix(name, "_desc") || strings.HasSuffix(name, "_asc")) && strings.Contains(p, "TEMP B-TREE") {
			t.Errorf("%s plan:\n%s", name, p)
		}
	}
}
