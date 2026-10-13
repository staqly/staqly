package httpapi_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

func runWorker(e *env) func() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.svc.RunWorker(ctx); close(done) }()
	return func() { cancel(); <-done }
}

func TestWaitForDecisionLongPoll(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Gate")
	it := e.item(c.ID, map[string]any{"title": "deploy prod?"})

	// Times out while pending.
	var d app.Decision
	start := time.Now()
	e.do(nil, "GET", "/v1/items/"+it.ID+"/wait?timeout=1", e.key, nil, 200, &d)
	if d.Decided || d.Item.Status != "pending" || time.Since(start) < 900*time.Millisecond {
		t.Fatalf("timeout: %+v after %s", d, time.Since(start))
	}
	// Returns as soon as a human decides.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(200 * time.Millisecond)
		e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/approve", e.key, map[string]any{"feedback": "go"}, 200, nil)
	}()
	start = time.Now()
	e.do(nil, "GET", "/v1/items/"+it.ID+"/wait?timeout=30", e.key, nil, 200, &d)
	wg.Wait()
	if !d.Decided || d.Item.Status != "approved" || time.Since(start) > 5*time.Second {
		t.Fatalf("decision: %+v after %s", d, time.Since(start))
	}
	e.do(nil, "GET", "/v1/items/itm_nope/wait?timeout=1", e.key, nil, 404, nil)

	// Report the outcome.
	var done app.Item
	e.do(nil, "POST", "/v1/items/"+it.ID+"/result", e.key, map[string]any{"status": "completed", "result": map[string]any{"deploy_id": 42}}, 200, &done)
	var meta map[string]any
	json.Unmarshal(done.Metadata, &meta)
	if done.Status != "completed" || meta["result"].(map[string]any)["deploy_id"] != float64(42) {
		t.Fatalf("result: %+v %v", done, meta)
	}
	e.do(nil, "POST", "/v1/items/"+it.ID+"/result", e.key, map[string]any{"status": "failed"}, 409, nil) // already completed
	pending := e.item(c.ID, map[string]any{"title": "not approved"})
	e.do(nil, "POST", "/v1/items/"+pending.ID+"/result", e.key, map[string]any{"status": "completed"}, 409, nil)
	var evs app.EventPage
	e.do(nil, "GET", "/v1/items/"+it.ID+"/events", e.key, nil, 200, &evs)
	if evs.Data[0].Type != "item.completed" {
		t.Fatalf("event: %s", evs.Data[0].Type)
	}
}

func TestExpiresInAndExpiredStatus(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Expiry")
	e.do(nil, "POST", "/v1/collections/"+c.ID+"/items", e.key, map[string]any{"title": "x", "expires_in": 5}, 422, nil)
	e.do(nil, "POST", "/v1/collections/"+c.ID+"/items", e.key, map[string]any{"title": "x", "expires_in": "soon"}, 422, nil)
	a := e.item(c.ID, map[string]any{"title": "seconds", "expires_in": 120})
	b := e.item(c.ID, map[string]any{"title": "duration", "expires_in": "1h"})
	if a.ExpiresAt == nil || b.ExpiresAt == nil || !(*a.ExpiresAt < *b.ExpiresAt) {
		t.Fatalf("expires_at: %v %v", a.ExpiresAt, b.ExpiresAt)
	}
	e.svc.Now = func() time.Time { return time.Now().Add(5 * time.Minute) }
	defer func() { e.svc.Now = time.Now }()
	stop := runWorker(e)
	defer stop()
	waitFor(t, func() bool {
		var it app.Item
		e.do(nil, "GET", "/v1/items/"+a.ID, e.key, nil, 200, &it)
		return it.Status == "expired"
	})
	var it app.Item
	e.do(nil, "GET", "/v1/items/"+b.ID, e.key, nil, 200, &it)
	if it.Status != "pending" {
		t.Fatalf("1h item expired too early: %s", it.Status)
	}
	var d app.Decision
	e.do(nil, "GET", "/v1/items/"+a.ID+"/wait?timeout=1", e.key, nil, 200, &d)
	if !d.Decided || d.Item.Status != "expired" {
		t.Fatalf("wait on expired: %+v", d)
	}
}

func TestEditableWhitelistAndDiff(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Posts")
	e.do(nil, "POST", "/v1/collections/"+c.ID+"/items", e.key, map[string]any{"title": "x", "editable": []string{"owner"}}, 422, nil)
	it := e.item(c.ID, map[string]any{"title": "Post", "content": "caption v1", "metadata": map[string]any{"caption": "c1", "seed": 7},
		"editable": []string{"content", "metadata.caption"}})
	if strings.Join(it.Editable, ",") != "content,metadata.caption" {
		t.Fatalf("editable: %v", it.Editable)
	}
	var errBody struct{ Error app.Error }
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/approve", e.key, map[string]any{"edits": map[string]any{"title": "Hacked"}}, 422, &errBody)
	if errBody.Error.Code != "not_editable" {
		t.Fatalf("whitelist: %+v", errBody.Error)
	}
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/approve", e.key, map[string]any{"edits": map[string]any{"metadata_patch": map[string]any{"seed": 1}}}, 422, nil)
	var out app.Item
	e.do(nil, "POST", "/v1/items/"+it.ID+"/actions/approve", e.key, map[string]any{"edits": map[string]any{
		"content": "caption v2", "metadata_patch": map[string]any{"caption": "c2"}}}, 200, &out)
	if out.Content != "caption v2" || out.Status != "approved" {
		t.Fatalf("approve with edits: %+v", out)
	}
	var evs app.EventPage
	e.do(nil, "GET", "/v1/items/"+it.ID+"/events", e.key, nil, 200, &evs)
	var data struct {
		Edited bool
		Diff   map[string]struct{ Before, After any }
	}
	json.Unmarshal(evs.Data[0].Data, &data)
	if !data.Edited || data.Diff["content"].Before != "caption v1" || data.Diff["content"].After != "caption v2" ||
		data.Diff["metadata.caption"].After != "c2" || len(data.Diff) != 2 {
		t.Fatalf("diff: %+v", data)
	}
}
