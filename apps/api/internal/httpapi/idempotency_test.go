package httpapi_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// rawPost sends body exactly as given (no re-encoding) with an Idempotency-Key.
func rawPost(e *env, path, idemKey, body string) (int, http.Header, map[string]any) {
	e.t.Helper()
	req, _ := http.NewRequest("POST", e.srv.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.key)
	req.Header.Set("Idempotency-Key", idemKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)

	return resp.StatusCode, resp.Header, out
}

// The same Idempotency-Key with a semantically equal JSON body replays, however the client formatted it
// (Python's json.dumps vs JavaScript's JSON.stringify), and a different body is still a mismatch.
func TestIdempotencyAcrossJSONFormatting(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Idem formats")
	path := "/v1/posts"

	cases := []struct {
		name, first, second string
		replay              bool
	}{
		{"whitespace (Python vs TypeScript)", `{"title": "Post", "collection": "` + c.ID + `", "content": "a", "metadata": {"n": 1, "tags": ["x", "y"]}}`,
			`{"title":"Post","collection":"` + c.ID + `","content":"a","metadata":{"n":1,"tags":["x","y"]}}`, true},
		{"key order, nested too", `{"title":"Post","collection":"` + c.ID + `","content":"b","metadata":{"a":1,"b":2}}`, `{"metadata":{"b":2,"a":1},"content":"b","collection":"` + c.ID + `","title":"Post"}`, true},
		{"string escapes", `{"title":"Post","collection":"` + c.ID + `","content":"caf\u00e9 \u003cb\u003e"}`, `{"title":"Post","collection":"` + c.ID + `","content":"café <b>"}`, true},
		{"different value", `{"title":"Post","collection":"` + c.ID + `","content":"c"}`, `{"title":"Post","collection":"` + c.ID + `","content":"c2"}`, false},
		{"array order matters", `{"title":"Post","collection":"` + c.ID + `","content":"d","metadata":{"l":[1,2]}}`, `{"title":"Post","collection":"` + c.ID + `","content":"d","metadata":{"l":[2,1]}}`, false},
		// Numbers keep their literal text: an integer field accepts 1 and rejects 1.0, so they differ.
		{"1 vs 1.0", `{"title":"Post","collection":"` + c.ID + `","content":"e","metadata":{"n":1}}`, `{"title":"Post","collection":"` + c.ID + `","content":"e","metadata":{"n":1.0}}`, false},
		{"extra field", `{"title":"Post","collection":"` + c.ID + `","content":"f"}`, `{"title":"Post","collection":"` + c.ID + `","content":"f","external_ref":"s"}`, false},
	}
	for i, tc := range cases {
		key := "fmt-" + string(rune('a'+i))
		code, h1, first := rawPost(e, path, key, tc.first)
		if code != 201 || h1.Get("Idempotent-Replayed") != "" {
			t.Fatalf("%s: first request %d %v", tc.name, code, first)
		}
		code, h2, second := rawPost(e, path, key, tc.second)
		if tc.replay {
			if code != 201 || h2.Get("Idempotent-Replayed") != "true" || second["id"] != first["id"] {
				t.Fatalf("%s: want replay of %v, got %d %v", tc.name, first["id"], code, second)
			}

			continue
		}
		if errObj, _ := second["error"].(map[string]any); code != 422 || errObj["code"] != "idempotency_mismatch" {
			t.Fatalf("%s: want 422 idempotency_mismatch, got %d %v", tc.name, code, second)
		}
	}
}

// A key reserved before canonical hashing (hash over the raw body bytes, no version prefix) still
// replays for the same bytes; a differently formatted body mismatches once, until the key expires.
func TestIdempotencyLegacyHashStillReplays(t *testing.T) {
	e := newEnv(t)
	c := e.collection("Idem legacy")
	path := "/v1/posts"
	body := `{"title":"Post","collection":"` + c.ID + `","content": "legacy"}`
	ctx := context.Background()
	p, err := e.svc.AuthenticateAPIKey(ctx, e.key)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("POST " + path + "?\n" + body))
	if _, err := e.svc.W.ReserveIdempotencyKey(ctx, db.ReserveIdempotencyKeyParams{WorkspaceID: p.WorkspaceID, PrincipalID: p.ID,
		Key: "old", RequestHash: hex.EncodeToString(sum[:]), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
		t.Fatal(err)
	}
	it := e.post(c.ID, map[string]any{"content": "stored"})
	stored, _ := json.Marshal(it)
	if err := e.svc.W.CompleteIdempotencyKey(ctx, db.CompleteIdempotencyKeyParams{StatusCode: sql.NullInt64{Int64: 201, Valid: true}, ResponseBody: stored,
		WorkspaceID: p.WorkspaceID, PrincipalID: p.ID, Key: "old"}); err != nil {
		t.Fatal(err)
	}
	if code, h, out := rawPost(e, path, "old", body); code != 201 || h.Get("Idempotent-Replayed") != "true" || out["id"] != it.ID {
		t.Fatalf("legacy replay: %d %v", code, out)
	}
	if code, _, out := rawPost(e, path, "old", `{"title":"Post","collection":"`+c.ID+`","content":"legacy"}`); code != 422 {
		t.Fatalf("legacy key, reformatted body: %d %v", code, out)
	}
}
