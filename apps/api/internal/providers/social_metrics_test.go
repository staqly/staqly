package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// metricsServer answers every path with body and records the path and query asked.
func metricsServer(t *testing.T, body any) (*httptest.Server, *string) {
	t.Helper()
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.Method + " " + r.URL.Path + "?" + r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &asked
}

func TestSocialMetrics(t *testing.T) {
	ctx := context.Background()
	is := func(p *int64, want int64) bool { return p != nil && *p == want }

	t.Run("bluesky", func(t *testing.T) {
		_, creds := useFakeBluesky(t)
		m, err := (Bluesky{}).Metrics(ctx, creds, "at://did:plc:jane/app.bsky.feed.post/p1|cid-p1")
		if err != nil || !is(m.Likes, 5) || !is(m.Reposts, 2) || !is(m.Replies, 3) || !is(m.Quotes, 1) {
			t.Fatalf("%+v %v", m, err)
		}
		if _, err := (Bluesky{}).Metrics(ctx, creds, "at://did:plc:jane/app.bsky.feed.post/gone|x"); err == nil {
			t.Fatal("a post that is gone")
		}
	})
	t.Run("mastodon", func(t *testing.T) {
		srv, asked := metricsServer(t, map[string]int64{"favourites_count": 4, "reblogs_count": 2, "replies_count": 1})
		m, err := (Mastodon{}).Metrics(ctx, Credentials{"instance": srv.URL, "access_token": "t"}, "110")
		if err != nil || !is(m.Likes, 4) || !is(m.Reposts, 2) || !is(m.Replies, 1) || *asked != "GET /api/v1/statuses/110?" {
			t.Fatalf("%+v %v %s", m, err, *asked)
		}
	})
	t.Run("reddit", func(t *testing.T) {
		srv, asked := metricsServer(t, map[string]any{"data": map[string]any{"children": []any{map[string]any{"data": map[string]int64{"score": 12, "num_comments": 3}}}}})
		old := redditOAuth
		redditOAuth = srv.URL
		defer func() { redditOAuth = old }()
		m, err := (Reddit{}).Metrics(ctx, Credentials{"access_token": "t"}, "t3_ab")
		if err != nil || !is(m.Likes, 12) || !is(m.Replies, 3) || *asked != "GET /api/info?raw_json=1&id=t3_ab" {
			t.Fatalf("%+v %v %s", m, err, *asked)
		}
	})
	t.Run("tumblr", func(t *testing.T) {
		srv, asked := metricsServer(t, map[string]any{"meta": map[string]int{"status": 200}, "response": map[string]any{"posts": []any{map[string]int64{"note_count": 9}}}})
		old := tumblrAPI
		tumblrAPI = srv.URL
		defer func() { tumblrAPI = old }()
		m, err := (Tumblr{}).Metrics(ctx, Credentials{"access_token": "t", "blog": "b1"}, "77")
		if err != nil || !is(m.Likes, 9) || *asked != "GET /v2/blog/b1/posts?notes_info=false&id=77" {
			t.Fatalf("%+v %v %s", m, err, *asked)
		}
	})
	t.Run("pinterest", func(t *testing.T) {
		srv, asked := metricsServer(t, map[string]any{"id": "5", "pin_metrics": map[string]any{"lifetime_metrics": map[string]int64{
			"impression": 100, "save": 7, "pin_click": 12, "outbound_click": 3}}})
		old := pinterestAPI
		pinterestAPI = srv.URL
		defer func() { pinterestAPI = old }()
		m, err := (Pinterest{}).Metrics(ctx, Credentials{"access_token": "t"}, "5")
		if err != nil || !is(m.Impressions, 100) || !is(m.Bookmarks, 7) || !is(m.Clicks, 12) || !is(m.LinkClicks, 3) || m.Likes != nil ||
			*asked != "GET /v5/pins/5?pin_metrics=true" {
			t.Fatalf("%+v %v %s", m, err, *asked)
		}
	})
	t.Run("tiktok", func(t *testing.T) {
		srv, asked := metricsServer(t, map[string]any{"error": map[string]string{"code": "ok"}, "data": map[string]any{"videos": []any{
			map[string]any{"id": "7300", "view_count": 500, "like_count": 40, "comment_count": 6, "share_count": 2}}}})
		old := tiktokAPI
		tiktokAPI = srv.URL
		defer func() { tiktokAPI = old }()
		m, err := (TikTok{}).Metrics(ctx, Credentials{"access_token": "t"}, "7300")
		if err != nil || !is(m.Impressions, 500) || !is(m.Likes, 40) || !is(m.Replies, 6) || !is(m.Shares, 2) ||
			*asked != "POST /v2/video/query/?fields=id,view_count,like_count,comment_count,share_count" {
			t.Fatalf("%+v %v %s", m, err, *asked)
		}
		if _, err := (TikTok{}).Metrics(ctx, Credentials{"access_token": "t"}, "v_pub_file~v2-1.77"); err == nil {
			t.Fatal("a publish id has no numbers to read")
		}
	})
}
