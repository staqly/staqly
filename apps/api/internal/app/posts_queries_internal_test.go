package app

import (
	"archive/zip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// countingDB counts the statements a service sends through s.Q.
type countingDB struct {
	db.DBTX
	n atomic.Int64
}

func (c *countingDB) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	c.n.Add(1)
	return c.DBTX.ExecContext(ctx, q, args...)
}

func (c *countingDB) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	c.n.Add(1)
	return c.DBTX.QueryContext(ctx, q, args...)
}

func (c *countingDB) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	c.n.Add(1)
	return c.DBTX.QueryRowContext(ctx, q, args...)
}

// seedPostsWithMedia writes n posts with three files each (and one more in a thread reply); waiting of them
// are custom targets still publishing (GET /work). Raw inserts, like seedPosts.
func seedPostsWithMedia(t testing.TB, s *Service, p Principal, n, waiting int) {
	t.Helper()
	ctx := context.Background()
	col, err := s.CreateCollection(ctx, p, CollectionInput{Name: ptrTo("Media bench")})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := s.ConnectChannel(ctx, p, ChannelInput{Provider: "custom", Name: "Media worker"})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(-365 * 24 * time.Hour)
	if err := s.tx(ctx, func(q *db.Queries) error {
		tx := s.sqlTx(q)
		for i := range n {
			id := fmt.Sprintf("post_media%06d", i)
			at := formatTime(base.Add(time.Duration(i) * time.Minute * 20))
			var media []string
			for j := range 4 {
				mid := fmt.Sprintf("med_%06d_%d", i, j)
				media = append(media, mid)
				if _, err := tx.ExecContext(ctx, `INSERT INTO media (id, workspace_id, storage_name, original_name, mime_type, size_bytes, sha256, created_at)
					VALUES (?, ?, ?, 'a.png', 'image/png', 1, 'x', ?)`, mid, p.WorkspaceID, mid+".png", at); err != nil {
					return err
				}
			}
			thread := mustJSON([]ThreadPart{{Content: "more", Media: media[3:]}})
			status, target := "published", "published"
			if i < waiting {
				status, target = "publishing", "publishing"
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO posts (id, workspace_id, collection_id, content, media, thread, status, publish_at, published_at, created_at, updated_at)
				VALUES (?, ?, ?, 'x', ?, ?, ?, ?, ?, ?, ?)`, id, p.WorkspaceID, col.ID, mustJSON(media[:3]), thread, status, at, at, at, at); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO post_targets (post_id, channel_id, status, published_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
				id, ch.ID, target, at, at); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func countQueries(s *Service) *countingDB {
	c := &countingDB{DBTX: s.DB}
	s.Q = db.New(c)
	return c
}

// A page of posts and the requested work read their media, targets and posts in a fixed number of
// statements, not one per file or per waiting target.
func TestPostListsQueryCount(t *testing.T) {
	s, p, _ := publishTest(t)
	seedPostsWithMedia(t, s, p, 120, 30)
	c := countQueries(s)
	ctx := context.Background()

	page, err := s.ListPosts(ctx, p, PostFilter{Limit: 50, Sort: "publish_desc"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Data) != 50 {
		t.Fatalf("got %d posts", len(page.Data))
	}
	for _, post := range page.Data {
		if len(post.Media) == 0 {
			continue // publishTest's own post
		}
		if len(post.MediaItems) != 3 || len(post.Thread) != 1 || len(post.Thread[0].MediaItems) != 1 {
			t.Fatalf("media of %s: %+v %+v", post.ID, post.MediaItems, post.Thread)
		}
	}
	t.Logf("ListPosts (50 posts, 200 files): %d statements", c.n.Load())
	if n := c.n.Load(); n > 10 {
		t.Fatalf("ListPosts sent %d statements", n)
	}

	c.n.Store(0)
	work, err := s.ListRequestedWork(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(work) != 30 {
		t.Fatalf("got %d work items", len(work))
	}
	for _, w := range work {
		if w.Post == nil || len(w.Post.MediaItems) != 3 || w.Channel == nil || w.Channel.Name != "Media worker" {
			t.Fatalf("work item: %+v", w)
		}
	}
	t.Logf("ListRequestedWork (30 targets): %d statements", c.n.Load())
	if n := c.n.Load(); n > 12 {
		t.Fatalf("ListRequestedWork sent %d statements", n)
	}
}

// go test -run '^$' -bench 'PostsWithMedia|RequestedWork' -benchmem ./internal/app
func BenchmarkListPostsWithMedia(b *testing.B) {
	s, p, _ := publishTest(b)
	seedPostsWithMedia(b, s, p, 3000, 0)
	ctx := context.Background()
	b.ResetTimer()
	for b.Loop() {
		if _, err := s.ListPosts(ctx, p, PostFilter{Limit: 50, Sort: "publish_desc"}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkListRequestedWork(b *testing.B) {
	s, p, _ := publishTest(b)
	seedPostsWithMedia(b, s, p, 3000, 200)
	ctx := context.Background()
	b.ResetTimer()
	for b.Loop() {
		if _, err := s.ListRequestedWork(ctx, p); err != nil {
			b.Fatal(err)
		}
	}
}

// The workspace export reads its posts a page at a time, not one post (and its files) at a time.
func TestExportPostsQueryCount(t *testing.T) {
	s, p, _ := publishTest(t)
	seedPostsWithMedia(t, s, p, 450, 0)
	c := countQueries(s)
	if err := s.fillExportZip(context.Background(), zip.NewWriter(io.Discard), p.WorkspaceID, false); err != nil {
		t.Fatal(err)
	}
	t.Logf("export (451 posts, 1800 files): %d statements", c.n.Load())
	if n := c.n.Load(); n > 60 {
		t.Fatalf("export sent %d statements", n)
	}
}

// go test -run '^$' -bench ExportPosts -benchmem ./internal/app
func BenchmarkExportPosts(b *testing.B) {
	s, p, _ := publishTest(b)
	seedPostsWithMedia(b, s, p, 3000, 0)
	ctx := context.Background()
	b.ResetTimer()
	for b.Loop() {
		if err := s.fillExportZip(ctx, zip.NewWriter(io.Discard), p.WorkspaceID, false); err != nil {
			b.Fatal(err)
		}
	}
}
