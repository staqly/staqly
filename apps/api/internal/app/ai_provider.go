package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// aiProvider is one AI service (fal.ai, Higgsfield). Both run requests in a queue: Submit hands one over, Check
// asks how it is going, Cancel asks to drop it. A new provider implements this and gets models in aiCatalog.
type aiProvider interface {
	Name() string
	// Submit starts a generation. Its error is an *aiError that says whether the provider took the request.
	// idemKey identifies the request (the job id): a provider that honours it (Idempotent) creates one
	// generation however often the same key is sent.
	Submit(ctx context.Context, model string, c aiCall, idemKey string) (aiRequest, error)
	// Check reports a submitted request's state; Done with OutputURL when the result is ready.
	Check(ctx context.Context, r aiRequest) (aiProgress, error)
	// Cancel asks the provider to drop the request; true when it confirmed it never ran (nothing is charged).
	Cancel(ctx context.Context, r aiRequest) (bool, error)
	// Forget deletes what the provider keeps of a finished request, where it offers that (best effort).
	Forget(ctx context.Context, r aiRequest)
	// Idempotent: the same idemKey may be submitted again after an unanswered submit without a second charge.
	Idempotent() bool
	// Supports: the provider's model does exactly what the call asks (e.g. Higgsfield's Kling text-to-video
	// makes 16:9 only, so a 9:16 request is not sent there).
	Supports(c aiCall) bool
	// download fetches a result file (the provider's CDN; no credentials are sent there).
	download(ctx context.Context, rawURL string, max int64) ([]byte, string, error)
}

// aiCall is a provider-neutral request.
type aiCall struct {
	Op     string
	Prompt string
	// Image and Mask: the picture and, for inpaint/erase, a mask of its size (white = change).
	Image, Mask             []byte
	ImageType               string
	ImageWidth, ImageHeight int
	// Width and Height: the picture generate makes.
	Width, Height int
	// Duration (seconds) and AspectRatio (text_to_video: 16:9, 9:16 or 1:1) of a video.
	Duration    int
	AspectRatio string
	// Expand: the new canvas and where the picture sits in it.
	CanvasWidth, CanvasHeight int
	ImageX, ImageY            int
	PlaceWidth, PlaceHeight   int
}

// aiRequest is a submitted request as the provider identifies it (stored in ai_jobs.provider_request).
type aiRequest struct {
	ID        string `json:"id"`
	StatusURL string `json:"status_url,omitempty"`
	ResultURL string `json:"result_url,omitempty"`
	CancelURL string `json:"cancel_url,omitempty"`
}

type aiProgress struct {
	Done      bool
	Failed    bool
	Refused   bool // content moderation refused the input or the output
	Message   string
	OutputURL string
}

// aiError kinds.
const (
	aiNotAccepted = "not_accepted" // the provider did not take the request (down, busy, refused the key): nothing was charged
	aiInvalid     = "invalid"      // the provider refused the input itself (another provider would too)
	aiUnknown     = "unknown"      // sent, but no answer: it may be running and charged
	aiFailed      = "failed"       // the provider took it and it failed
)

type aiError struct {
	Kind     string
	Provider string
	Status   int
	Msg      string
}

func (e *aiError) Error() string {
	if e.Status > 0 {
		return fmt.Sprintf("%s: %s (%s, HTTP %d)", e.Provider, e.Msg, e.Kind, e.Status)
	}
	return fmt.Sprintf("%s: %s (%s)", e.Provider, e.Msg, e.Kind)
}

func aiErrKind(err error) string {
	var e *aiError
	if errors.As(err, &e) {
		return e.Kind
	}
	return aiUnknown
}

// aiHTTP: each call has its own context deadline; this bounds a stuck connection.
var aiHTTP = &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse // a provider's API never redirects; a redirect would carry the key elsewhere
}}

// aiDownloadHTTP follows redirects (CDNs do) and never carries credentials.
var aiDownloadHTTP = &http.Client{Timeout: 5 * time.Minute}

// aiSubmitTimeout bounds a submit: it answers at once (the work is queued).
const aiSubmitTimeout = 60 * time.Second

// aiBase is what both clients share: the API base, the credentials header and the URL checks.
type aiBase struct {
	name  string
	base  *url.URL
	auth  string
	label string // the provider's name in messages
}

func newAIBase(name, label, rawBase, auth string) aiBase {
	u, err := url.Parse(strings.TrimRight(rawBase, "/"))
	if err != nil || u.Host == "" {
		u = &url.URL{Scheme: "https", Host: "invalid.invalid"}
	}
	return aiBase{name: name, label: label, base: u, auth: auth}
}

func (b aiBase) Name() string { return b.name }

// sameOrigin: URLs the provider answers with (status, result, cancel) get the credentials only on its own host.
func (b aiBase) sameOrigin(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == b.base.Scheme && u.Host == b.base.Host
}

// fileURL: a result or upload URL may be on any host but must be https (http only when the API itself is,
// i.e. in tests).
func (b aiBase) fileURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Host != "" && (u.Scheme == "https" || (u.Scheme == "http" && b.base.Scheme == "http"))
}

// call sends one API request with the credentials and decodes a JSON answer into out (when 2xx).
// It answers the status and the raw body, or an *aiError for a failure of the connection.
func (b aiBase) call(ctx context.Context, method, rawURL string, body any, headers map[string]string, out any) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, rd)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", b.auth)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := aiHTTP.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return 0, nil, ctx.Err()
		}
		kind := aiUnknown
		var op *net.OpError
		if errors.As(err, &op) && op.Op == "dial" {
			kind = aiNotAccepted // never connected: nothing was sent
		}
		return 0, nil, &aiError{Kind: kind, Provider: b.name, Msg: b.label + " did not answer"}
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return res.StatusCode, nil, &aiError{Kind: aiUnknown, Provider: b.name, Status: res.StatusCode, Msg: b.label + "'s answer broke off"}
	}
	if res.StatusCode < 300 && out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return res.StatusCode, raw, &aiError{Kind: aiUnknown, Provider: b.name, Status: res.StatusCode, Msg: b.label + " answered something unexpected"}
		}
	}
	return res.StatusCode, raw, nil
}

func (b aiBase) download(ctx context.Context, rawURL string, max int64) ([]byte, string, error) {
	if !b.fileURL(rawURL) {
		return nil, "", &aiError{Kind: aiFailed, Provider: b.name, Msg: b.label + " sent a result address that is not https"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	res, err := aiDownloadHTTP.Do(req)
	if err != nil {
		return nil, "", &aiError{Kind: aiFailed, Provider: b.name, Msg: "the result could not be downloaded from " + b.label}
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, "", &aiError{Kind: aiFailed, Provider: b.name, Status: res.StatusCode, Msg: "the result could not be downloaded from " + b.label}
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, max+1))
	if err != nil || int64(len(raw)) > max {
		return nil, "", &aiError{Kind: aiFailed, Provider: b.name, Msg: "the result from " + b.label + " is too large or broke off"}
	}
	ct, _, _ := strings.Cut(sniff(raw[:min(len(raw), 512)], ""), ";")
	return raw, ct, nil
}

// detail is the error text of a provider's JSON error body ({"detail": "..."} or {"detail": [{"msg": ...}]}).
func aiDetail(raw []byte) string {
	var v struct {
		Detail json.RawMessage `json:"detail"`
		Error  string          `json:"error"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	var s string
	if json.Unmarshal(v.Detail, &s) == nil && s != "" {
		return truncate(s, 300)
	}
	var list []struct {
		Msg string `json:"msg"`
	}
	if json.Unmarshal(v.Detail, &list) == nil && len(list) > 0 {
		return truncate(list[0].Msg, 300)
	}
	return truncate(v.Error, 300)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "..."
	}
	return s
}

// dataURI inlines a picture (fal.ai takes base64 data URIs as file inputs; the editor sends at most 2048 px).
func dataURI(ct string, b []byte) string {
	return "data:" + ct + ";base64," + base64.StdEncoding.EncodeToString(b)
}

// submitError turns a submit's HTTP status into an *aiError: refusals of the input are invalid, a provider
// that would not take the request (key, balance, limits, outage) is not_accepted, anything else unknown.
func (b aiBase) submitError(status int, raw []byte) *aiError {
	msg := aiDetail(raw)
	e := &aiError{Provider: b.name, Status: status, Msg: msg}
	switch {
	case status == 400 || status == 422:
		e.Kind = aiInvalid
		if e.Msg == "" {
			e.Msg = b.label + " refused the request"
		}
	case status == 401 || status == 403:
		e.Kind, e.Msg = aiNotAccepted, b.label+" refused the server's key or has no balance left"
	case status == 404 || status == 405 || status == 423 || status == 429 || status == 503 || status == 502:
		e.Kind, e.Msg = aiNotAccepted, b.label+" is not taking requests right now"
	default:
		e.Kind, e.Msg = aiUnknown, fmt.Sprintf("%s answered %d", b.label, status)
	}
	return e
}
