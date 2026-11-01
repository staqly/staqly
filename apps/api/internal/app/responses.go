package app

import (
	"context"
	"encoding/json"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// PostResponses: what people answered under a published post, per channel and overall, with the AI's reading of it.
type PostResponses struct {
	Overall  *ResponseInsight   `json:"overall"`
	Channels []ChannelResponses `json:"channels"`
}

// ChannelResponses: one channel's responses and their insight. Available is false for a platform staqly can't read.
type ChannelResponses struct {
	ChannelID   string           `json:"channel_id"`
	ChannelName string           `json:"channel_name"`
	Provider    string           `json:"provider"`
	Available   bool             `json:"available"`
	Error       string           `json:"error,omitempty"`
	Insight     *ResponseInsight `json:"insight"`
	Responses   []PostResponse   `json:"responses"`
}

// ResponseInsight: counts by sentiment and the AI's summary.
type ResponseInsight struct {
	Positive   int64    `json:"positive"`
	Neutral    int64    `json:"neutral"`
	Negative   int64    `json:"negative"`
	Summary    string   `json:"summary"`
	Themes     []string `json:"themes"`
	Questions  []string `json:"questions"`
	Status     string   `json:"status"`
	Error      string   `json:"error,omitempty"`
	AnalyzedAt *string  `json:"analyzed_at"`
}

// PostResponse: one stored response.
type PostResponse struct {
	ID           string `json:"id"`
	ParentID     string `json:"parent_id,omitempty"`
	Author       string `json:"author"`
	AuthorHandle string `json:"author_handle,omitempty"`
	Text         string `json:"text"`
	URL          string `json:"url,omitempty"`
	Sentiment    string `json:"sentiment"`
	Likes        *int64 `json:"likes"`
	CreatedAt    string `json:"created_at"`
}

// PostResponses lists a post's responses (all channels, or channelID) with the insights.
func (s *Service) PostResponses(ctx context.Context, p Principal, postID, channelID string) (PostResponses, error) {
	if err := p.require(ScopePostsRead); err != nil {
		return PostResponses{}, err
	}
	if _, err := s.Q.GetPost(ctx, db.GetPostParams{WorkspaceID: p.WorkspaceID, ID: postID}); isNoRows(err) {
		return PostResponses{}, notFound("post")
	} else if err != nil {
		return PostResponses{}, err
	}
	targets, err := s.Q.ListTargets(ctx, postID)
	if err != nil {
		return PostResponses{}, err
	}
	key := db.ListResponsesParams{WorkspaceID: p.WorkspaceID, PostID: postID}
	rows, err := s.Q.ListResponses(ctx, key)
	if err != nil {
		return PostResponses{}, err
	}
	insights, err := s.Q.ListResponseInsights(ctx, db.ListResponseInsightsParams(key))
	if err != nil {
		return PostResponses{}, err
	}
	out := PostResponses{Channels: []ChannelResponses{}}
	byChannel := map[string]*ResponseInsight{}
	failed := map[string]string{}
	for _, in := range insights {
		ri := toResponseInsight(in)
		if in.ChannelID == "" {
			out.Overall = ri
			continue
		}
		byChannel[in.ChannelID] = ri
		if in.Status == "failed" {
			failed[in.ChannelID] = in.Error
		}
	}
	found := false
	for _, t := range targets {
		if t.Status != "published" {
			continue
		}
		if channelID != "" && t.ChannelID != channelID {
			continue
		}
		found = true
		_, can := providerResponseReader(t.Provider)
		ch := ChannelResponses{ChannelID: t.ChannelID, ChannelName: t.ChannelName, Provider: t.Provider, Available: can,
			Error: failed[t.ChannelID], Insight: byChannel[t.ChannelID], Responses: []PostResponse{}}
		for _, r := range rows {
			if r.ChannelID != t.ChannelID {
				continue
			}
			pr := PostResponse{ID: r.ID, ParentID: r.ParentID, Author: r.Author, AuthorHandle: r.AuthorHandle, Text: r.Text,
				URL: r.Url, Sentiment: r.Sentiment, CreatedAt: r.CreatedAt}
			if r.Likes.Valid {
				pr.Likes = &r.Likes.Int64
			}
			ch.Responses = append(ch.Responses, pr)
		}
		out.Channels = append(out.Channels, ch)
	}
	if channelID != "" && !found {
		return PostResponses{}, invalid("channel", channelID+" is not a published channel of this post")
	}
	return out, nil
}

func providerResponseReader(provider string) (providers.ResponseReader, bool) {
	e, ok := providers.Get(provider)
	if !ok {
		return nil, false
	}
	r, ok := e.Provider.(providers.ResponseReader)
	return r, ok
}

func toResponseInsight(in db.PostResponseInsight) *ResponseInsight {
	ri := &ResponseInsight{Positive: in.Positive, Neutral: in.Neutral, Negative: in.Negative, Summary: in.Summary,
		Themes: []string{}, Questions: []string{}, Status: in.Status, Error: in.Error}
	_ = json.Unmarshal([]byte(in.Themes), &ri.Themes)
	_ = json.Unmarshal([]byte(in.Questions), &ri.Questions)
	if ri.Themes == nil {
		ri.Themes = []string{}
	}
	if ri.Questions == nil {
		ri.Questions = []string{}
	}
	if in.AnalyzedAt != "" {
		ri.AnalyzedAt = &in.AnalyzedAt
	}
	return ri
}

// RefreshPostResponses reads the post's responses from the platforms now and returns what is stored.
func (s *Service) RefreshPostResponses(ctx context.Context, p Principal, postID string) (PostResponses, error) {
	if err := p.require(ScopePostsWrite); err != nil {
		return PostResponses{}, err
	}
	cur, err := s.PostResponses(ctx, p, postID, "")
	if err != nil {
		return PostResponses{}, err
	}
	if len(cur.Channels) == 0 {
		return PostResponses{}, &Error{Code: "not_published", Status: 409, Message: "this post has nothing published to read responses from"}
	}
	if err := s.queueResponseReads(ctx, p.WorkspaceID, postID); err != nil {
		return PostResponses{}, err
	}
	return s.PostResponses(ctx, p, postID, "")
}
