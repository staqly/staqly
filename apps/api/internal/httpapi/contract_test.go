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
	"flag"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
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
	raw = normalize(raw)
	strictify(raw)
	b, err := json.Marshal(raw)
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

// strictify makes every documented object schema closed for the test
// (unevaluatedProperties: false unless additionalProperties is set), so a
// field the API sends or accepts but the document omits is a violation.
// The published document stays open for forward compatibility.
func strictify(doc any) {
	var walk func(s any, inAllOf bool)
	walk = func(s any, inAllOf bool) {
		m, ok := s.(map[string]any)
		if !ok {
			return
		}
		if props, ok := m["properties"].(map[string]any); ok {
			_, ap := m["additionalProperties"]
			_, up := m["unevaluatedProperties"]
			if !ap && !up && !inAllOf {
				m["unevaluatedProperties"] = false
			}
			for _, v := range props {
				walk(v, false)
			}
		}
		walk(m["items"], false)
		walk(m["additionalProperties"], false)
		for _, k := range []string{"oneOf", "anyOf", "allOf"} {
			arr, _ := m[k].([]any)
			for _, v := range arr {
				walk(v, k == "allOf")
			}
		}
	}
	for _, sch := range get(doc, "components", "schemas").(map[string]any) {
		walk(sch, false)
	}
	var walkContent func(c any)
	walkContent = func(c any) {
		cm, _ := c.(map[string]any)
		for _, mt := range cm {
			walk(get(mt, "schema"), false)
		}
	}
	for _, r := range get(doc, "components", "responses").(map[string]any) {
		walkContent(get(r, "content"))
	}
	for _, item := range get(doc, "paths").(map[string]any) {
		for _, op := range item.(map[string]any) {
			walkContent(get(op, "requestBody", "content"))
			resps, _ := get(op, "responses").(map[string]any)
			for _, r := range resps {
				walkContent(get(r, "content"))
			}
		}
	}
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
	// Outermost, so it also sees requests answered before routing (rate
	// limits, probes, decision links and chat callbacks).
	httpapi.RegisterOuterMiddleware(func(*httpapi.Server) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body []byte
				if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt == "application/json" && r.Body != nil {
					body, _ = io.ReadAll(r.Body)
					r.Body = io.NopCloser(bytes.NewReader(body))
				}
				method, path, query := r.Method, r.URL.Path, r.URL.Query()
				rec := &recorder{ResponseWriter: w}
				next.ServeHTTP(rec, r)
				validateExchange(r, method, path, query, body, rec)
			})
		}
	})
}

// specPaths matches a request path against the documented path templates,
// for requests that never reached a chi route.
var specPaths = sync.OnceValue(func() []struct {
	re   *regexp.Regexp
	path string
} {
	var out []struct {
		re   *regexp.Regexp
		path string
	}
	for p := range spec.doc["paths"].(map[string]any) {
		re := regexp.MustCompile("^" + regexp.MustCompile(`\\\{[a-z_]+\\\}`).ReplaceAllString(regexp.QuoteMeta(p), `[^/]+`) + "$")
		out = append(out, struct {
			re   *regexp.Regexp
			path string
		}{re, p})
	}
	// Literal segments win over parameters (/v1/items/bulk-actions vs /v1/items/{id}).
	sort.Slice(out, func(i, j int) bool {
		ci, cj := strings.Count(out[i].path, "{"), strings.Count(out[j].path, "{")
		if ci != cj {
			return ci < cj
		}
		return out[i].path < out[j].path
	})
	return out
})

func operationPath(r *http.Request, rawPath string) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		if p := rctx.RoutePattern(); p != "" && p != "/*" {
			return p
		}
	}
	for _, sp := range specPaths() {
		if sp.re.MatchString(rawPath) {
			return sp.path
		}
	}
	return ""
}

// parameters lists the names of the documented query parameters of an operation.
func (s *contractSpec) queryParams(path, method string) map[string]bool {
	out := map[string]bool{}
	for _, list := range []any{get(s.doc, "paths", path, "parameters"), get(s.doc, "paths", path, method, "parameters")} {
		arr, _ := list.([]any)
		for _, p := range arr {
			if ref, ok := get(p, "$ref").(string); ok {
				p = get(s.doc, strings.Split(strings.TrimPrefix(ref, "#/"), "/")...)
			}
			if get(p, "in") == "query" {
				out[get(p, "name").(string)] = true
			}
		}
	}
	return out
}

func validateExchange(r *http.Request, method, rawPath string, query url.Values, reqBody []byte, rec *recorder) {
	path := operationPath(r, rawPath)
	if path == "" || path == "/mcp" {
		return
	}
	m := strings.ToLower(method)
	if m == "head" {
		m = "get"
	}
	op := get(spec.doc, "paths", path, m)
	if op == nil {
		return // 404/405 for an undocumented method; TestOpenAPIRoutesMatchRouter covers the router
	}
	status := rec.status
	if status == 0 {
		status = 200
	}
	seenOps.Store(strings.ToUpper(m)+" "+path, true)
	params := spec.queryParams(path, m)
	for k := range query {
		if !params[k] {
			violation("%s %s: query parameter %q is not documented", method, path, k)
		}
	}
	code := strconv.Itoa(status)
	resp := get(op, "responses", code)
	if resp == nil {
		if status != 500 && status != 503 { // any request may fail with 500 / 503 busy (documented globally)
			violation("%s %s → %d is not documented", method, path, status)
		}
		if status < 400 {
			return
		}
	}
	ct, _, _ := mime.ParseMediaType(rec.Header().Get("Content-Type"))
	if resp != nil && rec.buf.Len() > 0 && ct != "" {
		if ref, ok := get(resp, "$ref").(string); ok {
			resp = get(spec.doc, strings.Split(strings.TrimPrefix(ref, "#/"), "/")...)
		}
		if content, _ := get(resp, "content").(map[string]any); len(content) > 0 && content[ct] == nil &&
			content["*/*"] == nil && content[strings.Split(ct, "/")[0]+"/*"] == nil {
			violation("%s %s → %d: content type %s is not documented", method, path, status, ct)
		} else if len(content) == 0 && status < 400 && status != 304 && status != 206 {
			violation("%s %s → %d: response body (%s) is not documented", method, path, status, ct)
		}
	}
	if status < 300 && len(reqBody) > 0 {
		if get(op, "requestBody", "content", "application/json", "schema") == nil {
			if strings.TrimSpace(string(reqBody)) == "{}" {
				return // an empty object where no body is expected is harmless
			}
			violation("%s %s: JSON request body is not documented", method, path)
		} else if sch, err := spec.schema([]string{"paths", path, m, "requestBody", "content", "application/json", "schema"}); err != nil {
			violation("%s %s: cannot compile request schema: %v", method, path, err)
		} else if v, err := jsonschema.UnmarshalJSON(bytes.NewReader(reqBody)); err == nil {
			if err := sch.Validate(v); err != nil {
				violation("%s %s: accepted request body does not match the documented schema:\n%s", method, path, trim(err.Error()))
			}
		}
	}
	if ct != "application/json" || rec.buf.Len() == 0 {
		return
	}
	var ptr []string
	if p, documented := spec.responsePointer(path, m, code); documented && p != nil {
		ptr = p
	} else if status >= 400 {
		ptr = []string{"components", "schemas", "ErrorResponse"}
	} else {
		return
	}
	sch, err := spec.schema(ptr)
	if err != nil {
		violation("%s %s → %d: cannot compile schema: %v", method, path, status, err)
		return
	}
	body, err := jsonschema.UnmarshalJSON(bytes.NewReader(rec.buf.Bytes()))
	if err != nil {
		violation("%s %s → %d: invalid JSON: %v", method, path, status, err)
		return
	}
	if err := sch.Validate(body); err != nil {
		violation("%s %s → %d does not match the documented schema:\n%s", method, path, status, trim(err.Error()))
	}
}

func trim(msg string) string {
	if len(msg) > 800 {
		return msg[:800] + "..."
	}
	return msg
}

// Operations no test is expected to reach.
var notExercised = map[string]bool{
	"POST /mcp": true, // protocol endpoint, covered by the MCP tests
}

func TestMain(m *testing.M) {
	code := m.Run()
	// Only a full run reaches every operation.
	if f := flag.Lookup("test.run"); code == 0 && f != nil && f.Value.String() == "" {
		var missing []string
		for p, item := range spec.doc["paths"].(map[string]any) {
			for m := range item.(map[string]any) {
				op := strings.ToUpper(m) + " " + p
				if _, seen := seenOps.Load(op); !seen && !notExercised[op] && slices.Contains([]string{"get", "post", "put", "patch", "delete"}, m) {
					missing = append(missing, op)
				}
			}
		}
		sort.Strings(missing)
		for _, op := range missing {
			violation("%s is documented but no test exercises it", op)
		}
	}
	violMu.Lock()
	defer violMu.Unlock()
	if len(violations) > 0 {
		seen := map[string]bool{}
		fmt.Fprintf(os.Stderr, "\nOpenAPI contract violations (apps/api/openapi.yaml vs real requests and responses):\n")
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

// The document itself is served (embedded) at /openapi.yaml.
func TestOpenAPIDocumentServed(t *testing.T) {
	database, err := store.Open(context.Background(), t.TempDir()+"/w.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(httpapi.New(app.New(database, log, app.Config{UploadDir: t.TempDir()}), log, httpapi.Config{OpenAPISpec: api.OpenAPI}))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/yaml" || !bytes.Equal(b, api.OpenAPI) {
		t.Fatalf("GET /openapi.yaml: %d %s (%d bytes)", resp.StatusCode, resp.Header.Get("Content-Type"), len(b))
	}
}
