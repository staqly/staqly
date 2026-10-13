package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Client is a thin HTTP client for the staq.ly REST API. It only speaks the
// public contract (apps/api/openapi.yaml); it never imports server packages.
type Client struct {
	BaseURL   string
	Key       string
	HTTP      *http.Client
	UserAgent string
}

// APIError is a non-2xx response decoded from the standard error envelope.
type APIError struct {
	Status     int
	Code       string
	Message    string
	Fields     map[string]string
	Details    map[string]any
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%s (%d %s)", e.Message, e.Status, e.Code)
	}
	return fmt.Sprintf("%s (HTTP %d)", e.Message, e.Status)
}

// NetError means the server could not be reached at all.
type NetError struct {
	URL string
	Err error
}

func (e *NetError) Error() string { return fmt.Sprintf("cannot reach %s: %v", e.URL, e.Err) }
func (e *NetError) Unwrap() error { return e.Err }

// Request describes one API call. Body is JSON-encoded unless it is an
// io.Reader, in which case ContentType must be set.
type Request struct {
	Method         string
	Path           string
	Query          url.Values
	Body           any
	ContentType    string
	IdempotencyKey string
}

// Do performs the request and returns the raw response body (nil for 204).
func (c *Client) Do(ctx context.Context, r Request) (json.RawMessage, error) {
	u := strings.TrimRight(c.BaseURL, "/") + r.Path
	if len(r.Query) > 0 {
		u += "?" + r.Query.Encode()
	}
	var body io.Reader
	ct := r.ContentType
	switch b := r.Body.(type) {
	case nil:
	case io.Reader:
		body = b
	default:
		buf, err := json.Marshal(b)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(buf)
		ct = "application/json"
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, u, body)
	if err != nil {
		return nil, err
	}
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	req.Header.Set("Accept", "application/json")
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	if c.Key != "" {
		req.Header.Set("Authorization", "Bearer "+c.Key)
	}
	if r.IdempotencyKey != "" {
		req.Header.Set("Idempotency-Key", r.IdempotencyKey)
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &NetError{URL: c.BaseURL, Err: err}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, &NetError{URL: c.BaseURL, Err: err}
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if len(bytes.TrimSpace(data)) == 0 {
			return nil, nil
		}
		return data, nil
	}
	return nil, decodeAPIError(resp, data)
}

func decodeAPIError(resp *http.Response, data []byte) *APIError {
	e := &APIError{Status: resp.StatusCode}
	var env struct {
		Error struct {
			Code    string            `json:"code"`
			Message string            `json:"message"`
			Fields  map[string]string `json:"fields"`
			Details map[string]any    `json:"details"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &env) == nil && (env.Error.Code != "" || env.Error.Message != "") {
		e.Code, e.Message, e.Fields, e.Details = env.Error.Code, env.Error.Message, env.Error.Fields, env.Error.Details
	} else {
		msg := strings.TrimSpace(string(data))
		if len(msg) > 200 {
			msg = msg[:200] + "…"
		}
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		e.Message = msg
	}
	if ra := resp.Header.Get("Retry-After"); ra != "" {
		if n, err := strconv.Atoi(ra); err == nil {
			e.RetryAfter = time.Duration(n) * time.Second
		} else if t, err := http.ParseTime(ra); err == nil {
			e.RetryAfter = time.Until(t)
		}
	}
	return e
}

// Get/Post/Patch/Delete decode the JSON response into out (if non-nil) and
// also return the raw body so --json can print it verbatim.
func (c *Client) Get(ctx context.Context, path string, q url.Values, out any) (json.RawMessage, error) {
	return c.call(ctx, Request{Method: http.MethodGet, Path: path, Query: q}, out)
}

func (c *Client) Post(ctx context.Context, path string, body, out any) (json.RawMessage, error) {
	return c.call(ctx, Request{Method: http.MethodPost, Path: path, Body: body}, out)
}

func (c *Client) Patch(ctx context.Context, path string, body, out any) (json.RawMessage, error) {
	return c.call(ctx, Request{Method: http.MethodPatch, Path: path, Body: body}, out)
}

func (c *Client) Delete(ctx context.Context, path string, q url.Values) error {
	_, err := c.call(ctx, Request{Method: http.MethodDelete, Path: path, Query: q}, nil)
	return err
}

func (c *Client) call(ctx context.Context, r Request, out any) (json.RawMessage, error) {
	raw, err := c.Do(ctx, r)
	if err != nil {
		return nil, err
	}
	if out != nil && raw != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return raw, fmt.Errorf("unexpected response from server: %w", err)
		}
	}
	return raw, nil
}

func newIdempotencyKey() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return "cli-" + hex.EncodeToString(b[:])
}

// friendlyError turns API and network errors into a message for humans.
func friendlyError(err error) string {
	var ae *APIError
	var ne *NetError
	switch {
	case errors.As(err, &ae):
		return friendlyAPIError(ae)
	case errors.As(err, &ne):
		return fmt.Sprintf("cannot reach the staq.ly API at %s (%v).\nIs the server running? Set the address with --url or STAQLY_API_URL.", ne.URL, rootCause(ne.Err))
	case errors.Is(err, context.DeadlineExceeded):
		return "request timed out"
	}
	return err.Error()
}

func rootCause(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

func friendlyAPIError(e *APIError) string {
	var b strings.Builder
	switch {
	case e.Status == 401:
		fmt.Fprintf(&b, "authentication failed: %s\nPass an API key with --key or STAQLY_API_KEY, or store one with `staqly login --url URL --key stk_…`.", e.Message)
	case e.Code == "not_local":
		fmt.Fprintf(&b, "this server only accepts keyless requests from its own machine: %s\nRun the CLI on the server host, or use an API key against a cloud-mode server.", e.Message)
	case e.Code == "csrf_failed":
		fmt.Fprintf(&b, "request rejected (CSRF): %s\nThe CLI should authenticate with an API key, not a browser session.", e.Message)
	case e.Status == 403:
		fmt.Fprintf(&b, "permission denied: %s\nThe API key is missing a scope for this command (see `staqly keys list`).", e.Message)
	case e.Status == 402 || e.Code == "plan_limit_exceeded":
		fmt.Fprintf(&b, "plan limit reached: %s", e.Message)
		if len(e.Details) > 0 {
			fmt.Fprintf(&b, "\n  limit: %v  used: %v  max: %v  plan: %v",
				e.Details["limit"], e.Details["used"], e.Details["max"], e.Details["plan"])
		}
		b.WriteString("\nSee `staqly status` for current usage.")
	case e.Status == 404:
		fmt.Fprintf(&b, "not found: %s", e.Message)
	case e.Status == 409:
		fmt.Fprintf(&b, "conflict: %s\nThe resource changed or was already decided; fetch it again and retry.", e.Message)
	case e.Status == 413:
		fmt.Fprintf(&b, "too large: %s", e.Message)
	case e.Status == 415:
		fmt.Fprintf(&b, "unsupported file type: %s", e.Message)
	case e.Status == 422 || e.Status == 400:
		fmt.Fprintf(&b, "invalid request: %s", e.Message)
		keys := make([]string, 0, len(e.Fields))
		for k := range e.Fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "\n  %s: %s", k, e.Fields[k])
		}
	case e.Status == 429:
		fmt.Fprintf(&b, "rate limited: %s", e.Message)
		if e.RetryAfter > 0 {
			fmt.Fprintf(&b, "\nRetry after %s.", e.RetryAfter.Round(time.Second))
		}
	case e.Status >= 500:
		fmt.Fprintf(&b, "server error: %s (HTTP %d). Check the server logs.", e.Message, e.Status)
	default:
		b.WriteString(e.Error())
	}
	return b.String()
}
