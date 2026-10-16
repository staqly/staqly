package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// POST /v1/items/{id}/summarize asks the model (an OpenAI-compatible endpoint) with the reviewer's context,
// stores the summary and, for an untagged item, the tags.
func TestSummarizeItem(t *testing.T) {
	e := newEnv(t)
	col := e.collection("Posts")
	it := e.item(col.ID, map[string]any{"title": "Spring sale", "content": "30% off sneakers"})
	e.do(nil, "POST", "/v1/items/"+it.ID+"/summarize", e.key, map[string]any{}, 409, nil)

	var prompt string
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct{ Content string } `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		prompt = req.Messages[len(req.Messages)-1].Content
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"tags\":[\"sale\"],\"summary\":\"Sneaker sale post\"}"}}]}`))
	}))
	t.Cleanup(model.Close)
	e.svc.Cfg.Tagger = app.TaggerConfig{URL: model.URL, Model: "m"}

	var got app.Item
	e.do(nil, "POST", "/v1/items/"+it.ID+"/summarize", e.key, map[string]any{"context": "for Instagram"}, 200, &got)
	if got.Summary != "Sneaker sale post" || !slices.Equal(got.Tags, []string{"sale"}) {
		t.Fatalf("summary %q tags %v", got.Summary, got.Tags)
	}
	if !strings.Contains(prompt, "Context from the reviewer: for Instagram") {
		t.Fatalf("context not in prompt: %q", prompt)
	}
}

// A bulk of new items all get summaries through the queue, several at a time; a model that fails at first is
// retried (backoff shortened by moving the item's next try to now).
func TestSummaryQueue(t *testing.T) {
	e := newEnv(t)
	col := e.collection("Bulk")
	var calls, inFlight, peak atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		cur := inFlight.Add(1)
		defer inFlight.Add(-1)
		for p := peak.Load(); cur > p && !peak.CompareAndSwap(p, cur); p = peak.Load() {
		}
		time.Sleep(20 * time.Millisecond)
		if n == 1 { // the first call fails: that item is retried
			http.Error(w, "busy", 503)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"tags\":[\"bulk\"],\"summary\":\"A bulk item\"}"}}]}`))
	}))
	t.Cleanup(model.Close)
	e.svc.Cfg.Tagger = app.TaggerConfig{URL: model.URL, Model: "m", Concurrency: 3}

	var ids []string
	for i := range 12 {
		ids = append(ids, e.item(col.ID, map[string]any{"title": fmt.Sprintf("Item %d", i), "content": "x"}).ID)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.svc.RunSummaries(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	deadline := time.Now().Add(10 * time.Second)
	for _, id := range ids {
		for {
			var it app.Item
			e.do(nil, "GET", "/v1/items/"+id, e.key, nil, 200, &it)
			if it.Summary == "A bulk item" && slices.Equal(it.Tags, []string{"bulk"}) {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s never summarized: %+v", id, it)
			}
			// The failed item waits 30 s for its retry: bring it forward.
			_, _ = e.svc.DB.Exec(`UPDATE items SET summary_next_at = '2000-01-01T00:00:00Z' WHERE summary_next_at IS NOT NULL AND summary_attempts > 0 AND summary = ''`)
			time.Sleep(50 * time.Millisecond)
		}
	}
	if peak.Load() < 2 || peak.Load() > 3 {
		t.Fatalf("parallel calls: peak %d, want 2-3", peak.Load())
	}
}

// Hosted models that refuse temperature (400) are asked again without it.
func TestSummarizeRetriesWithoutTemperature(t *testing.T) {
	e := newEnv(t)
	it := e.item(e.collection("Posts").ID, map[string]any{"title": "Post", "content": "x"})
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			http.Error(w, "no key", 401)
			return
		}
		if _, ok := body["temperature"]; ok {
			http.Error(w, `{"error":{"message":"Unsupported value: 'temperature'"}}`, 400)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"tags\":[\"post\"],\"summary\":\"A post\"}"}}]}`))
	}))
	t.Cleanup(model.Close)
	e.svc.Cfg.Tagger = app.TaggerConfig{URL: model.URL, Model: "gpt-5-mini", APIKey: "sk-test"}
	var got app.Item
	e.do(nil, "POST", "/v1/items/"+it.ID+"/summarize", e.key, nil, 200, &got)
	if got.Summary != "A post" {
		t.Fatalf("summary %q", got.Summary)
	}
}

// The item list carries the summary too (it is read by a hand-written query, not sqlc's).
func TestListItemsIncludesSummary(t *testing.T) {
	e := newEnv(t)
	it := e.item(e.collection("Posts").ID, map[string]any{"title": "Post", "content": "x"})
	if _, err := e.svc.DB.Exec(`UPDATE items SET summary = 'A post' WHERE id = ?`, it.ID); err != nil {
		t.Fatal(err)
	}
	var page struct{ Data []app.Item }
	e.do(nil, "GET", "/v1/items", e.key, nil, 200, &page)
	if len(page.Data) != 1 || page.Data[0].Summary != "A post" {
		t.Fatalf("list: %+v", page.Data)
	}
}

// Summarize calls the model on demand, so a caller can't fire them without limit (burst 10, then 20 a minute).
func TestSummarizeIsRateLimited(t *testing.T) {
	e := newEnv(t)
	it := e.item(e.collection("Posts").ID, map[string]any{"title": "Post", "content": "x"})
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"tags\":[\"post\"],\"summary\":\"A post\"}"}}]}`))
	}))
	t.Cleanup(model.Close)
	e.svc.Cfg.Tagger = app.TaggerConfig{URL: model.URL, Model: "m"}
	for range 10 {
		e.do(nil, "POST", "/v1/items/"+it.ID+"/summarize", e.key, nil, 200, nil)
	}
	if resp := e.do(nil, "POST", "/v1/items/"+it.ID+"/summarize", e.key, nil, 429, nil); resp.Header.Get("Retry-After") == "" {
		t.Fatal("no Retry-After")
	}
	e.do(nil, "POST", "/v1/items/"+it.ID+"/summarize", e.keyWith(app.ScopeItemsRead, app.ScopeItemsWrite), nil, 200, nil) // per key
}

// The model's reply is read up to a bound (a runaway endpoint can't make the server buffer gigabytes), and a
// failed call doesn't tell a cloud tenant where the operator's model runs.
func TestSummarizeBoundsReplyAndHidesModelAddress(t *testing.T) {
	e := newEnv(t)
	it := e.item(e.collection("Posts").ID, map[string]any{"title": "Post", "content": "x"})
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"tags\":[\"post\"],\"summary\":\"A post\"}"}}],"pad":"`))
		_, _ = w.Write([]byte(strings.Repeat("a", 2<<20)))
		_, _ = w.Write([]byte(`"}`))
	}))
	t.Cleanup(model.Close)
	e.svc.Cfg.Tagger = app.TaggerConfig{URL: model.URL, Model: "m"}
	var res struct{ Error app.Error }
	e.do(nil, "POST", "/v1/items/"+it.ID+"/summarize", e.key, nil, 502, &res)

	model.Close() // nothing listens there now
	e.do(nil, "POST", "/v1/items/"+it.ID+"/summarize", e.key, nil, 502, &res)
	if host := strings.TrimPrefix(model.URL, "http://"); strings.Contains(res.Error.Message, host) {
		t.Fatalf("error names the model's address: %q", res.Error.Message)
	}
}

// Locally the summary comes from the user's own AI CLI (TAGGER_COMMAND / claude / codex): prompt on stdin,
// reply on stdout; a failing command is a 502 with its error on a local install.
func TestSummarizeWithLocalCommand(t *testing.T) {
	e := newEnv(t)
	it := e.item(e.collection("Posts").ID, map[string]any{"title": "Sneaker sale", "content": "30% off"})
	e.svc.Cfg.Tagger = app.TaggerConfig{Command: []string{"sh", "-c",
		`grep -q "Sneaker sale" && printf 'Sure:\n{"tags":["sale"],"summary":"Sneaker sale post"}\n'`}}
	var got app.Item
	e.do(nil, "POST", "/v1/items/"+it.ID+"/summarize", e.key, nil, 200, &got)
	if got.Summary != "Sneaker sale post" || !slices.Equal(got.Tags, []string{"sale"}) {
		t.Fatalf("summary %q tags %v", got.Summary, got.Tags)
	}
	e.svc.Cfg.Tagger = app.TaggerConfig{Command: []string{"sh", "-c", "echo boom >&2; exit 3"}}
	e.do(nil, "POST", "/v1/items/"+it.ID+"/summarize", e.key, nil, 502, nil)
}
