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
	"strings"
)

// Moltbook, the social network for AI agents, over its REST API (https://www.moltbook.com/skill.md): the agent's API
// key as a bearer token, a post (title plus text, or a link) into a submolt, the thread as comments. Moltbook takes
// no media through its API. It may hold a new post or comment until the agent answers a verification challenge (an
// obfuscated arithmetic puzzle meant for the agent itself); staqly does not answer those and says so.
type Moltbook struct{ Base }

// Endpoints, variables so tests point them at httptest. Always the www host: moltbook.com without it redirects and
// the redirect drops the Authorization header.
var (
	moltbookAPI = "https://www.moltbook.com/api/v1"
	moltbookWeb = "https://www.moltbook.com"
)

func init() {
	Register(Moltbook{NewBase("moltbook", "Moltbook", AuthToken, Limits{MaxChars: 40000, NeedsTitle: true}).WithSettings(
		titleField(300),
		Field{Key: "submolt", Label: "Submolt", Kind: "text", Default: "general", Placeholder: "general", MaxLength: 30,
			Help: "The community the post goes to, by its name (lowercase, as in moltbook.com/m/general)."},
		Field{Key: "link", Label: "Link", Kind: "url", Placeholder: "https://example.com/page",
			Help: "Makes it a link post; the text goes with it."},
	)}, StatusAvailable)
}

func (Moltbook) CredentialFields() []Field {
	return []Field{{Key: "api_key", Label: "API key", Kind: "secret", Required: true, Placeholder: "moltbook_...",
		Help: "The key Moltbook gave the agent when it registered (POST /api/v1/agents/register)."}}
}

func (m Moltbook) Validate(p PostContent, s Settings) error {
	if err := CheckLimits(m.Limits(), p, s); err != nil {
		return err
	}
	if sub := moltbookSubmolt(s); strings.ContainsAny(sub, " /?#") {
		return invalid("submolt", "write the submolt's name, like general")
	}
	return CheckFields(m.SettingsFields(), s)
}

// moltbookSubmolt is the submolt's name as Moltbook takes it ("m/General" -> "general"), "general" when none is set.
func moltbookSubmolt(s Settings) string {
	sub := strings.ToLower(strings.TrimSpace(s["submolt"]))
	sub = strings.TrimPrefix(strings.TrimPrefix(sub, "/"), "m/")
	if sub == "" {
		return "general"
	}
	return sub
}

// Connect checks the key and reads the agent's profile.
func (Moltbook) Connect(ctx context.Context, f map[string]string) (Credentials, Profile, error) {
	key := strings.TrimSpace(f["api_key"])
	if key == "" {
		return nil, Profile{}, invalid("api_key", "paste the agent's API key")
	}
	var out struct {
		Agent struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			DisplayName string `json:"display_name"`
			Avatar      string `json:"avatar_url"`
		} `json:"agent"`
	}
	if err := moltbookCall(ctx, key, http.MethodGet, "/agents/me", nil, &out); err != nil {
		if errors.Is(err, ErrReauth) {
			return nil, Profile{}, invalid("api_key", "Moltbook did not accept this API key")
		}
		return nil, Profile{}, err
	}
	a := out.Agent
	if a.Name == "" {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Moltbook did not say which agent this key belongs to"}
	}
	name := a.DisplayName
	if name == "" {
		name = a.Name
	}
	return Credentials{"api_key": key, "agent": a.Name}, Profile{
		Handle:     "u/" + a.Name,
		Name:       name,
		AvatarURL:  a.Avatar,
		ProfileURL: moltbookWeb + "/u/" + url.PathEscape(a.Name),
	}, nil
}

// Refresh checks the key still works (keys do not expire, but an agent can be suspended or its key reset).
func (Moltbook) Refresh(ctx context.Context, _ App, c Credentials) (Credentials, error) {
	if c["api_key"] == "" {
		return nil, &Failure{Kind: ErrReauth, Msg: "Moltbook: no API key stored; reconnect the agent"}
	}
	if err := moltbookCall(ctx, c["api_key"], http.MethodGet, "/agents/me", nil, nil); err != nil {
		return nil, err
	}
	return c, nil
}

// Moltbook publishes a post's thread as comments under it.
var _ Replier = Moltbook{}

// moltbookItem is a created post or comment: its id and, when Moltbook holds it, the challenge it waits on.
type moltbookItem struct {
	ID                 string `json:"id"`
	VerificationStatus string `json:"verification_status"`
	Verification       *struct {
		Code      string `json:"verification_code"`
		Challenge string `json:"challenge_text"`
		ExpiresAt string `json:"expires_at"`
	} `json:"verification"`
}

// held says why Moltbook keeps the item hidden, or "" when it is out. what is "post" or "comment".
func (i moltbookItem) held(what string) string {
	if i.Verification == nil && i.VerificationStatus != "pending" {
		return ""
	}
	code := ""
	if i.Verification != nil && i.Verification.Code != "" {
		code = " (code " + i.Verification.Code + ")"
	}
	return "Moltbook created the " + what + " but hides it until the agent answers Moltbook's verification challenge" + code +
		" within 5 minutes, and staqly cannot answer it for the agent: let the agent answer it with POST /api/v1/verify, " +
		"or post with an agent Moltbook trusts (trusted agents skip the challenge)"
}

func (m Moltbook) Publish(ctx context.Context, c Credentials, p PostContent, s Settings) (Result, error) {
	key := c["api_key"]
	if key == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "Moltbook: no API key stored; reconnect the agent"}
	}
	body := map[string]any{"submolt_name": moltbookSubmolt(s), "title": strings.TrimSpace(s["title"])}
	body["type"] = "text"
	if link := strings.TrimSpace(s["link"]); link != "" {
		body["type"], body["url"] = "link", link
	}
	if strings.TrimSpace(p.Text) != "" {
		body["content"] = p.Text
	}
	var out struct {
		Post moltbookItem `json:"post"`
	}
	// Moltbook has no idempotency key: a post whose answer was lost may be out, so it is not sent again.
	if err := moltbookCall(ctx, key, http.MethodPost, "/posts", body, &out); err != nil {
		var f *Failure
		if !errors.As(err, &f) {
			return Result{}, &Failure{Kind: ErrRejected, Msg: "Moltbook may have published the post, but its answer was lost (" + err.Error() + "); check the agent's profile before posting it again"}
		}
		return Result{}, err
	}
	if out.Post.ID == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Moltbook published the post but did not say its id; check the agent's profile"}
	}
	if why := out.Post.held("post"); why != "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: why}
	}
	return Result{ExternalID: out.Post.ID, URL: moltbookWeb + "/post/" + url.PathEscape(out.Post.ID)}, nil
}

// Reply comments under the post. parentExternalID is the post or, from the second part on, the comment before
// ("comment:<post>:<comment>"); each part answers the one before it.
func (Moltbook) Reply(ctx context.Context, c Credentials, parentExternalID string, p PostContent, _ Settings) (Result, error) {
	key := c["api_key"]
	if key == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "Moltbook: no API key stored; reconnect the agent"}
	}
	post, parent := parentExternalID, ""
	if rest, ok := strings.CutPrefix(parentExternalID, "comment:"); ok {
		post, parent, _ = strings.Cut(rest, ":")
	}
	if post == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Moltbook: nothing to comment on (the post has no id)"}
	}
	if len(p.Media) > 0 {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Moltbook comments take text only"}
	}
	body := map[string]any{"content": p.Text}
	if parent != "" {
		body["parent_id"] = parent
	}
	var out struct {
		Comment moltbookItem `json:"comment"`
	}
	if err := moltbookCall(ctx, key, http.MethodPost, "/posts/"+url.PathEscape(post)+"/comments", body, &out); err != nil {
		var f *Failure
		if !errors.As(err, &f) {
			return Result{}, &Failure{Kind: ErrRejected, Msg: "Moltbook may have published the comment, but its answer was lost (" + err.Error() + "); check the post before sending it again"}
		}
		return Result{}, err
	}
	if out.Comment.ID == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Moltbook published the comment but did not say its id; check the post"}
	}
	if why := out.Comment.held("comment"); why != "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: why}
	}
	return Result{ExternalID: "comment:" + post + ":" + out.Comment.ID, URL: moltbookWeb + "/post/" + url.PathEscape(post)}, nil
}

// moltbookCall sends a JSON request with the agent's key and decodes a 2xx reply into out.
func moltbookCall(ctx context.Context, key, method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, moltbookAPI+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return moltbookDo(ctx, req, out)
}

// moltbookDo sends a request and decodes a 2xx reply into out. A request that never got an answer is a plain error
// (not a Failure), so Publish can tell it apart.
func moltbookDo(ctx context.Context, req *http.Request, out any) error {
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		return fmt.Errorf("Moltbook could not be reached (%s)", req.URL.Host)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out != nil && len(raw) > 0 {
			if err := json.Unmarshal(raw, out); err != nil {
				return &Failure{Kind: ErrRejected, Msg: "Moltbook answered in a way staqly does not understand"}
			}
		}
		return nil
	}
	return moltbookError(resp.StatusCode, raw)
}

// moltbookError maps Moltbook's {"success":false,"error":"...","hint":"..."} answers to what staqly does next.
func moltbookError(status int, raw []byte) error {
	var e struct {
		Error      string `json:"error"`
		Hint       string `json:"hint"`
		RetryAfter int    `json:"retry_after_seconds"`
	}
	_ = json.Unmarshal(raw, &e)
	detail := e.Error
	if detail == "" {
		detail = strings.TrimSpace(string(raw))
		if len(detail) > 200 {
			detail = detail[:200] + "..."
		}
	}
	if detail == "" {
		detail = http.StatusText(status)
	}
	if e.Hint != "" {
		detail += "; " + e.Hint
	}
	switch {
	case status == http.StatusUnauthorized:
		return &Failure{Kind: ErrReauth, Msg: "Moltbook no longer accepts the agent's API key (" + detail + "); reconnect it with a working key"}
	case status == http.StatusTooManyRequests:
		// Moltbook lets an agent post once every 30 minutes (2 hours in its first day) and comment every 20 seconds.
		msg := "Moltbook rate limit reached (" + detail + ")"
		if e.RetryAfter > 0 {
			msg += fmt.Sprintf("; it takes the next one in %d seconds", e.RetryAfter)
		}
		return &Failure{Msg: msg + "; staqly tries again"}
	case status >= 500:
		return &Failure{Msg: "Moltbook is unavailable (" + detail + "); staqly tries again in a few minutes"}
	case status == http.StatusNotFound:
		return &Failure{Kind: ErrRejected, Msg: "Moltbook did not find it (" + detail + "); check the submolt's name"}
	case status == http.StatusForbidden:
		return &Failure{Kind: ErrRejected, Msg: "Moltbook refused it (" + detail + "); a new agent must be claimed by its owner, and a suspended one cannot post"}
	}
	return &Failure{Kind: ErrRejected, Msg: "Moltbook refused it (" + detail + ")"}
}
