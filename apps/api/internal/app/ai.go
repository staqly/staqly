package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// The design editor's AI tools run on the workspace's own provider account: an admin adds an OpenAI API key,
// which is sealed like channel credentials and never sent back to a browser (only its last four characters).
// Every call goes through here: scopes checked, workspace scoped, rate limited, with a timeout, and only when a
// person clicks an AI action. staqly never pays for these calls; the provider bills the key's owner.

// AIConfig: AI_DISABLED turns the tools off for the whole server; AI_OPENAI_BASE_URL points at another
// OpenAI-compatible endpoint (tests use a fake one).
type AIConfig struct {
	Disabled      bool
	OpenAIBaseURL string
}

// AICapabilities: what the configured model can do for the editor.
type AICapabilities struct {
	Generate bool `json:"generate"` // a picture from a prompt
	Edit     bool `json:"edit"`     // change a picture by a prompt
	Inpaint  bool `json:"inpaint"`  // change only a marked area (remove an object)
	Expand   bool `json:"expand"`   // fill the new area around a picture
}

// aiModels are the OpenAI image models the editor knows, with what each can do and the sizes it makes.
var aiModels = map[string]struct {
	caps  AICapabilities
	sizes []string
}{
	"gpt-image-1":      {AICapabilities{true, true, true, true}, []string{"1024x1024", "1536x1024", "1024x1536", "auto"}},
	"gpt-image-1-mini": {AICapabilities{true, true, true, true}, []string{"1024x1024", "1536x1024", "1024x1536", "auto"}},
	"dall-e-3":         {AICapabilities{Generate: true}, []string{"1024x1024", "1792x1024", "1024x1792"}},
	"dall-e-2":         {AICapabilities{Generate: true, Edit: true, Inpaint: true}, []string{"256x256", "512x512", "1024x1024"}},
}

// DefaultAIModel is what a new key uses unless the admin picks another image model.
const DefaultAIModel = "gpt-image-1"

// AIStatus is what the editor shows: whether AI is set up, with which model, and what it can do.
type AIStatus struct {
	Configured   bool           `json:"configured"`
	Provider     string         `json:"provider"`
	Model        string         `json:"model"`
	KeyHint      string         `json:"key_hint"`
	Capabilities AICapabilities `json:"capabilities"`
	// Reason says why AI is off or limited, in plain words ("" when everything works).
	Reason    string   `json:"reason"`
	Models    []string `json:"models"`
	CanManage bool     `json:"can_manage"`
	UpdatedAt string   `json:"updated_at,omitempty"`
}

type aiCredentials struct {
	APIKey string `json:"api_key"`
}

func knownAIModels() []string {
	out := make([]string, 0, len(aiModels))
	for m := range aiModels {
		out = append(out, m)
	}
	slices.Sort(out)
	return out
}

// GetAIStatus tells anyone who can use the media library whether the editor's AI tools work.
func (s *Service) GetAIStatus(ctx context.Context, p Principal) (AIStatus, error) {
	if err := p.require(ScopeMediaRead); err != nil {
		return AIStatus{}, err
	}
	st := AIStatus{Provider: "openai", Models: knownAIModels(), CanManage: p.Has(ScopeAdmin)}
	if s.Cfg.AI.Disabled {
		st.Reason = "AI tools are turned off on this server."
		return st, nil
	}
	row, err := s.Q.GetWorkspaceAI(ctx, p.WorkspaceID)
	if isNoRows(err) {
		st.Reason = "Add an OpenAI API key under Integrations to use the AI tools."
		return st, nil
	}
	if err != nil {
		return AIStatus{}, err
	}
	st.Configured, st.Provider, st.Model, st.KeyHint, st.UpdatedAt = true, row.Provider, row.Model, row.KeyHint, row.UpdatedAt
	if m, ok := aiModels[row.Model]; ok {
		st.Capabilities = m.caps
	} else {
		st.Reason = row.Model + " makes no pictures: choose an image model."
	}
	return st, nil
}

// AISettingsInput sets the workspace's AI key (admin only). An empty APIKey keeps the stored one (to change
// only the model).
type AISettingsInput struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	APIKey   string `json:"api_key"`
}

// SetAISettings checks the key with the provider (listing the model costs nothing), then seals and stores it.
func (s *Service) SetAISettings(ctx context.Context, p Principal, in AISettingsInput) (AIStatus, error) {
	if err := p.require(ScopeAdmin); err != nil {
		return AIStatus{}, err
	}
	if s.Cfg.AI.Disabled {
		return AIStatus{}, &Error{Code: "not_available", Status: 409, Message: "AI tools are turned off on this server"}
	}
	if in.Provider == "" {
		in.Provider = "openai"
	}
	if in.Provider != "openai" {
		return AIStatus{}, invalid("provider", "must be openai")
	}
	if in.Model == "" {
		in.Model = DefaultAIModel
	}
	if _, ok := aiModels[in.Model]; !ok {
		return AIStatus{}, invalid("model", "must be one of "+strings.Join(knownAIModels(), ", "))
	}
	key := strings.TrimSpace(in.APIKey)
	if key == "" {
		row, err := s.Q.GetWorkspaceAI(ctx, p.WorkspaceID)
		if err != nil {
			return AIStatus{}, invalid("api_key", "is required")
		}
		var c aiCredentials
		if err := s.unseal(row.Credentials, &c); err != nil {
			return AIStatus{}, err
		}
		key = c.APIKey
	}
	if len(key) < 20 || len(key) > 400 || strings.ContainsAny(key, " \r\n\t") {
		return AIStatus{}, invalid("api_key", "does not look like an API key")
	}
	if err := s.checkAIKey(ctx, key, in.Model); err != nil {
		return AIStatus{}, err
	}
	sealed, err := s.seal(aiCredentials{APIKey: key})
	if err != nil {
		return AIStatus{}, err
	}
	if err := s.W.UpsertWorkspaceAI(ctx, db.UpsertWorkspaceAIParams{WorkspaceID: p.WorkspaceID, Provider: in.Provider,
		Model: in.Model, Credentials: sealed, KeyHint: "..." + key[len(key)-4:], UpdatedBy: p.UserID, UpdatedAt: s.now()}); err != nil {
		return AIStatus{}, err
	}
	return s.GetAIStatus(ctx, p)
}

// DeleteAISettings forgets the workspace's AI key (admin only).
func (s *Service) DeleteAISettings(ctx context.Context, p Principal) error {
	if err := p.require(ScopeAdmin); err != nil {
		return err
	}
	_, err := s.W.DeleteWorkspaceAI(ctx, p.WorkspaceID)
	return err
}

// AIImageRequest is one generate or edit call. Image and Mask are PNG/JPEG/WebP bytes; the mask's transparent
// pixels mark what may change (OpenAI's convention).
type AIImageRequest struct {
	Prompt string
	Size   string
	Image  []byte
	Mask   []byte
}

// MaxAIPromptRunes and MaxAIImageBytes bound one request (the provider's own limits are close to these).
const (
	MaxAIPromptRunes = 4000
	MaxAIImageBytes  = 20 << 20
)

// AIImage runs one AI picture operation for the design editor ("generate" or "edit") and answers PNG bytes.
func (s *Service) AIImage(ctx context.Context, p Principal, op string, in AIImageRequest) ([]byte, error) {
	if err := p.require(ScopeMediaWrite); err != nil {
		return nil, err
	}
	st, err := s.GetAIStatus(ctx, p)
	if err != nil {
		return nil, err
	}
	if !st.Configured || s.Cfg.AI.Disabled {
		return nil, &Error{Code: "not_available", Status: 409, Message: st.Reason}
	}
	in.Prompt = strings.TrimSpace(in.Prompt)
	switch {
	case in.Prompt == "":
		return nil, invalid("prompt", "is required")
	case len([]rune(in.Prompt)) > MaxAIPromptRunes:
		return nil, invalid("prompt", fmt.Sprintf("must be at most %d characters", MaxAIPromptRunes))
	case len(in.Image) > MaxAIImageBytes || len(in.Mask) > MaxAIImageBytes:
		return nil, &Error{Code: "too_large", Status: 413, Message: "the picture is larger than 20 MB"}
	}
	model := aiModels[st.Model]
	if in.Size == "" {
		in.Size = model.sizes[0]
	}
	if !slices.Contains(model.sizes, in.Size) {
		return nil, invalid("size", st.Model+" makes "+strings.Join(model.sizes, ", "))
	}
	switch {
	case op == "generate" && !model.caps.Generate,
		op == "edit" && len(in.Mask) == 0 && !model.caps.Edit,
		op == "edit" && len(in.Mask) > 0 && !model.caps.Inpaint:
		return nil, &Error{Code: "not_available", Status: 409, Message: st.Model + " cannot do this; choose gpt-image-1 under Integrations"}
	case op == "edit" && len(in.Image) == 0:
		return nil, invalid("image", "is required")
	case op != "generate" && op != "edit":
		return nil, notFound("AI operation")
	}
	if !aiLimiter.allow(p.WorkspaceID, s.clock()) {
		return nil, &Error{Code: "rate_limited", Status: 429, Message: fmt.Sprintf("at most %d AI pictures per %s per workspace; try again in a few minutes", aiLimitCalls, aiLimitWindow)}
	}
	row, err := s.Q.GetWorkspaceAI(ctx, p.WorkspaceID)
	if err != nil {
		return nil, err
	}
	var c aiCredentials
	if err := s.unseal(row.Credentials, &c); err != nil {
		return nil, err
	}
	start := s.clock()
	out, err := s.callOpenAIImage(ctx, c.APIKey, st.Model, op, in)
	ms := s.clock().Sub(start).Milliseconds()
	s.logger(ctx).Info("ai image", "op", op, "model", st.Model, "ms", ms, "ok", err == nil)
	s.trackAs(nil, p, "ai_image_requested", "op", op, "model", st.Model, "duration_ms", ms, "ok", err == nil)
	return out, err
}

// clock is time.Now, kept in one place for the limiter.
func (s *Service) clock() time.Time { return time.Now() }

// aiLimiter: a sliding window per workspace. ponytail: in memory, per process; a cluster of servers allows
// each its own window (use the jobs table if that ever matters).
var aiLimiter = &windowLimiter{hits: map[string][]time.Time{}}

const (
	aiLimitCalls  = 30
	aiLimitWindow = 10 * time.Minute
)

type windowLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func (l *windowLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if now.Sub(t) < aiLimitWindow {
			kept = append(kept, t)
		}
	}
	if len(kept) >= aiLimitCalls {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	return true
}

// aiHTTP waits long enough for a large picture (gpt-image-1 takes up to a minute or two).
var aiHTTP = &http.Client{Timeout: 3 * time.Minute}

// checkAIKey asks the provider about the model: 401 = wrong key, 404 = the key cannot use that model.
func (s *Service) checkAIKey(ctx context.Context, key, model string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.Cfg.AI.OpenAIBaseURL+"/models/"+model, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	res, err := aiHTTP.Do(req)
	if err != nil {
		return &Error{Code: "ai_provider_error", Status: 502, Message: "OpenAI did not answer; try again"}
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
	switch {
	case res.StatusCode == http.StatusUnauthorized:
		return invalid("api_key", "OpenAI does not accept this key")
	case res.StatusCode == http.StatusNotFound, res.StatusCode == http.StatusForbidden:
		return invalid("model", "this key cannot use "+model+" (verify the organization for gpt-image-1, or pick another model)")
	case res.StatusCode >= 300:
		return &Error{Code: "ai_provider_error", Status: 502, Message: fmt.Sprintf("OpenAI answered %d; try again", res.StatusCode)}
	}
	return nil
}

func (s *Service) callOpenAIImage(ctx context.Context, key, model, op string, in AIImageRequest) ([]byte, error) {
	var (
		body        io.Reader
		contentType string
	)
	b64 := !strings.HasPrefix(model, "gpt-image") // gpt-image answers base64 always and rejects response_format
	if op == "generate" {
		payload := map[string]any{"model": model, "prompt": in.Prompt, "size": in.Size, "n": 1}
		if b64 {
			payload["response_format"] = "b64_json"
		}
		raw, _ := json.Marshal(payload)
		body, contentType = bytes.NewReader(raw), "application/json"
	} else {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		_ = mw.WriteField("model", model)
		_ = mw.WriteField("prompt", in.Prompt)
		_ = mw.WriteField("size", in.Size)
		_ = mw.WriteField("n", "1")
		if b64 {
			_ = mw.WriteField("response_format", "b64_json")
		}
		for name, data := range map[string][]byte{"image": in.Image, "mask": in.Mask} {
			if len(data) == 0 {
				continue
			}
			ct := http.DetectContentType(data)
			h := make(map[string][]string)
			h["Content-Disposition"] = []string{fmt.Sprintf(`form-data; name=%q; filename="%s.png"`, name, name)}
			h["Content-Type"] = []string{ct}
			w, err := mw.CreatePart(h)
			if err != nil {
				return nil, err
			}
			_, _ = w.Write(data)
		}
		_ = mw.Close()
		body, contentType = &buf, mw.FormDataContentType()
	}
	path := "/images/generations"
	if op == "edit" {
		path = "/images/edits"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.Cfg.AI.OpenAIBaseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", contentType)
	res, err := aiHTTP.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil, ctx.Err()
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, &Error{Code: "timeout", Status: 504, Message: "OpenAI took too long; try again or ask for a smaller picture"}
		}
		return nil, &Error{Code: "ai_provider_error", Status: 502, Message: "OpenAI did not answer; try again"}
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	if err != nil {
		return nil, &Error{Code: "ai_provider_error", Status: 502, Message: "OpenAI's answer broke off; try again"}
	}
	var out struct {
		Data []struct {
			B64 string `json:"b64_json"`
		} `json:"data"`
		Error *struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &out)
	if res.StatusCode >= 300 || len(out.Data) == 0 {
		return nil, openAIError(res.StatusCode, out.Error)
	}
	img, err := base64.StdEncoding.DecodeString(out.Data[0].B64)
	if err != nil || len(img) == 0 {
		return nil, &Error{Code: "ai_provider_error", Status: 502, Message: "OpenAI sent no picture; try again"}
	}
	return img, nil
}

// openAIError turns a provider failure into what the editor shows (the provider's own message when it is about
// the request, e.g. a refused prompt).
func openAIError(status int, e *struct {
	Message string `json:"message"`
	Code    string `json:"code"`
}) error {
	msg := ""
	if e != nil {
		msg = e.Message
	}
	switch {
	case status == http.StatusUnauthorized:
		return &Error{Code: "ai_key_rejected", Status: 409, Message: "OpenAI no longer accepts the workspace's key; an admin can replace it under Integrations"}
	case status == http.StatusTooManyRequests:
		return &Error{Code: "rate_limited", Status: 429, Message: "OpenAI is limiting this key (rate or billing): " + msg}
	case status == http.StatusBadRequest && msg != "":
		return &Error{Code: "validation_failed", Status: 422, Message: "OpenAI refused it: " + msg}
	}
	return &Error{Code: "ai_provider_error", Status: 502, Message: fmt.Sprintf("OpenAI answered %d; try again", status)}
}
