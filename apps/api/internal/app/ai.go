package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// The design editor's and the media library's AI tools run on the server's own provider accounts: the operator
// sets AI_FAL_KEY (fal.ai) and/or AI_HIGGSFIELD_API_KEY (Higgsfield), tried in AI_MEDIA_PROVIDERS' order. Workspaces never
// see or set a key, and no key reaches a browser; without one the AI tools are hidden and everything else works.
//
// Every request is a job (ai_jobs + the job queue): it is checked here (scopes, workspace, input, rate limit,
// the monthly cost limit), its pictures are kept as temporary files, and the worker hands it to the first provider
// that has a model for it (ai_catalog.go), falling back to the next only when the first did not take it. The
// worker never sends a request again when it cannot tell whether the provider took it (unless the provider
// deduplicates by the job id, as Higgsfield does), and nothing is retried automatically after a provider failed
// it: a person asks again. The result waits as a temporary file until the person keeps it (it becomes a library
// file) or discards it; a day later the files and the prompt are deleted and the row stays as usage.

// AIConfig is the server's AI setup (internal/config reads the AI_* variables).
type AIConfig struct {
	FalKey            string
	FalBaseURL        string
	HiggsfieldKey     string // "<key id>:<key secret>"
	HiggsfieldBaseURL string
	// Operations offered (empty: every operation a configured provider has a model for).
	Operations []string
	// Prices overrides the catalog's price of a model (AI_PRICES).
	Prices map[string]float64
	// TokenPrices: the assistant's language models per million tokens (ai_prices.json + AI_PRICES_FILE).
	TokenPrices map[string]TokenPrice
	// Assistant: the language model of the dashboard's assistant (AI_ASSISTANT_*). AssistantFallback answers when
	// it can't (AI_ASSISTANT_FALLBACK_*; optional): the workspace's allowance ran out, or the model refused the
	// request before answering (its own quota, an outage).
	Assistant, AssistantFallback AIEndpoint
	// AssistantBrowserModel: a WebLLM model the dashboard can run on the device, text only, when neither answers
	// (AI_ASSISTANT_BROWSER_MODEL; "" = none).
	AssistantBrowserModel string
	// MediaProviders: the order the picture and video tools try the providers in (AI_MEDIA_PROVIDERS; nil = fal,
	// higgsfield). A provider left out is not used for media.
	MediaProviders []string
	// PollInterval: how often a running job asks its provider (0: 2 s for pictures, 5 s for videos; tests shorten it).
	PollInterval time.Duration
}

// AIEndpoint is an OpenAI-compatible language model; without BaseURL it is fal.ai's router (AI_FAL_KEY).
type AIEndpoint struct {
	Model   string // "" = DefaultAssistantModel
	BaseURL string // e.g. https://api.openai.com/v1
	APIKey  string // empty for none (a local model server)
	// Prices: USD per million input and output tokens, counted when the endpoint reports no cost
	// (nil = Gemini 2.5 Flash's; 0,0 for a model that costs nothing, such as a local one).
	Prices *[2]float64
}

// aiMonthlyLimit: what the workspace may spend on AI this month in USD (estimated), from its plan's
// ai_monthly_cents; < 0 = no limit (local mode, and every plan for now: usage is recorded so limits can come).
func (s *Service) aiMonthlyLimit(ctx context.Context, q *db.Queries, workspaceID string) (float64, error) {
	if s.Cfg.Mode == ModeLocal {
		return -1, nil
	}
	plan, err := s.planFor(ctx, q, workspaceID)
	if err != nil || plan.Limits.AIMonthlyCents < 0 {
		return -1, err
	}
	return float64(plan.Limits.AIMonthlyCents) / 100, nil
}

// Bounds of one request.
const (
	MaxAIPromptRunes = 4000
	MaxAIImageBytes  = 20 << 20
	maxAIEdge        = 4096
	maxAIImageResult = 50 << 20
	maxAIVideoResult = 300 << 20
	aiMaxLive        = 4              // queued or running jobs per workspace
	aiFilesKept      = 24 * time.Hour // inputs, results and the prompt
)

// aiProviders are the configured media providers, in AI_MEDIA_PROVIDERS' order.
func (s *Service) aiProviders() []aiProvider {
	var out []aiProvider
	order := s.Cfg.AI.MediaProviders
	if order == nil {
		order = []string{AIProviderFal, AIProviderHiggsfield}
	}
	for _, name := range order {
		switch {
		case name == AIProviderFal && s.Cfg.AI.FalKey != "":
			out = append(out, newFal(s.Cfg.AI.FalKey, s.Cfg.AI.FalBaseURL))
		case name == AIProviderHiggsfield && s.Cfg.AI.HiggsfieldKey != "":
			out = append(out, newHiggsfield(s.Cfg.AI.HiggsfieldKey, s.Cfg.AI.HiggsfieldBaseURL))
		}
	}
	return out
}

func (s *Service) aiProvider(name string) aiProvider {
	for _, p := range s.aiProviders() {
		if p.Name() == name {
			return p
		}
	}
	return nil
}

func aiModelFor(provider, op string) (aiModel, bool) {
	for _, m := range aiCatalog {
		if m.Provider == provider && m.Op == op {
			return m, true
		}
	}
	return aiModel{}, false
}

type aiCandidate struct {
	prov  aiProvider
	model aiModel
}

// aiChain: who runs the call, in order: every configured provider with a model for the operation that does
// exactly what the call asks.
func (s *Service) aiChain(c aiCall) []aiCandidate {
	var out []aiCandidate
	for _, p := range s.aiProviders() {
		if m, ok := aiModelFor(p.Name(), c.Op); ok && p.Supports(c) {
			out = append(out, aiCandidate{p, m})
		}
	}
	return out
}

func (s *Service) aiOffered(op string) bool {
	if len(s.Cfg.AI.Operations) > 0 && !slices.Contains(s.Cfg.AI.Operations, op) {
		return false
	}
	for _, p := range s.aiProviders() {
		if _, ok := aiModelFor(p.Name(), op); ok {
			return true
		}
	}
	return false
}

// AIStatus is what the editor needs: whether AI is on, the operations it can offer and the month's usage.
// It holds no key and nothing else secret.
type AIStatus struct {
	Configured bool          `json:"configured"`
	Providers  []string      `json:"providers"`
	Operations []AIOperation `json:"operations"`
	Usage      AIUsage       `json:"usage"`
	// AssistantBrowserModel: the WebLLM model the dashboard may run on the device when the assistant can't
	// answer on the server ("" = none).
	AssistantBrowserModel string `json:"assistant_browser_model,omitempty"`
}

// AIOperation is one offered operation: what it takes, and who runs it (Provider with Model, Fallback when that
// provider does not take a request).
type AIOperation struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Prompt   string `json:"prompt"`
	Image    bool   `json:"image"`
	Mask     bool   `json:"mask"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Fallback string `json:"fallback"`
}

// AIUsage: the workspace's estimated AI spending this month (UTC) and its limit (nil: none).
type AIUsage struct {
	Period   string   `json:"period"`
	UsedUSD  float64  `json:"used_usd"`
	LimitUSD *float64 `json:"limit_usd"`
}

// GetAIStatus tells anyone who can use the media library which AI tools work.
func (s *Service) GetAIStatus(ctx context.Context, p Principal) (AIStatus, error) {
	if err := p.require(ScopeMediaRead); err != nil {
		return AIStatus{}, err
	}
	st := AIStatus{Providers: []string{}, Operations: []AIOperation{}, AssistantBrowserModel: s.Cfg.AI.AssistantBrowserModel}
	for _, pr := range s.aiProviders() {
		st.Providers = append(st.Providers, pr.Name())
	}
	for _, o := range aiOps {
		if o.ID == AIAssistant { // its own endpoints, or fal.ai's router
			if s.AssistantAvailable() {
				eps := s.assistantEndpoints()
				op := AIOperation{ID: o.ID, Kind: o.Kind, Prompt: o.Prompt, Provider: eps[0].provider(), Model: eps[0].Model}
				if len(eps) > 1 {
					op.Fallback = eps[1].provider()
				}
				st.Operations = append(st.Operations, op)
			}
			continue
		}
		if !s.aiOffered(o.ID) {
			continue
		}
		op := AIOperation{ID: o.ID, Kind: o.Kind, Prompt: o.Prompt, Image: o.Image, Mask: o.Mask}
		for _, pr := range s.aiProviders() {
			if m, ok := aiModelFor(pr.Name(), o.ID); ok {
				if op.Provider == "" {
					op.Provider, op.Model = pr.Name(), m.ID
				} else if op.Fallback == "" {
					op.Fallback = pr.Name()
				}
			}
		}
		st.Operations = append(st.Operations, op)
	}
	st.Configured = len(st.Operations) > 0
	used, err := s.Q.SumAICost(ctx, db.SumAICostParams{WorkspaceID: p.WorkspaceID, CreatedAt: s.monthStart()})
	if err != nil {
		return AIStatus{}, err
	}
	st.Usage = AIUsage{Period: s.period(), UsedUSD: float64(used) / 1e6}
	l, err := s.aiMonthlyLimit(ctx, s.Q, p.WorkspaceID)
	if err != nil {
		return AIStatus{}, err
	}
	if l >= 0 {
		st.Usage.LimitUSD = &l
	}
	return st, nil
}

func (s *Service) monthStart() string {
	n := s.Now().UTC()
	return formatTime(time.Date(n.Year(), n.Month(), 1, 0, 0, 0, 0, time.UTC))
}

// AIJobInput is one request (POST /v1/ai/jobs).
type AIJobInput struct {
	Operation      string
	Prompt         string
	Image, Mask    []byte
	Width, Height  int
	Duration       int
	AspectRatio    string
	CanvasWidth    int
	CanvasHeight   int
	ImageX, ImageY int
	PlaceWidth     int
	PlaceHeight    int
	IdempotencyKey string
}

// aiParams are a job's settings besides the prompt and the files (ai_jobs.params).
type aiParams struct {
	ImageType    string `json:"image_type,omitempty"`
	ImageWidth   int    `json:"image_width,omitempty"`
	ImageHeight  int    `json:"image_height,omitempty"`
	Width        int    `json:"width,omitempty"`
	Height       int    `json:"height,omitempty"`
	Duration     int    `json:"duration,omitempty"`
	AspectRatio  string `json:"aspect_ratio,omitempty"`
	CanvasWidth  int    `json:"canvas_width,omitempty"`
	CanvasHeight int    `json:"canvas_height,omitempty"`
	ImageX       int    `json:"image_x,omitempty"`
	ImageY       int    `json:"image_y,omitempty"`
	PlaceWidth   int    `json:"place_width,omitempty"`
	PlaceHeight  int    `json:"place_height,omitempty"`
}

func (c aiCall) params() aiParams {
	return aiParams{c.ImageType, c.ImageWidth, c.ImageHeight, c.Width, c.Height, c.Duration, c.AspectRatio,
		c.CanvasWidth, c.CanvasHeight, c.ImageX, c.ImageY, c.PlaceWidth, c.PlaceHeight}
}

func (pr aiParams) call(op, prompt string, image, mask []byte) aiCall {
	return aiCall{Op: op, Prompt: prompt, Image: image, Mask: mask, ImageType: pr.ImageType, ImageWidth: pr.ImageWidth,
		ImageHeight: pr.ImageHeight, Width: pr.Width, Height: pr.Height, Duration: pr.Duration, AspectRatio: pr.AspectRatio,
		CanvasWidth: pr.CanvasWidth, CanvasHeight: pr.CanvasHeight, ImageX: pr.ImageX, ImageY: pr.ImageY,
		PlaceWidth: pr.PlaceWidth, PlaceHeight: pr.PlaceHeight}
}

// aiPicture checks an input picture: PNG, JPEG or WebP, at most 20 MB and 4096 px a side.
func aiPicture(field string, b []byte, pngOnly bool) (string, int, int, error) {
	if len(b) > MaxAIImageBytes {
		return "", 0, 0, &Error{Code: "too_large", Status: 413, Message: "the picture is larger than 20 MB"}
	}
	ct := sniff(b[:min(len(b), 512)], "")
	if pngOnly && ct != "image/png" {
		return "", 0, 0, invalid(field, "must be a PNG")
	}
	if ct != "image/png" && ct != "image/jpeg" && ct != "image/webp" {
		return "", 0, 0, invalid(field, "must be a PNG, JPEG or WebP picture")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return "", 0, 0, invalid(field, "is not a readable picture")
	}
	if cfg.Width < 16 || cfg.Height < 16 || cfg.Width > maxAIEdge || cfg.Height > maxAIEdge {
		return "", 0, 0, invalid(field, fmt.Sprintf("must be 16 to %d pixels on each side", maxAIEdge))
	}
	return ct, cfg.Width, cfg.Height, nil
}

// aiCallFor validates a request and turns it into a provider-neutral call.
func aiCallFor(op aiOpSpec, in AIJobInput) (aiCall, error) {
	c := aiCall{Op: op.ID, Prompt: strings.TrimSpace(in.Prompt)}
	switch {
	case op.Prompt == "none":
		c.Prompt = ""
	case op.Prompt == "required" && c.Prompt == "":
		return c, invalid("prompt", "is required")
	case len([]rune(c.Prompt)) > MaxAIPromptRunes:
		return c, invalid("prompt", fmt.Sprintf("must be at most %d characters", MaxAIPromptRunes))
	}
	if op.Image {
		if len(in.Image) == 0 {
			return c, invalid("image", "is required")
		}
		var err error
		if c.ImageType, c.ImageWidth, c.ImageHeight, err = aiPicture("image", in.Image, op.ID == AIUpscale); err != nil {
			return c, err
		}
		c.Image = in.Image
	}
	if op.Mask {
		if len(in.Mask) == 0 {
			return c, invalid("mask", "is required")
		}
		_, w, h, err := aiPicture("mask", in.Mask, true)
		if err != nil {
			return c, err
		}
		if w != c.ImageWidth || h != c.ImageHeight {
			return c, invalid("mask", "must be the size of the picture")
		}
		c.Mask = in.Mask
	}
	switch op.ID {
	case AIGenerate:
		c.Width, c.Height = in.Width, in.Height
		if c.Width == 0 && c.Height == 0 {
			c.Width, c.Height = 1024, 1024
		}
		if c.Width < 256 || c.Height < 256 || c.Width > 2048 || c.Height > 2048 {
			return c, invalid("width", "width and height must be 256 to 2048 pixels")
		}
		c.Width, c.Height = c.Width/16*16, c.Height/16*16
	case AITextToVideo, AIImageToVideo:
		c.Duration = in.Duration
		if c.Duration == 0 {
			c.Duration = 5
		}
		if c.Duration != 5 && c.Duration != 10 {
			return c, invalid("duration", "must be 5 or 10 seconds")
		}
		if op.ID == AITextToVideo {
			c.AspectRatio = in.AspectRatio
			if c.AspectRatio == "" {
				c.AspectRatio = "16:9"
			}
			if !slices.Contains([]string{"16:9", "9:16", "1:1"}, c.AspectRatio) {
				return c, invalid("aspect_ratio", "must be 16:9, 9:16 or 1:1")
			}
		}
	case AIExpand:
		c.CanvasWidth, c.CanvasHeight, c.ImageX, c.ImageY = in.CanvasWidth, in.CanvasHeight, in.ImageX, in.ImageY
		c.PlaceWidth, c.PlaceHeight = in.PlaceWidth, in.PlaceHeight
		if c.PlaceWidth == 0 && c.PlaceHeight == 0 {
			c.PlaceWidth, c.PlaceHeight = c.ImageWidth, c.ImageHeight
		}
		switch {
		case c.CanvasWidth < 64 || c.CanvasHeight < 64 || c.CanvasWidth > maxAIEdge || c.CanvasHeight > maxAIEdge:
			return c, invalid("canvas_width", fmt.Sprintf("the new size must be 64 to %d pixels on each side", maxAIEdge))
		case c.PlaceWidth < 16 || c.PlaceHeight < 16 || c.ImageX < 0 || c.ImageY < 0 ||
			c.ImageX+c.PlaceWidth > c.CanvasWidth || c.ImageY+c.PlaceHeight > c.CanvasHeight:
			return c, invalid("image_x", "the picture must sit inside the new size")
		case c.PlaceWidth == c.CanvasWidth && c.PlaceHeight == c.CanvasHeight:
			return c, invalid("canvas_width", "the new size must be larger than the picture")
		}
	}
	return c, nil
}

func aiInputHash(c aiCall) string {
	h := sha256.New()
	img, mask := sha256.Sum256(c.Image), sha256.Sum256(c.Mask)
	fmt.Fprintf(h, "%s\n%s\n%s\n%x\n%x", c.Op, c.Prompt, mustJSON(c.params()), img, mask)
	return hex.EncodeToString(h.Sum(nil))
}

func aiFileKey(workspaceID, jobID, name string) string {
	return workspaceKey(workspaceID) + "ai/" + jobID + "/" + name
}

// AIJob is one AI request as clients see it.
type AIJob struct {
	ID        string `json:"id"`
	Operation string `json:"operation"`
	Kind      string `json:"kind"`
	// Status: queued, running, succeeded, failed or canceled.
	Status string `json:"status"`
	// Provider and Model that ran (or run) it; FallbackFrom: the provider that did not take it first.
	Provider     string       `json:"provider"`
	Model        string       `json:"model"`
	FallbackFrom string       `json:"fallback_from"`
	Error        *AIJobError  `json:"error"`
	Result       *AIJobResult `json:"result"`
	MediaID      string       `json:"media_id"`
	CostUSD      float64      `json:"estimated_cost_usd"`
	DurationMs   int64        `json:"duration_ms"`
	CreatedAt    string       `json:"created_at"`
	FinishedAt   string       `json:"finished_at"`
}

type AIJobError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// AIJobResult: the made file, waiting at GET /v1/ai/jobs/{id}/result until it is kept or discarded.
type AIJobResult struct {
	MimeType string `json:"mime_type"`
	Width    int64  `json:"width"`
	Height   int64  `json:"height"`
}

func toAIJob(r db.AiJob) AIJob {
	j := AIJob{ID: r.ID, Operation: r.Operation, Kind: r.Kind, Status: r.Status, Provider: r.Provider, Model: r.Model,
		FallbackFrom: r.FallbackFrom, MediaID: r.MediaID, CostUSD: float64(r.CostMicros) / 1e6, DurationMs: r.DurationMs,
		CreatedAt: r.CreatedAt, FinishedAt: r.FinishedAt}
	if r.ErrorCode != "" {
		j.Error = &AIJobError{Code: r.ErrorCode, Message: r.Error}
	}
	if r.ResultKey != "" {
		j.Result = &AIJobResult{MimeType: r.ResultType, Width: r.ResultWidth, Height: r.ResultHeight}
	}
	return j
}

// actor: who a job belongs to for duplicates and idempotency keys (the person, or the key/session).
func (p Principal) actor() string {
	if p.UserID != "" {
		return p.UserID
	}
	return p.Kind + ":" + p.ID
}

// CreateAIJob queues one AI request. created is false when an earlier job answers it: the same Idempotency-Key,
// or the same request from the same person still running (a double click).
func (s *Service) CreateAIJob(ctx context.Context, p Principal, in AIJobInput) (AIJob, bool, error) {
	if err := p.require(ScopeMediaWrite); err != nil {
		return AIJob{}, false, err
	}
	op, ok := aiOpByID(in.Operation)
	if !ok || op.Kind == "text" {
		return AIJob{}, false, invalid("operation", "is not an AI operation for pictures or videos")
	}
	if !s.aiOffered(op.ID) {
		return AIJob{}, false, &Error{Code: "not_available", Status: 409, Message: "this AI tool is not available on this server"}
	}
	if len(in.IdempotencyKey) > 200 {
		return AIJob{}, false, invalid("Idempotency-Key", "is too long")
	}
	c, err := aiCallFor(op, in)
	if err != nil {
		return AIJob{}, false, err
	}
	chain := s.aiChain(c)
	if len(chain) == 0 {
		return AIJob{}, false, &Error{Code: "not_available", Status: 409, Message: "no AI provider on this server can do this with these settings"}
	}
	hash := aiInputHash(c)
	actor := p.actor()
	if in.IdempotencyKey != "" {
		row, err := s.Q.GetAIJobByIdempotencyKey(ctx, db.GetAIJobByIdempotencyKeyParams{WorkspaceID: p.WorkspaceID, CreatedBy: actor, IdempotencyKey: in.IdempotencyKey})
		if err == nil {
			if row.InputHash != hash {
				return AIJob{}, false, &Error{Code: "idempotency_mismatch", Status: 422, Message: "Idempotency-Key reused with a different request"}
			}
			return toAIJob(row), false, nil
		} else if !isNoRows(err) {
			return AIJob{}, false, err
		}
	}
	if row, err := s.Q.FindLiveAIJob(ctx, db.FindLiveAIJobParams{WorkspaceID: p.WorkspaceID, CreatedBy: actor, InputHash: hash}); err == nil {
		return toAIJob(row), false, nil
	} else if !isNoRows(err) {
		return AIJob{}, false, err
	}
	if !aiLimiter.allow(p.WorkspaceID, s.clock()) {
		return AIJob{}, false, &Error{Code: "rate_limited", Status: 429, Message: fmt.Sprintf("at most %d AI requests per %s per workspace; try again in a few minutes", aiLimitCalls, aiLimitWindow)}
	}

	id := newID("aij")
	var inputs []string
	for _, f := range []struct {
		name, ct string
		data     []byte
	}{{"image", c.ImageType, c.Image}, {"mask", "image/png", c.Mask}} {
		if len(f.data) == 0 {
			continue
		}
		name := f.name
		if err := s.Cfg.Blob.Put(ctx, aiFileKey(p.WorkspaceID, id, name), bytes.NewReader(f.data), int64(len(f.data)), f.ct); err != nil {
			s.deleteAIFiles(ctx, p.WorkspaceID, id, inputs)
			return AIJob{}, false, err
		}
		inputs = append(inputs, name)
	}
	estimate := s.estimateCost(chain[0].model, c)
	var row db.AiJob
	err = s.tx(ctx, func(q *db.Queries) error {
		n, err := q.CountLiveAIJobs(ctx, p.WorkspaceID)
		if err != nil {
			return err
		}
		if n >= aiMaxLive {
			return &Error{Code: "rate_limited", Status: 429, Message: fmt.Sprintf("at most %d AI requests run at once per workspace; wait for one to finish", aiMaxLive)}
		}
		limit, err := s.aiMonthlyLimit(ctx, q, p.WorkspaceID)
		if err != nil {
			return err
		}
		if limit >= 0 {
			used, err := q.SumAICost(ctx, db.SumAICostParams{WorkspaceID: p.WorkspaceID, CreatedAt: s.monthStart()})
			if err != nil {
				return err
			}
			if float64(used+estimate)/1e6 > limit {
				return &Error{Code: "ai_limit_reached", Status: 402,
					Message: "This workspace has used its AI allowance for this month; it renews on the 1st.",
					Details: map[string]any{"used_usd": float64(used) / 1e6, "limit_usd": limit, "estimate_usd": float64(estimate) / 1e6}}
			}
		}
		if err := q.CreateAIJob(ctx, db.CreateAIJobParams{ID: id, WorkspaceID: p.WorkspaceID, CreatedBy: actor, Operation: op.ID,
			Kind: op.Kind, Prompt: c.Prompt, Params: mustJSON(c.params()), Inputs: strings.Join(inputs, ","), InputHash: hash,
			IdempotencyKey: in.IdempotencyKey, CostMicros: estimate, CreatedAt: s.now()}); err != nil {
			return err
		}
		if err := s.enqueue(ctx, q, p.WorkspaceID, "ai_run", "ai_run:"+id, jobPayload{AIJobID: id}, s.Now(), aiJobAttempts); err != nil {
			return err
		}
		row, err = q.GetAIJob(ctx, db.GetAIJobParams{WorkspaceID: p.WorkspaceID, ID: id})
		return err
	})
	if err != nil {
		s.deleteAIFiles(ctx, p.WorkspaceID, id, inputs)
		if in.IdempotencyKey != "" { // the same key raced in from another request
			if prev, gerr := s.Q.GetAIJobByIdempotencyKey(ctx, db.GetAIJobByIdempotencyKeyParams{WorkspaceID: p.WorkspaceID, CreatedBy: actor, IdempotencyKey: in.IdempotencyKey}); gerr == nil && prev.InputHash == hash {
				return toAIJob(prev), false, nil
			}
		}
		return AIJob{}, false, err
	}
	s.logger(ctx).Info("ai job queued", "ai_job_id", id, "op", op.ID, "provider", chain[0].prov.Name(), "model", chain[0].model.ID, "estimate_micros", estimate)
	s.trackAs(nil, p, "ai_job_requested", "op", op.ID, "provider", chain[0].prov.Name(), "model", chain[0].model.ID)
	return toAIJob(row), true, nil
}

// aiJobAttempts: an attempt that runs out of its budget while the provider still works resumes polling (it never
// submits again); after three the job fails.
const aiJobAttempts = 3

func (s *Service) aiJob(ctx context.Context, p Principal, id string) (db.AiJob, error) {
	row, err := s.Q.GetAIJob(ctx, db.GetAIJobParams{WorkspaceID: p.WorkspaceID, ID: id})
	if isNoRows(err) {
		return row, notFound("AI job")
	}
	return row, err
}

// GetAIJob reports a job (the editor polls it while it runs).
func (s *Service) GetAIJob(ctx context.Context, p Principal, id string) (AIJob, error) {
	if err := p.require(ScopeMediaRead); err != nil {
		return AIJob{}, err
	}
	row, err := s.aiJob(ctx, p, id)
	if err != nil {
		return AIJob{}, err
	}
	return toAIJob(row), nil
}

// ReadAIJobResult reads the made file for the preview.
func (s *Service) ReadAIJobResult(ctx context.Context, p Principal, id string) (io.ReadSeekCloser, string, error) {
	if err := p.require(ScopeMediaRead); err != nil {
		return nil, "", err
	}
	row, err := s.aiJob(ctx, p, id)
	if err != nil {
		return nil, "", err
	}
	if row.ResultKey == "" {
		return nil, "", notFound("AI result")
	}
	f, err := s.Cfg.Blob.Open(ctx, row.ResultKey)
	if err != nil {
		return nil, "", notFound("AI result")
	}
	return f, row.ResultType, nil
}

// AcceptAIJob keeps the result: it becomes a library file (plan limits apply as for any upload). Accepting again
// answers the same file.
func (s *Service) AcceptAIJob(ctx context.Context, p Principal, id string) (Media, error) {
	if err := p.require(ScopeMediaWrite); err != nil {
		return Media{}, err
	}
	row, err := s.aiJob(ctx, p, id)
	if err != nil {
		return Media{}, err
	}
	if row.MediaID != "" {
		return s.GetMediaInfo(ctx, p, row.MediaID)
	}
	if row.Status != "succeeded" || row.ResultKey == "" {
		return Media{}, conflict("this AI job has no result to keep")
	}
	f, err := s.Cfg.Blob.Open(ctx, row.ResultKey)
	if err != nil {
		return Media{}, conflict("the AI result is no longer kept; run it again")
	}
	defer f.Close()
	ext := ".png"
	if strings.HasPrefix(row.ResultType, "video/") {
		ext = ".mp4"
	} else if row.ResultType == "image/jpeg" {
		ext = ".jpg"
	}
	// ponytail: two accepts at the same moment make two library files; the editor sends one.
	m, err := s.UploadMedia(ctx, p, aiFileName(row)+ext, f)
	if err != nil {
		return Media{}, err
	}
	if err := s.W.SetAIJobMedia(ctx, db.SetAIJobMediaParams{MediaID: m.ID, WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
		return Media{}, err
	}
	_ = s.Cfg.Blob.Delete(ctx, row.ResultKey)
	return m, nil
}

// aiFileName: "AI <what it was> <the prompt's first words>".
func aiFileName(r db.AiJob) string {
	words := strings.Map(func(c rune) rune {
		if c == ' ' || c == '-' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			return c
		}
		return -1
	}, truncate(r.Prompt, 40))
	name := "AI " + strings.ReplaceAll(r.Operation, "_", " ")
	if w := strings.TrimSpace(strings.TrimSuffix(words, "...")); w != "" {
		name += " " + w
	}
	return name
}

// DiscardAIJob cancels a job that still runs (asking the provider to drop it) and deletes its files. A kept result
// stays in the library.
func (s *Service) DiscardAIJob(ctx context.Context, p Principal, id string) error {
	if err := p.require(ScopeMediaWrite); err != nil {
		return err
	}
	row, err := s.aiJob(ctx, p, id)
	if err != nil {
		return err
	}
	if row.Status == "queued" || row.Status == "running" {
		cost := int64(0)
		if row.SubmitState != "" {
			cost = row.CostMicros // it may run anyway: counted unless the provider confirms it dropped it
			if prov := s.aiProvider(row.Provider); prov != nil && row.SubmitState == "submitted" {
				var req aiRequest
				_ = json.Unmarshal([]byte(row.ProviderRequest), &req)
				cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
				dropped, cerr := prov.Cancel(cctx, req)
				cancel()
				if cerr != nil {
					s.logger(ctx).Warn("ai job: the provider's cancel failed", "ai_job_id", id, "provider", row.Provider, "err", cerr)
				}
				if dropped {
					cost = 0
				}
			}
		}
		err := s.tx(ctx, func(q *db.Queries) error {
			if _, err := q.FinishAIJob(ctx, db.FinishAIJobParams{Status: "canceled", ErrorCode: "canceled", Error: "Cancelled",
				CostMicros: cost, FinishedAt: s.now(), ID: id}); err != nil {
				return err
			}
			return s.cancelJobs(ctx, q, "ai_run:"+id)
		})
		if err != nil {
			return err
		}
	}
	return s.purgeAIJob(ctx, row)
}

func (s *Service) deleteAIFiles(ctx context.Context, workspaceID, id string, names []string) {
	for _, n := range names {
		_ = s.Cfg.Blob.Delete(ctx, aiFileKey(workspaceID, id, n))
	}
}

// purgeAIJob deletes a job's files and forgets its prompt; the row stays (usage).
func (s *Service) purgeAIJob(ctx context.Context, row db.AiJob) error {
	names := []string{}
	if row.Inputs != "" {
		names = strings.Split(row.Inputs, ",")
	}
	s.deleteAIFiles(ctx, row.WorkspaceID, row.ID, names)
	if row.ResultKey != "" {
		_ = s.Cfg.Blob.Delete(ctx, row.ResultKey)
	}
	return s.W.PurgeAIJobFiles(ctx, row.ID)
}

// sweepAIFiles deletes the files and prompts of jobs finished more than a day ago (the minute sweep).
func (s *Service) sweepAIFiles(ctx context.Context) {
	rows, err := s.Q.ListAIJobsToPurge(ctx, formatTime(s.Now().Add(-aiFilesKept)))
	if err != nil {
		s.logger(ctx).Error("ai files sweep", "err", err)
		return
	}
	for _, r := range rows {
		if err := s.purgeAIJob(ctx, r); err != nil {
			s.logger(ctx).Error("ai files sweep", "ai_job_id", r.ID, "err", err)
		}
	}
}

// ---- the worker (job kind ai_run) ----

func (s *Service) aiPollEvery(kind string) time.Duration {
	if d := s.Cfg.AI.PollInterval; d > 0 {
		return d
	}
	if kind == "video" {
		return 5 * time.Second
	}
	return 2 * time.Second
}

// aiFailure is how a job that did not succeed reads: a code and plain words.
func aiFailure(err error, label string) (string, string) {
	var e *aiError
	errors.As(err, &e)
	switch aiErrKind(err) {
	case aiInvalid:
		msg := label + " refused the request"
		if e != nil && e.Msg != "" {
			msg += ": " + e.Msg
		}
		return "ai_refused", msg
	case aiNotAccepted:
		return "ai_unavailable", "The AI service is not available right now; try again in a few minutes."
	case aiUnknown:
		return "ai_status_unknown", "The AI service did not answer, so it is unknown whether it started; it was not sent again, so it is not paid twice. Try again if nothing arrives."
	}
	return "ai_failed", "The AI service could not make it; try again."
}

func (s *Service) finishAIJob(ctx context.Context, row db.AiJob, status, code, msg string, cost int64, result db.AiJob) (bool, error) {
	ms := int64(0)
	if t, err := time.Parse(timeFormat, row.StartedAt); err == nil {
		ms = s.Now().Sub(t).Milliseconds()
	}
	n, err := s.W.FinishAIJob(ctx, db.FinishAIJobParams{Status: status, ResultKey: result.ResultKey, ResultType: result.ResultType,
		ResultWidth: result.ResultWidth, ResultHeight: result.ResultHeight, ErrorCode: code, Error: msg, CostMicros: cost,
		DurationMs: ms, FinishedAt: s.now(), ID: row.ID})
	if err != nil || n == 0 {
		return false, err
	}
	s.logger(ctx).Info("ai job finished", "ai_job_id", row.ID, "op", row.Operation, "provider", row.Provider, "model", row.Model,
		"fallback_from", row.FallbackFrom, "status", status, "code", code, "ms", ms, "cost_micros", cost)
	s.trackAs(nil, Principal{WorkspaceID: row.WorkspaceID, Kind: PrincipalSystem}, "ai_job_finished", "op", row.Operation,
		"provider", row.Provider, "model", row.Model, "status", status, "error_code", code, "fallback", row.FallbackFrom != "",
		"duration_ms", ms)
	return true, nil
}

// runAIJob is the ai_run job: submit (once), then poll until the provider is done, then keep the result.
func (s *Service) runAIJob(ctx context.Context, workspaceID, id string) error {
	row, err := s.Q.GetAIJob(ctx, db.GetAIJobParams{WorkspaceID: workspaceID, ID: id})
	if isNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if row.Status == "queued" {
		if _, err := s.W.StartAIJob(ctx, db.StartAIJobParams{StartedAt: s.now(), ID: id}); err != nil {
			return err
		}
		row.Status, row.StartedAt = "running", s.now()
	}
	if row.Status != "running" {
		return nil // finished or cancelled
	}
	fail := func(code, msg string, cost int64) error {
		_, err := s.finishAIJob(ctx, row, "failed", code, msg, cost, db.AiJob{})
		return err
	}
	var params aiParams
	_ = json.Unmarshal([]byte(row.Params), &params)
	read := func(name string) []byte {
		if !strings.Contains(","+row.Inputs+",", ","+name+",") {
			return nil
		}
		f, err := s.Cfg.Blob.Open(ctx, aiFileKey(workspaceID, id, name))
		if err != nil {
			return nil
		}
		defer f.Close()
		b, _ := io.ReadAll(io.LimitReader(f, MaxAIImageBytes+1))
		return b
	}
	c := params.call(row.Operation, row.Prompt, read("image"), read("mask"))
	if op, _ := aiOpByID(row.Operation); (op.Image && len(c.Image) == 0) || (op.Mask && len(c.Mask) == 0) {
		return fail("ai_failed", "The picture for this request is gone; run it again.", 0)
	}

	var prov aiProvider
	var req aiRequest
	switch row.SubmitState {
	case "submitted":
		if prov = s.aiProvider(row.Provider); prov == nil {
			return fail("ai_unavailable", "The AI provider that runs this request is no longer set up on this server.", row.CostMicros)
		}
		_ = json.Unmarshal([]byte(row.ProviderRequest), &req)
	default:
		chain := s.aiChain(c)
		if row.SubmitState == "submitting" {
			// An earlier attempt sent it and never saw the answer: only a provider that deduplicates by the job id
			// may get it again; anything else would risk a second paid generation.
			p := s.aiProvider(row.Provider)
			m, ok := aiModelFor(row.Provider, row.Operation)
			if p == nil || !ok || !p.Idempotent() {
				code, msg := aiFailure(&aiError{Kind: aiUnknown}, "")
				return fail(code, msg, row.CostMicros)
			}
			chain = []aiCandidate{{p, m}}
		}
		if len(chain) == 0 {
			return fail("ai_unavailable", "No AI provider on this server can do this any more.", 0)
		}
		for i, cand := range chain {
			from := row.FallbackFrom
			if i > 0 {
				from = chain[0].prov.Name()
			}
			cost := s.estimateCost(cand.model, c)
			n, err := s.W.SetAIJobSubmitting(ctx, db.SetAIJobSubmittingParams{Provider: cand.prov.Name(), Model: cand.model.ID,
				FallbackFrom: from, CostMicros: cost, ID: id})
			if err != nil {
				return err
			}
			if n == 0 {
				return nil // cancelled meanwhile
			}
			row.Provider, row.Model, row.FallbackFrom, row.CostMicros, row.SubmitState = cand.prov.Name(), cand.model.ID, from, cost, "submitting"
			req, err = cand.prov.Submit(ctx, cand.model.ID, c, id)
			if err != nil && aiErrKind(err) == aiUnknown && cand.prov.Idempotent() && ctx.Err() == nil {
				req, err = cand.prov.Submit(ctx, cand.model.ID, c, id) // the same key: one generation at most
			}
			if err == nil {
				prov = cand.prov
				if err := s.W.SetAIJobSubmitted(ctx, db.SetAIJobSubmittedParams{ProviderRequest: mustJSON(req), ID: id}); err != nil {
					return err
				}
				break
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			kind := aiErrKind(err)
			s.logger(ctx).Warn("ai job: the provider did not take it", "ai_job_id", id, "provider", cand.prov.Name(), "kind", kind, "err", err)
			if kind == aiNotAccepted && i+1 < len(chain) {
				continue // fallback: the first provider took nothing, so nothing is paid twice
			}
			if kind == aiNotAccepted || kind == aiInvalid {
				cost = 0
			}
			code, msg := aiFailure(err, providerLabel(cand.prov.Name()))
			return fail(code, msg, cost)
		}
	}

	every := s.aiPollEvery(row.Kind)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err() // the run's budget: a later attempt resumes polling
		case <-time.After(every):
		}
		cur, err := s.Q.GetAIJob(ctx, db.GetAIJobParams{WorkspaceID: workspaceID, ID: id})
		if err != nil || cur.Status != "running" {
			return nil // cancelled (the cancel told the provider) or gone
		}
		prog, err := prov.Check(ctx, req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			s.logger(ctx).Warn("ai job: status check failed; asking again", "ai_job_id", id, "provider", prov.Name(), "err", err)
			continue
		}
		if !prog.Done {
			continue
		}
		if prog.Failed {
			code := "ai_failed"
			if prog.Refused {
				code = "ai_refused"
			}
			prov.Forget(ctx, req)
			return fail(code, prog.Message, 0) // the providers do not charge failed or refused requests
		}
		max := int64(maxAIImageResult)
		if row.Kind == "video" {
			max = maxAIVideoResult
		}
		data, ct, err := prov.download(ctx, prog.OutputURL, max)
		if err != nil {
			return fail("ai_failed", "The result could not be fetched from the AI service; try again.", row.CostMicros)
		}
		if (row.Kind == "image" && !strings.HasPrefix(ct, "image/")) || (row.Kind == "video" && !strings.HasPrefix(ct, "video/")) {
			return fail("ai_failed", "The AI service sent something other than a "+row.Kind+".", row.CostMicros)
		}
		res := db.AiJob{ResultKey: aiFileKey(workspaceID, id, "result"), ResultType: ct}
		if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
			res.ResultWidth, res.ResultHeight = int64(cfg.Width), int64(cfg.Height)
		}
		if err := s.Cfg.Blob.Put(ctx, res.ResultKey, bytes.NewReader(data), int64(len(data)), ct); err != nil {
			return err
		}
		prov.Forget(ctx, req)
		cost := row.CostMicros
		if m, ok := aiModelFor(row.Provider, row.Operation); ok && m.Unit == aiPerMegapixel && res.ResultWidth > 0 {
			c.Width, c.Height, c.ImageWidth, c.ImageHeight, c.CanvasWidth, c.CanvasHeight = int(res.ResultWidth), int(res.ResultHeight),
				int(res.ResultWidth), int(res.ResultHeight), int(res.ResultWidth), int(res.ResultHeight)
			if row.Operation == AIEnhance {
				c.ImageWidth, c.ImageHeight = int(res.ResultWidth)/2, int(res.ResultHeight)/2
			}
			cost = s.estimateCost(m, c) // the pixels it really made
		}
		done, err := s.finishAIJob(ctx, row, "succeeded", "", "", cost, res)
		if err != nil || !done {
			_ = s.Cfg.Blob.Delete(context.WithoutCancel(ctx), res.ResultKey) // cancelled while it finished
		}
		return err
	}
}

// aiRunFailed settles a job whose attempts ran out while the provider still worked.
func (s *Service) aiRunFailed(ctx context.Context, workspaceID, id string) {
	row, err := s.Q.GetAIJob(ctx, db.GetAIJobParams{WorkspaceID: workspaceID, ID: id})
	if err != nil {
		return
	}
	if _, err := s.finishAIJob(ctx, row, "failed", "ai_timeout", "The AI service took too long; try again.", row.CostMicros, db.AiJob{}); err != nil {
		s.logger(ctx).Error("ai job not settled after its attempts", "ai_job_id", id, "err", err)
	}
}

func providerLabel(name string) string {
	switch name {
	case AIProviderFal:
		return "fal.ai"
	case AIProviderHiggsfield:
		return "Higgsfield"
	}
	return name
}

// clock is time.Now, kept in one place for the limiter.
func (s *Service) clock() time.Time { return time.Now() }

// aiLimiter: a sliding window per workspace on new requests. ponytail: in memory, per process; a cluster of
// servers allows each its own window (the monthly cost limit and aiMaxLive are in the database and hold).
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
