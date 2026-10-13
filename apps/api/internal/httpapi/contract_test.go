package httpapi_test

// Contract tests keep the hand-written OpenAPI document (the source of truth
// for the dashboard and SDKs) honest:
//   - every route registered in the router is documented, and vice versa;
//   - every JSON response produced by ANY test in this package is validated
//     against the documented schema (2xx: the operation's response schema;
//     4xx/5xx: the shared ErrorBody envelope). Violations fail the run.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/santhosh-tekuri/jsonschema/v6"
	api "github.com/staqly/staqly/apps/api"
	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/httpapi"
	"github.com/staqly/staqly/apps/api/internal/store"
	"go.yaml.in/yaml/v3"
)

// Routes answered outside the chi router (root-level interceptors) or that
// are protocol endpoints rather than JSON APIs.
var notInRouter = map[string]bool{
	"GET /livez": true, "GET /readyz": true, "GET /metrics": true,
	"GET /v1/decide/{token}": true, "POST /v1/decide/{token}": true,
	"POST /v1/integrations/telegram/{channel_id}": true,
	"POST /v1/integrations/slack/interactions":    true,
	"POST /v1/integrations/discord/interactions":  true,
}

type contractSpec struct {
	doc      map[string]any
	compiler *jsonschema.Compiler
	mu       sync.Mutex
	cache    map[string]*jsonschema.Schema
}

var (
	spec       *contractSpec
	violations []string
	violMu     sync.Mutex
	seenOps    sync.Map
)

func normalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			t[k] = normalize(x)
		}
		return t
	case map[any]any:
		m := map[string]any{}
		for k, x := range t {
			m[fmt.Sprint(k)] = normalize(x)
		}
		return m
	case []any:
		for i, x := range t {
			t[i] = normalize(x)
		}
	}
	return v
}

func loadSpec() (*contractSpec, error) {
	var raw any
	if err := yaml.Unmarshal(api.OpenAPI, &raw); err != nil {
		return nil, err
	}
	b, err := json.Marshal(normalize(raw))
	if err != nil {
		return nil, err
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	if err := c.AddResource("https://staqly.ai/openapi.json", doc); err != nil {
		return nil, err
	}
	var plain map[string]any
	_ = json.Unmarshal(b, &plain)
	return &contractSpec{doc: plain, compiler: c, cache: map[string]*jsonschema.Schema{}}, nil
}

func ptrEscape(seg string) string {
	seg = strings.ReplaceAll(strings.ReplaceAll(seg, "~", "~0"), "/", "~1")
	return url.PathEscape(seg)
}

func (s *contractSpec) schema(pointer []string) (*jsonschema.Schema, error) {
	parts := make([]string, len(pointer))
	for i, p := range pointer {
		parts[i] = ptrEscape(p)
	}
	loc := "https://staqly.ai/openapi.json#/" + strings.Join(parts, "/")
	s.mu.Lock()
	defer s.mu.Unlock()
	if sch, ok := s.cache[loc]; ok {
		return sch, nil
	}
	sch, err := s.compiler.Compile(loc)
	if err == nil {
		s.cache[loc] = sch
	}
	return sch, err
}

func get(m any, keys ...string) any {
	for _, k := range keys {
		mm, ok := m.(map[string]any)
		if !ok {
			return nil
		}
		m = mm[k]
	}
	return m
}

// responsePointer resolves the JSON pointer of the application/json schema
// documented for (path, method, status), following a response $ref.
func (s *contractSpec) responsePointer(path, method, status string) ([]string, bool) {
	resp := get(s.doc, "paths", path, method, "responses", status)
	ptr := []string{"paths", path, method, "responses", status}
	if resp == nil {
		return nil, false
	}
	if ref, ok := get(resp, "$ref").(string); ok {
		ptr = strings.Split(strings.TrimPrefix(ref, "#/"), "/")
		resp = get(s.doc, ptr...)
	}
	if get(resp, "content", "application/json", "schema") == nil {
		return nil, true // documented without a JSON body
	}
	return append(ptr, "content", "application/json", "schema"), true
}

func violation(format string, a ...any) {
	violMu.Lock()
	violations = append(violations, fmt.Sprintf(format, a...))
	violMu.Unlock()
}

type recorder struct {
	http.ResponseWriter
	status int
	buf    bytes.Buffer
}

func (r *recorder) WriteHeader(c int) { r.status = c; r.ResponseWriter.WriteHeader(c) }
func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = 200
	}
	if r.buf.Len() < 4<<20 {
		r.buf.Write(b)
	}
	return r.ResponseWriter.Write(b)
}
func (r *recorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func init() {
	var err error
	if spec, err = loadSpec(); err != nil {
		panic("load openapi.yaml: " + err.Error())
	}
	httpapi.RegisterMiddleware(func(*httpapi.Server) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				rec := &recorder{ResponseWriter: w}
				next.ServeHTTP(rec, r)
				validateResponse(r, rec)
			})
		}
	})
}

func validateResponse(r *http.Request, rec *recorder) {
	rctx := chi.RouteContext(r.Context())
	if rctx == nil || rctx.RoutePattern() == "" || rctx.RoutePattern() == "/*" {
		return
	}
	path, method := rctx.RoutePattern(), strings.ToLower(r.Method)
	status := rec.status
	if status == 0 {
		status = 200
	}
	ct := rec.Header().Get("Content-Type")
	if path == "/mcp" || !strings.HasPrefix(ct, "application/json") || rec.buf.Len() == 0 {
		return
	}
	seenOps.Store(strings.ToUpper(method)+" "+path, true)
	var ptr []string
	if status >= 400 {
		ptr = []string{"components", "responses", "Error", "content", "application/json", "schema"}
	} else {
		p, documented := spec.responsePointer(path, method, strconv.Itoa(status))
		if !documented {
			violation("%s %s → %d is not documented", r.Method, path, status)
			return
		}
		if p == nil {
			return
		}
		ptr = p
	}
	sch, err := spec.schema(ptr)
	if err != nil {
		violation("%s %s → %d: cannot compile schema: %v", r.Method, path, status, err)
		return
	}
	body, err := jsonschema.UnmarshalJSON(bytes.NewReader(rec.buf.Bytes()))
	if err != nil {
		violation("%s %s → %d: invalid JSON: %v", r.Method, path, status, err)
		return
	}
	if err := sch.Validate(body); err != nil {
		msg := err.Error()
		if len(msg) > 600 {
			msg = msg[:600] + "…"
		}
		violation("%s %s → %d does not match the documented schema:\n%s", r.Method, path, status, msg)
	}
}

func TestMain(m *testing.M) {
	code := m.Run()
	violMu.Lock()
	defer violMu.Unlock()
	if len(violations) > 0 {
		seen := map[string]bool{}
		fmt.Fprintf(os.Stderr, "\nOpenAPI contract violations (apps/api/openapi.yaml vs real responses):\n")
		for _, v := range violations {
			if !seen[v] {
				seen[v] = true
				fmt.Fprintf(os.Stderr, "  - %s\n", v)
			}
		}
		code = 1
	}
	os.Exit(code)
}

// Every route in the router is documented and every documented operation
// exists (apart from the interceptor-served endpoints listed above).
func TestOpenAPIRoutesMatchRouter(t *testing.T) {
	database, err := store.Open(context.Background(), t.TempDir()+"/w.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := httpapi.New(app.New(database, log, app.Config{UploadDir: t.TempDir()}), log, httpapi.Config{MCP: http.NotFoundHandler()})
	routes, ok := h.(chi.Routes)
	if !ok {
		t.Fatal("router is not a chi.Routes")
	}
	inRouter := map[string]bool{}
	_ = chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		route = strings.TrimSuffix(strings.ReplaceAll(route, "/*/", "/"), "/")
		if route == "" || strings.HasSuffix(route, "*") {
			return nil
		}
		switch method {
		case "HEAD", "OPTIONS", "CONNECT", "TRACE":
			return nil
		}
		if route == "/mcp" {
			inRouter["POST /mcp"] = true
			return nil
		}
		inRouter[method+" "+route] = true
		return nil
	})
	inSpec := map[string]bool{}
	for p, item := range spec.doc["paths"].(map[string]any) {
		for m := range item.(map[string]any) {
			if slices.Contains([]string{"get", "post", "put", "patch", "delete"}, m) {
				inSpec[strings.ToUpper(m)+" "+p] = true
			}
		}
	}
	var missing, extra []string
	for k := range inRouter {
		if !inSpec[k] {
			missing = append(missing, k)
		}
	}
	for k := range inSpec {
		if !inRouter[k] && !notInRouter[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 {
		t.Errorf("routes missing from openapi.yaml:\n  %s", strings.Join(missing, "\n  "))
	}
	if len(extra) > 0 {
		t.Errorf("documented in openapi.yaml but not served:\n  %s", strings.Join(extra, "\n  "))
	}
}
