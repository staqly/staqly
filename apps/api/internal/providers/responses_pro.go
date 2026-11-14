package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const proRespMax = 500 // the most responses one read returns (see ResponseReader)

var (
	_ ResponseReader = YouTube{}
	_ ResponseReader = LinkedIn{}
	_ ResponseReader = LinkedInPage{}
	_ ResponseReader = Reddit{}
	_ ResponseReader = Slack{}
)

func proRespInt(n int64) *int64 { return &n }

// Responses reads the video's comments and their replies (YouTube returns a thread's first replies inline).
func (YouTube) Responses(ctx context.Context, c Credentials, externalID string, since time.Time) ([]Response, error) {
	type comment struct {
		ID      string `json:"id"`
		Snippet struct {
			Author    string `json:"authorDisplayName"`
			ChannelID struct {
				Value string `json:"value"`
			} `json:"authorChannelId"`
			ChannelURL string    `json:"authorChannelUrl"`
			Text       string    `json:"textOriginal"`
			Likes      int64     `json:"likeCount"`
			Published  time.Time `json:"publishedAt"`
			ParentID   string    `json:"parentId"`
		} `json:"snippet"`
	}
	var out []Response
	add := func(cm comment) {
		s := cm.Snippet
		if !since.IsZero() && !s.Published.After(since) {
			return
		}
		out = append(out, Response{ID: cm.ID, ParentID: s.ParentID, Author: s.Author, Text: s.Text,
			URL: "https://www.youtube.com/watch?v=" + externalID + "&lc=" + cm.ID, CreatedAt: s.Published,
			Likes: proRespInt(s.Likes), Own: c["channel_id"] != "" && s.ChannelID.Value == c["channel_id"]})
	}
	for page, token := 0, ""; page < 5 && len(out) < proRespMax; page++ {
		q := url.Values{"part": {"snippet,replies"}, "videoId": {externalID}, "maxResults": {"100"}, "order": {"time"}, "textFormat": {"plainText"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var res struct {
			Next  string `json:"nextPageToken"`
			Items []struct {
				Snippet struct {
					Top comment `json:"topLevelComment"`
				} `json:"snippet"`
				Replies struct {
					Comments []comment `json:"comments"`
				} `json:"replies"`
			} `json:"items"`
		}
		if err := googleGet(ctx, "YouTube", c["access_token"], youtubeAPI+"/youtube/v3/commentThreads?"+q.Encode(), &res); err != nil {
			return nil, err
		}
		for _, it := range res.Items {
			add(it.Snippet.Top)
			for _, r := range it.Replies.Comments {
				add(r)
			}
		}
		if token = res.Next; token == "" {
			break
		}
	}
	return out[:min(len(out), proRespMax)], nil
}

func (LinkedIn) Responses(ctx context.Context, c Credentials, externalID string, since time.Time) ([]Response, error) {
	own := ""
	if c["person_id"] != "" {
		own = "urn:li:person:" + c["person_id"]
	}
	return linkedinResponses(ctx, c["access_token"], own, externalID, since)
}

func (LinkedInPage) Responses(ctx context.Context, c Credentials, externalID string, since time.Time) ([]Response, error) {
	own := ""
	if c["organization_id"] != "" {
		own = "urn:li:organization:" + c["organization_id"]
	}
	return linkedinResponses(ctx, c["access_token"], own, externalID, since)
}

// linkedinResponses reads the comments under a post URN (LinkedIn nests replies: each comment names its parent).
func linkedinResponses(ctx context.Context, token, own, urn string, since time.Time) ([]Response, error) {
	var out []Response
	for start := 0; start < proRespMax; start += 100 {
		req, err := linkedinJSON(ctx, token, http.MethodGet,
			linkedinAPI+"/rest/socialActions/"+linkedinURN(urn)+"/comments?count=100&start="+strconv.Itoa(start), nil)
		if err != nil {
			return nil, err
		}
		var res struct {
			Elements []struct {
				URN     string `json:"$URN"`
				ID      string `json:"id"`
				Actor   string `json:"actor"`
				Parent  string `json:"parentComment"`
				Message struct {
					Text string `json:"text"`
				} `json:"message"`
				Created struct {
					Time int64 `json:"time"`
				} `json:"created"`
				Likes struct {
					Total *int64 `json:"totalLikes"`
				} `json:"likesSummary"`
			} `json:"elements"`
		}
		if _, err := linkedinDo(ctx, req, &res); err != nil {
			if f, ok := err.(*Failure); ok && f.Kind == ErrRejected && (strings.HasPrefix(f.Msg, "LinkedIn refused it") || strings.Contains(f.Msg, "hasn't enabled comments")) {
				return nil, &Failure{Kind: ErrRejected, Msg: "LinkedIn: reading comments needs the app approved for the Community Management API (" + f.Msg + ")"}
			}
			return nil, err
		}
		for _, e := range res.Elements {
			t := time.UnixMilli(e.Created.Time).UTC()
			if !since.IsZero() && !t.After(since) {
				continue
			}
			id := e.URN
			if id == "" {
				id = e.ID
			}
			out = append(out, Response{ID: id, ParentID: e.Parent, Author: e.Actor, Text: e.Message.Text, URL: linkedinPostURL(urn),
				CreatedAt: t, Likes: e.Likes.Total, Own: own != "" && e.Actor == own})
		}
		if len(res.Elements) < 100 {
			break
		}
	}
	return out[:min(len(out), proRespMax)], nil
}

// Responses reads the post's comment tree, flattened; "more" stubs (collapsed branches) are skipped.
// An empty externalID (a post with no Reddit id) has nothing to read.
// redditNode is a comment listing; a comment's "replies" is "" when it has none, so it decodes leniently.
type redditNode struct {
	Data struct {
		Children []struct {
			Kind string `json:"kind"`
			Data struct {
				ID        string          `json:"id"`
				ParentID  string          `json:"parent_id"`
				Author    string          `json:"author"`
				Body      string          `json:"body"`
				Score     *int64          `json:"score"`
				Created   float64         `json:"created_utc"`
				Permalink string          `json:"permalink"`
				Replies   json.RawMessage `json:"replies"`
			} `json:"data"`
		} `json:"children"`
	} `json:"data"`
}

func (Reddit) Responses(ctx context.Context, c Credentials, externalID string, since time.Time) ([]Response, error) {
	id := strings.TrimPrefix(externalID, "t3_")
	if id == "" {
		return nil, nil
	}
	req, err := redditRequest(ctx, http.MethodGet, redditOAuth+"/comments/"+url.PathEscape(id)+"?raw_json=1&limit=500&sort=new", c["access_token"], nil)
	if err != nil {
		return nil, err
	}
	var tree []redditNode
	if err := redditDo(ctx, req, &tree); err != nil {
		return nil, err
	}
	var out []Response
	var walk func(n redditNode)
	walk = func(n redditNode) {
		for _, ch := range n.Data.Children {
			if ch.Kind != "t1" || len(out) >= proRespMax {
				continue
			}
			d := ch.Data
			t := time.Unix(int64(d.Created), 0).UTC()
			if since.IsZero() || t.After(since) {
				parent := ""
				if p, ok := strings.CutPrefix(d.ParentID, "t1_"); ok {
					parent = p
				}
				out = append(out, Response{ID: d.ID, ParentID: parent, Author: d.Author, AuthorHandle: "u/" + d.Author, Text: d.Body,
					URL: "https://www.reddit.com" + d.Permalink, CreatedAt: t, Likes: d.Score, Own: c["username"] != "" && strings.EqualFold(d.Author, c["username"])})
			}
			var sub redditNode
			if json.Unmarshal(d.Replies, &sub) == nil {
				walk(sub)
			}
		}
	}
	if len(tree) > 1 {
		walk(tree[1])
	}
	return out, nil
}

// Responses reads the replies in the thread of every message the post made ("<channel>:<ts>", comma-joined); the
// message itself, the first of each thread, is the post and is skipped.
func (Slack) Responses(ctx context.Context, c Credentials, externalID string, since time.Time) ([]Response, error) {
	var out []Response
	for _, part := range slackRoots(c, externalID) {
		var res struct {
			Messages []struct {
				TS        string `json:"ts"`
				User      string `json:"user"`
				Username  string `json:"username"`
				BotID     string `json:"bot_id"`
				Text      string `json:"text"`
				Reactions []struct {
					Count int64 `json:"count"`
				} `json:"reactions"`
			} `json:"messages"`
		}
		q := url.Values{"channel": {part.channel}, "ts": {part.ts}, "limit": {"200"}}
		req, err := slackRequest(ctx, c["access_token"], "conversations.replies?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		if err := slackDo(ctx, req, &res); err != nil {
			return nil, err
		}
		for _, m := range res.Messages {
			secs, perr := strconv.ParseFloat(m.TS, 64)
			if m.TS == part.ts || perr != nil {
				continue
			}
			t := time.UnixMicro(int64(secs * 1e6)).UTC()
			if !since.IsZero() && !t.After(since) {
				continue
			}
			var likes int64
			for _, r := range m.Reactions {
				likes += r.Count
			}
			author := m.User
			if author == "" {
				author = m.Username
			}
			out = append(out, Response{ID: part.channel + ":" + m.TS, Author: author, Text: m.Text, CreatedAt: t, Likes: &likes, Own: m.BotID != ""})
		}
	}
	return out[:min(len(out), proRespMax)], nil
}
