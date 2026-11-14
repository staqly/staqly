package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"sync"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/store"
	"github.com/staqly/staqly/apps/api/internal/store/storetest"
)

// Two server processes (each with its own connection pool) on one PostgreSQL database claim the same
// queue at once: every job runs exactly once.
func TestJobsTwoProcessesOnPostgres(t *testing.T) {
	if !storetest.Postgres() {
		t.Skip("needs STAQLY_TEST_DATABASE_URL")
	}
	first, err := storetest.Open(t, "")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	var schema string
	if err := first.QueryRow("SELECT current_schema()").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(os.Getenv("STAQLY_TEST_DATABASE_URL"))
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	second, err := store.OpenPostgres(context.Background(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	a, b := New(first, log, Config{}), New(second, log, Config{})
	a.Instance, b.Instance = "process-a", "process-b"
	var mu sync.Mutex
	runs := map[string]int{}
	testJobKinds["test_count"] = func(_ context.Context, j jobArgs) error {
		mu.Lock()
		runs[j.Key]++
		mu.Unlock()
		return nil
	}
	t.Cleanup(func() { delete(testJobKinds, "test_count") })
	const n = 60
	for i := range n {
		if err := a.tx(context.Background(), func(q *db.Queries) error {
			return a.enqueue(context.Background(), q, "ws_test", "test_count", fmt.Sprintf("count:%d", i), map[string]string{}, a.Now(), 3)
		}); err != nil {
			t.Fatal(err)
		}
	}
	// Both servers run their queue (River's producers fetch from one table) until every job is done.
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for _, s := range []*Service{a, b} {
		wg.Add(1)
		go func() { defer wg.Done(); s.RunJobs(ctx) }()
	}
	waitFor(t, "every job", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(runs) == n
	})
	cancel()
	wg.Wait()
	if len(runs) != n {
		t.Fatalf("%d of %d jobs ran", len(runs), n)
	}
	for k, c := range runs {
		if c != 1 {
			t.Fatalf("%s ran %d times", k, c)
		}
	}
	var done int
	if err := first.QueryRow("SELECT COUNT(*) FROM river_job WHERE state = 'completed'").Scan(&done); err != nil || done != n {
		t.Fatalf("done %d (%v)", done, err)
	}
}
