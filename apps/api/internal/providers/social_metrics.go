package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// Numbers of a published post on Bluesky, Mastodon, Reddit, Tumblr, Pinterest and TikTok (MetricsReader), read with
// the channel's own sign-in. A number the platform doesn't give stays nil.

var (
	_ MetricsReader = Bluesky{}
	_ MetricsReader = Mastodon{}
	_ MetricsReader = Reddit{}
	_ MetricsReader = Tumblr{}
	_ MetricsReader = Pinterest{}
	_ MetricsReader = TikTok{}
)

func n64(v int64) *int64 { return &v }

// Metrics: likes, reposts, replies and quotes of the post (app.bsky.feed.getPosts).
func (Bluesky) Metrics(ctx context.Context, c Credentials, externalID string) (Metrics, error) {
	uri, _, _ := strings.Cut(externalID, "|")
	sess, err := blueskyOpen(ctx, c)
	if err != nil {
		return Metrics{}, err
	}
	var out struct {
		Posts []struct {
			Likes   int64 `json:"likeCount"`
			Reposts int64 `json:"repostCount"`
			Replies int64 `json:"replyCount"`
			Quotes  int64 `json:"quoteCount"`
		} `json:"posts"`
	}
	if err := sess.call(ctx, http.MethodGet, "app.bsky.feed.getPosts?uris="+url.QueryEscape(uri), nil, "", &out); err != nil {
		return Metrics{}, err
	}
	if len(out.Posts) == 0 {
		return Metrics{}, &Failure{Kind: ErrRejected, Msg: "Bluesky: the post is not there any more"}
	}
	p := out.Posts[0]
	return Metrics{Likes: n64(p.Likes), Reposts: n64(p.Reposts), Replies: n64(p.Replies), Quotes: n64(p.Quotes)}, nil
}

// Metrics: favourites, boosts and replies of the status.
func (Mastodon) Metrics(ctx context.Context, c Credentials, externalID string) (Metrics, error) {
	instance, token := c["instance"], c["access_token"]
	if instance == "" || token == "" {
		return Metrics{}, &Failure{Kind: ErrReauth, Msg: "Mastodon: no account stored; reconnect the account"}
	}
	var s struct {
		Favourites int64 `json:"favourites_count"`
		Reblogs    int64 `json:"reblogs_count"`
		Replies    int64 `json:"replies_count"`
	}
	if err := mastodonCall(ctx, http.MethodGet, instance, token, "/api/v1/statuses/"+url.PathEscape(externalID), nil, "", "", &s); err != nil {
		return Metrics{}, err
	}
	return Metrics{Likes: n64(s.Favourites), Reposts: n64(s.Reblogs), Replies: n64(s.Replies)}, nil
}

// Metrics: the post's score (upvotes less downvotes) as likes, and its comments as replies (/api/info).
func (Reddit) Metrics(ctx context.Context, c Credentials, externalID string) (Metrics, error) {
	name := externalID
	if !strings.HasPrefix(name, "t3_") {
		name = "t3_" + name
	}
	req, err := redditRequest(ctx, http.MethodGet, redditOAuth+"/api/info?raw_json=1&id="+url.QueryEscape(name), c["access_token"], nil)
	if err != nil {
		return Metrics{}, err
	}
	var out struct {
		Data struct {
			Children []struct {
				Data struct {
					Score    int64 `json:"score"`
					Comments int64 `json:"num_comments"`
				} `json:"data"`
			} `json:"children"`
		} `json:"data"`
	}
	if err := redditDo(ctx, req, &out); err != nil {
		return Metrics{}, err
	}
	if len(out.Data.Children) == 0 {
		return Metrics{}, &Failure{Kind: ErrRejected, Msg: "Reddit: the post is not there any more"}
	}
	d := out.Data.Children[0].Data
	return Metrics{Likes: n64(d.Score), Replies: n64(d.Comments)}, nil
}

// Metrics: the post's notes (likes, reblogs and replies together) as likes, the only number Tumblr gives.
func (Tumblr) Metrics(ctx context.Context, c Credentials, externalID string) (Metrics, error) {
	blog := c["blog"]
	if blog == "" || c["access_token"] == "" {
		return Metrics{}, &Failure{Kind: ErrReauth, Msg: "Tumblr: no blog stored; reconnect the account"}
	}
	req, err := tumblrRequest(ctx, http.MethodGet, "/v2/blog/"+url.PathEscape(blog)+"/posts?notes_info=false&id="+url.QueryEscape(externalID),
		c["access_token"], nil, "")
	if err != nil {
		return Metrics{}, err
	}
	var out struct {
		Response struct {
			Posts []struct {
				Notes int64 `json:"note_count"`
			} `json:"posts"`
		} `json:"response"`
	}
	if err := tumblrDo(ctx, req, &out); err != nil {
		return Metrics{}, err
	}
	if len(out.Response.Posts) == 0 {
		return Metrics{}, &Failure{Kind: ErrRejected, Msg: "Tumblr: the post is not there any more"}
	}
	return Metrics{Likes: n64(out.Response.Posts[0].Notes)}, nil
}

// Metrics: the pin's lifetime impressions, saves, pin clicks and link clicks (pin_metrics, pins:read).
func (Pinterest) Metrics(ctx context.Context, c Credentials, externalID string) (Metrics, error) {
	var out struct {
		PinMetrics map[string]map[string]json.Number `json:"pin_metrics"`
	}
	if err := pinterestGet(ctx, c["access_token"], "/v5/pins/"+url.PathEscape(externalID)+"?pin_metrics=true", &out); err != nil {
		return Metrics{}, err
	}
	m := out.PinMetrics["lifetime_metrics"]
	if m == nil {
		m = out.PinMetrics["all_time"]
	}
	get := func(k string) *int64 {
		v, err := m[k].Int64()
		if err != nil {
			return nil
		}
		return &v
	}
	return Metrics{Impressions: get("impression"), Bookmarks: get("save"), Clicks: get("pin_click"), LinkClicks: get("outbound_click"),
		Replies: get("comment"), Likes: get("reaction")}, nil
}

// Metrics: views, likes, comments and shares of the video (video.list). A post TikTok hasn't given a public id yet
// (only you can see it, or it is still under review) has none to read.
func (TikTok) Metrics(ctx context.Context, c Credentials, externalID string) (Metrics, error) {
	if externalID == "" || strings.HasPrefix(externalID, "v_") || strings.Contains(externalID, "~") {
		return Metrics{}, &Failure{Kind: ErrRejected, Msg: "TikTok hasn't made the video public yet, so it has no numbers to read"}
	}
	b, _ := json.Marshal(map[string]any{"filters": map[string]any{"video_ids": []string{externalID}}})
	req, err := tiktokRequest(ctx, http.MethodPost, "/v2/video/query/?fields=id,view_count,like_count,comment_count,share_count", c["access_token"], b)
	if err != nil {
		return Metrics{}, err
	}
	var out struct {
		Data struct {
			Videos []struct {
				Views    int64 `json:"view_count"`
				Likes    int64 `json:"like_count"`
				Comments int64 `json:"comment_count"`
				Shares   int64 `json:"share_count"`
			} `json:"videos"`
		} `json:"data"`
	}
	if err := tiktokDo(ctx, req, &out); err != nil {
		return Metrics{}, err
	}
	if len(out.Data.Videos) == 0 {
		return Metrics{}, &Failure{Kind: ErrRejected, Msg: "TikTok: the video is not there any more"}
	}
	v := out.Data.Videos[0]
	return Metrics{Impressions: n64(v.Views), Likes: n64(v.Likes), Replies: n64(v.Comments), Shares: n64(v.Shares)}, nil
}
