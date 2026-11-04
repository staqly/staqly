package providers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

// Whop forums over Whop's REST API (https://docs.whop.com/api-reference): a business's Account API key, a forum (an
// experience, exp_...) of that business, posts and comments with POST /api/v1/forum_posts, files uploaded to the
// address Whop hands out. A token, not Whop's OAuth: the key does not expire, and an OAuth sign-in's refresh token
// rotates on every use, so several forums connected from one sign-in would knock each other out.
type Whop struct{ Base }

// Endpoints, variables so tests point them at httptest.
var (
	whopAPI = "https://api.whop.com/api/v1"
	whopWeb = "https://whop.com"
	// whopPoll is how long to wait between checks of a file Whop is still processing.
	whopPoll = 2 * time.Second
)

// whopLimits and whopSettings are shared by both ways in: Whop (OAuth, the server's Whop app) and Whop with an API key.
var whopLimits = Limits{MaxChars: 5000, MaxMedia: 4, MediaKinds: []string{"image", "video"}}

func init() {
	Register(Whop{NewBase("whop", "Whop", AuthOAuth2, whopLimits).WithSettings(append([]Field{whopForumField}, whopSettings()...)...)}, StatusAvailable)
	Register(WhopKey{Whop{NewBase("whop_key", "Whop (API key)", AuthToken, whopLimits).WithSettings(whopSettings()...)}}, StatusAvailable)
}

// WhopKey connects a forum with a business's Account API key instead of signing in (the same publishing, numbers and
// webhook as Whop). Channels connected before Whop had OAuth are Whop channels holding an api_key: they keep working.
type WhopKey struct{ Whop }

func whopSettings() []Field {
	return []Field{
		{Key: "title", Label: "Title", Kind: "text", MaxLength: 200, Help: "The post's headline in the forum (optional)."},
		{Key: "notify", Label: "Notify members", Kind: "boolean", Default: "false",
			Help: "Whop sends a mention notification to members of the forum who allow them."},
		{Key: "pinned", Label: "Pin to the top", Kind: "boolean", Default: "false"},
	}
}

func (WhopKey) CredentialFields() []Field {
	return []Field{
		{Key: "api_key", Label: "Account API key", Kind: "secret", Required: true,
			Help: "Whop dashboard → Developer → Account API keys → Create, with the forum permissions (read forums, create forum posts)."},
		{Key: "experience_id", Label: "Forum", Kind: "text", Placeholder: "exp_XXXXXXXXXXXXXX",
			Help: "The forum's id, needed when the business has more than one forum; staqly lists them if you leave it empty."},
	}
}

func (w Whop) Validate(p PostContent, s Settings) error {
	if err := CheckLimits(w.Limits(), p, s); err != nil {
		return err
	}
	return CheckFields(w.SettingsFields(), s)
}

type whopForum struct {
	ID         string `json:"id"`
	WhoCanPost string `json:"who_can_post"`
	Experience struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"experience"`
}

type whopBiz struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Route string `json:"route"`
	Logo  string `json:"logo_url"`
}

// whopBusiness reads a business and its forums: the key's own (id ""), or one a sign-in may act for. A sign-in is the
// person's (Whop: "user tokens do not have a requesting account"), so it names the business.
func whopBusiness(ctx context.Context, token, id string) (whopBiz, []whopForum, error) {
	var biz whopBiz
	path := "/accounts/me"
	if id != "" {
		path = "/accounts/" + url.PathEscape(id)
	}
	if err := whopCall(ctx, token, http.MethodGet, path, nil, &biz); err != nil {
		return biz, nil, err
	}
	if biz.ID == "" {
		return biz, nil, &Failure{Kind: ErrRejected, Msg: "Whop did not say which business this sign-in acts for"}
	}
	forums, err := whopForumsOf(ctx, token, biz.ID)
	return biz, forums, err
}

// whopForumsOf lists a business's forums.
func whopForumsOf(ctx context.Context, token, bizID string) ([]whopForum, error) {
	var forums []whopForum
	after := ""
	for page := 0; page < 10; page++ {
		q := url.Values{"account_id": {bizID}, "first": {"50"}}
		if after != "" {
			q.Set("after", after)
		}
		var out struct {
			Data     []whopForum `json:"data"`
			PageInfo struct {
				EndCursor   string `json:"end_cursor"`
				HasNextPage bool   `json:"has_next_page"`
			} `json:"page_info"`
		}
		if err := whopCall(ctx, token, http.MethodGet, "/forums?"+q.Encode(), nil, &out); err != nil {
			return nil, err
		}
		forums = append(forums, out.Data...)
		if !out.PageInfo.HasNextPage || out.PageInfo.EndCursor == "" {
			break
		}
		after = out.PageInfo.EndCursor
	}
	return forums, nil
}

// whopProfile is a forum's channel: the business's name, logo and page, and the forum.
func whopProfile(biz whopBiz, forum whopForum) Profile {
	name := forum.Experience.Name
	if biz.Title != "" {
		name = biz.Title + ": " + name
	}
	handle := biz.Route
	if handle == "" {
		handle = biz.ID
	}
	return Profile{Handle: handle + "/" + forum.Experience.ID, Name: name, AvatarURL: biz.Logo, ProfileURL: whopBusinessURL(biz.Route)}
}

// Connect checks the key, reads the business and picks the forum: the one named, or the only one.
func (WhopKey) Connect(ctx context.Context, f map[string]string) (Credentials, Profile, error) {
	key := strings.TrimSpace(f["api_key"])
	if key == "" {
		return nil, Profile{}, invalid("api_key", "paste the business's Account API key")
	}
	biz, forums, err := whopBusiness(ctx, key, "")
	if err != nil {
		if errors.Is(err, ErrReauth) {
			return nil, Profile{}, invalid("api_key", "Whop did not accept this API key")
		}
		return nil, Profile{}, err
	}
	want := strings.TrimSpace(f["experience_id"])
	var forum *whopForum
	for i := range forums {
		if forums[i].Experience.ID == want || (want != "" && forums[i].ID == want) {
			forum = &forums[i]
		}
	}
	switch {
	case len(forums) == 0:
		return nil, Profile{}, invalid("api_key", biz.Title+" has no forum on Whop: add the Forums app to one of its products, then connect again")
	case forum == nil && want == "" && len(forums) == 1:
		forum = &forums[0]
	case forum == nil:
		var names []string
		for _, fo := range forums {
			names = append(names, fo.Experience.Name+" ("+fo.Experience.ID+")")
		}
		msg := "choose the forum: "
		if want != "" {
			msg = want + " is not one of " + biz.Title + "'s forums; choose one of: "
		}
		return nil, Profile{}, invalid("experience_id", msg+strings.Join(names, ", "))
	}
	return Credentials{"api_key": key, "account_id": biz.ID, "route": biz.Route, "experience_id": forum.Experience.ID},
		whopProfile(biz, *forum), nil
}

func whopBusinessURL(route string) string {
	if route == "" {
		return whopWeb
	}
	return whopWeb + "/" + url.PathEscape(route) + "/"
}

// Refresh renews a sign-in (Whop rotates the refresh token on every use, so the pair is kept together), or for a key
// checks it still works (keys do not expire, but they can be deleted).
func (Whop) Refresh(ctx context.Context, app App, c Credentials) (Credentials, error) {
	if c["refresh_token"] != "" {
		// A token made for a business is renewed for the same business.
		t, err := whopToken(ctx, app, map[string]string{"grant_type": "refresh_token", "refresh_token": c["refresh_token"], "company_id": c["account_id"]})
		if err != nil {
			return nil, err
		}
		return t.into(c, time.Now()), nil
	}
	if c["api_key"] == "" {
		return nil, &Failure{Kind: ErrReauth, Msg: "Whop: no API key stored; reconnect the forum"}
	}
	if err := whopCall(ctx, c["api_key"], http.MethodGet, "/accounts/me", nil, nil); err != nil {
		return nil, err
	}
	return c, nil
}

// Whop publishes a post's thread as comments under it.
var _ Replier = Whop{}

func (w Whop) Publish(ctx context.Context, c Credentials, p PostContent, s Settings) (Result, error) {
	body := map[string]any{"content": p.Text}
	if t := strings.TrimSpace(s["title"]); t != "" {
		body["title"] = t
	}
	if s["notify"] == "true" {
		body["is_mention"] = true
	}
	if s["pinned"] == "true" {
		body["pinned"] = true
	}
	return w.send(ctx, c, s, p.Media, body, "post")
}

// Reply comments under the post (parentExternalID: the post, or from the second part on the comment before).
func (w Whop) Reply(ctx context.Context, c Credentials, parentExternalID string, p PostContent, s Settings) (Result, error) {
	if parentExternalID == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Whop: nothing to comment on (the post has no id)"}
	}
	exp, parent := whopPostID(c, parentExternalID)
	rs := Settings{}
	for k, v := range s {
		rs[k] = v
	}
	if exp != "" {
		rs["forum"] = exp // the comment goes to the forum of the post it answers
	}
	return w.send(ctx, c, rs, p.Media, map[string]any{"content": p.Text, "parent_id": parent}, "comment")
}

// whopPostID splits an external id into the forum (experience) and the post. Posts are stored as "<exp>:<post>": a
// sign-in picks the forum per post, and Whop lists comments by forum. An older bare post id is in the channel's forum.
func whopPostID(c Credentials, externalID string) (exp, post string) {
	if e, p, ok := strings.Cut(externalID, ":"); ok && strings.HasPrefix(e, "exp_") {
		return e, p
	}
	return c["experience_id"], externalID
}

type whopPost struct {
	ID        string    `json:"id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
	Views     *int64    `json:"view_count"`
	Likes     *int64    `json:"like_count"`
	Comments  *int64    `json:"comment_count"`
	User      struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Name     string `json:"name"`
	} `json:"user"`
}

var (
	_ MetricsReader  = Whop{}
	_ ResponseReader = Whop{}
)

// Metrics reads the post's views, likes and comments (GET /forum_posts/{id}).
func (Whop) Metrics(ctx context.Context, c Credentials, externalID string) (Metrics, error) {
	key := whopBearer(c)
	if key == "" {
		return Metrics{}, &Failure{Kind: ErrReauth, Msg: "Whop: no sign-in stored; reconnect it"}
	}
	_, id := whopPostID(c, externalID)
	var p whopPost
	if err := whopCall(ctx, key, http.MethodGet, "/forum_posts/"+url.PathEscape(id), nil, &p); err != nil {
		return Metrics{}, err
	}
	return Metrics{Impressions: p.Views, Likes: p.Likes, Replies: p.Comments}, nil
}

// Responses reads the comments under the post and the replies to them (GET /forum_posts?parent_id=).
// shortcut: one level of replies under each comment; deeper threads are not read, add recursion if Whop users nest.
func (Whop) Responses(ctx context.Context, c Credentials, externalID string, since time.Time) ([]Response, error) {
	key := whopBearer(c)
	if key == "" {
		return nil, &Failure{Kind: ErrReauth, Msg: "Whop: no sign-in stored; reconnect it"}
	}
	exp, id := whopPostID(c, externalID)
	if exp == "" {
		return nil, &Failure{Kind: ErrRejected, Msg: "Whop: this post's forum is not known, so its comments can't be read"}
	}
	var out []Response
	list := func(parent, under string) ([]whopPost, error) {
		var all []whopPost
		after := ""
		for page := 0; page < 10 && len(out) < 500; page++ {
			q := url.Values{"experience_id": {exp}, "parent_id": {parent}, "first": {"50"}}
			if after != "" {
				q.Set("after", after)
			}
			var res struct {
				Data     []whopPost `json:"data"`
				PageInfo struct {
					EndCursor   string `json:"end_cursor"`
					HasNextPage bool   `json:"has_next_page"`
				} `json:"page_info"`
			}
			if err := whopCall(ctx, key, http.MethodGet, "/forum_posts?"+q.Encode(), nil, &res); err != nil {
				return nil, err
			}
			for _, p := range res.Data {
				all = append(all, p)
				if !p.CreatedAt.After(since) || len(out) >= 500 {
					continue
				}
				name := p.User.Name
				if name == "" {
					name = p.User.Username
				}
				out = append(out, Response{ID: p.ID, ParentID: under, Author: name, AuthorHandle: p.User.Username,
					Text: p.Content, CreatedAt: p.CreatedAt, Likes: p.Likes, Own: p.User.ID != "" && p.User.ID == c["user_id"]})
			}
			if !res.PageInfo.HasNextPage || res.PageInfo.EndCursor == "" {
				break
			}
			after = res.PageInfo.EndCursor
		}
		return all, nil
	}
	top, err := list(id, "")
	if err != nil {
		return nil, err
	}
	for _, p := range top {
		if p.Comments != nil && *p.Comments > 0 && len(out) < 500 {
			if _, err := list(p.ID, p.ID); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// send uploads the files and creates the forum post or comment.
func (Whop) send(ctx context.Context, c Credentials, s Settings, media []MediaFile, body map[string]any, what string) (Result, error) {
	key := whopBearer(c)
	if key == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "Whop: no sign-in stored; reconnect it"}
	}
	exp := strings.TrimSpace(s["forum"])
	if exp == "" {
		exp = c["experience_id"] // a channel connected with an API key is one forum
	}
	if exp == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Whop: pick the business the post goes to in the post's settings"}
	}
	body["experience_id"] = exp
	var files []map[string]string
	for _, m := range media {
		id, err := whopUpload(ctx, key, m)
		if err != nil {
			return Result{}, err
		}
		files = append(files, map[string]string{"id": id})
	}
	if len(files) > 0 {
		body["attachments"] = files
	}
	var out struct {
		ID string `json:"id"`
	}
	// Whop has no idempotency key: a post whose answer was lost may be out, so it is not sent again.
	if err := whopCall(ctx, key, http.MethodPost, "/forum_posts", body, &out); err != nil {
		var f *Failure
		if !errors.As(err, &f) {
			return Result{}, &Failure{Kind: ErrRejected, Msg: "Whop may have published the " + what + ", but its answer was lost (" + err.Error() + "); check the forum before posting it again"}
		}
		return Result{}, err
	}
	if out.ID == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Whop published the " + what + " but did not say its id; check the forum"}
	}
	// Whop's API gives no address for a forum post; the target links to the business's page.
	route := c["route"]
	if route == "" {
		route = whopForumRoute(ctx, key, exp)
	}
	return Result{ExternalID: exp + ":" + out.ID, URL: whopBusinessURL(route)}, nil
}

// whopForumRoute: the address name of the business a forum belongs to (a sign-in picks it per post), "" when Whop
// does not say; the post is out by then, so a failed lookup only costs the link.
func whopForumRoute(ctx context.Context, key, exp string) string {
	var out struct {
		Company struct {
			Route string `json:"route"`
		} `json:"company"`
	}
	if whopCall(ctx, key, http.MethodGet, "/experiences/"+url.PathEscape(exp), nil, &out) != nil {
		return ""
	}
	return out.Company.Route
}

// whopUpload creates a file, sends its bytes to the address Whop hands out and waits until Whop has it.
// Every failure here comes before the post exists, so it is retried unless Whop refused the file itself.
func whopUpload(ctx context.Context, key string, m MediaFile) (string, error) {
	name := m.Name
	if name == "" {
		name = path.Base(m.Path)
	}
	var file struct {
		ID            string            `json:"id"`
		UploadURL     string            `json:"upload_url"`
		UploadHeaders map[string]string `json:"upload_headers"`
	}
	if err := whopCall(ctx, key, http.MethodPost, "/files", map[string]any{"filename": name}, &file); err != nil {
		return "", whopRetry(err)
	}
	if file.ID == "" || file.UploadURL == "" {
		return "", &Failure{Msg: "Whop did not give an upload address for " + name + "; staqly tries again in a few minutes"}
	}
	rc, err := m.Reader(ctx)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, file.UploadURL, rc)
	if err != nil {
		return "", err
	}
	req.ContentLength = m.Size
	if m.MimeType != "" {
		req.Header.Set("Content-Type", m.MimeType)
	}
	for k, v := range file.UploadHeaders {
		req.Header.Set(k, v)
	}
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		// The upload address is signed: name the host only.
		return "", &Failure{Msg: "Whop's file storage could not be reached (" + req.URL.Host + "); staqly tries again in a few minutes"}
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", &Failure{Msg: fmt.Sprintf("Whop's file storage refused %s (%d %s); staqly tries again in a few minutes", name, resp.StatusCode, blogSnippet(raw))}
	}
	for {
		var st struct {
			Status string `json:"upload_status"`
		}
		if err := whopCall(ctx, key, http.MethodGet, "/files/"+url.PathEscape(file.ID), nil, &st); err != nil {
			return "", whopRetry(err)
		}
		switch st.Status {
		case "ready":
			return file.ID, nil
		case "failed":
			return "", &Failure{Kind: ErrRejected, Msg: "Whop could not process " + name}
		}
		// The job's own deadline ends a file that never finishes; its next attempt uploads it again.
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(whopPoll):
		}
	}
}

// whopRetry keeps a Whop failure as it is and makes a lost answer retryable (nothing is posted yet).
func whopRetry(err error) error {
	var f *Failure
	if errors.As(err, &f) {
		return err
	}
	return &Failure{Msg: err.Error() + "; staqly tries again in a few minutes"}
}

// whopCall sends a JSON request with the key and decodes a 2xx reply into out.
func whopCall(ctx context.Context, key, method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, whopAPI+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return whopDo(ctx, req, out)
}

// whopDo sends a request and decodes a 2xx reply into out. A request that never got an answer is a plain error
// (not a Failure), so Publish can tell it apart.
func whopDo(ctx context.Context, req *http.Request, out any) error {
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		return fmt.Errorf("Whop could not be reached (%s)", req.URL.Host)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out != nil && len(raw) > 0 {
			if err := json.Unmarshal(raw, out); err != nil {
				return &Failure{Kind: ErrRejected, Msg: "Whop answered in a way staqly does not understand"}
			}
		}
		return nil
	}
	return whopError(resp.StatusCode, raw)
}

// whopError maps Whop's answers ({"error":{"type","message"}}, or a plain message) to what staqly does next.
func whopError(status int, raw []byte) error {
	var e struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
	}
	_ = json.Unmarshal(raw, &e)
	detail := e.Message
	var inner struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	}
	var plain string
	switch {
	case json.Unmarshal(e.Error, &inner) == nil && inner.Message != "":
		detail = inner.Message
	case detail == "" && json.Unmarshal(e.Error, &plain) == nil:
		detail = plain
	}
	if detail == "" {
		detail = strings.TrimSpace(string(raw))
		if len(detail) > 200 {
			detail = detail[:200] + "..."
		}
	}
	if detail == "" {
		detail = http.StatusText(status)
	}
	switch {
	case status == http.StatusUnauthorized:
		return &Failure{Kind: ErrReauth, Msg: "Whop no longer accepts the API key (" + detail + "); reconnect the forum with a working key"}
	case status == http.StatusForbidden:
		return &Failure{Kind: ErrRejected, Msg: "Whop refused it (" + detail + "); the API key needs the forum permissions (create forum posts), or the forum lets only admins post"}
	case status == http.StatusTooManyRequests:
		return &Failure{Msg: "Whop rate limit reached; staqly tries again in a few minutes"}
	case status >= 500:
		return &Failure{Msg: "Whop is unavailable (" + detail + "); staqly tries again in a few minutes"}
	case status == http.StatusNotFound:
		return &Failure{Kind: ErrRejected, Msg: "Whop did not find the forum or post (" + detail + "); reconnect the channel if the forum was removed"}
	}
	return &Failure{Kind: ErrRejected, Msg: "Whop refused it (" + detail + ")"}
}

// WhopEvents are the events a Whop channel's webhook asks for: someone joined the business, a membership started.
// They count in the analytics' Audience (members, memberships).
var WhopEvents = []string{"member.created", "membership.activated"}

// Whop sends the business's member events to staqly's webhook, one per channel.
var _ EventSubscriber = Whop{}

// SubscribeEvents keeps a webhook for this channel on Whop: the one made before when it still exists (turned back on
// when Whop disabled it after failures), else a new one, whose id and signing secret come back to be stored. Whop shows
// a webhook's secret only when it is made.
//
// shortcut: two forums of one business have a webhook each, so its members count once per connected forum; share one
// webhook per business when that matters.
func (Whop) SubscribeEvents(ctx context.Context, c Credentials, hookURL string) (Credentials, error) {
	key, account := whopBearer(c), c["account_id"]
	if account == "" {
		return nil, nil // a person's sign-in: their businesses' members aren't one channel's (shortcut: no Audience for it yet)
	}
	if key == "" || account == "" {
		return nil, &Failure{Kind: ErrReauth, Msg: "Whop: no API key stored; reconnect the forum"}
	}
	if hookURL == "" {
		return nil, &Failure{Kind: ErrRejected, Msg: "Whop: this server has no public API address (PUBLIC_API_URL) for Whop's webhook"}
	}
	type hook struct {
		ID      string `json:"id"`
		URL     string `json:"url"`
		Enabled bool   `json:"enabled"`
		Secret  string `json:"webhook_secret"`
	}
	if id := c["webhook_id"]; id != "" && c["webhook_secret"] != "" {
		var h hook
		found, err := whopFind(ctx, key, "/webhooks/"+url.PathEscape(id), &h)
		switch {
		case err != nil:
			return nil, whopWebhookError(err)
		case found && h.URL == hookURL && h.Enabled:
			return nil, nil
		case found && h.URL == hookURL:
			return nil, whopWebhookError(whopCall(ctx, key, http.MethodPatch, "/webhooks/"+url.PathEscape(id), map[string]any{"enabled": true}, nil))
		}
		// Gone, or for another address (the server moved): make a new one.
	}
	var h hook
	body := map[string]any{"url": hookURL, "events": WhopEvents, "resource_id": account, "title": "staqly"}
	if err := whopCall(ctx, key, http.MethodPost, "/webhooks", body, &h); err != nil {
		return nil, whopWebhookError(err)
	}
	if h.ID == "" || h.Secret == "" {
		return nil, &Failure{Kind: ErrRejected, Msg: "Whop made the webhook but did not give its id and signing secret"}
	}
	out := Credentials{}
	for k, v := range c {
		out[k] = v
	}
	out["webhook_id"], out["webhook_secret"] = h.ID, h.Secret
	return out, nil
}

// whopWebhookError: a refused webhook call (403: a key made before staqly asked for webhooks) says which permissions
// the key needs; the rest passes through.
func whopWebhookError(err error) error {
	var f *Failure
	if errors.As(err, &f) && f.Kind == ErrRejected {
		return &Failure{Kind: ErrRejected, Msg: "Whop did not let staqly make its webhook (" + f.Msg + "); give the API key developer:manage_webhook and webhook_receive:memberships, then reconnect"}
	}
	return err
}

// WhopSigned checks a webhook call (Standard Webhooks): webhook-signature holds one or more "v1,<base64>" HMAC-SHA256
// signatures of "<webhook-id>.<webhook-timestamp>.<body>", keyed with the webhook's secret as Whop gives it.
func WhopSigned(secret, id, timestamp string, body []byte, header string) bool {
	if secret == "" || id == "" || timestamp == "" {
		return false
	}
	key := []byte(secret)
	if rest, ok := strings.CutPrefix(secret, "whsec_"); ok { // the spec's own form: the key base64 encoded
		if k, err := base64.StdEncoding.DecodeString(rest); err == nil {
			key = k
		}
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + timestamp + "."))
	mac.Write(body)
	want := mac.Sum(nil)
	for _, part := range strings.Fields(header) {
		v, sig, ok := strings.Cut(part, ",")
		if !ok || v != "v1" {
			continue
		}
		if got, err := base64.StdEncoding.DecodeString(sig); err == nil && hmac.Equal(got, want) {
			return true
		}
	}
	return false
}

// whopFind reads one object; found is false when Whop answers 404 (deleted).
func whopFind(ctx context.Context, key, p string, out any) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, whopAPI+p, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		return false, fmt.Errorf("Whop could not be reached (%s)", req.URL.Host)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return false, nil
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return true, json.Unmarshal(raw, out)
	}
	return false, whopError(resp.StatusCode, raw)
}

// whopBearer is what a channel calls Whop with: its sign-in's access token, or its Account API key.
func whopBearer(c Credentials) string {
	if c["access_token"] != "" {
		return c["access_token"]
	}
	return c["api_key"]
}

// OAuth: the person signs in on Whop and approves the server's Whop app with the permissions it asks for, which Whop
// grants on all the businesses they run. The channel is the person; each post picks the forum to go to (the forum
// setting, a list of their businesses' forums).
var (
	whopOAuth               = "https://api.whop.com/oauth"
	_          OptionLister = Whop{}
	whopScopes              = "openid profile company:basic:read forum:read forum:post:create"
	// The business a post goes to: one choice per forum, named after its business ("Business: Forum" when it has
	// several), so a business and its forum are picked at once.
	whopForumField = Field{Key: "forum", Label: "Business", Kind: "remote", Required: true, MaxItems: 1,
		Help: "Which of your businesses (and its forum) the post goes to."}
)

func (Whop) AuthURL(app App, redirect, state string, extra map[string]string) (string, error) {
	verifier := extra["code_verifier"]
	if verifier == "" {
		return "", errors.New("Whop needs a PKCE code verifier")
	}
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {app.ClientID},
		"redirect_uri":          {redirect},
		"scope":                 {whopScopes},
		"state":                 {state},
		"nonce":                 {state},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
	}
	return whopOAuth + "/authorize?" + q.Encode(), nil
}

type whopTokenReply struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

func (t whopTokenReply) into(c Credentials, now time.Time) Credentials {
	out := Credentials{}
	for k, v := range c {
		out[k] = v
	}
	out["access_token"] = t.AccessToken
	if t.RefreshToken != "" {
		out["refresh_token"] = t.RefreshToken
	}
	if t.ExpiresIn > 0 {
		out["expires_at"] = now.Add(time.Duration(t.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)
	}
	return out
}

// whopToken calls Whop's token endpoint (JSON; a confidential app sends its secret too).
func whopToken(ctx context.Context, app App, body map[string]string) (whopTokenReply, error) {
	var t whopTokenReply
	body["client_id"], body["client_secret"] = app.ClientID, app.ClientSecret
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, whopOAuth+"/token", bytes.NewReader(raw))
	if err != nil {
		return t, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		return t, fmt.Errorf("Whop could not be reached (%s)", req.URL.Host)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 && json.Unmarshal(out, &t) == nil && t.AccessToken != "" {
		return t, nil
	}
	var e struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	_ = json.Unmarshal(out, &e)
	detail := e.Description
	if detail == "" {
		detail = blogSnippet(out)
	}
	switch {
	case e.Error == "invalid_grant", resp.StatusCode == http.StatusUnauthorized && e.Error != "invalid_client":
		return t, &Failure{Kind: ErrReauth, Msg: "Whop no longer accepts this sign-in (" + detail + "); reconnect the forum"}
	case e.Error == "invalid_client":
		// For whoever runs the server: PROVIDER_WHOP_CLIENT_ID and _SECRET are on the app's OAuth page in Whop's dashboard.
		return t, &Failure{Kind: ErrRejected, Msg: "Whop did not accept this server's app (" + detail + ")"}
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return t, &Failure{Msg: "Whop is unavailable (" + strconv.Itoa(resp.StatusCode) + "); try again in a minute"}
	}
	return t, &Failure{Kind: ErrRejected, Msg: "Whop refused the sign-in (" + detail + ")"}
}

func (Whop) Exchange(ctx context.Context, app App, redirect, code string, extra map[string]string) (Credentials, Profile, error) {
	t, err := whopToken(ctx, app, map[string]string{"grant_type": "authorization_code", "code": code, "redirect_uri": redirect,
		"code_verifier": extra["code_verifier"]})
	if err != nil {
		return nil, Profile{}, err
	}
	creds := t.into(Credentials{}, time.Now())
	// The channel is the person who signed in.
	var me struct {
		Sub      string `json:"sub"`
		Name     string `json:"name"`
		Username string `json:"preferred_username"`
		Picture  string `json:"picture"`
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, whopOAuth+"/userinfo", nil)
	if err != nil {
		return nil, Profile{}, err
	}
	req.Header.Set("Authorization", "Bearer "+creds["access_token"])
	if err := whopDo(ctx, req, &me); err != nil {
		return nil, Profile{}, err
	}
	creds["user_id"] = me.Sub
	name := me.Name
	if name == "" {
		name = me.Username
	}
	return creds, Profile{Name: name, Handle: me.Username, AvatarURL: me.Picture}, nil
}

// Options lists the forums of the businesses the person runs, each named after its business.
func (Whop) Options(ctx context.Context, c Credentials, key string) ([]Option, error) {
	if key != whopForumField.Key {
		return nil, nil
	}
	token := whopBearer(c)
	var out struct {
		Data []whopBiz `json:"data"`
	}
	if err := whopCall(ctx, token, http.MethodGet, "/accounts?first=50", nil, &out); err != nil {
		return nil, err
	}
	var opts []Option
	for _, biz := range out.Data {
		forums, err := whopForumsOf(ctx, token, biz.ID)
		if err != nil {
			return nil, err
		}
		for _, f := range forums {
			label := biz.Title
			if len(forums) > 1 {
				label += ": " + f.Experience.Name
			}
			opts = append(opts, Option{Value: f.Experience.ID, Label: label, Icon: biz.Logo})
		}
	}
	if len(opts) == 0 {
		return nil, &Failure{Kind: ErrRejected, Msg: "Whop lists no business with a forum that you run: add the Forums app to one of its products"}
	}
	return opts, nil
}
