package httpapi_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// The AI providers are fakes that answer like their documented APIs (fal.ai's queue API, Higgsfield's request
// API); nothing billable is ever called.

const (
	testFalKey = "fal-test-key-0123456789"
	testHFKey  = "hf-test-id:hf-test-secret"
)

// fakeMP4 starts like an MP4 file (enough for content sniffing).
var fakeMP4 = append([]byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm', 0, 0, 2, 0, 'i', 's', 'o', 'm', 'i', 's', 'o', '2'}, make([]byte, 64)...)

// fakeAI is one fake provider. submit decides a submit's answer: 0 = accept it, anything else is the HTTP status
// to answer (503: not taken, 500: unknown, 422: refused).
type fakeAI struct {
	srv     *httptest.Server
	mu      sync.Mutex
	submit  int
	slow    bool // requests stay in progress
	fail    bool // requests end failed
	submits []string
	bodies  []map[string]any
	idem    []string
	cancels atomic.Int32
	forgets atomic.Int32
	uploads atomic.Int32
	png     []byte
}

func (f *fakeAI) set(fn func(f *fakeAI)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *fakeAI) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.submits)
}

func (f *fakeAI) last() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.bodies) == 0 {
		return nil
	}
	return f.bodies[len(f.bodies)-1]
}

func (f *fakeAI) recordSubmit(r *http.Request) (int, bool, bool) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.submits = append(f.submits, r.URL.Path)
	f.bodies = append(f.bodies, body)
	f.idem = append(f.idem, r.Header.Get("Idempotency-Key"))
	return f.submit, f.slow, f.fail
}

// newFakeFal: POST /<model> queues, GET <app>/requests/<id>/status says COMPLETED (or IN_PROGRESS when slow),
// GET <app>/requests/<id> answers the result, PUT .../cancel drops it, /files/* are the CDN.
func newFakeFal(t *testing.T) *fakeAI {
	t.Helper()
	f := &fakeAI{png: realPNG(t, 64, 48)}
	var n atomic.Int32
	state := map[string]string{} // request id -> "slow" | "fail" | "video" | ""
	var smu sync.Mutex
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/files/"):
			if strings.HasSuffix(r.URL.Path, ".mp4") {
				_, _ = w.Write(fakeMP4)
				return
			}
			_, _ = w.Write(f.png)
			return
		case r.Header.Get("Authorization") != "Key "+testFalKey:
			w.WriteHeader(401)
			_, _ = io.WriteString(w, `{"detail":"Invalid key"}`)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/chat/completions") {
			fakeAssistant(w, r)
			return
		}
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if i := indexOf(parts, "requests"); i >= 0 && i+1 < len(parts) {
			id := parts[i+1]
			smu.Lock()
			st, ok := state[id]
			smu.Unlock()
			if !ok {
				w.WriteHeader(404)
				return
			}
			switch {
			case r.Method == "PUT" && strings.HasSuffix(r.URL.Path, "/cancel"):
				f.cancels.Add(1)
				_, _ = io.WriteString(w, `{"status":"OK"}`)
			case strings.HasSuffix(r.URL.Path, "/status"):
				if st == "slow" {
					_, _ = io.WriteString(w, `{"status":"IN_PROGRESS"}`)
					return
				}
				_, _ = io.WriteString(w, `{"status":"COMPLETED"}`)
			case st == "fail":
				w.WriteHeader(500)
				_, _ = io.WriteString(w, `{"detail":"The model ran out of memory"}`)
			case st == "video":
				fmt.Fprintf(w, `{"video":{"url":%q}}`, f.srv.URL+"/files/out.mp4")

			default:
				fmt.Fprintf(w, `{"images":[{"url":%q,"width":64,"height":48}],"has_nsfw_concepts":[false]}`, f.srv.URL+"/files/out.png")
			}
			return
		}
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		code, slow, fail := f.recordSubmit(r)
		if code != 0 {
			w.WriteHeader(code)
			_, _ = io.WriteString(w, `{"detail":[{"msg":"prompt was flagged"}]}`)
			return
		}
		id := fmt.Sprintf("req-%d", n.Add(1))
		st := ""
		switch {
		case slow:
			st = "slow"
		case fail:
			st = "fail"
		case strings.Contains(r.URL.Path, "video"):
			st = "video"
		}
		smu.Lock()
		state[id] = st
		smu.Unlock()
		app := "/" + parts[0] + "/" + parts[1]
		base := f.srv.URL + app + "/requests/" + id
		fmt.Fprintf(w, `{"request_id":%q,"status_url":%q,"response_url":%q,"cancel_url":%q,"queue_position":0}`, id, base+"/status", base, base+"/cancel")
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// newFakeHiggsfield: POST /<model> queues (Idempotency-Key: one request per key), GET /requests/<id>/status
// answers completed with images or video, POST .../cancel, DELETE /requests/<id>, and the upload URL flow.
func newFakeHiggsfield(t *testing.T) *fakeAI {
	t.Helper()
	f := &fakeAI{png: realPNG(t, 64, 64)}
	byKey := map[string]string{}
	kinds := map[string]string{}
	var smu sync.Mutex
	var n atomic.Int32
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/cdn/"):
			if strings.HasSuffix(r.URL.Path, ".mp4") {
				_, _ = w.Write(fakeMP4)
				return
			}
			_, _ = w.Write(f.png)
			return
		case r.URL.Path == "/upload/in" && r.Method == "PUT":
			if r.Header.Get("Authorization") != "" {
				w.WriteHeader(400) // credentials must not go to storage
				return
			}
			f.uploads.Add(1)
			return
		case r.Header.Get("Authorization") != "Key "+testHFKey:
			w.WriteHeader(401)
			_, _ = io.WriteString(w, `{"detail":"Missing or invalid credentials"}`)
			return
		case r.URL.Path == "/files/generate-upload-url":
			fmt.Fprintf(w, `{"upload_url":%q,"upload_headers":{"Content-Type":"image/png"},"public_url":%q}`, f.srv.URL+"/upload/in", f.srv.URL+"/cdn/in.png")
			return
		case strings.HasPrefix(r.URL.Path, "/requests/"):
			id := strings.Split(strings.TrimPrefix(r.URL.Path, "/requests/"), "/")[0]
			smu.Lock()
			kind := kinds[id]
			smu.Unlock()
			switch {
			case r.Method == "DELETE":
				f.forgets.Add(1)
			case strings.HasSuffix(r.URL.Path, "/cancel"):
				f.cancels.Add(1)
				w.WriteHeader(202)
			case kind == "video":
				fmt.Fprintf(w, `{"status":"completed","request_id":%q,"video":{"url":%q}}`, id, f.srv.URL+"/cdn/out.mp4")
			default:
				fmt.Fprintf(w, `{"status":"completed","request_id":%q,"images":[{"url":%q}]}`, id, f.srv.URL+"/cdn/out.png")
			}
			return
		}
		code, _, _ := f.recordSubmit(r)
		if code != 0 {
			w.WriteHeader(code)
			_, _ = io.WriteString(w, `{"detail":"no"}`)
			return
		}
		key := r.Header.Get("Idempotency-Key")
		smu.Lock()
		id, ok := byKey[key]
		if !ok {
			id = fmt.Sprintf("hf-%d", n.Add(1))
			byKey[key] = id
			if strings.Contains(r.URL.Path, "video") {
				kinds[id] = "video"
			}
		}
		smu.Unlock()
		fmt.Fprintf(w, `{"status":"queued","request_id":%q,"status_url":%q,"cancel_url":%q}`, id,
			f.srv.URL+"/requests/"+id+"/status", f.srv.URL+"/requests/"+id+"/cancel")
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func indexOf(list []string, v string) int {
	for i, x := range list {
		if x == v {
			return i
		}
	}
	return -1
}

// aiSetup points the service at the fakes (nil: that provider is not configured).
func (e *env) aiSetup(fal, hf *fakeAI) {
	e.svc.Cfg.AI = app.AIConfig{PollInterval: 5 * time.Millisecond}
	if fal != nil {
		e.svc.Cfg.AI.FalKey, e.svc.Cfg.AI.FalBaseURL = testFalKey, fal.srv.URL
	}
	if hf != nil {
		e.svc.Cfg.AI.HiggsfieldKey, e.svc.Cfg.AI.HiggsfieldBaseURL = testHFKey, hf.srv.URL
	}
}

// aiJob sends POST /v1/ai/jobs as the editor does (fields first, then the files) and answers status and body.
func (e *env) aiJob(key string, fields map[string]string, image, mask []byte, headers ...string) (int, app.AIJob, string) {
	e.t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = w.WriteField(k, v)
	}
	for name, data := range map[string][]byte{"image": image, "mask": mask} {
		if data != nil {
			fw, _ := w.CreateFormFile(name, name+".png")
			_, _ = fw.Write(data)
		}
	}
	_ = w.Close()
	req, _ := http.NewRequest("POST", e.srv.URL+"/v1/ai/jobs", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+key)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var j app.AIJob
	_ = json.Unmarshal(raw, &j)
	return res.StatusCode, j, string(raw)
}

// runAI runs the queued jobs and answers the job as it ended.
func (e *env) runAI(id string) app.AIJob {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	e.svc.RunDueJobsOnce(ctx)
	var j app.AIJob
	e.do(nil, "GET", "/v1/ai/jobs/"+id, e.key, nil, 200, &j)
	return j
}

func opIDs(st app.AIStatus) []string {
	var out []string
	for _, o := range st.Operations {
		out = append(out, o.ID)
	}
	return out
}

// No key: the AI tools are off (the editor hides them) and a request is refused before anything runs.
func TestAIOffWithoutKeys(t *testing.T) {
	e := newEnv(t)
	e.paid()
	e.aiSetup(nil, nil)
	var st app.AIStatus
	e.do(nil, "GET", "/v1/ai", e.key, nil, 200, &st)
	if st.Configured || len(st.Operations) != 0 || len(st.Providers) != 0 {
		t.Fatalf("no key: %+v", st)
	}
	if code, _, body := e.aiJob(e.key, map[string]string{"operation": "generate", "prompt": "a cat"}, nil, nil); code != 409 {
		t.Fatalf("generate without a key: %d %s", code, body)
	}
}

// fal.ai alone: every image operation and both video ones, no fallback; a picture is generated through the
// queue, previewed, kept in the library once (accepting again answers the same file) and its temporary file goes.
func TestAIFalOnly(t *testing.T) {
	e := newEnv(t)
	e.paid()
	fal := newFakeFal(t)
	e.aiSetup(fal, nil)

	var st app.AIStatus
	e.do(nil, "GET", "/v1/ai", e.key, nil, 200, &st)
	if !st.Configured || len(st.Operations) != 12 || st.Providers[0] != "fal" || st.Usage.LimitUSD != nil {
		t.Fatalf("fal only: %+v", st)
	}
	for _, o := range st.Operations {
		if o.Provider != "fal" || o.Fallback != "" || o.Model == "" {
			t.Fatalf("operation %+v", o)
		}
	}
	raw, _ := json.Marshal(st)
	if strings.Contains(string(raw), testFalKey) {
		t.Fatal("the status carries the key")
	}

	code, job, body := e.aiJob(e.key, map[string]string{"operation": "generate", "prompt": "a cat", "width": "1080", "height": "1350"}, nil, nil)
	if code != 202 || job.Status != "queued" {
		t.Fatalf("queue: %d %s", code, body)
	}
	job = e.runAI(job.ID)
	if job.Status != "succeeded" || job.Provider != "fal" || job.Model != "fal-ai/flux/schnell" || job.Result == nil || job.Result.Width != 64 || job.CostUSD <= 0 {
		t.Fatalf("done: %+v", job)
	}
	sent := fal.last()
	if size, _ := sent["image_size"].(map[string]any); size["width"] != float64(1072) || size["height"] != float64(1344) || sent["prompt"] != "a cat" {
		t.Fatalf("fal input: %v", sent)
	}

	req, _ := http.NewRequest("GET", e.srv.URL+"/v1/ai/jobs/"+job.ID+"/result", nil)
	req.Header.Set("Authorization", "Bearer "+e.key)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	pic, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !bytes.Equal(pic, fal.png) || res.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("result: %d %v", res.StatusCode, res.Header)
	}

	var m, again app.Media
	e.do(nil, "POST", "/v1/ai/jobs/"+job.ID+"/accept", e.key, nil, 201, &m)
	if m.ID == "" || m.MimeType != "image/png" || !strings.HasPrefix(m.OriginalName, "AI generate a cat") {
		t.Fatalf("kept: %+v", m)
	}
	e.do(nil, "POST", "/v1/ai/jobs/"+job.ID+"/accept", e.key, nil, 201, &again)
	if again.ID != m.ID {
		t.Fatalf("accepted twice: %s %s", m.ID, again.ID)
	}
	e.do(nil, "GET", "/v1/ai/jobs/"+job.ID+"/result", e.key, nil, 404, nil)
	e.do(nil, "GET", "/v1/ai/jobs/"+job.ID, e.key, nil, 200, &job)
	if job.MediaID != m.ID || job.Result != nil {
		t.Fatalf("after accept: %+v", job)
	}

	// An edit sends the picture inline; a video arrives as a video and is kept as one.
	code, job, body = e.aiJob(e.key, map[string]string{"operation": "edit", "prompt": "make it blue"}, realPNG(t, 32, 32), nil)
	if code != 202 {
		t.Fatalf("edit: %d %s", code, body)
	}
	if job = e.runAI(job.ID); job.Status != "succeeded" || job.Model != "fal-ai/flux-pro/kontext" {
		t.Fatalf("edit: %+v", job)
	}
	if u, _ := fal.last()["image_url"].(string); !strings.HasPrefix(u, "data:image/png;base64,") {
		t.Fatalf("image input: %.40s", u)
	}
	code, job, body = e.aiJob(e.key, map[string]string{"operation": "image_to_video", "prompt": "waves move", "duration": "10"}, realPNG(t, 32, 32), nil)
	if code != 202 || job.Kind != "video" {
		t.Fatalf("video: %d %s", code, body)
	}
	if job = e.runAI(job.ID); job.Status != "succeeded" || job.Result == nil || job.Result.MimeType != "video/mp4" || job.CostUSD != 0.7 {
		t.Fatalf("video: %+v", job)
	}
	if fal.last()["duration"] != "10" {
		t.Fatalf("video input: %v", fal.last())
	}

	// A provider failure ends the job with its reason and is not retried (and costs nothing).
	fal.set(func(f *fakeAI) { f.fail = true })
	before := fal.count()
	_, job, _ = e.aiJob(e.key, map[string]string{"operation": "generate", "prompt": "a dog"}, nil, nil)
	if job = e.runAI(job.ID); job.Status != "failed" || job.Error == nil || job.Error.Code != "ai_failed" || !strings.Contains(job.Error.Message, "out of memory") || job.CostUSD != 0 {
		t.Fatalf("failed: %+v", job)
	}
	if fal.count() != before+1 {
		t.Fatalf("a failed request was sent again: %d", fal.count()-before)
	}
}

// Higgsfield alone: only what its public API has (text-to-image and the Kling videos); the image editing
// operations are not offered. Its requests carry the job id as Idempotency-Key, an input picture is uploaded
// (without credentials) and the request is deleted at Higgsfield once the result is in.
func TestAIHiggsfieldOnly(t *testing.T) {
	e := newEnv(t)
	e.paid()
	hf := newFakeHiggsfield(t)
	e.aiSetup(nil, hf)

	var st app.AIStatus
	e.do(nil, "GET", "/v1/ai", e.key, nil, 200, &st)
	if got := strings.Join(opIDs(st), ","); got != "generate,text_to_video,image_to_video" {
		t.Fatalf("higgsfield operations: %s", got)
	}
	if code, _, _ := e.aiJob(e.key, map[string]string{"operation": "edit", "prompt": "x"}, realPNG(t, 32, 32), nil); code != 409 {
		t.Fatalf("edit on higgsfield: %d", code)
	}
	_, job, _ := e.aiJob(e.key, map[string]string{"operation": "generate", "prompt": "a cat", "width": "1920", "height": "1080"}, nil, nil)
	if job = e.runAI(job.ID); job.Status != "succeeded" || job.Provider != "higgsfield" || job.Model != "higgsfield-ai/soul/standard" {
		t.Fatalf("generate: %+v", job)
	}
	if hf.last()["aspect_ratio"] != "16:9" || hf.idem[0] != job.ID || hf.forgets.Load() != 1 {
		t.Fatalf("higgsfield request: %v %v forgets %d", hf.last(), hf.idem, hf.forgets.Load())
	}
	_, job, _ = e.aiJob(e.key, map[string]string{"operation": "image_to_video", "prompt": "waves"}, realPNG(t, 32, 32), nil)
	if job = e.runAI(job.ID); job.Status != "succeeded" || job.Result.MimeType != "video/mp4" || hf.uploads.Load() != 1 {
		t.Fatalf("video: %+v uploads %d", job, hf.uploads.Load())
	}
	if u, _ := hf.last()["image_url"].(string); u != hf.srv.URL+"/cdn/in.png" || hf.last()["duration"] != float64(5) {
		t.Fatalf("video input: %v", hf.last())
	}
}

// Both: fal.ai first. Higgsfield takes over only when fal.ai did not take the request, and only for operations
// it does the same way; when fal.ai may have started it (no answer) or refused the input, nothing is sent again.
func TestAIFallback(t *testing.T) {
	e := newEnv(t)
	e.paid()
	fal, hf := newFakeFal(t), newFakeHiggsfield(t)
	e.aiSetup(fal, hf)

	var st app.AIStatus
	e.do(nil, "GET", "/v1/ai", e.key, nil, 200, &st)
	for _, o := range st.Operations {
		want := ""
		if o.ID == "generate" || o.ID == "text_to_video" || o.ID == "image_to_video" {
			want = "higgsfield"
		}
		if o.Provider != "fal" || o.Fallback != want {
			t.Fatalf("operation %+v", o)
		}
	}

	// Healthy: fal.ai runs it, Higgsfield is not asked.
	_, job, _ := e.aiJob(e.key, map[string]string{"operation": "generate", "prompt": "one"}, nil, nil)
	if job = e.runAI(job.ID); job.Provider != "fal" || hf.count() != 0 {
		t.Fatalf("healthy: %+v", job)
	}

	// fal.ai is down (503): Higgsfield makes it, and the job says so.
	fal.set(func(f *fakeAI) { f.submit = 503 })
	_, job, _ = e.aiJob(e.key, map[string]string{"operation": "generate", "prompt": "two"}, nil, nil)
	if job = e.runAI(job.ID); job.Status != "succeeded" || job.Provider != "higgsfield" || job.FallbackFrom != "fal" || job.Model != "higgsfield-ai/soul/standard" {
		t.Fatalf("fallback: %+v", job)
	}

	// fal.ai refuses the server's key: the same.
	fal.set(func(f *fakeAI) { f.submit = 401 })
	_, job, _ = e.aiJob(e.key, map[string]string{"operation": "generate", "prompt": "three"}, nil, nil)
	if job = e.runAI(job.ID); job.Provider != "higgsfield" {
		t.Fatalf("key refused: %+v", job)
	}

	// An edit has no fallback: it fails as unavailable.
	_, job, _ = e.aiJob(e.key, map[string]string{"operation": "edit", "prompt": "blue"}, realPNG(t, 32, 32), nil)
	if job = e.runAI(job.ID); job.Status != "failed" || job.Error.Code != "ai_unavailable" || job.CostUSD != 0 {
		t.Fatalf("edit without fallback: %+v", job)
	}

	// A 9:16 video: Higgsfield's Kling makes 16:9 only, so it is not sent there.
	hfBefore := hf.count()
	_, job, _ = e.aiJob(e.key, map[string]string{"operation": "text_to_video", "prompt": "tall", "aspect_ratio": "9:16"}, nil, nil)
	if job = e.runAI(job.ID); job.Status != "failed" || hf.count() != hfBefore {
		t.Fatalf("no equal fallback: %+v", job)
	}

	// No answer from fal.ai (500): it may be running, so neither fal.ai nor Higgsfield gets it again.
	fal.set(func(f *fakeAI) { f.submit = 500 })
	falBefore, hfBefore := fal.count(), hf.count()
	_, job, _ = e.aiJob(e.key, map[string]string{"operation": "generate", "prompt": "four"}, nil, nil)
	if job = e.runAI(job.ID); job.Status != "failed" || job.Error.Code != "ai_status_unknown" || job.CostUSD == 0 {
		t.Fatalf("unknown: %+v", job)
	}
	if fal.count() != falBefore+1 || hf.count() != hfBefore {
		t.Fatalf("sent again after no answer: fal %d higgsfield %d", fal.count()-falBefore, hf.count()-hfBefore)
	}

	// fal.ai refuses the prompt (422): another provider would too; nothing else is asked.
	fal.set(func(f *fakeAI) { f.submit = 422 })
	_, job, _ = e.aiJob(e.key, map[string]string{"operation": "generate", "prompt": "five"}, nil, nil)
	if job = e.runAI(job.ID); job.Error == nil || job.Error.Code != "ai_refused" || !strings.Contains(job.Error.Message, "flagged") || hf.count() != hfBefore {
		t.Fatalf("refused: %+v", job)
	}

	// AI_MEDIA_PROVIDERS=higgsfield,fal: Higgsfield first, fal.ai only where Higgsfield has no model.
	fal.set(func(f *fakeAI) { f.submit = 0 })
	e.svc.Cfg.AI.MediaProviders = []string{"higgsfield", "fal"}
	e.do(nil, "GET", "/v1/ai", e.key, nil, 200, &st)
	if st.Providers[0] != "higgsfield" {
		t.Fatalf("order: %v", st.Providers)
	}
	falBefore = fal.count()
	_, job, _ = e.aiJob(e.key, map[string]string{"operation": "generate", "prompt": "six"}, nil, nil)
	if job = e.runAI(job.ID); job.Provider != "higgsfield" || job.FallbackFrom != "" || fal.count() != falBefore {
		t.Fatalf("higgsfield first: %+v", job)
	}
	_, job, _ = e.aiJob(e.key, map[string]string{"operation": "edit", "prompt": "blue"}, realPNG(t, 32, 32), nil)
	if job = e.runAI(job.ID); job.Provider != "fal" {
		t.Fatalf("an edit stays on fal.ai: %+v", job)
	}
	// Only Higgsfield: fal.ai's media models are not offered at all (the assistant still talks to its router).
	e.svc.Cfg.AI.MediaProviders = []string{"higgsfield"}
	e.do(nil, "GET", "/v1/ai", e.key, nil, 200, &st)
	for _, o := range st.Operations {
		if o.ID != "assistant" && (o.Provider != "higgsfield" || o.Fallback != "") {
			t.Fatalf("higgsfield only: %+v", o)
		}
	}
}

// Input checks, scopes and workspaces, duplicates, cancelling, the per-workspace limits.
func TestAIJobRules(t *testing.T) {
	e := newEnv(t)
	e.paid()
	fal := newFakeFal(t)
	e.aiSetup(fal, nil)
	pic := realPNG(t, 32, 32)

	for _, c := range []struct {
		fields      map[string]string
		image, mask []byte
		want        int
	}{
		{map[string]string{"operation": "dance"}, nil, nil, 422},
		{map[string]string{"operation": "generate"}, nil, nil, 422},                                            // no prompt
		{map[string]string{"operation": "generate", "prompt": "x", "width": "99999"}, nil, nil, 422},           // size
		{map[string]string{"operation": "generate", "prompt": "x", "width": "wide"}, nil, nil, 422},            // not a number
		{map[string]string{"operation": "edit", "prompt": "x"}, nil, nil, 422},                                 // no picture
		{map[string]string{"operation": "edit", "prompt": "x"}, []byte("not a picture at all"), nil, 422},      // not a picture
		{map[string]string{"operation": "erase"}, pic, nil, 422},                                               // no mask
		{map[string]string{"operation": "erase"}, pic, realPNG(t, 16, 16), 422},                                // mask size
		{map[string]string{"operation": "expand", "canvas_width": "32", "canvas_height": "32"}, pic, nil, 422}, // not larger
		{map[string]string{"operation": "image_to_video", "prompt": "x", "duration": "7"}, pic, nil, 422},
		{map[string]string{"operation": "text_to_video", "prompt": "x", "aspect_ratio": "4:3"}, nil, nil, 422},
	} {
		if code, _, body := e.aiJob(e.key, c.fields, c.image, c.mask); code != c.want {
			t.Fatalf("%v: %d %s", c.fields, code, body)
		}
	}
	if fal.count() != 0 {
		t.Fatalf("invalid requests reached the provider: %d", fal.count())
	}

	// Scopes: media:read sees jobs but cannot start, keep or discard one; another workspace sees nothing.
	viewer := e.keyWith(app.ScopeMediaRead)
	if code, _, _ := e.aiJob(viewer, map[string]string{"operation": "generate", "prompt": "x"}, nil, nil); code != 403 {
		t.Fatalf("media:read starts a job: %d", code)
	}
	_, job, _ := e.aiJob(e.key, map[string]string{"operation": "erase"}, pic, realPNG(t, 32, 32))
	e.do(nil, "GET", "/v1/ai/jobs/"+job.ID, viewer, nil, 200, nil)
	e.do(nil, "DELETE", "/v1/ai/jobs/"+job.ID, viewer, nil, 403, nil)
	other := newWorkspace(e, "Other")
	e.do(nil, "GET", "/v1/ai/jobs/"+job.ID, other.key, nil, 404, nil)
	e.do(nil, "DELETE", "/v1/ai/jobs/"+job.ID, other.key, nil, 404, nil)
	e.do(nil, "POST", "/v1/ai/jobs/"+job.ID+"/accept", other.key, nil, 404, nil)

	// The same request again while it waits (a double click) answers the same job; so does a repeated
	// Idempotency-Key, which may not carry another request.
	code, dup, _ := e.aiJob(e.key, map[string]string{"operation": "erase"}, pic, realPNG(t, 32, 32))
	if code != 200 || dup.ID != job.ID {
		t.Fatalf("double click: %d %s %s", code, dup.ID, job.ID)
	}
	_, k1, _ := e.aiJob(e.key, map[string]string{"operation": "generate", "prompt": "keyed"}, nil, nil, "Idempotency-Key", "k-1")
	e.do(nil, "DELETE", "/v1/ai/jobs/"+k1.ID, e.key, nil, 204, nil)
	if code, k2, _ := e.aiJob(e.key, map[string]string{"operation": "generate", "prompt": "keyed"}, nil, nil, "Idempotency-Key", "k-1"); code != 200 || k2.ID != k1.ID || k2.Status != "canceled" {
		t.Fatalf("idempotent replay: %d %+v", code, k2)
	}
	if code, _, _ := e.aiJob(e.key, map[string]string{"operation": "generate", "prompt": "other"}, nil, nil, "Idempotency-Key", "k-1"); code != 422 {
		t.Fatalf("key reused for another request: %d", code)
	}

	// Discarding a queued job cancels it before it reaches the provider: it costs nothing and never runs.
	e.do(nil, "DELETE", "/v1/ai/jobs/"+job.ID, e.key, nil, 204, nil)
	if job = e.runAI(job.ID); job.Status != "canceled" || job.CostUSD != 0 || fal.count() != 0 {
		t.Fatalf("cancelled: %+v, submits %d", job, fal.count())
	}
	e.do(nil, "POST", "/v1/ai/jobs/"+job.ID+"/accept", e.key, nil, 409, nil)

	// Only a few at once per workspace.
	for i := range 4 {
		if code, _, body := e.aiJob(e.key, map[string]string{"operation": "generate", "prompt": fmt.Sprint("live ", i)}, nil, nil); code != 202 {
			t.Fatalf("live %d: %d %s", i, code, body)
		}
	}
	if code, _, _ := e.aiJob(e.key, map[string]string{"operation": "generate", "prompt": "one too many"}, nil, nil); code != 429 {
		t.Fatalf("live limit: %d", code)
	}
	e.runAI(job.ID)

	// The monthly limit: a request that would pass it is refused with 402.
	// A plan without AI this month (every plan, whichever the workspace has).
	saved := append([]app.Plan(nil), app.Plans...)
	for i := range app.Plans {
		app.Plans[i].Limits.AIMonthlyCents = 0
	}
	t.Cleanup(func() { copy(app.Plans, saved) })
	if code, _, body := e.aiJob(e.key, map[string]string{"operation": "generate", "prompt": "over"}, nil, nil); code != 402 || !strings.Contains(body, "ai_limit_reached") {
		t.Fatalf("monthly limit: %d %s", code, body)
	}
	var st app.AIStatus
	e.do(nil, "GET", "/v1/ai", e.key, nil, 200, &st)
	if st.Usage.UsedUSD <= 0 || st.Usage.LimitUSD == nil || *st.Usage.LimitUSD != 0 {
		t.Fatalf("usage: %+v", st.Usage)
	}
	copy(app.Plans, saved)

	// AI_OPERATIONS turns operations off.
	e.svc.Cfg.AI.Operations = []string{"generate"}
	e.do(nil, "GET", "/v1/ai", e.key, nil, 200, &st)
	if got := strings.Join(opIDs(st), ","); got != "generate" {
		t.Fatalf("operations: %s", got)
	}
	if code, _, _ := e.aiJob(e.key, map[string]string{"operation": "upscale"}, pic, nil); code != 409 {
		t.Fatalf("turned off: %d", code)
	}
}

// Discarding a running job asks the provider to drop it; the worker stops polling and keeps nothing.
func TestAICancelRunning(t *testing.T) {
	e := newEnv(t)
	e.paid()
	fal := newFakeFal(t)
	e.aiSetup(fal, nil)
	fal.set(func(f *fakeAI) { f.slow = true })
	_, job, _ := e.aiJob(e.key, map[string]string{"operation": "generate", "prompt": "slow"}, nil, nil)

	done := make(chan struct{})
	go func() {
		defer close(done)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		e.svc.RunDueJobsOnce(ctx)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for fal.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond) // the submit is recorded
	e.do(nil, "DELETE", "/v1/ai/jobs/"+job.ID, e.key, nil, 204, nil)
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("the worker kept polling a cancelled job")
	}
	e.do(nil, "GET", "/v1/ai/jobs/"+job.ID, e.key, nil, 200, &job)
	if job.Status != "canceled" || fal.cancels.Load() != 1 || job.CostUSD != 0 || job.Result != nil {
		t.Fatalf("cancelled while running: %+v cancels %d", job, fal.cancels.Load())
	}
}

// fakeAssistant plays the router's chat completions for the assistant (streamed): asked by a person it looks up
// the collections; with them it creates a draft in the first one; once that ran it says so.
func fakeAssistant(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Messages []struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"messages"`
		Tools []json.RawMessage `json:"tools"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	w.Header().Set("Content-Type", "text/event-stream")
	chunk := func(v string) { fmt.Fprintf(w, "data: %s\n\n", v) }
	call := func(id, name, args string) {
		raw, _ := json.Marshal(args)
		chunk(fmt.Sprintf(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":%q,"function":{"name":%q,"arguments":%s}}]}}]}`, id, name, raw))
	}
	last := body.Messages[len(body.Messages)-1]
	prev := ""
	for _, m := range body.Messages {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			prev = m.ToolCalls[0].Function.Name
		}
	}
	switch {
	case len(body.Tools) == 0:
		chunk(`{"error":{"message":"no tools sent"}}`)
	case last.Role == "user":
		chunk(`{"choices":[{"delta":{"content":"Let me look. "}}]}`)
		call("c1", "list_collections", "{}")
	case prev == "list_collections":
		var out struct {
			Collections []struct {
				ID string `json:"id"`
			} `json:"collections"`
		}
		_ = json.Unmarshal([]byte(last.Content), &out)
		call("c2", "create_post", fmt.Sprintf(`{"collection":%q,"content":"Hello from the assistant"}`, out.Collections[0].ID))
	case strings.HasPrefix(last.Content, "The person declined"):
		chunk(`{"choices":[{"delta":{"content":"Okay, I left it."}}]}`)
	default:
		chunk(`{"choices":[{"delta":{"content":"Done: "}}]}`)
		chunk(`{"choices":[{"delta":{"content":"the draft is ready."}}]}`)
	}
	chunk(`{"choices":[],"usage":{"prompt_tokens":1000,"completion_tokens":50,"cost":0.0004}}`)
	chunk("[DONE]")
}

// assistantTurn posts a turn and reads its events.
func (e *env) assistantTurn(msgs []app.AssistantMessage, decision *app.AssistantDecision) []app.AssistantEvent {
	e.t.Helper()
	return e.assistantRequest(app.AssistantRequest{Messages: msgs, Decision: decision})
}

func (e *env) assistantRequest(body app.AssistantRequest) []app.AssistantEvent {
	e.t.Helper()
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", e.srv.URL+"/v1/assistant/chat", strings.NewReader(string(raw)))
	req.Header.Set("Authorization", "Bearer "+e.key)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "text/event-stream" {
		b, _ := io.ReadAll(res.Body)
		e.t.Fatalf("assistant: %d %s", res.StatusCode, b)
	}
	var out []app.AssistantEvent
	sc := bufio.NewScanner(res.Body)
	for sc.Scan() {
		if d, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
			var ev app.AssistantEvent
			_ = json.Unmarshal([]byte(d), &ev)
			out = append(out, ev)
		}
	}
	return out
}

func TestAssistant(t *testing.T) {
	e := newEnv(t)
	e.paid()
	e.aiSetup(newFakeFal(t), nil)
	e.do(nil, "POST", "/v1/collections", e.key, map[string]any{"name": "Launch"}, 201, nil)

	// Asked, it reads at once (list_collections), then waits for the person before it creates a post.
	msgs := []app.AssistantMessage{{Role: "user", Content: "Write a draft about our launch"}}
	evs := e.assistantTurn(msgs, nil)
	var confirm *app.AssistantToolCall
	text := ""
	for _, ev := range evs {
		switch ev.Type {
		case "message":
			msgs = append(msgs, *ev.Message)
		case "confirm":
			confirm = ev.ToolCall
		case "text":
			text += ev.Text
		case "error":
			t.Fatalf("error: %+v", ev.Error)
		}
	}
	if confirm == nil || confirm.Name != "create_post" || text != "Let me look. " || len(msgs) != 4 {
		t.Fatalf("first turn: %+v", evs)
	}
	var posts struct{ Data []app.Post }
	e.do(nil, "GET", "/v1/posts", e.key, nil, 200, &posts)
	if len(posts.Data) != 0 {
		t.Fatal("created before it was allowed")
	}

	// Allowed: the post is created as the person, and the assistant says so.
	evs = e.assistantTurn(msgs, &app.AssistantDecision{ToolCallID: confirm.ID, Allow: true})
	text = ""
	for _, ev := range evs {
		if ev.Type == "text" {
			text += ev.Text
		}
		if ev.Type == "tool" && (!ev.OK || ev.Tool != "create_post") {
			t.Fatalf("tool: %+v", ev)
		}
	}
	if text != "Done: the draft is ready." || evs[len(evs)-1].Type != "done" {
		t.Fatalf("second turn: %+v", evs)
	}
	e.do(nil, "GET", "/v1/posts", e.key, nil, 200, &posts)
	if len(posts.Data) != 1 || posts.Data[0].Content != "Hello from the assistant" {
		t.Fatalf("posts: %+v", posts.Data)
	}

	// Declined: nothing is created. Every model call counts as AI usage.
	evs = e.assistantTurn(msgs, &app.AssistantDecision{ToolCallID: confirm.ID, Allow: false})
	if evs[len(evs)-1].Type != "done" {
		t.Fatalf("declined: %+v", evs)
	}
	var st app.AIStatus
	e.do(nil, "GET", "/v1/ai", e.key, nil, 200, &st)
	if st.Usage.UsedUSD < 0.0016 {
		t.Fatalf("usage: %+v", st.Usage)
	}
	// Without AI on the server there is no assistant.
	e.aiSetup(nil, nil)
	e.do(nil, "POST", "/v1/assistant/chat", e.key, map[string]any{"messages": msgs}, 409, nil)

	// Its own OpenAI-compatible endpoint (OpenAI, DigitalOcean, a local model) needs no fal.ai: a Bearer key and
	// the model it names.
	var auth, model, path string
	own := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body struct{ Model string }
		_ = json.Unmarshal(b, &body)
		auth, model, path = r.Header.Get("Authorization"), body.Model, r.URL.Path
		r.Body = io.NopCloser(bytes.NewReader(b))
		fakeAssistant(w, r)
	}))
	defer own.Close()
	e.svc.Cfg.AI = app.AIConfig{Assistant: app.AIEndpoint{BaseURL: own.URL + "/v1", APIKey: "sk-test", Model: "gpt-test",
		Prices: &[2]float64{0, 0}}}
	for _, ev := range e.assistantTurn([]app.AssistantMessage{{Role: "user", Content: "Write a draft about our launch"}}, nil) {
		if ev.Type == "error" {
			t.Fatalf("own endpoint: %+v", ev.Error)
		}
	}
	if auth != "Bearer sk-test" || model != "gpt-test" || path != "/v1/chat/completions" {
		t.Fatalf("own endpoint got %q %q %q", auth, model, path)
	}
	e.do(nil, "GET", "/v1/ai", e.key, nil, 200, &st)
	if len(st.Operations) != 1 || st.Operations[0].ID != "assistant" || st.Operations[0].Provider != "openai_compatible" {
		t.Fatalf("status with its own endpoint: %+v", st.Operations)
	}

	// The model refuses before answering (its own quota): the fallback answers the same call.
	asked := 0
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked++
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"error":{"message":"You exceeded your current quota","code":"insufficient_quota"}}`))
	}))
	defer empty.Close()
	e.svc.Cfg.AI = app.AIConfig{Assistant: app.AIEndpoint{BaseURL: empty.URL + "/v1", Model: "gpt-big"},
		AssistantFallback:     app.AIEndpoint{BaseURL: own.URL + "/v1", APIKey: "sk-cheap", Model: "gpt-cheap"},
		AssistantBrowserModel: "Qwen2.5-3B-Instruct-q4f16_1-MLC"}
	for _, ev := range e.assistantTurn([]app.AssistantMessage{{Role: "user", Content: "Write a draft about our launch"}}, nil) {
		if ev.Type == "error" {
			t.Fatalf("fallback: %+v", ev.Error)
		}
	}
	if auth != "Bearer sk-cheap" || model != "gpt-cheap" {
		t.Fatalf("fallback got %q %q", auth, model)
	}
	e.do(nil, "GET", "/v1/ai", e.key, nil, 200, &st)
	if op := st.Operations[0]; op.Model != "gpt-big" || op.Fallback != "openai_compatible" || st.AssistantBrowserModel != "Qwen2.5-3B-Instruct-q4f16_1-MLC" {
		t.Fatalf("status with a fallback: %+v %q", op, st.AssistantBrowserModel)
	}

	// The month's allowance ran out: the model isn't asked, the fallback answers; without one it is a 402.
	saved := append([]app.Plan(nil), app.Plans...)
	for i := range app.Plans {
		app.Plans[i].Limits.AIMonthlyCents = 0
	}
	t.Cleanup(func() { copy(app.Plans, saved) })
	asked, model = 0, ""
	for _, ev := range e.assistantTurn([]app.AssistantMessage{{Role: "user", Content: "Write a draft about our launch"}}, nil) {
		if ev.Type == "error" {
			t.Fatalf("over the allowance: %+v", ev.Error)
		}
	}
	if model != "gpt-cheap" || asked != 0 {
		t.Fatalf("over the allowance got %q, the model asked %d times", model, asked)
	}
	e.svc.Cfg.AI.AssistantFallback = app.AIEndpoint{}
	evs = e.assistantTurn([]app.AssistantMessage{{Role: "user", Content: "Write a draft about our launch"}}, nil)
	if last := evs[len(evs)-1]; last.Type != "error" || last.Error.Code != "ai_limit_reached" {
		t.Fatalf("over the allowance without a fallback: %+v", evs)
	}
}

// An agent (and the assistant) makes pictures and videos over MCP: generate_media, get_generated_media,
// keep_generated_media.
func TestMCPGenerateMedia(t *testing.T) {
	e := newEnv(t)
	e.paid()
	e.aiSetup(newFakeFal(t), newFakeHiggsfield(t))

	job := mcpResult(e, e.key, "generate_media", map[string]any{"kind": "image", "prompt": "a lighthouse at dusk", "aspect_ratio": "16:9"})
	if job["operation"] != "generate" || job["status"] != "queued" {
		t.Fatalf("image: %v", job)
	}
	e.runAI(job["id"].(string))
	got := mcpResult(e, e.key, "get_generated_media", map[string]any{"job_id": job["id"], "wait_seconds": 5})
	if got["status"] != "succeeded" {
		t.Fatalf("status: %v", got)
	}
	m := mcpResult(e, e.key, "keep_generated_media", map[string]any{"job_id": job["id"]})
	if id, _ := m["id"].(string); !strings.HasPrefix(id, "med_") {
		t.Fatalf("kept: %v", m)
	}

	// A video from that picture, and one from a prompt alone.
	v := mcpResult(e, e.key, "generate_media", map[string]any{"kind": "video", "prompt": "waves move", "media_id": m["id"]})
	if v["operation"] != "image_to_video" {
		t.Fatalf("video from a picture: %v", v)
	}
	if v = mcpResult(e, e.key, "generate_media", map[string]any{"kind": "video", "prompt": "a timelapse", "aspect_ratio": "9:16"}); v["operation"] != "text_to_video" {
		t.Fatalf("video: %v", v)
	}
	mcpErr(e, e.key, "generate_media", map[string]any{"kind": "song", "prompt": "x"}, "kind must be image or video")
	mcpErr(e, e.key, "generate_media", map[string]any{"kind": "image", "prompt": "x", "aspect_ratio": "3:1"}, "aspect_ratio")
}

// Chats are kept on the server per person: saved after each turn, listed newest first a page at a time, found by
// title, opened with their messages and the change still waiting, removed; nobody else sees them.
func TestAssistantChats(t *testing.T) {
	e := newEnv(t)
	e.paid()
	e.aiSetup(newFakeFal(t), nil)
	e.do(nil, "POST", "/v1/collections", e.key, map[string]any{"name": "Launch"}, 201, nil)

	e.assistantRequest(app.AssistantRequest{ChatID: "ach_first0001", Messages: []app.AssistantMessage{{Role: "user", Content: "Write a draft about our launch"}}})
	for i := range 3 {
		e.assistantRequest(app.AssistantRequest{ChatID: fmt.Sprintf("ach_other%04d", i), Messages: []app.AssistantMessage{{Role: "user", Content: fmt.Sprintf("hello %d", i)}}})
	}
	var page app.AssistantChatPage
	e.do(nil, "GET", "/v1/assistant/chats?limit=2", e.key, nil, 200, &page)
	if len(page.Data) != 2 || page.NextCursor == "" || page.Data[0].Messages != nil {
		t.Fatalf("first page %+v", page)
	}
	var rest app.AssistantChatPage
	e.do(nil, "GET", "/v1/assistant/chats?limit=2&cursor="+page.NextCursor, e.key, nil, 200, &rest)
	if len(rest.Data) != 2 || rest.NextCursor != "" || rest.Data[1].ID == page.Data[1].ID {
		t.Fatalf("second page %+v", rest)
	}
	var found app.AssistantChatPage
	e.do(nil, "GET", "/v1/assistant/chats?q=LAUNCH", e.key, nil, 200, &found)
	if len(found.Data) != 1 || found.Data[0].ID != "ach_first0001" || found.Data[0].Title != "Write a draft about our launch" {
		t.Fatalf("search %+v", found)
	}
	// The draft waits for Allow: the saved chat has it.
	var chat app.AssistantChat
	e.do(nil, "GET", "/v1/assistant/chats/ach_first0001", e.key, nil, 200, &chat)
	if len(chat.Messages) < 2 || chat.Pending == nil || chat.Pending.Name != "create_post" {
		t.Fatalf("chat %+v", chat)
	}

	// Another key is someone else: no list, no chat, and its id can't overwrite the first's.
	var theirs app.AssistantChatPage
	e.do(nil, "GET", "/v1/assistant/chats", e.admin, nil, 200, &theirs)
	if len(theirs.Data) != 0 {
		t.Fatalf("other principal sees %+v", theirs)
	}
	e.do(nil, "GET", "/v1/assistant/chats/ach_first0001", e.admin, nil, 404, nil)
	e.do(nil, "DELETE", "/v1/assistant/chats/ach_first0001", e.admin, nil, 404, nil)

	e.do(nil, "DELETE", "/v1/assistant/chats/ach_first0001", e.key, nil, 204, nil)
	e.do(nil, "GET", "/v1/assistant/chats/ach_first0001", e.key, nil, 404, nil)
	e.do(nil, "GET", "/v1/assistant/chats?cursor=nope!", e.key, nil, 422, nil)
	e.do(nil, "POST", "/v1/assistant/chat", e.key, map[string]any{"chat_id": "../x", "messages": []map[string]string{{"role": "user", "content": "hi"}}}, 422, nil)
}

// A screenshot added to a message reaches the model as a picture, named with its media id (so it can go on a
// post); a file that is not there is named as missing; the saved chat keeps the attachments.
func TestAssistantAttachments(t *testing.T) {
	e := newEnv(t)
	e.paid()
	var pic app.Media
	e.upload("/v1/media", e.key, nil, "screen.png", pngBytes, 201, &pic)

	var sent []map[string]any
	own := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body struct{ Messages []map[string]any }
		_ = json.Unmarshal(b, &body)
		sent = body.Messages
		r.Body = io.NopCloser(bytes.NewReader([]byte(`{"messages":[{"role":"user","content":"hi"}]}`)))
		fakeAssistant(w, r)
	}))
	defer own.Close()
	e.svc.Cfg.AI = app.AIConfig{Assistant: app.AIEndpoint{BaseURL: own.URL + "/v1", Model: "m", Prices: &[2]float64{0, 0}}}
	e.assistantRequest(app.AssistantRequest{ChatID: "ach_attach0001", Messages: []app.AssistantMessage{{Role: "user", Content: "What is on it?",
		Attachments: []app.AssistantAttachment{{MediaID: pic.ID}, {MediaID: "med_nope"}}}}})

	parts, _ := sent[len(sent)-1]["content"].([]any)
	if len(parts) != 2 {
		t.Fatalf("parts %v", sent[len(sent)-1])
	}
	text, _ := parts[0].(map[string]any)["text"].(string)
	img, _ := parts[1].(map[string]any)["image_url"].(map[string]any)
	if !strings.Contains(text, pic.ID) || !strings.Contains(text, "could not be opened") ||
		!strings.HasPrefix(img["url"].(string), "data:image/png;base64,") {
		t.Fatalf("text %q img %v", text, img)
	}
	var chat app.AssistantChat
	e.do(nil, "GET", "/v1/assistant/chats/ach_attach0001", e.key, nil, 200, &chat)
	if len(chat.Messages) == 0 || len(chat.Messages[0].Attachments) != 2 {
		t.Fatalf("saved %+v", chat.Messages)
	}
}

// The design editor's text tools: the text rewritten by the assistant's model, without tools, as plain text.
func TestAIText(t *testing.T) {
	e := newEnv(t)
	e.paid()
	var hadTools bool
	var asked string
	own := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Tools    any
			Messages []struct{ Content any }
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		hadTools, asked = body.Tools != nil, fmt.Sprint(body.Messages[len(body.Messages)-1].Content)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"\\\"Less is more.\\\"\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer own.Close()

	e.do(nil, "POST", "/v1/ai/text", e.key, map[string]any{"text": "Simplicity", "instruction": "Shorter"}, 409, nil)
	e.svc.Cfg.AI = app.AIConfig{Assistant: app.AIEndpoint{BaseURL: own.URL + "/v1", Model: "m", Prices: &[2]float64{0, 0}}}
	var out app.AIText
	e.do(nil, "POST", "/v1/ai/text", e.key, map[string]any{"text": "Simplicity is the ultimate sophistication.", "instruction": "Shorter"}, 200, &out)
	if out.Text != "Less is more." || hadTools || !strings.Contains(asked, "Shorter") || !strings.Contains(asked, "Simplicity") {
		t.Fatalf("%+v tools=%v asked=%q", out, hadTools, asked)
	}
	e.do(nil, "POST", "/v1/ai/text", e.key, map[string]any{"text": "", "instruction": "Shorter"}, 422, nil)
	e.do(nil, "POST", "/v1/ai/text", e.key, map[string]any{"text": "x", "instruction": ""}, 422, nil)
}
