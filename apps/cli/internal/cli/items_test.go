package cli

import (
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestItemsListQueryAndTable(t *testing.T) {
	f := newFake(t)
	f.on("GET /v1/collections/Blog posts", 200, collection("col_1", "Blog posts"))
	f.on("GET /v1/collections", 200, map[string]any{"data": []any{collection("col_1", "Blog posts")}})
	f.on("GET /v1/items", 200, map[string]any{"data": []any{item("itm_1", "pending")}, "next_cursor": "c2"})

	r := f.run("items", "list", "--collection", "Blog posts", "--status", "pending", "--q", "launch", "--limit", "10").mustCode(t, 0)
	c := f.last("GET", "/v1/items")
	for k, want := range map[string]string{"collection_id": "col_1", "status": "pending", "q": "launch", "limit": "10"} {
		if got := c.Query[k]; len(got) != 1 || got[0] != want {
			t.Errorf("query %s = %v, want %s", k, got, want)
		}
	}
	mustContain(t, r.out, "ID", "STATUS", "itm_1", "pending", "Blog posts", "Title itm_1")
	mustContain(t, r.err, "--cursor c2")
}

func TestItemsListAllFollowsCursors(t *testing.T) {
	f := newFake(t)
	f.handle("GET /v1/items", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") == "" {
			writeJSON(w, 200, map[string]any{"data": []any{item("itm_1", "approved")}, "next_cursor": "p2"})
			return
		}
		writeJSON(w, 200, map[string]any{"data": []any{item("itm_2", "approved")}})
	})
	r := f.run("items", "list", "--all", "--json").mustCode(t, 0)
	m := mustJSON(t, r.out)
	if n := len(m["data"].([]any)); n != 2 {
		t.Fatalf("got %d items, want 2", n)
	}
	if n := len(f.find("GET", "/v1/items")); n != 2 {
		t.Fatalf("made %d list calls, want 2", n)
	}
}

func TestItemsGetJSONIsVerbatim(t *testing.T) {
	f := newFake(t)
	it := item("itm_1", "pending")
	it["future_field"] = "kept"
	f.on("GET /v1/items/itm_1", 200, it)
	r := f.run("items", "get", "itm_1", "--json").mustCode(t, 0)
	if mustJSON(t, r.out)["future_field"] != "kept" {
		t.Fatal("--json must print the server response verbatim")
	}
}

func TestItemsGetHuman(t *testing.T) {
	f := newFake(t)
	it := item("itm_1", "pending")
	it["metadata"] = map[string]any{"seed": 42}
	it["content"] = "line one\nline two"
	f.on("GET /v1/items/itm_1", 200, it)
	f.on("GET /v1/collections/col_1", 200, collection("col_1", "Blog posts"))
	f.on("GET /v1/items/itm_1/actions", 200, map[string]any{"data": []any{
		map[string]any{"name": "approve"}, map[string]any{"name": "regenerate", "requires_feedback": true}}})
	r := f.run("items", "get", "itm_1").mustCode(t, 0)
	mustContain(t, r.out, "Blog posts", `"seed": 42`, "  line two", "approve, regenerate*")
	if strings.Contains(r.out, "Decided") {
		t.Error("undecided item should not print a Decided line")
	}
}

func TestItemsCreateJSONBody(t *testing.T) {
	f := newFake(t)
	f.on("POST /v1/collections/Blog posts/items", 201, item("itm_9", "pending"))
	r := f.run("items", "create", "Blog posts", "--title", "Hello", "--content", "Body",
		"--metadata", `{"a":1}`, "--meta", "seed=42", "--meta", "tag=x", "--url", "https://example.com/x", "--external-ref", "PR-7").mustCode(t, 0)
	mustContain(t, r.out, "Created", "itm_9")
	c := f.last("POST", "/v1/collections/Blog posts/items")
	b := c.json(t)
	if b["title"] != "Hello" || b["content"] != "Body" || b["source"] != "cli" || b["url"] != "https://example.com/x" || b["external_ref"] != "PR-7" {
		t.Fatalf("unexpected body %v", b)
	}
	md := b["metadata"].(map[string]any)
	if md["a"] != float64(1) || md["seed"] != float64(42) || md["tag"] != "x" {
		t.Fatalf("metadata = %v", md)
	}
	if !strings.HasPrefix(c.Header.Get("Idempotency-Key"), "cli-") {
		t.Fatalf("missing Idempotency-Key header")
	}
	if c.Header.Get("Authorization") != "" {
		t.Fatal("keyless (local) requests must not send Authorization")
	}
}

func TestItemsCreateContentSources(t *testing.T) {
	f := newFake(t)
	f.on("POST /v1/collections/c/items", 201, item("itm_9", "pending"))
	path := filepath.Join(t.TempDir(), "post.md")
	os.WriteFile(path, []byte("# From file"), 0o600)
	f.run("items", "create", "c", "--content", "@"+path).mustCode(t, 0)
	if got := f.last("POST", "/v1/collections/c/items").json(t)["content"]; got != "# From file" {
		t.Fatalf("content = %v", got)
	}
	f.runWith(runOpts{stdin: "from stdin"}, "items", "create", "c", "--stdin").mustCode(t, 0)
	if got := f.last("POST", "/v1/collections/c/items").json(t)["content"]; got != "from stdin" {
		t.Fatalf("content = %v", got)
	}
	f.runWith(runOpts{stdin: "dash"}, "items", "create", "c", "--content", "-").mustCode(t, 0)
	if got := f.last("POST", "/v1/collections/c/items").json(t)["content"]; got != "dash" {
		t.Fatalf("content = %v", got)
	}
	f.run("items", "create", "c", "--metadata", "[1]").mustCode(t, ExitUsage)
	f.run("items", "create", "c", "--meta", "novalue").mustCode(t, ExitUsage)
	f.run("items", "create").mustCode(t, ExitUsage)
}

func TestItemsCreateMultipartFile(t *testing.T) {
	f := newFake(t)
	var parts = map[string]string{}
	var fileName string
	f.handle("POST /v1/collections/Avatars/items", func(w http.ResponseWriter, r *http.Request) {
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Errorf("content type: %v", err)
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			b, _ := io.ReadAll(p)
			parts[p.FormName()] = string(b)
			if p.FormName() == "file" {
				fileName = p.FileName()
			}
		}
		writeJSON(w, 201, item("itm_5", "pending"))
	})
	path := filepath.Join(t.TempDir(), "avatar.png")
	os.WriteFile(path, []byte("PNGDATA"), 0o600)
	f.run("items", "create", "Avatars", "--file", path, "--title", "Avatar").mustCode(t, 0)
	if parts["file"] != "PNGDATA" || fileName != "avatar.png" {
		t.Fatalf("file part = %q (%s)", parts["file"], fileName)
	}
	if !strings.Contains(parts["item"], `"title":"Avatar"`) {
		t.Fatalf("item part = %q", parts["item"])
	}
	f.run("items", "create", "Avatars", "--file", "/does/not/exist").mustCode(t, ExitError)
}

func respond(status int, body any) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) { writeJSON(w, status, body) }
}

func decided(id, status, feedback string) map[string]any {
	it := item(id, status)
	it["feedback"] = feedback
	return it
}

func TestPushWaitExitCodes(t *testing.T) {
	cases := []struct {
		final    string
		feedback string
		code     int
		want     string
	}{
		{"approved", "ship it", ExitOK, "Approved"},
		{"rejected", "too long", ExitRejected, "Rejected"},
		{"expired", "", ExitTimeout, "Expired"},
		{"processing", "", ExitOther, "is now processing"},
	}
	for _, tc := range cases {
		t.Run(tc.final, func(t *testing.T) {
			f := newFake(t)
			f.on("POST /v1/collections/Deploys/items", 201, item("itm_1", "pending"))
			f.seq("GET /v1/items/itm_1", respond(200, item("itm_1", "pending")), respond(200, item("itm_1", "pending")),
				respond(200, decided("itm_1", tc.final, tc.feedback)))
			r := f.run("push", "Deploys", "--title", "Deploy").mustCode(t, tc.code)
			mustContain(t, r.out, tc.want)
			if tc.feedback != "" {
				mustContain(t, r.out, "Feedback: "+tc.feedback)
			}
			if len(f.find("GET", "/v1/items/itm_1")) != 3 {
				t.Fatalf("expected 3 polls")
			}
			// Backoff grows: 1s, 1.5s.
			if len(r.sleeps) != 2 || r.sleeps[0] != time.Second || r.sleeps[1] != 1500*time.Millisecond {
				t.Fatalf("sleeps = %v", r.sleeps)
			}
		})
	}
}

func TestPushWaitTimeout(t *testing.T) {
	f := newFake(t)
	f.on("POST /v1/collections/D/items", 201, item("itm_1", "pending"))
	f.on("GET /v1/items/itm_1", 200, item("itm_1", "pending"))
	r := f.run("push", "D", "--timeout", "20s").mustCode(t, ExitTimeout)
	mustContain(t, r.err, "Timed out after 20s")
	var total time.Duration
	for _, d := range r.sleeps {
		total += d
		if d > 10*time.Second {
			t.Fatalf("backoff exceeded the 10s cap: %v", r.sleeps)
		}
	}
	if total != 20*time.Second {
		t.Fatalf("waited %v, want exactly the 20s timeout", total)
	}
}

func TestPushWaitJSON(t *testing.T) {
	f := newFake(t)
	f.on("POST /v1/collections/D/items", 201, item("itm_1", "pending"))
	f.on("GET /v1/items/itm_1", 200, decided("itm_1", "rejected", "nope"))
	r := f.run("push", "D", "--json").mustCode(t, ExitRejected)
	if m := mustJSON(t, r.out); m["status"] != "rejected" || m["feedback"] != "nope" {
		t.Fatalf("json = %v", m)
	}
}

func TestPushWaitRetriesTransientErrors(t *testing.T) {
	f := newFake(t)
	f.on("POST /v1/collections/D/items", 201, item("itm_1", "pending"))
	f.seq("GET /v1/items/itm_1",
		respond(503, apiErr(503, "internal", "down")),
		func(w http.ResponseWriter) {
			w.Header().Set("Retry-After", "7")
			writeJSON(w, 429, apiErr(429, "rate_limited", "slow down"))
		},
		respond(200, decided("itm_1", "approved", "")))
	r := f.run("push", "D").mustCode(t, ExitOK)
	if len(r.sleeps) != 2 || r.sleeps[1] != 7*time.Second {
		t.Fatalf("sleeps = %v, want Retry-After 7s honoured", r.sleeps)
	}
	mustContain(t, r.err, "still waiting")
}

func TestPushWaitFatalError(t *testing.T) {
	f := newFake(t)
	f.on("POST /v1/collections/D/items", 201, item("itm_1", "pending"))
	f.on("GET /v1/items/itm_1", 404, apiErr(404, "not_found", "item not found"))
	r := f.run("push", "D").mustCode(t, ExitError)
	mustContain(t, r.err, "not found")
}

func TestPushNoWaitAndStatusConflict(t *testing.T) {
	f := newFake(t)
	f.on("POST /v1/collections/D/items", 201, item("itm_1", "pending"))
	f.run("push", "D", "--no-wait").mustCode(t, 0)
	if len(f.find("GET", "/v1/items/itm_1")) != 0 {
		t.Fatal("--no-wait must not poll")
	}
	f.run("push", "D", "--status", "draft").mustCode(t, ExitUsage)
	f.run("items", "create", "D", "--status", "draft").mustCode(t, 0)
}

func TestItemsCreateWaitFlag(t *testing.T) {
	f := newFake(t)
	f.on("POST /v1/collections/D/items", 201, item("itm_1", "pending"))
	f.on("GET /v1/items/itm_1", 200, decided("itm_1", "approved", ""))
	f.run("items", "create", "D", "--wait", "--timeout", "1m").mustCode(t, 0)
}

func TestApproveRejectAction(t *testing.T) {
	f := newFake(t)
	f.on("POST /v1/items/itm_1/actions/approve", 200, item("itm_1", "approved"))
	f.on("POST /v1/items/itm_1/actions/reject", 200, item("itm_1", "rejected"))
	f.on("POST /v1/items/itm_1/actions/regenerate", 200, item("itm_1", "processing"))

	r := f.run("items", "approve", "itm_1", "--feedback", "great", "--expected-version", "3").mustCode(t, 0)
	mustContain(t, r.out, "Approved itm_1")
	b := f.last("POST", "/v1/items/itm_1/actions/approve").json(t)
	if b["feedback"] != "great" || b["expected_version"] != float64(3) {
		t.Fatalf("body = %v", b)
	}
	f.run("items", "reject", "itm_1", "-m", "too long").mustCode(t, 0)
	if b := f.last("POST", "/v1/items/itm_1/actions/reject").json(t); b["feedback"] != "too long" {
		t.Fatalf("body = %v", b)
	}
	f.run("items", "action", "itm_1", "regenerate", "--feedback", "smile", "--payload", `{"n":2}`).mustCode(t, 0)
	b = f.last("POST", "/v1/items/itm_1/actions/regenerate").json(t)
	if b["feedback"] != "smile" || b["payload"].(map[string]any)["n"] != float64(2) {
		t.Fatalf("body = %v", b)
	}
	f.run("items", "action", "itm_1", "regenerate", "--payload", "{bad").mustCode(t, ExitUsage)
	f.run("items", "approve").mustCode(t, ExitUsage)
}

func TestApproveMultipleWithFailure(t *testing.T) {
	f := newFake(t)
	f.on("POST /v1/items/itm_1/actions/approve", 200, item("itm_1", "approved"))
	f.on("POST /v1/items/itm_2/actions/approve", 409, apiErr(409, "conflict", "item already decided"))
	r := f.run("items", "approve", "itm_1", "itm_2").mustCode(t, ExitError)
	mustContain(t, r.out, "Approved itm_1")
	mustContain(t, r.err, "itm_2", "conflict", "1 of 2 failed")

	r = f.run("items", "approve", "itm_1", "itm_2", "--json").mustCode(t, ExitError)
	if !strings.Contains(r.out, `"ok": false`) || !strings.Contains(r.out, `"code": "conflict"`) {
		t.Fatalf("json = %s", r.out)
	}
}

func TestItemsUpdate(t *testing.T) {
	f := newFake(t)
	f.on("PATCH /v1/items/itm_1", 200, item("itm_1", "completed"))
	f.run("items", "update", "itm_1", "--status", "completed", "--metadata-patch", `{"url":"https://x","old":null}`).mustCode(t, 0)
	b := f.last("PATCH", "/v1/items/itm_1").json(t)
	mp := b["metadata_patch"].(map[string]any)
	if b["status"] != "completed" || mp["url"] != "https://x" || mp["old"] != nil {
		t.Fatalf("body = %v", b)
	}
	if _, ok := mp["old"]; !ok {
		t.Fatal("null must be sent to remove a key")
	}
	f.run("items", "update", "itm_1").mustCode(t, ExitUsage)
	f.run("items", "update", "itm_1", "--metadata-patch", "nope").mustCode(t, ExitUsage)
}

func TestItemsHistoryChronological(t *testing.T) {
	f := newFake(t)
	f.on("GET /v1/items/itm_1/events", 200, map[string]any{"data": []any{
		map[string]any{"id": "e2", "seq": 2, "type": "item.approved", "actor": map[string]any{"type": "local", "name": "Local"}, "data": map[string]any{"feedback": "ok"}, "created_at": "2026-10-13T10:01:00Z"},
		map[string]any{"id": "e1", "seq": 1, "type": "item.created", "actor": map[string]any{"type": "api_key", "name": "Agent"}, "data": map[string]any{}, "created_at": "2026-10-13T10:00:00Z"},
	}})
	r := f.run("items", "history", "itm_1").mustCode(t, 0)
	if strings.Index(r.out, "item.created") > strings.Index(r.out, "item.approved") {
		t.Fatalf("history must be oldest first:\n%s", r.out)
	}
	mustContain(t, r.out, "feedback=ok", "Agent")
}

func TestDecisionExitCode(t *testing.T) {
	for status, want := range map[string]int{"approved": 0, "rejected": 2, "expired": 3, "completed": 4, "archived": 4} {
		if got := decisionExitCode(status, false); got != want {
			t.Errorf("%s → %d, want %d", status, got, want)
		}
	}
	if decisionExitCode("pending", true) != ExitTimeout {
		t.Error("timeout must map to 3")
	}
}

func waitResp(decided bool, it map[string]any) map[string]any {
	return map[string]any{"decided": decided, "item": it}
}

func TestPushLongPollsWait(t *testing.T) {
	f := newFake(t)
	f.on("POST /v1/collections/D/items", 201, item("itm_1", "pending"))
	var advance func(time.Duration)
	var timeouts []string
	f.handle("GET /v1/items/itm_1/wait", func(w http.ResponseWriter, r *http.Request) {
		timeouts = append(timeouts, r.URL.Query().Get("timeout"))
		if len(timeouts) < 3 {
			// Simulate the server holding the connection for the full timeout.
			n, _ := time.ParseDuration(r.URL.Query().Get("timeout") + "s")
			advance(n)
			writeJSON(w, 200, waitResp(false, item("itm_1", "pending")))
			return
		}
		writeJSON(w, 200, waitResp(true, decided("itm_1", "approved", "lgtm")))
	})
	r := f.runWith(runOpts{clock: func(a func(time.Duration)) { advance = a }}, "push", "D", "--timeout", "130s").mustCode(t, 0)
	mustContain(t, r.out, "Approved", "Feedback: lgtm")
	if strings.Join(timeouts, ",") != "60,60,10" {
		t.Fatalf("wait timeouts = %v, want 60,60,10 (capped at 60, then the remainder)", timeouts)
	}
	if len(r.sleeps) != 0 {
		t.Fatalf("long-polls must not sleep in between: %v", r.sleeps)
	}
	if len(f.find("GET", "/v1/items/itm_1")) != 0 {
		t.Fatal("must not fall back to plain polling when /wait exists")
	}
	// With a 100 s budget the same server times out after 60+40 s.
	timeouts = nil
	f.runWith(runOpts{clock: func(a func(time.Duration)) { advance = a }}, "push", "D", "--timeout", "100s").mustCode(t, ExitTimeout)
	if strings.Join(timeouts, ",") != "60,40" {
		t.Fatalf("timeouts = %v", timeouts)
	}
}

func TestPushWaitNoBusyLoop(t *testing.T) {
	// A server that answers /wait immediately must not be hammered.
	f := newFake(t)
	f.on("POST /v1/collections/D/items", 201, item("itm_1", "pending"))
	f.on("GET /v1/items/itm_1/wait", 200, waitResp(false, item("itm_1", "pending")))
	r := f.run("push", "D", "--timeout", "30s").mustCode(t, ExitTimeout)
	if len(r.sleeps) == 0 || len(f.find("GET", "/v1/items/itm_1/wait")) > 10 {
		t.Fatalf("expected backoff between immediate answers: %d calls, sleeps %v", len(f.find("GET", "/v1/items/itm_1/wait")), r.sleeps)
	}
}

func TestPushWaitExpiredAndJSON(t *testing.T) {
	f := newFake(t)
	f.on("POST /v1/collections/D/items", 201, item("itm_1", "pending"))
	f.on("GET /v1/items/itm_1/wait", 200, waitResp(true, item("itm_1", "expired")))
	r := f.run("push", "D").mustCode(t, ExitTimeout)
	mustContain(t, r.out, "Expired")
	r = f.run("push", "D", "--json").mustCode(t, ExitTimeout)
	if m := mustJSON(t, r.out); m["status"] != "expired" || m["id"] != "itm_1" {
		t.Fatalf("--json must print the item, got %v", m)
	}
}

func TestPushWaitTransientOnWait(t *testing.T) {
	f := newFake(t)
	f.on("POST /v1/collections/D/items", 201, item("itm_1", "pending"))
	f.seq("GET /v1/items/itm_1/wait",
		respond(502, apiErr(502, "bad_gateway", "proxy")),
		respond(200, waitResp(true, decided("itm_1", "rejected", "no"))))
	r := f.run("push", "D").mustCode(t, ExitRejected)
	if len(r.sleeps) != 1 || r.sleeps[0] != time.Second {
		t.Fatalf("sleeps = %v", r.sleeps)
	}
}

func TestCreateExpiresEditable(t *testing.T) {
	f := newFake(t)
	f.on("POST /v1/collections/D/items", 201, item("itm_1", "pending"))
	f.run("items", "create", "D", "--expires-in", "24h", "--editable", "title,content", "--editable", "metadata.price").mustCode(t, 0)
	b := f.last("POST", "/v1/collections/D/items").json(t)
	if b["expires_in"] != "24h" || len(b["editable"].([]any)) != 3 {
		t.Fatalf("body = %v", b)
	}
	f.run("push", "D", "--expires-in", "3600", "--no-wait").mustCode(t, 0)
	if b := f.last("POST", "/v1/collections/D/items").json(t); b["expires_in"] != float64(3600) {
		t.Fatalf("seconds must be sent as a number: %v", b["expires_in"])
	}
	f.run("items", "create", "D", "--expires-in", "soon").mustCode(t, ExitUsage)
}

func TestItemsResult(t *testing.T) {
	f := newFake(t)
	f.on("POST /v1/items/itm_1/result", 200, item("itm_1", "completed"))
	r := f.run("items", "result", "itm_1", "--status", "completed", "--result", `{"url":"https://x"}`).mustCode(t, 0)
	mustContain(t, r.out, "Reported itm_1", "completed")
	b := f.last("POST", "/v1/items/itm_1/result").json(t)
	if b["status"] != "completed" || b["result"].(map[string]any)["url"] != "https://x" {
		t.Fatalf("body = %v", b)
	}
	f.run("items", "result", "itm_1", "--status", "failed", "--error", "boom").mustCode(t, 0)
	if b := f.last("POST", "/v1/items/itm_1/result").json(t); b["error"] != "boom" || b["status"] != "failed" {
		t.Fatalf("body = %v", b)
	}
	f.run("items", "result", "itm_1").mustCode(t, ExitUsage)
	f.run("items", "result", "itm_1", "--status", "approved").mustCode(t, ExitUsage)
	f.run("items", "result", "itm_1", "--status", "failed", "--result", "{x").mustCode(t, ExitUsage)
}
