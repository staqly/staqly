package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var (
	_ ResponseReader = X{}
	_ ResponseReader = Bluesky{}
	_ ResponseReader = Mastodon{}
)

// Responses searches the conversation (replies of the last 7 days: X's recent search is all a basic plan reads).
func (X) Responses(ctx context.Context, c Credentials, externalID string, since time.Time) ([]Response, error) {
	if c["access_token"] == "" {
		return nil, &Failure{Kind: ErrReauth, Msg: "X: no account stored; reconnect the account"}
	}
	var out []Response
	token := ""
	for page := 0; page < 5 && len(out) < maxResponses; page++ {
		q := url.Values{"query": {"conversation_id:" + externalID}, "max_results": {"100"}, "expansions": {"author_id"},
			"tweet.fields": {"created_at,author_id,public_metrics,referenced_tweets"}, "user.fields": {"username,name"}}
		if token != "" {
			q.Set("next_token", token)
		}
		var res struct {
			Data []struct {
				ID, Text string
				AuthorID string    `json:"author_id"`
				Created  time.Time `json:"created_at"`
				Metrics  struct {
					Likes int64 `json:"like_count"`
				} `json:"public_metrics"`
				Refs []struct{ Type, ID string } `json:"referenced_tweets"`
			}
			Includes struct {
				Users []struct{ ID, Name, Username string }
			}
			Meta struct {
				Next string `json:"next_token"`
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, xAPI+"/2/tweets/search/recent?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c["access_token"])
		if err := xSearch(ctx, req, &res); err != nil {
			return nil, err
		}
		users := map[string][2]string{}
		for _, u := range res.Includes.Users {
			users[u.ID] = [2]string{u.Name, u.Username}
		}
		for _, d := range res.Data {
			if d.ID == externalID || (!since.IsZero() && !d.Created.After(since)) {
				continue
			}
			parent := ""
			for _, r := range d.Refs {
				if r.Type == "replied_to" && r.ID != externalID {
					parent = r.ID
				}
			}
			u := users[d.AuthorID]
			likes := d.Metrics.Likes
			r := Response{ID: d.ID, ParentID: parent, Author: u[0], Text: d.Text, CreatedAt: d.Created, Likes: &likes,
				Own: d.AuthorID != "" && d.AuthorID == c["user_id"]}
			if u[1] != "" {
				r.AuthorHandle, r.URL = "@"+u[1], "https://x.com/"+u[1]+"/status/"+d.ID
			}
			if len(out) < maxResponses {
				out = append(out, r)
			}
		}
		if token = res.Meta.Next; token == "" {
			break
		}
	}
	return out, nil
}

// xSearch is xDo, except that X's answer for an app without paid access gets its own explanation.
func xSearch(ctx context.Context, req *http.Request, out any) error {
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		return fmt.Errorf("X could not be reached: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode/100 == 2:
		return json.Unmarshal(raw, out)
	case resp.StatusCode == http.StatusForbidden && strings.Contains(string(raw), "client-forbidden"):
		return &Failure{Kind: ErrRejected, Msg: "X: reading replies needs a paid X API plan"}
	}
	return xError(resp.StatusCode, string(raw))
}

type bskyNode struct {
	Post struct {
		URI    string `json:"uri"`
		Author struct {
			DID         string `json:"did"`
			Handle      string `json:"handle"`
			DisplayName string `json:"displayName"`
		} `json:"author"`
		Record struct {
			Text      string    `json:"text"`
			CreatedAt time.Time `json:"createdAt"`
		} `json:"record"`
		Likes int64 `json:"likeCount"`
	} `json:"post"`
	Replies []bskyNode `json:"replies"`
}

// Responses flattens the post's thread (getPostThread, depth 100); each reply names the post it answers.
func (Bluesky) Responses(ctx context.Context, c Credentials, externalID string, since time.Time) ([]Response, error) {
	uri, _, _ := strings.Cut(externalID, "|")
	sess, err := blueskyOpen(ctx, c)
	if err != nil {
		return nil, err
	}
	var out struct {
		Thread bskyNode `json:"thread"`
	}
	if err := sess.call(ctx, http.MethodGet, "app.bsky.feed.getPostThread?depth=100&parentHeight=0&uri="+url.QueryEscape(uri), nil, "", &out); err != nil {
		return nil, err
	}
	var rs []Response
	var walk func(n bskyNode, parent string)
	walk = func(n bskyNode, parent string) {
		for _, r := range n.Replies {
			p := r.Post
			if p.URI == "" { // blocked or deleted: nothing to read
				continue
			}
			if len(rs) < maxResponses && (since.IsZero() || p.Record.CreatedAt.After(since)) {
				likes := p.Likes
				rs = append(rs, Response{ID: p.URI, ParentID: parent, Author: p.Author.DisplayName, AuthorHandle: "@" + p.Author.Handle,
					Text: p.Record.Text, URL: blueskyApp + "/profile/" + p.Author.Handle + "/post/" + p.URI[strings.LastIndex(p.URI, "/")+1:],
					CreatedAt: p.Record.CreatedAt, Likes: &likes, Own: p.Author.DID == sess.did})
			}
			walk(r, p.URI)
		}
	}
	walk(out.Thread, "")
	return rs, nil
}

var (
	mastoBreakRe = regexp.MustCompile(`(?i)<br\s*/?>|</p>`)
	mastoTagRe   = regexp.MustCompile(`<[^>]*>`)
)

// mastodonText turns a status's HTML into plain text.
func mastodonText(s string) string {
	return strings.TrimSpace(html.UnescapeString(mastoTagRe.ReplaceAllString(mastoBreakRe.ReplaceAllString(s, "\n"), "")))
}

// Responses reads the status's descendants (/api/v1/statuses/{id}/context).
func (Mastodon) Responses(ctx context.Context, c Credentials, externalID string, since time.Time) ([]Response, error) {
	instance, token := c["instance"], c["access_token"]
	if instance == "" || token == "" {
		return nil, &Failure{Kind: ErrReauth, Msg: "Mastodon: no account stored; reconnect the account"}
	}
	me := mastodonAccount{ID: c["account_id"]} // a stored id saves the verify_credentials call on every read
	if me.ID == "" {
		if err := mastodonCall(ctx, http.MethodGet, instance, token, "/api/v1/accounts/verify_credentials", nil, "", "", &me); err != nil {
			return nil, err
		}
	}
	var res struct {
		Descendants []struct {
			ID         string    `json:"id"`
			ParentID   string    `json:"in_reply_to_id"`
			Content    string    `json:"content"`
			URL        string    `json:"url"`
			Created    time.Time `json:"created_at"`
			Favourites int64     `json:"favourites_count"`
			Account    struct {
				ID, Acct    string
				DisplayName string `json:"display_name"`
			}
		}
	}
	if err := mastodonCall(ctx, http.MethodGet, instance, token, "/api/v1/statuses/"+url.PathEscape(externalID)+"/context", nil, "", "", &res); err != nil {
		return nil, err
	}
	var out []Response
	for _, d := range res.Descendants {
		if len(out) >= maxResponses || (!since.IsZero() && !d.Created.After(since)) {
			continue
		}
		parent := d.ParentID
		if parent == externalID {
			parent = ""
		}
		likes := d.Favourites
		out = append(out, Response{ID: d.ID, ParentID: parent, Author: d.Account.DisplayName, AuthorHandle: "@" + d.Account.Acct,
			Text: mastodonText(d.Content), URL: d.URL, CreatedAt: d.Created, Likes: &likes, Own: me.ID != "" && d.Account.ID == me.ID})
	}
	return out, nil
}
