package httpapi_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// gpexFile is a stand-in project file: OpenGPEX's container starts with "GPEX".
func gpexFile(body string) []byte { return append([]byte("GPEX\x01\x00\x14\x00"), body...) }

// sendFile is upload with any method (PUT for a new version of a project).
func (e *env) sendFile(method, path, key string, fields map[string]string, content []byte, want int, out any) {
	e.t.Helper()
	body, ct := multipartBody(e.t, fields, "file.bin", content)
	req, _ := http.NewRequest(method, e.srv.URL+path, body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		e.t.Fatalf("%s %s: status %d, want %d: %s", method, path, resp.StatusCode, want, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			e.t.Fatalf("decode: %v: %s", err, raw)
		}
	}
}

// Replace the original: the edit becomes a new file with the original's name, alt text, folder and tags,
// drafts that used the original use the edit, and the original goes to Deleted. A file that a scheduled (or
// published) post uses is refused, so those posts keep what they had.
func TestMediaReplace(t *testing.T) {
	e := newEnv(t)
	e.paid()
	c := e.collection("Edits")
	ch := e.customChannel("Own")

	var a, b app.Media
	e.upload("/v1/media", e.key, nil, "photo.png", realPNG(t, 20, 10), 201, &a)
	e.upload("/v1/media", e.key, nil, "other.png", realPNG(t, 30, 10), 201, &b)
	e.do(nil, "PATCH", "/v1/media/"+a.ID, e.key, map[string]any{"alt_text": "A runner"}, 200, nil)
	draft := e.post(c.ID, map[string]any{"media": []string{a.ID}})
	scheduled := e.post(c.ID, map[string]any{"media": []string{b.ID}, "targets": []map[string]any{{"channel_id": ch.ID}}})
	e.do(nil, "POST", "/v1/posts/"+scheduled.ID+"/schedule", e.key, map[string]any{"publish_at": future(time.Hour)}, 200, nil)

	// The default agent key may not delete: replacing trashes the original, so it needs posts:delete too.
	e.upload("/v1/media/"+a.ID+"/replace", e.key, nil, "edit.png", realPNG(t, 40, 40), 403, nil)
	var edited app.Media
	e.upload("/v1/media/"+a.ID+"/replace", e.admin, nil, "edit.png", realPNG(t, 40, 40), 201, &edited)
	if edited.ID == a.ID || edited.OriginalName != "photo.png" || edited.AltText != "A runner" || edited.Width != 40 {
		t.Fatalf("edited: %+v", edited)
	}
	var after app.Post
	e.do(nil, "GET", "/v1/posts/"+draft.ID, e.key, nil, 200, &after)
	if len(after.Media) != 1 || after.Media[0] != edited.ID {
		t.Fatalf("the draft uses the edit: %v", after.Media)
	}
	var old app.Media
	e.do(nil, "GET", "/v1/media/"+a.ID+"/info", e.key, nil, 200, &old)
	if old.DeletedAt == nil {
		t.Fatalf("the original is in Deleted: %+v", old)
	}

	e.upload("/v1/media/"+b.ID+"/replace", e.admin, nil, "edit.png", realPNG(t, 40, 40), 409, nil)
	e.do(nil, "GET", "/v1/posts/"+scheduled.ID, e.key, nil, 200, &after)
	if after.Media[0] != b.ID {
		t.Fatalf("the scheduled post keeps its file: %v", after.Media)
	}
}

// Projects: an editable .gpex file per picture, listed by the files it was made from, saved again in place,
// readable back byte for byte, deleted with posts:delete, invisible to another workspace.
func TestMediaProjects(t *testing.T) {
	e := newEnv(t)
	e.paid()
	ctx := context.Background()
	var a app.Media
	e.upload("/v1/media", e.key, nil, "photo.png", realPNG(t, 20, 10), 201, &a)

	e.sendFile("POST", "/v1/media-projects", e.key, map[string]string{"name": "Poster"}, []byte("not a project"), 415, nil)
	e.sendFile("POST", "/v1/media-projects", e.key, map[string]string{"source_ids": "med_nope"}, gpexFile("x"), 422, nil)

	var p app.MediaProject
	e.sendFile("POST", "/v1/media-projects", e.key, map[string]string{"name": "Poster", "source_ids": a.ID}, gpexFile("v1"), 201, &p)
	if p.Name != "Poster" || len(p.SourceIDs) != 1 || p.SourceIDs[0] != a.ID {
		t.Fatalf("project: %+v", p)
	}
	var list struct{ Data []app.MediaProject }
	e.do(nil, "GET", "/v1/media-projects?media="+a.ID, e.key, nil, 200, &list)
	if len(list.Data) != 1 || list.Data[0].ID != p.ID {
		t.Fatalf("by media: %+v", list)
	}
	e.do(nil, "GET", "/v1/media-projects?media=med_other", e.key, nil, 200, &list)
	if len(list.Data) != 0 {
		t.Fatalf("other media: %+v", list)
	}

	e.sendFile("PUT", "/v1/media-projects/"+p.ID, e.key, map[string]string{"name": "Poster 2", "source_ids": a.ID, "output_id": a.ID}, gpexFile("v2"), 200, &p)
	if p.Name != "Poster 2" || p.OutputID != a.ID {
		t.Fatalf("saved again: %+v", p)
	}
	req, _ := http.NewRequest("GET", e.srv.URL+"/v1/media-projects/"+p.ID+"/file", nil)
	req.Header.Set("Authorization", "Bearer "+e.key)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !bytes.Equal(body, gpexFile("v2")) {
		t.Fatalf("file: %d %q", res.StatusCode, body)
	}

	other := app.Principal{WorkspaceID: "ws_projects_other", Kind: app.PrincipalAPIKey, ID: "key_o", Name: "o", Scopes: app.AdminKeyScopes}
	if _, err := e.svc.GetMediaProject(ctx, other, p.ID); err == nil {
		t.Fatal("another workspace read a project")
	}
	if err := e.svc.DeleteMediaProject(ctx, other, p.ID); err == nil {
		t.Fatal("another workspace deleted a project")
	}

	e.do(nil, "DELETE", "/v1/media-projects/"+p.ID, e.key, nil, 403, nil) // posts:delete
	e.do(nil, "DELETE", "/v1/media-projects/"+p.ID, e.admin, nil, 204, nil)
	e.do(nil, "GET", "/v1/media-projects/"+p.ID, e.key, nil, 404, nil)
}

// fakeOpenAI answers like OpenAI's image API: 401 for a wrong key, 404 for a model the key cannot use, a
// picture otherwise; "slow" in a prompt makes it hang (timeouts), "refuse" makes it refuse the prompt.
func fakeOpenAI(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	pic := base64.StdEncoding.EncodeToString(realPNG(t, 8, 8))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-test-good-key-0123456789" {
			w.WriteHeader(401)
			_, _ = io.WriteString(w, `{"error":{"message":"Incorrect API key"}}`)
			return
		}
		switch {
		case r.Method == "GET" && r.URL.Path == "/models/gpt-image-1", r.Method == "GET" && r.URL.Path == "/models/dall-e-3":
			_, _ = io.WriteString(w, `{"id":"x"}`)
		case r.Method == "GET":
			w.WriteHeader(404)
		case r.URL.Path == "/images/generations" || r.URL.Path == "/images/edits":
			calls.Add(1)
			var prompt string
			if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				var in struct{ Prompt, Model string }
				_ = json.NewDecoder(r.Body).Decode(&in)
				prompt = in.Prompt
			} else {
				_ = r.ParseMultipartForm(32 << 20)
				prompt = r.FormValue("prompt")
				if _, _, err := r.FormFile("image"); err != nil {
					w.WriteHeader(400)
					return
				}
			}
			switch {
			case strings.Contains(prompt, "slow"):
				select {
				case <-r.Context().Done():
				case <-time.After(5 * time.Second):
				}
				return
			case strings.Contains(prompt, "refuse"):
				w.WriteHeader(400)
				_, _ = io.WriteString(w, `{"error":{"message":"Your request was rejected by the safety system."}}`)
				return
			}
			_, _ = io.WriteString(w, `{"data":[{"b64_json":"`+pic+`"}]}`)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// aiCall posts a generate or edit request as the editor does (multipart) and answers status and body.
func (e *env) aiCall(op, key string, fields map[string]string, image []byte) (int, []byte) {
	e.t.Helper()
	var body *bytes.Buffer
	var ct string
	if image != nil {
		body, ct = multipartImage(e.t, fields, image)
	} else {
		body, ct = multipartBody(e.t, fields, "unused.txt", nil)
	}
	req, _ := http.NewRequest("POST", e.srv.URL+"/v1/ai/images/"+op, body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("Authorization", "Bearer "+key)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, raw
}

func multipartImage(t testing.TB, fields map[string]string, image []byte) (*bytes.Buffer, string) {
	t.Helper()
	body, ct := multipartBody(t, fields, "image.png", image)
	// multipartBody names the file "file": the AI endpoint reads "image".
	return bytes.NewBufferString(strings.Replace(body.String(), `name="file"`, `name="image"`, 1)), ct
}

// The editor's AI tools on the workspace's own key: off until an admin adds a key the provider accepts, the
// key stored sealed and shown by its last four characters only, capabilities by model, generate and edit
// through the server, the provider's refusals and timeouts as plain errors, and a per-workspace rate limit.
func TestAIImages(t *testing.T) {
	e := newEnv(t)
	e.paid()
	fake, calls := fakeOpenAI(t)
	e.svc.Cfg.AI.OpenAIBaseURL = fake.URL

	var st app.AIStatus
	e.do(nil, "GET", "/v1/ai", e.key, nil, 200, &st)
	if st.Configured || st.CanManage || st.Reason == "" {
		t.Fatalf("not set up: %+v", st)
	}
	if code, _ := e.aiCall("generate", e.key, map[string]string{"prompt": "a cat"}, nil); code != 409 {
		t.Fatalf("generate without a key: %d", code)
	}

	// Only an admin sets the key; the provider must accept it and let it use the model.
	e.do(nil, "PUT", "/v1/ai", e.key, map[string]any{"api_key": "sk-test-good-key-0123456789"}, 403, nil)
	e.do(nil, "PUT", "/v1/ai", e.admin, map[string]any{"api_key": "sk-test-wrong-key-0123456789"}, 422, nil)
	e.do(nil, "PUT", "/v1/ai", e.admin, map[string]any{"api_key": "sk-test-good-key-0123456789", "model": "gpt-image-1-mini"}, 422, nil)
	e.do(nil, "PUT", "/v1/ai", e.admin, map[string]any{"api_key": "sk-test-good-key-0123456789", "model": "gpt-4o"}, 422, nil)
	e.do(nil, "PUT", "/v1/ai", e.admin, map[string]any{"api_key": "sk-test-good-key-0123456789"}, 200, &st)
	if !st.Configured || st.Model != "gpt-image-1" || st.KeyHint != "...6789" || !st.Capabilities.Expand || !st.CanManage {
		t.Fatalf("set up: %+v", st)
	}
	row, err := e.svc.Q.GetWorkspaceAI(context.Background(), workspaceID(e))
	if err != nil || strings.Contains(row.Credentials, "good-key") {
		t.Fatalf("the key is stored sealed: %v %q", err, row.Credentials)
	}

	// Generate and edit answer a picture; the editor's prompt mistakes and the provider's refusals are errors.
	code, pic := e.aiCall("generate", e.key, map[string]string{"prompt": "a cat", "size": "1024x1024"}, nil)
	if code != 200 || !bytes.HasPrefix(pic, []byte("\x89PNG")) {
		t.Fatalf("generate: %d %q", code, pic[:min(len(pic), 80)])
	}
	if code, _ := e.aiCall("edit", e.key, map[string]string{"prompt": "make it blue"}, realPNG(t, 8, 8)); code != 200 {
		t.Fatalf("edit: %d", code)
	}
	if code, _ := e.aiCall("generate", e.key, map[string]string{"prompt": "a cat", "size": "999x999"}, nil); code != 422 {
		t.Fatalf("size: %d", code)
	}
	if code, _ := e.aiCall("edit", e.key, map[string]string{"prompt": "no picture"}, nil); code != 422 {
		t.Fatalf("edit without a picture: %d", code)
	}
	if code, body := e.aiCall("generate", e.key, map[string]string{"prompt": "refuse this"}, nil); code != 422 || !strings.Contains(string(body), "safety system") {
		t.Fatalf("refused: %d %s", code, body)
	}
	if code, _ := e.aiCall("upscale", e.key, map[string]string{"prompt": "x"}, nil); code != 404 {
		t.Fatalf("unknown op: %d", code)
	}
	viewer := e.keyWith(app.ScopeMediaRead)
	if code, _ := e.aiCall("generate", viewer, map[string]string{"prompt": "a cat"}, nil); code != 403 {
		t.Fatalf("media:write needed: %d", code)
	}

	// A model that only generates: editing is refused before any call to the provider.
	e.do(nil, "PUT", "/v1/ai", e.admin, map[string]any{"model": "dall-e-3"}, 200, &st)
	if st.Capabilities.Edit || !st.Capabilities.Generate || st.KeyHint != "...6789" {
		t.Fatalf("dall-e-3 keeps the key: %+v", st)
	}
	before := calls.Load()
	if code, _ := e.aiCall("edit", e.key, map[string]string{"prompt": "x", "size": "1024x1024"}, realPNG(t, 8, 8)); code != 409 || calls.Load() != before {
		t.Fatalf("unsupported edit: %d, calls %d -> %d", code, before, calls.Load())
	}
	e.do(nil, "PUT", "/v1/ai", e.admin, map[string]any{"model": "gpt-image-1"}, 200, nil)

	// Another workspace sees no key.
	other := app.Principal{WorkspaceID: "ws_ai_other", Kind: app.PrincipalAPIKey, ID: "key_o", Name: "o", Scopes: app.AdminKeyScopes}
	if s, err := e.svc.GetAIStatus(context.Background(), other); err != nil || s.Configured {
		t.Fatalf("other workspace: %+v %v", s, err)
	}
	if _, err := e.svc.AIImage(context.Background(), other, "generate", app.AIImageRequest{Prompt: "x"}); err == nil {
		t.Fatal("another workspace used the key")
	}

	// A provider that hangs: the caller's deadline ends the call.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	p := app.Principal{WorkspaceID: workspaceID(e), Kind: app.PrincipalAPIKey, ID: "key_t", Name: "t", Scopes: app.AdminKeyScopes}
	start := time.Now()
	if _, err := e.svc.AIImage(ctx, p, "generate", app.AIImageRequest{Prompt: "slow please"}); err == nil || time.Since(start) > 3*time.Second {
		t.Fatalf("timeout: %v after %s", err, time.Since(start))
	}

	// The per-workspace limit.
	limited := 0
	for range 40 {
		if code, _ := e.aiCall("generate", e.key, map[string]string{"prompt": "a cat"}, nil); code == 429 {
			limited++
		}
	}
	if limited == 0 {
		t.Fatal("no rate limit")
	}

	// Forgetting the key turns AI off again.
	e.do(nil, "DELETE", "/v1/ai", e.admin, nil, 204, nil)
	e.do(nil, "GET", "/v1/ai", e.key, nil, 200, &st)
	if st.Configured {
		t.Fatalf("forgotten: %+v", st)
	}
}
