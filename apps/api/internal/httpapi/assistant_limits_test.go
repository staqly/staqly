package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/blob"
)

// assistantModel is a model endpoint that answers every call with answer (data lines) and counts the calls.
func assistantModel(t *testing.T, calls *atomic.Int32, answer func(r *http.Request) string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, answer(r)+"data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv
}

const assistantHi = "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}],\"usage\":{\"prompt_tokens\":1000,\"completion_tokens\":1000}}\n\n"

// The assistant spends the workspace's AI allowance: it takes posts:read, which every role has (a viewer's
// assistant only looks) and a key made for something else does not. Dashboard sessions and local mode hold every
// scope.
func TestAssistantNeedsPostsRead(t *testing.T) {
	e := newEnv(t)
	var calls atomic.Int32
	model := assistantModel(t, &calls, func(*http.Request) string { return assistantHi })
	e.svc.Cfg.AI = app.AIConfig{Assistant: app.AIEndpoint{BaseURL: model.URL + "/v1", Model: "m", Prices: &[2]float64{1, 1}}}
	turn := map[string]any{"messages": []map[string]any{{"role": "user", "content": "hi"}}}

	for _, scopes := range [][]string{{"events:read"}, {"media:read", "media:write", "channels:read"}} {
		e.do(nil, "POST", "/v1/assistant/chat", e.keyWith(scopes...), turn, 403, nil)
	}
	var st app.AIStatus
	e.do(nil, "GET", "/v1/ai", e.key, nil, 200, &st)
	if calls.Load() != 0 || st.Usage.UsedUSD != 0 {
		t.Fatalf("refused keys reached the model: %d calls, used $%v", calls.Load(), st.Usage.UsedUSD)
	}
	e.do(nil, "POST", "/v1/assistant/chat", e.keyWith("posts:read"), turn, 200, nil)
	e.do(e.browser, "POST", "/v1/assistant/chat", "", turn, 200, nil, "X-CSRF-Token", e.csrf)
	if calls.Load() != 2 {
		t.Fatalf("a posts:read key and a session: %d model calls", calls.Load())
	}
	local := app.Principal{WorkspaceID: workspaceID(e), Kind: app.PrincipalLocal}
	if err := e.svc.CheckAssistant(local, app.AssistantRequest{Messages: []app.AssistantMessage{{Role: "user", Content: "hi"}}}); err != nil {
		t.Fatalf("local mode: %v", err)
	}
}

type countingBlob struct {
	blob.Store
	opens atomic.Int64
}

func (c *countingBlob) Open(ctx context.Context, key string) (io.ReadSeekCloser, error) {
	c.opens.Add(1)
	return c.Store.Open(ctx, key)
}

// One request cannot make the server read without bound: a conversation has caps, a turn that is refused anyway
// reads no attachment, and a turn that runs reads what the model gets (the last 60 messages), each picture once.
func TestAssistantAttachmentsAreBounded(t *testing.T) {
	e := newEnv(t)
	e.paid()
	var calls atomic.Int32
	var pictures int
	model := assistantModel(t, &calls, func(r *http.Request) string {
		raw, _ := io.ReadAll(r.Body)
		pictures = strings.Count(string(raw), `"image_url":{`)
		return assistantHi
	})
	e.svc.Cfg.AI = app.AIConfig{Assistant: app.AIEndpoint{BaseURL: model.URL + "/v1", Model: "m", Prices: &[2]float64{1, 1}}}
	var pic app.Media
	e.upload("/v1/media", e.key, nil, "p.png", pngBytes, 201, &pic)
	counter := &countingBlob{Store: e.svc.Cfg.Blob}
	e.svc.Cfg.Blob = counter

	conversation := func(messages, attachments int) []app.AssistantMessage {
		att := make([]app.AssistantAttachment, attachments)
		for i := range att {
			att[i] = app.AssistantAttachment{MediaID: pic.ID}
		}
		msgs := make([]app.AssistantMessage, messages)
		for i := range msgs {
			msgs[i] = app.AssistantMessage{Role: "user", Content: "x"}
		}
		// On the oldest messages, which the model no longer gets, and on the latest one.
		for i := 0; i < messages && attachments > 0; i += max(1, messages-1) {
			msgs[i].Attachments = att
		}
		return msgs
	}

	// Refused: more messages or attachments than one conversation takes.
	crowded := conversation(21, 10)
	for i := range crowded {
		crowded[i].Attachments = crowded[0].Attachments // 210 in all
	}
	for _, msgs := range [][]app.AssistantMessage{conversation(1001, 0), crowded} {
		e.do(nil, "POST", "/v1/assistant/chat", e.key, app.AssistantRequest{Messages: msgs}, 422, nil)
	}

	// No allowance: the turn ends with ai_limit_reached and nothing was read for it.
	e.limits(map[string]int64{app.LimitAIMonthlyCents: 0})
	evs := e.assistantTurn(conversation(300, 10), nil)
	if last := evs[len(evs)-1]; last.Type != "error" || last.Error.Code != "ai_limit_reached" {
		t.Fatalf("no allowance: %+v", last)
	}
	if calls.Load() != 0 || counter.opens.Load() != 0 {
		t.Fatalf("a refused turn made %d model calls and read the picture %d times", calls.Load(), counter.opens.Load())
	}

	// With an allowance: 20 mentions of one picture in two messages, one of them cut from the history.
	e.limits(map[string]int64{app.LimitAIMonthlyCents: 10_000})
	evs = e.assistantTurn(conversation(300, 10), nil)
	if last := evs[len(evs)-1]; last.Type != "done" {
		t.Fatalf("turn: %+v", last)
	}
	if counter.opens.Load() != 1 || pictures != 1 {
		t.Fatalf("the picture was read %d times and sent %d times, want once", counter.opens.Load(), pictures)
	}
}

// A call the model endpoint refused costs nothing, also when the fallback then answers.
func TestAssistantRefusedCallCostsNothing(t *testing.T) {
	e := newEnv(t)
	e.paid()
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer down.Close()
	var calls atomic.Int32
	up := assistantModel(t, &calls, func(*http.Request) string {
		return "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2,\"cost\":0.0004}}\n\n"
	})
	used := func() float64 {
		var st app.AIStatus
		e.do(nil, "GET", "/v1/ai", e.key, nil, 200, &st)
		return st.Usage.UsedUSD
	}
	hi := []app.AssistantMessage{{Role: "user", Content: "hi"}}

	e.svc.Cfg.AI = app.AIConfig{Assistant: app.AIEndpoint{BaseURL: down.URL + "/v1", Model: "big", Prices: &[2]float64{3, 15}},
		AssistantFallback: app.AIEndpoint{BaseURL: down.URL + "/v1", Model: "cheap", Prices: &[2]float64{3, 15}}}
	if evs := e.assistantTurn(hi, nil); evs[len(evs)-1].Type != "error" {
		t.Fatalf("both endpoints down: %+v", evs)
	}
	if got := used(); got != 0 {
		t.Fatalf("both endpoints answered 503, yet used_usd = %v", got)
	}

	e.svc.Cfg.AI.AssistantFallback.BaseURL = up.URL + "/v1"
	if evs := e.assistantTurn(hi, nil); evs[len(evs)-1].Type != "done" {
		t.Fatalf("fallback: %+v", evs)
	}
	if got := used(); got != 0.0004 {
		t.Fatalf("the fallback answered for $0.0004 after a refusal, yet used_usd = %v", got)
	}
}

// One approval runs one tool call, also when the model gives two calls the same id.
func TestAssistantApprovalRunsOneCall(t *testing.T) {
	e := newEnv(t)
	e.paid()
	col := e.collection("Launch")
	call := func(index int, title string) string {
		args, _ := json.Marshal(fmt.Sprintf(`{"title":%q,"collection":%q,"content":"hello"}`, title, col.ID))
		return fmt.Sprintf("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":%d,\"id\":\"dup\",\"function\":{\"name\":\"create_post\",\"arguments\":%s}}]}}]}\n\n", index, args)
	}
	var calls atomic.Int32
	model := assistantModel(t, &calls, func(r *http.Request) string {
		var body struct{ Messages []struct{ Role string } }
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Messages[len(body.Messages)-1].Role == "user" {
			return call(0, "One") + call(1, "Two")
		}
		return "data: {\"choices\":[{\"delta\":{\"content\":\"Done.\"}}]}\n\n"
	})
	e.svc.Cfg.AI = app.AIConfig{Assistant: app.AIEndpoint{BaseURL: model.URL + "/v1", Model: "m", Prices: &[2]float64{0, 0}}}
	titles := func() string {
		var posts struct{ Data []app.Post }
		e.do(nil, "GET", "/v1/posts", e.key, nil, 200, &posts)
		var out []string
		for _, p := range posts.Data {
			out = append(out, p.Title)
		}
		return strings.Join(out, ",")
	}
	// turn sends the conversation and answers it as the turn left it, with the call that waits.
	turn := func(msgs []app.AssistantMessage, d *app.AssistantDecision) ([]app.AssistantMessage, *app.AssistantToolCall) {
		var confirm *app.AssistantToolCall
		for _, ev := range e.assistantTurn(msgs, d) {
			switch ev.Type {
			case "message":
				msgs = append(msgs, *ev.Message)
			case "confirm":
				confirm = ev.ToolCall
			case "error":
				t.Fatalf("error: %+v", ev.Error)
			}
		}
		return msgs, confirm
	}

	// From the model: the two calls get ids of their own, so each waits for its own decision.
	msgs, first := turn([]app.AssistantMessage{{Role: "user", Content: "Write two posts"}}, nil)
	asked := msgs[len(msgs)-1].ToolCalls
	if first == nil || len(asked) != 2 || asked[0].ID == asked[1].ID {
		t.Fatalf("the model's calls: %+v, waiting %+v", asked, first)
	}
	msgs, second := turn(msgs, &app.AssistantDecision{ToolCallID: first.ID, Allow: true})
	if got := titles(); got != "One" || second == nil || second.ID == first.ID {
		t.Fatalf("one approval: posts %q, waiting %+v", got, second)
	}

	// From a client's conversation, where two calls share an id: the approval still runs one.
	same := []app.AssistantMessage{{Role: "user", Content: "Write two posts"}, {Role: "assistant", ToolCalls: []app.AssistantToolCall{
		{ID: "dup", Name: "create_post", Arguments: fmt.Sprintf(`{"title":"Three","collection":%q,"content":"hello"}`, col.ID)},
		{ID: "dup", Name: "create_post", Arguments: fmt.Sprintf(`{"title":"Four","collection":%q,"content":"hello"}`, col.ID)}}}}
	turn(same, &app.AssistantDecision{ToolCallID: "dup", Allow: true})
	if got := titles(); strings.Contains(got, "Four") || !strings.Contains(got, "Three") {
		t.Fatalf("one approval for a shared id: posts %q", got)
	}
}
