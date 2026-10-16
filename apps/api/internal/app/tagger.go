package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// TaggerConfig: the model that writes a one-line summary of every new item and suggests tags for those that
// arrive without any. Locally it is the user's own AI CLI (Command: claude, codex...), found on PATH by default;
// in cloud mode the operator's OpenAI-compatible endpoint (URL). Off when neither is set.
type TaggerConfig struct {
	URL    string // OpenAI-compatible base URL, e.g. https://api.openai.com/v1
	Model  string // model at URL, e.g. gpt-4o-mini
	APIKey string // optional bearer token for URL
	// Command: a program and its arguments that reads the prompt on stdin and prints the reply, e.g.
	// claude -p --model haiku. Local mode only.
	Command []string
	// Concurrency: items summarized at once.
	Concurrency int
}

// On reports whether summaries are set up.
func (c TaggerConfig) On() bool { return c.URL != "" || len(c.Command) > 0 }

// maxAutoTags: a few labels to filter by; maxSummary: one line under the title.
const (
	maxAutoTags = 3
	maxSummary  = 120
)

const taggerPrompt = `You label items in a review inbox. Read the item and reply with only one JSON object, no other ` +
	`text, in this form: {"tags": ["tag one", "tag two"], "summary": "short sentence"}. tags: 1 to 3 lowercase ` +
	`words or two-word phrases naming the topic or kind of content (never empty). summary: what the item is, one ` +
	`plain sentence of at most 100 characters. Example reply: {"tags": ["social media", "marketing"], ` +
	`"summary": "Instagram post announcing the spring sale"}`

// Queue limits: tries per item, and the backoff between them (30 s, 1 min, 2 min... at most 30 min).
const (
	summaryMaxAttempts = 5
	summaryLease       = 5 * time.Minute
)

// maxModelReply bounds what is read of a model's reply: a summary and a few tags fit in far less, and a
// misbehaving endpoint (or a proxy in front of it) must not make the server buffer an unbounded body.
const maxModelReply = 1 << 20

func summaryBackoff(attempt int64) time.Duration {
	return min(30*time.Second<<max(attempt-1, 0), 30*time.Minute)
}

// wakeSummaries tells the worker a new item is queued (no-op if it is already awake).
func (s *Service) wakeSummaries() {
	select {
	case s.summaryWake <- struct{}{}:
	default:
	}
}

// RunSummaries works through the summary queue (items.summary_next_at) until ctx ends: every new item gets a
// summary (and tags if it came without any), TAGGER_CONCURRENCY at a time, with retries and backoff. The
// queue is durable: items queued while the model is down, or before a restart, are done later. Without
// TAGGER_URL it waits; items created meanwhile stay queued for when a model is set up.
func (s *Service) RunSummaries(ctx context.Context) {
	if !s.Cfg.Tagger.On() {
		<-ctx.Done()
		return
	}
	n := max(s.Cfg.Tagger.Concurrency, 1)
	sem := make(chan struct{}, n)
	var wg sync.WaitGroup
	defer wg.Wait()
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		due, err := s.Q.DueSummaries(ctx, db.DueSummariesParams{Now: nullStr(s.now()), Lim: int64(4 * n)})
		if err != nil && ctx.Err() == nil {
			s.logger(ctx).Error("summary poll failed", "err", err)
		}
		for _, d := range due {
			leased, err := s.W.LeaseSummary(ctx, db.LeaseSummaryParams{
				LeaseUntil: nullStr(formatTime(s.Now().Add(summaryLease))), WorkspaceID: d.WorkspaceID, ID: d.ID, Expected: d.SummaryNextAt})
			if err != nil || leased == 0 {
				continue
			}
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			wg.Add(1)
			go func(id, workspaceID string) {
				defer func() { <-sem; wg.Done() }()
				s.summarizeQueued(context.WithoutCancel(ctx), id, workspaceID)
			}(d.ID, d.WorkspaceID)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-s.summaryWake:
		}
	}
}

// summarizeQueued runs one leased item; a failure schedules the next try, or gives up after the last.
func (s *Service) summarizeQueued(ctx context.Context, id, workspaceID string) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	p := Principal{WorkspaceID: workspaceID, Kind: PrincipalSystem, ID: "tagger", Name: "Auto-tagger"}
	row, err := s.Q.GetItem(ctx, db.GetItemParams{WorkspaceID: workspaceID, ID: id})
	if err != nil { // deleted meanwhile
		return
	}
	if _, err = s.summarize(ctx, p, s.toItem(ctx, row), ""); err == nil {
		return
	}
	// The lease already counted this try: row.SummaryAttempts is its number (1 for the first).
	next := sql.NullString{}
	if row.SummaryAttempts < summaryMaxAttempts {
		next = nullStr(formatTime(s.Now().Add(summaryBackoff(row.SummaryAttempts))))
	}
	s.logger(ctx).Warn("summary failed", "item", id, "attempt", row.SummaryAttempts, "retry", next.Valid, "err", err)
	_ = s.W.RetrySummary(ctx, db.RetrySummaryParams{NextAt: next, WorkspaceID: workspaceID, ID: id})
}

// SummarizeInput: extra context from a person to help the model (what the item is for, who it's for...).
type SummarizeInput struct {
	Context string `json:"context"`
}

// Summarize writes the item's summary again now (tags too, if it has none), with optional extra context.
// It waits for the model.
func (s *Service) Summarize(ctx context.Context, p Principal, id string, in SummarizeInput) (Item, error) {
	if err := p.require(ScopeItemsWrite); err != nil {
		return Item{}, err
	}
	if !s.Cfg.Tagger.On() {
		msg := "no AI is set up for summaries: install Claude Code or Codex on this computer, or set TAGGER_COMMAND / TAGGER_URL"
		if s.Cfg.Mode == ModeCloud {
			msg = "summaries are not set up on this instance"
		}
		return Item{}, &Error{Code: "not_configured", Status: 409, Message: msg}
	}
	extra, err := trimmed(in.Context, "context", 500, false)
	if err != nil {
		return Item{}, err
	}
	it, err := s.GetItem(ctx, p, id)
	if err != nil {
		return Item{}, err
	}
	// A local AI CLI takes 10-30 s.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if _, err := s.summarize(ctx, p, it, extra); err != nil {
		var de *Error
		if asError(err, &de) {
			return Item{}, err // e.g. the item was deleted meanwhile
		}
		s.logger(ctx).Warn("summary failed", "item", id, "err", err)
		msg := "the model did not answer"
		if s.Cfg.Mode == ModeLocal { // the operator's own machine: the reason helps; in cloud it names internal hosts
			msg += ": " + err.Error()
		}
		return Item{}, &Error{Code: "model_failed", Status: 502, Message: msg}
	}
	return s.GetItem(ctx, p, id)
}

// summarize asks the model, stores the summary and, if the item still has no tags, the tags. One event
// (item.summarized) tells open dashboards to reload it.
func (s *Service) summarize(ctx context.Context, p Principal, it Item, extra string) (string, error) {
	summary, tags, err := s.suggest(ctx, it, extra)
	if err != nil {
		return "", err
	}
	if summary == "" && len(tags) == 0 {
		return "", fmt.Errorf("no summary in the reply")
	}
	err = s.tx(ctx, func(q *db.Queries) error {
		cur, err := q.GetItem(ctx, db.GetItemParams{WorkspaceID: p.WorkspaceID, ID: it.ID})
		if isNoRows(err) {
			return notFound("item")
		}
		if err != nil {
			return err
		}
		data := map[string]any{}
		if summary == "" {
			summary = cur.Summary // keep the old one; the call still takes the item off the queue
		}
		if err := q.SetItemSummary(ctx, db.SetItemSummaryParams{Summary: summary, WorkspaceID: p.WorkspaceID, ID: it.ID}); err != nil {
			return err
		}
		if summary != "" {
			data["summary"] = summary
		}
		if len(tags) > 0 && cur.Tags == "[]" { // never over tags someone set
			if _, err := q.SetItemTags(ctx, db.SetItemTagsParams{Tags: mustJSON(tags), WorkspaceID: p.WorkspaceID, ID: it.ID}); err != nil {
				return err
			}
			data["tags"] = tags
		}
		_, err = s.emit(ctx, q, p, "item.summarized", cur.CollectionID, it.ID, data)
		return err
	})
	return summary, err
}

func (s *Service) suggest(ctx context.Context, it Item, extra string) (summary string, tags []string, err error) {
	cfg := s.Cfg.Tagger
	text := clip("Title: "+it.Title+"\nType: "+it.Type+"\nSource: "+it.Source+"\n\n"+it.Content+"\n"+it.URL, 4000)
	if extra != "" {
		text += "\n\nContext from the reviewer: " + extra
	}
	if len(cfg.Command) > 0 {
		reply, err := s.runTaggerCommand(ctx, taggerPrompt+"\n\n"+text)
		if err != nil {
			return "", nil, err
		}
		summary, tags = parseSuggestion(reply)
		return summary, tags, nil
	}
	msgs := []map[string]string{{"role": "system", "content": taggerPrompt}, {"role": "user", "content": text}}
	// temperature 0 keeps small local models to the JSON format; some hosted models (OpenAI reasoning models)
	// accept only their default and answer 400, so that call is repeated without it.
	resp, err := s.chat(ctx, map[string]any{"model": cfg.Model, "temperature": 0, "messages": msgs})
	if err == nil && resp.StatusCode == http.StatusBadRequest {
		resp.Body.Close()
		resp, err = s.chat(ctx, map[string]any{"model": cfg.Model, "messages": msgs})
	}
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return "", nil, fmt.Errorf("tagger returned %s: %s", resp.Status, bytes.TrimSpace(msg))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxModelReply)).Decode(&out); err != nil || len(out.Choices) == 0 {
		return "", nil, fmt.Errorf("tagger: unexpected response")
	}
	summary, tags = parseSuggestion(out.Choices[0].Message.Content)
	return summary, tags, nil
}

// runTaggerCommand runs the local AI CLI with the prompt on stdin (no shell) in a scratch directory, so it never
// reads a project's files or instructions, and returns what it prints.
func (s *Service) runTaggerCommand(ctx context.Context, prompt string) (string, error) {
	cfg := s.Cfg.Tagger
	cmd := exec.CommandContext(ctx, cfg.Command[0], cfg.Command[1:]...)
	cmd.Dir = os.TempDir()
	cmd.Stdin = strings.NewReader(prompt)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &limitedWriter{w: &out, n: maxModelReply}, &limitedWriter{w: &errb, n: 4096}
	// The timeout kills only the CLI itself: a child it started may still hold stdout open, and Wait would
	// block on it (and the worker slot with it) until that child ends.
	cmd.WaitDelay = 5 * time.Second
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s: %v: %s", cfg.Command[0], err, clip(strings.TrimSpace(errb.String()), 300))
	}
	return out.String(), nil
}

// limitedWriter keeps the first n bytes and drops the rest (a chatty CLI can't grow memory).
type limitedWriter struct {
	w io.Writer
	n int64
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.n > 0 {
		k := min(int64(len(p)), l.n)
		_, _ = l.w.Write(p[:k])
		l.n -= k
	}
	return len(p), nil
}

// chat posts one OpenAI-style chat completion request to TAGGER_URL.
func (s *Service) chat(ctx context.Context, body map[string]any) (*http.Response, error) {
	cfg := s.Cfg.Tagger
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(cfg.URL, "/")+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	return http.DefaultClient.Do(req)
}

// parseSuggestion reads the first JSON object in a model's reply (small models add prose or code fences).
func parseSuggestion(reply string) (summary string, tags []string) {
	start, end := strings.Index(reply, "{"), strings.LastIndex(reply, "}")
	if start < 0 || end <= start {
		return "", parseTags(reply)
	}
	var raw struct {
		Summary string          `json:"summary"`
		Tags    json.RawMessage `json:"tags"`
	}
	if json.Unmarshal([]byte(reply[start:end+1]), &raw) != nil {
		return "", nil
	}
	summary = strings.Join(strings.Fields(raw.Summary), " ")
	if r := []rune(summary); len(r) > maxSummary {
		summary = strings.TrimSpace(string(r[:maxSummary-3])) + "..."
	}
	return summary, parseTags(string(raw.Tags))
}

// parseTags takes the first JSON array in a model's reply (small models add prose or code fences) and keeps
// up to maxAutoTags valid tags.
func parseTags(reply string) []string {
	start, end := strings.Index(reply, "["), strings.LastIndex(reply, "]")
	if start < 0 || end <= start {
		return nil
	}
	var raw []string
	if json.Unmarshal([]byte(reply[start:end+1]), &raw) != nil {
		return nil
	}
	var out []string
	for _, t := range raw {
		t = strings.ToLower(strings.Trim(strings.Join(strings.Fields(t), " "), "#.\"' "))
		if t == "" || len(t) > 40 || strings.Contains(t, ",") {
			continue
		}
		out = append(out, t)
	}
	tags, _ := normalizeTags(out)
	if len(tags) > maxAutoTags {
		tags = tags[:maxAutoTags]
	}
	return tags
}
