package app

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// seedPosts writes n published posts with one target each on two channels (raw inserts: the shape of a
// workspace after a year or two of posting).
func seedPosts(b *testing.B, s *Service, p Principal, n int) (string, []string) {
	b.Helper()
	ctx := context.Background()
	col, err := s.CreateCollection(ctx, p, CollectionInput{Name: ptrTo("Bench")})
	if err != nil {
		b.Fatal(err)
	}
	var chans []string
	for i := range 2 {
		ch, err := s.ConnectChannel(ctx, p, ChannelInput{Provider: "custom", Name: fmt.Sprintf("W%d", i)})
		if err != nil {
			b.Fatal(err)
		}
		chans = append(chans, ch.ID)
	}
	base := time.Now().Add(-365 * 24 * time.Hour)
	if err := s.tx(ctx, func(q *db.Queries) error {
		tx := s.sqlTx(q)
		for i := range n {
			id := fmt.Sprintf("post_bench%06d", i)
			at := formatTime(base.Add(time.Duration(i) * time.Minute * 20))
			if _, err := tx.ExecContext(ctx, `INSERT INTO posts (id, workspace_id, collection_id, content, status, publish_at, published_at, created_at, updated_at)
				VALUES (?, ?, ?, 'x', 'published', ?, ?, ?, ?)`, id, p.WorkspaceID, col.ID, at, at, at, at); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO post_targets (post_id, channel_id, status, published_at, updated_at) VALUES (?, ?, 'published', ?, ?)`,
				id, chans[i%2], at, at); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		b.Fatal(err)
	}
	return col.ID, chans
}

func BenchmarkListChannelsActivity(b *testing.B) {
	s, p, _ := publishTest(b)
	seedPosts(b, s, p, 20000)
	ctx := context.Background()
	b.ResetTimer()
	for b.Loop() {
		if _, err := s.ListChannels(ctx, p, ChannelFilter{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkListPostsPage(b *testing.B) {
	s, p, _ := publishTest(b)
	seedPosts(b, s, p, 20000)
	ctx := context.Background()
	b.ResetTimer()
	for b.Loop() {
		if _, err := s.ListPosts(ctx, p, PostFilter{Limit: 50, Sort: "publish_desc"}); err != nil {
			b.Fatal(err)
		}
	}
}
