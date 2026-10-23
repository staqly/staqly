package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
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

func init() {
	Register(Whop{NewBase("whop", "Whop", AuthToken, Limits{MaxChars: 5000, MaxMedia: 4, MediaKinds: []string{"image", "video"}}).WithSettings(
		Field{Key: "title", Label: "Title", Kind: "text", MaxLength: 200, Help: "The post's headline in the forum (optional)."},
		Field{Key: "notify", Label: "Notify members", Kind: "boolean", Default: "false",
			Help: "Whop sends a mention notification to members of the forum who allow them."},
		Field{Key: "pinned", Label: "Pin to the top", Kind: "boolean", Default: "false"},
	)}, StatusAvailable)
}

func (Whop) CredentialFields() []Field {
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

// Connect checks the key, reads the business and picks the forum: the one named, or the only one.
func (Whop) Connect(ctx context.Context, f map[string]string) (Credentials, Profile, error) {
	key := strings.TrimSpace(f["api_key"])
	if key == "" {
		return nil, Profile{}, invalid("api_key", "paste the business's Account API key")
	}
	var biz struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Route string `json:"route"`
		Logo  string `json:"logo_url"`
	}
	if err := whopCall(ctx, key, http.MethodGet, "/accounts/me", nil, &biz); err != nil {
		if errors.Is(err, ErrReauth) {
			return nil, Profile{}, invalid("api_key", "Whop did not accept this API key")
		}
		return nil, Profile{}, err
	}
	if biz.ID == "" {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Whop did not say which business this key belongs to; use an Account API key"}
	}
	var forums []whopForum
	after := ""
	for page := 0; page < 10; page++ {
		q := url.Values{"account_id": {biz.ID}, "first": {"50"}}
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
		if err := whopCall(ctx, key, http.MethodGet, "/forums?"+q.Encode(), nil, &out); err != nil {
			return nil, Profile{}, err
		}
		forums = append(forums, out.Data...)
		if !out.PageInfo.HasNextPage || out.PageInfo.EndCursor == "" {
			break
		}
		after = out.PageInfo.EndCursor
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
	name := forum.Experience.Name
	if biz.Title != "" {
		name = biz.Title + ": " + name
	}
	handle := biz.Route
	if handle == "" {
		handle = biz.ID
	}
	return Credentials{"api_key": key, "account_id": biz.ID, "route": biz.Route, "experience_id": forum.Experience.ID},
		Profile{
			Handle:     handle + "/" + forum.Experience.ID,
			Name:       name,
			AvatarURL:  biz.Logo,
			ProfileURL: whopBusinessURL(biz.Route),
		}, nil
}

func whopBusinessURL(route string) string {
	if route == "" {
		return whopWeb
	}
	return whopWeb + "/" + url.PathEscape(route) + "/"
}

// Refresh checks the key still works (keys do not expire, but they can be deleted).
func (Whop) Refresh(ctx context.Context, _ App, c Credentials) (Credentials, error) {
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
	return w.send(ctx, c, p.Media, body, "post")
}

// Reply comments under the post (parentExternalID: the post, or from the second part on the comment before).
func (w Whop) Reply(ctx context.Context, c Credentials, parentExternalID string, p PostContent, _ Settings) (Result, error) {
	if parentExternalID == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Whop: nothing to comment on (the post has no id)"}
	}
	return w.send(ctx, c, p.Media, map[string]any{"content": p.Text, "parent_id": parentExternalID}, "comment")
}

// send uploads the files and creates the forum post or comment.
func (Whop) send(ctx context.Context, c Credentials, media []MediaFile, body map[string]any, what string) (Result, error) {
	key, exp := c["api_key"], c["experience_id"]
	if key == "" || exp == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "Whop: no forum stored; reconnect it"}
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
	return Result{ExternalID: out.ID, URL: whopBusinessURL(c["route"])}, nil
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
