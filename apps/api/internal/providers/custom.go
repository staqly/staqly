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

// Custom is "your own automation". Without a URL staqly emits
// post.publish_requested for the target and waits for the result your webhook
// receiver or MCP agent reports. With a URL staqly calls it itself (method and
// headers of your choice): 2xx is published (a JSON reply with "url" or
// "external_id" is recorded), 202 or {"status":"publishing"} means the result
// comes later through POST /posts/{id}/targets/{channel}/result, anything else
// is a failure the job queue retries.
type Custom struct{ Base }

func init() {
	Register(Custom{NewBase("custom", "Your own automation", AuthNone, Limits{MaxMedia: 10, MediaKinds: []string{"image", "video", "gif", "file"}})}, StatusAvailable)
}

var customMethods = []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodGet}

func (Custom) CredentialFields() []Field {
	methods := make([]Option, len(customMethods))
	for i, m := range customMethods {
		methods[i] = Option{Value: m, Label: m}
	}
	endpoint := &Condition{Key: "delivery", Value: "endpoint"}
	return []Field{
		{Key: "delivery", Label: "How posts reach your automation", Kind: "select", Default: "endpoint",
			Options: []Option{{Value: "endpoint", Label: "Send to my endpoint"}, {Value: "agent", Label: "Let my agents and webhooks pick it up"}},
			Help:    "Agents and webhooks: staqly announces each post and waits for your automation to report the result."},
		{Key: "url", Label: "Endpoint URL", Kind: "url", Required: true, When: endpoint, Placeholder: "https://n8n.example.com/webhook/staqly",
			Help: "Staqly sends each post here; a 2xx reply publishes it, 202 means your automation reports the result later."},
		{Key: "method", Label: "Method", Kind: "select", Options: methods, Default: http.MethodPost, When: endpoint,
			Help: "GET sends the post as query parameters; the others send JSON."},
		{Key: "headers", Label: "Headers", Kind: "textarea", When: endpoint, Placeholder: "Authorization: Bearer your-token\nX-Team: social",
			Help: "One per line as Name: value. Stored sealed, like every credential."},
	}
}

// Connect checks the endpoint settings; nothing is called yet.
func (Custom) Connect(_ context.Context, f map[string]string) (Credentials, Profile, error) {
	profile := Profile{Name: "Your own automation"}
	raw := strings.TrimSpace(f["url"])
	if f["delivery"] == "agent" {
		return Credentials{}, profile, nil // agents and webhooks: nothing to call
	}
	if f["delivery"] == "endpoint" && raw == "" {
		return nil, Profile{}, invalid("url", "enter the endpoint URL")
	}
	if raw == "" {
		if f["headers"] != "" {
			return nil, Profile{}, invalid("url", "headers need an endpoint URL")
		}
		return Credentials{}, profile, nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, Profile{}, invalid("url", "enter an http:// or https:// URL")
	}
	method := strings.ToUpper(strings.TrimSpace(f["method"]))
	if method == "" {
		method = http.MethodPost
	}
	known := false
	for _, m := range customMethods {
		known = known || m == method
	}
	if !known {
		return nil, Profile{}, invalid("method", "use POST, PUT, PATCH or GET")
	}
	if _, err := parseHeaders(f["headers"]); err != nil {
		return nil, Profile{}, err
	}
	profile.Handle = u.Host
	return Credentials{"url": raw, "method": method, "headers": strings.TrimSpace(f["headers"])}, profile, nil
}

// parseHeaders reads "Name: value" lines; blank lines are skipped.
func parseHeaders(s string) (http.Header, error) {
	h := http.Header{}
	for i, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		if !ok || name == "" || strings.ContainsAny(name, " \t\r") || strings.ContainsAny(value, "\r\n") {
			return nil, invalid("headers", fmt.Sprintf("line %d: write Name: value", i+1))
		}
		h.Add(name, value)
	}
	return h, nil
}

type clientKey struct{}

// WithHTTPClient hands providers the client to call platforms with (the app's SSRF-guarded one).
func WithHTTPClient(ctx context.Context, c *http.Client) context.Context {
	return context.WithValue(ctx, clientKey{}, c)
}

func httpClient(ctx context.Context) *http.Client {
	if c, ok := ctx.Value(clientKey{}).(*http.Client); ok && c != nil {
		return c
	}
	return http.DefaultClient
}

func (Custom) Publish(ctx context.Context, c Credentials, p PostContent, s Settings) (Result, error) {
	if c["url"] == "" {
		return Result{}, ErrAsync
	}
	mediaOf := func(files []MediaFile) []map[string]any {
		out := make([]map[string]any, len(files))
		for i, m := range files {
			out[i] = map[string]any{"id": m.ID, "name": m.Name, "mime_type": m.MimeType, "size": m.Size, "url": m.URL}
		}
		return out
	}
	media := mediaOf(p.Media)
	// The thread goes along: your automation publishes the replies (staqly marks them skipped).
	thread := make([]map[string]any, len(p.Thread))
	for i, t := range p.Thread {
		thread[i] = map[string]any{"content": t.Text, "media": mediaOf(t.Media), "delay_seconds": t.DelaySeconds}
	}
	method := c["method"]
	if method == "" {
		method = http.MethodPost
	}
	var body io.Reader
	target := c["url"]
	if method == http.MethodGet {
		u, err := url.Parse(target)
		if err != nil {
			return Result{}, err
		}
		q := u.Query()
		q.Set("post_id", p.PostID)
		q.Set("channel_id", p.ChannelID)
		q.Set("text", p.Text)
		u.RawQuery = q.Encode()
		target = u.String()
	} else {
		b, err := json.Marshal(map[string]any{"post_id": p.PostID, "channel_id": p.ChannelID, "text": p.Text,
			"media": media, "tags": p.Tags, "settings": s, "thread": thread})
		if err != nil {
			return Result{}, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return Result{}, err
	}
	headers, err := parseHeaders(c["headers"])
	if err != nil {
		return Result{}, err
	}
	req.Header = headers
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", "staqly")
	req.Header.Set("X-Staqly-Post", p.PostID)
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("your endpoint could not be reached: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		snippet := strings.TrimSpace(string(raw))
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}
		return Result{}, fmt.Errorf("your endpoint answered %d %s", resp.StatusCode, snippet)
	}
	var reply struct {
		Status     string `json:"status"`
		URL        string `json:"url"`
		ExternalID string `json:"external_id"`
	}
	_ = json.Unmarshal(raw, &reply) // a plain 2xx without JSON is a success too
	if resp.StatusCode == http.StatusAccepted || reply.Status == "publishing" {
		return Result{}, ErrAsync
	}
	if reply.Status == "failed" {
		return Result{}, errors.New("your endpoint reported a failure")
	}
	return Result{URL: reply.URL, ExternalID: reply.ExternalID}, nil
}
