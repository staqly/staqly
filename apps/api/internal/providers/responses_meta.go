package providers

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"time"
)

var (
	_ ResponseReader = Facebook{}
	_ ResponseReader = Instagram{}
	_ ResponseReader = Threads{}
)

const maxResponses = 500

// metaResponses walks a Graph comment list and keeps what parse returns, newer than since, at most 500.
// parse may return several responses (Instagram nests replies); the walk is not cut mid-page, only the result.
func metaResponses(ctx context.Context, g metaGraph, token, path, fields string, extra url.Values, since time.Time, parse func(json.RawMessage) []Response) ([]Response, error) {
	var out []Response
	q := url.Values{"fields": {fields}, "limit": {"100"}}
	for k, v := range extra {
		q[k] = v
	}
	err := metaEach(ctx, g, token, path, q, func(raw json.RawMessage) {
		for _, r := range parse(raw) {
			if len(out) < maxResponses && (since.IsZero() || r.CreatedAt.After(since)) {
				out = append(out, r)
			}
		}
	})
	return out, err
}

func (Facebook) Responses(ctx context.Context, c Credentials, externalID string, since time.Time) ([]Response, error) {
	page, _, _ := strings.Cut(externalID, "_")
	return metaResponses(ctx, fbGraph, c["access_token"], "/"+url.PathEscape(externalID)+"/comments",
		"id,from,message,created_time,like_count,parent{id},permalink_url", url.Values{"filter": {"stream"}}, since, func(raw json.RawMessage) []Response {
			var d struct {
				ID, Message string
				From        struct{ ID, Name string }
				Created     string `json:"created_time"`
				Likes       *int64 `json:"like_count"`
				Parent      struct{ ID string }
				Permalink   string `json:"permalink_url"`
			}
			if json.Unmarshal(raw, &d) != nil || d.ID == "" {
				return nil
			}
			return []Response{{ID: d.ID, ParentID: d.Parent.ID, Author: d.From.Name, Text: d.Message, URL: d.Permalink,
				CreatedAt: metaTime(d.Created), Likes: d.Likes, Own: d.From.ID != "" && d.From.ID == page}}
		})
}

type igComment struct {
	ID, Username, Text string
	From               struct{ ID string }
	Timestamp          string
	Likes              *int64 `json:"like_count"`
	Replies            struct {
		Data []igComment
	}
}

func (Instagram) Responses(ctx context.Context, c Credentials, externalID string, since time.Time) ([]Response, error) {
	flat := "id,username,text,timestamp,like_count,from"
	return metaResponses(ctx, igGraph, c["access_token"], "/"+url.PathEscape(externalID)+"/comments",
		flat+",replies{"+flat+"}", nil, since, func(raw json.RawMessage) []Response {
			var d igComment
			if json.Unmarshal(raw, &d) != nil || d.ID == "" {
				return nil
			}
			conv := func(x igComment, parent string) Response {
				return Response{ID: x.ID, ParentID: parent, AuthorHandle: "@" + x.Username, Text: x.Text, CreatedAt: metaTime(x.Timestamp),
					Likes: x.Likes, Own: x.From.ID != "" && x.From.ID == c["user_id"]}
			}
			out := []Response{conv(d, "")}
			for _, r := range d.Replies.Data {
				out = append(out, conv(r, d.ID))
			}
			return out
		})
}

func (Threads) Responses(ctx context.Context, c Credentials, externalID string, since time.Time) ([]Response, error) {
	return metaResponses(ctx, thGraph, c["access_token"], "/"+url.PathEscape(externalID)+"/conversation",
		"id,text,username,timestamp,permalink,is_reply_owned_by_me,replied_to{id}", nil, since, func(raw json.RawMessage) []Response {
			var d struct {
				ID, Text, Username, Permalink string
				Timestamp                     string
				Own                           bool                `json:"is_reply_owned_by_me"`
				RepliedTo                     struct{ ID string } `json:"replied_to"`
			}
			if json.Unmarshal(raw, &d) != nil || d.ID == "" {
				return nil
			}
			parent := d.RepliedTo.ID
			if parent == externalID {
				parent = ""
			}
			return []Response{{ID: d.ID, ParentID: parent, AuthorHandle: "@" + d.Username, Text: d.Text, URL: d.Permalink,
				CreatedAt: metaTime(d.Timestamp), Own: d.Own}}
		})
}

// metaTime reads Meta's timestamps ("2026-01-01T00:00:00+0000", no colon in the offset).
func metaTime(s string) time.Time {
	t, err := time.Parse("2006-01-02T15:04:05-0700", s)
	if err != nil {
		t, _ = time.Parse(time.RFC3339, s)
	}
	return t
}
