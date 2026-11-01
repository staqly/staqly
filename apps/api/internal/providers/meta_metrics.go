package providers

import (
	"context"
	"errors"
	"net/url"
	"strings"
)

// Meta's platforms report a published post's numbers: Facebook its reactions, comments and shares (the Page token's
// pages_read_engagement), Instagram and Threads their insights (instagram_manage_insights, threads_manage_insights).
var (
	_ MetricsReader = Facebook{}
	_ MetricsReader = Instagram{}
	_ MetricsReader = Threads{}
)

// metaInsights reads /{id}/insights: each metric's lifetime value by name. Media insights answer values[0], newer
// metrics total_value.
func metaInsights(ctx context.Context, g metaGraph, token, id, metrics string) (map[string]int64, error) {
	var out struct {
		Data []struct {
			Name   string `json:"name"`
			Values []struct {
				Value int64 `json:"value"`
			} `json:"values"`
			Total *struct {
				Value int64 `json:"value"`
			} `json:"total_value"`
		} `json:"data"`
	}
	if err := g.get(ctx, token, "/"+url.PathEscape(id)+"/insights", url.Values{"metric": {metrics}}, &out); err != nil {
		return nil, err
	}
	m := map[string]int64{}
	for _, d := range out.Data {
		switch {
		case d.Total != nil:
			m[d.Name] = d.Total.Value
		case len(d.Values) > 0:
			m[d.Name] = d.Values[0].Value
		}
	}
	return m, nil
}

// num is a metric the platform reported, nil when it did not.
func num(m map[string]int64, name string) *int64 {
	if v, ok := m[name]; ok {
		return &v
	}
	return nil
}

func (Facebook) Metrics(ctx context.Context, c Credentials, externalID string) (Metrics, error) {
	fields := "reactions.summary(total_count).limit(0),comments.summary(total_count).limit(0)"
	post := strings.Contains(externalID, "_") // a Page post is <page>_<post>; videos and reels are a bare video id, without shares
	if post {
		fields += ",shares"
	}
	var out struct {
		Reactions struct {
			Summary struct {
				Total int64 `json:"total_count"`
			} `json:"summary"`
		} `json:"reactions"`
		Comments struct {
			Summary struct {
				Total int64 `json:"total_count"`
			} `json:"summary"`
		} `json:"comments"`
		Shares struct {
			Count int64 `json:"count"`
		} `json:"shares"`
	}
	if err := fbGraph.get(ctx, c["access_token"], "/"+url.PathEscape(externalID), url.Values{"fields": {fields}}, &out); err != nil {
		return Metrics{}, err
	}
	m := Metrics{Likes: &out.Reactions.Summary.Total, Replies: &out.Comments.Summary.Total}
	if post { // Facebook leaves shares out while there are none
		m.Shares = &out.Shares.Count
	}
	return m, nil
}

func (Instagram) Metrics(ctx context.Context, c Credentials, externalID string) (Metrics, error) {
	token := c["access_token"]
	var media struct {
		Likes    *int64 `json:"like_count"`
		Comments *int64 `json:"comments_count"`
		Product  string `json:"media_product_type"`
	}
	if err := igGraph.get(ctx, token, "/"+url.PathEscape(externalID), url.Values{"fields": {"like_count,comments_count,media_product_type"}}, &media); err != nil {
		return Metrics{}, err
	}
	m := Metrics{Likes: media.Likes, Replies: media.Comments}
	metrics := "views,shares,saved"
	if media.Product == "STORY" {
		metrics = "views,shares"
	}
	in, err := metaInsights(ctx, igGraph, token, externalID, metrics)
	var f *Failure
	if errors.As(err, &f) && f.Kind == ErrRejected {
		// A channel connected without instagram_manage_insights, or a story gone after a day: likes and comments only.
		return m, nil
	}
	if err != nil {
		return Metrics{}, err
	}
	m.Impressions, m.Shares, m.Bookmarks = num(in, "views"), num(in, "shares"), num(in, "saved")
	return m, nil
}

func (Threads) Metrics(ctx context.Context, c Credentials, externalID string) (Metrics, error) {
	in, err := metaInsights(ctx, thGraph, c["access_token"], externalID, "views,likes,replies,reposts,quotes,shares")
	if err != nil {
		return Metrics{}, err
	}
	return Metrics{Impressions: num(in, "views"), Likes: num(in, "likes"), Replies: num(in, "replies"),
		Reposts: num(in, "reposts"), Quotes: num(in, "quotes"), Shares: num(in, "shares")}, nil
}
