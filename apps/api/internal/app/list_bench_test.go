package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/store"
)

// go test -run '^$' -bench ListItems -benchmem ./internal/app/
func BenchmarkListItems(b *testing.B) {
	ctx := context.Background()
	dir := b.TempDir()
	database, err := store.Open(ctx, dir+"/bench.db")
	if err != nil {
		b.Fatal(err)
	}
	defer database.Close()
	s := New(database, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{UploadDir: dir + "/uploads"})
	if err := s.EnsureLocalWorkspace(ctx); err != nil {
		b.Fatal(err)
	}
	p, err := s.LocalPrincipal(ctx)
	if err != nil {
		b.Fatal(err)
	}
	cols := make([]string, 4)
	for i := range cols {
		name := fmt.Sprint("Bench ", i)
		c, err := s.CreateCollection(ctx, p, CollectionInput{Name: &name})
		if err != nil {
			b.Fatal(err)
		}
		cols[i] = c.ID
	}
	for i := range 2000 {
		title := fmt.Sprint("item ", i)
		if _, err := s.CreateItem(ctx, p, cols[i%len(cols)], CreateItemInput{itemFields: itemFields{Title: &title}}); err != nil {
			b.Fatal(err)
		}
	}

	for _, f := range []ItemFilter{
		{Limit: 50},
		{Limit: 50, CollectionID: cols[1]},
		{Limit: 50, Status: "pending", Sort: "decided_desc"},
		{Limit: 50, Q: "item"},
	} {
		b.Run(fmt.Sprintf("col=%t,status=%s,sort=%s,q=%s", f.CollectionID != "", f.Status, f.Sort, f.Q), func(b *testing.B) {
			for b.Loop() {
				page, err := s.ListItems(ctx, p, f)
				if err != nil || len(page.Data) != 50 {
					b.Fatalf("%v %d", err, len(page.Data))
				}
			}
		})
	}
}
