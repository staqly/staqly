package app

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// Writing posts with AI: a language model on fal.ai (its OpenRouter router) writes a post or a thread from what the
// person describes. It answers in a few seconds, so the request waits for it (no job to poll); it is recorded as an
// AI job like the pictures, so it counts towards the same monthly allowance.

// aiWriteModel: the language model behind the router. ponytail: one fixed model, AI_WRITE_MODEL if operators ask.
const aiWriteModel = "google/gemini-2.5-flash"

const aiWriteTimeout = 90 * time.Second

// AIWriteInput is what to write.
type AIWriteInput struct {
	Prompt string `json:"prompt"`
	// Format: short, long, thread_short or thread_long.
	Format string `json:"format"`
	// Voice: personal ("I") or company ("we").
	Voice string `json:"voice"`
	// Picture: also suggest a description for a picture to go with it.
	Picture bool `json:"picture"`
}

// AIWriting is the result: one post, or a thread's posts in order, and a picture's description when asked for.
type AIWriting struct {
	Posts         []string `json:"posts"`
	PicturePrompt string   `json:"picture_prompt,omitempty"`
}

var aiWriteFormats = map[string]string{
	"short":        "one short post (at most 280 characters)",
	"long":         "one longer post (a few short paragraphs, at most 2000 characters)",
	"thread_short": "a thread of 3 to 6 short posts (each at most 280 characters)",
	"thread_long":  "a thread of 3 to 6 posts (each a short paragraph, at most 600 characters)",
}

var aiWriteVoices = map[string]string{
	"personal": `in a personal voice, first person singular ("I am happy to announce")`,
	"company":  `in a company voice, first person plural ("We are happy to announce")`,
}

// aiWriteSystem tells the model how to answer, so the answer can be split into posts.
func aiWriteSystem(in AIWriteInput) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You write social media posts. Write %s %s, in the language of the request. ", aiWriteFormats[in.Format], aiWriteVoices[in.Voice])
	b.WriteString("Plain text only: no Markdown, no quotes around the posts, no numbering like 1/5, at most two hashtags. ")
	b.WriteString("Answer with the posts only; put a line with only --- between posts.")
	if in.Picture {
		b.WriteString(" Then, on the last line, write IMAGE: followed by a one-sentence description of a picture to go with the posts (no text in the picture).")
	}
	return b.String()
}

// parseAIWriting splits the model's answer into posts and the picture's description.
func parseAIWriting(out string) AIWriting {
	w := AIWriting{Posts: []string{}}
	var body []string
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "IMAGE:"); ok {
			w.PicturePrompt = strings.TrimSpace(rest)
			continue
		}
		body = append(body, line)
	}
	for _, p := range strings.Split(strings.Join(body, "\n"), "\n---") {
		if p = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(p), "---")); p != "" {
			w.Posts = append(w.Posts, p)
		}
	}
	return w
}

// WriteWithAI writes a post or a thread; the dashboard puts it in the composer, where the person edits it.
func (s *Service) WriteWithAI(ctx context.Context, p Principal, in AIWriteInput) (AIWriting, error) {
	if err := p.require(ScopePostsWrite); err != nil {
		return AIWriting{}, err
	}
	in.Prompt = strings.TrimSpace(in.Prompt)
	switch {
	case in.Prompt == "":
		return AIWriting{}, invalid("prompt", "is required")
	case utf8.RuneCountInString(in.Prompt) > MaxAIPromptRunes:
		return AIWriting{}, invalid("prompt", fmt.Sprintf("must be at most %d characters", MaxAIPromptRunes))
	case aiWriteFormats[in.Format] == "":
		return AIWriting{}, invalid("format", "must be short, long, thread_short or thread_long")
	case aiWriteVoices[in.Voice] == "":
		return AIWriting{}, invalid("voice", "must be personal or company")
	}
	fal, _ := s.aiProvider(AIProviderFal).(*falProvider)
	m, _ := aiModelFor(AIProviderFal, AIWrite)
	if fal == nil || !s.aiOffered(AIWrite) {
		return AIWriting{}, &Error{Code: "not_available", Status: 409, Message: "writing with AI is not available on this server"}
	}
	if !aiLimiter.allow(p.WorkspaceID, s.clock()) {
		return AIWriting{}, &Error{Code: "rate_limited", Status: 429, Message: fmt.Sprintf("at most %d AI requests per %s per workspace; try again in a few minutes", aiLimitCalls, aiLimitWindow)}
	}

	// Recorded (with its estimate, against the allowance) before the call; the call runs outside any transaction.
	id := newID("aij")
	estimate := s.estimateCost(m, aiCall{Op: AIWrite})
	params := map[string]any{"format": in.Format, "voice": in.Voice, "picture": in.Picture}
	err := s.tx(ctx, func(q *db.Queries) error {
		if limit := s.aiMonthlyLimit(); limit >= 0 {
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
		return q.CreateAIJob(ctx, db.CreateAIJobParams{ID: id, WorkspaceID: p.WorkspaceID, CreatedBy: p.actor(), Operation: AIWrite,
			Kind: "text", Prompt: in.Prompt, Params: mustJSON(params), InputHash: newID("h"), CostMicros: estimate, CreatedAt: s.now()})
	})
	if err != nil {
		return AIWriting{}, err
	}
	started := s.Now()
	out, cost, err := fal.write(ctx, m.ID, aiWriteSystem(in), in.Prompt, estimate)
	status, code, msg := "succeeded", "", ""
	if err != nil {
		status, code, msg = "failed", "ai_failed", err.Error()
	}
	_, _ = s.finishAIJob(context.WithoutCancel(ctx), db.AiJob{ID: id, WorkspaceID: p.WorkspaceID, CreatedAt: s.now(),
		StartedAt: formatTime(started), Operation: AIWrite, Provider: AIProviderFal, Model: m.ID}, status, code, msg, cost, db.AiJob{})
	if err != nil {
		return AIWriting{}, err
	}
	w := parseAIWriting(out)
	if len(w.Posts) == 0 {
		return AIWriting{}, &Error{Code: "ai_failed", Status: 502, Message: "The AI service wrote nothing; try again or describe the post differently."}
	}
	return w, nil
}

// write runs the router on fal.ai's queue and waits for its answer. The cost is the estimate once fal.ai took the
// request, 0 when it did not.
func (f *falProvider) write(ctx context.Context, model, system, prompt string, estimate int64) (string, int64, error) {
	ctx, cancel := context.WithTimeout(ctx, aiWriteTimeout)
	defer cancel()
	var req struct {
		RequestID   string `json:"request_id"`
		StatusURL   string `json:"status_url"`
		ResponseURL string `json:"response_url"`
	}
	in := map[string]any{"model": aiWriteModel, "system_prompt": system, "prompt": prompt, "temperature": 0.8}
	status, raw, err := f.call(ctx, http.MethodPost, f.base.String()+"/"+model, in, nil, &req)
	if err != nil {
		return "", 0, &Error{Code: "ai_failed", Status: 502, Message: "fal.ai could not be reached; try again."}
	}
	if status >= 300 {
		e := f.submitError(status, raw)
		return "", 0, &Error{Code: "ai_failed", Status: 502, Message: e.Error()}
	}
	if !f.sameOrigin(req.StatusURL) || !f.sameOrigin(req.ResponseURL) {
		return "", estimate, &Error{Code: "ai_failed", Status: 502, Message: "fal.ai answered without a usable request"}
	}
	for {
		var st struct {
			Status string `json:"status"`
		}
		if _, _, err := f.call(ctx, http.MethodGet, req.StatusURL, nil, nil, &st); err != nil && ctx.Err() != nil {
			return "", estimate, &Error{Code: "ai_failed", Status: 502, Message: "The AI service took too long; try again."}
		}
		if st.Status == "COMPLETED" {
			break
		}
		select {
		case <-ctx.Done():
			return "", estimate, &Error{Code: "ai_failed", Status: 502, Message: "The AI service took too long; try again."}
		case <-time.After(700 * time.Millisecond):
		}
	}
	var res struct {
		Output string `json:"output"`
		Error  string `json:"error"`
	}
	status, raw, err = f.call(ctx, http.MethodGet, req.ResponseURL, nil, nil, &res)
	if err != nil || status >= 300 || res.Error != "" {
		msg := res.Error
		if msg == "" {
			msg = aiDetail(raw)
		}
		if msg == "" {
			msg = "the AI service could not write it"
		}
		return "", 0, &Error{Code: "ai_failed", Status: 502, Message: msg} // fal.ai does not charge failed requests
	}
	return res.Output, estimate, nil
}
