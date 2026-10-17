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
}
