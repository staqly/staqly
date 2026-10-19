package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"short", 10, "short"},
		{"  a \n\t b  ", 10, "a b"},
		{"abcdefghij", 5, "ab..."},
		{"abcdefghij", 3, "abc"},
		{"abcdefghij", 1, "a"},
		{"żółćżółćżółć", 6, "żół..."},
	}
	for _, tc := range cases {
		if got := truncate(tc.in, tc.n); got != tc.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
		}
	}
}

func TestParseSeconds(t *testing.T) {
	ok := map[string]int{"0": 0, "90": 90, "5m": 300, "1h30m": 5400, "1500ms": 2}
	for in, want := range ok {
		got, err := parseSeconds(in)
		if err != nil || got != want {
			t.Errorf("parseSeconds(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "abc", "-5", "-5m", "5 minutes"} {
		if _, err := parseSeconds(in); err == nil {
			t.Errorf("parseSeconds(%q) should fail", in)
		}
	}
}

func TestMaskKey(t *testing.T) {
	cases := map[string]string{"": "", "stk_1": "****", "stk_1234": "****", "stk_12345": "stk_1234...", "stk_abcdefghijkl": "stk_abcd..."}
	for in, want := range cases {
		if got := maskKey(in); got != want {
			t.Errorf("maskKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConfigPath(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if got := configPath(env(map[string]string{"STAQLY_CONFIG": "/x/c.json", "XDG_CONFIG_HOME": "/xdg", "HOME": "/h"})); got != "/x/c.json" {
		t.Fatalf("STAQLY_CONFIG not honoured: %s", got)
	}
	if got := configPath(env(map[string]string{"XDG_CONFIG_HOME": "/xdg", "HOME": "/h"})); got != filepath.Join("/xdg", "staqly", "config.json") {
		t.Fatalf("XDG_CONFIG_HOME not honoured: %s", got)
	}
	if got := configPath(env(map[string]string{"HOME": "/h"})); got != filepath.Join("/h", ".config", "staqly", "config.json") {
		t.Fatalf("HOME not honoured: %s", got)
	}
	home, err := os.UserHomeDir()
	if err == nil {
		if got := configPath(env(nil)); got != filepath.Join(home, ".config", "staqly", "config.json") {
			t.Fatalf("fallback to UserHomeDir: %s", got)
		}
	}
}

func TestLoadAndSaveConfig(t *testing.T) {
	dir := t.TempDir()

	// Missing file: empty config, not an error.
	cfg, err := loadConfig(filepath.Join(dir, "missing.json"))
	if err != nil || cfg.Profiles == nil || len(cfg.Profiles) != 0 {
		t.Fatalf("missing config = %+v, %v", cfg, err)
	}

	// "profiles": null still yields a usable map.
	p := filepath.Join(dir, "null.json")
	os.WriteFile(p, []byte(`{"current":"work","profiles":null}`), 0o600)
	cfg, err = loadConfig(p)
	if err != nil || cfg.Profiles == nil || cfg.Current != "work" {
		t.Fatalf("null profiles = %+v, %v", cfg, err)
	}

	// Invalid JSON names the file.
	p = filepath.Join(dir, "bad.json")
	os.WriteFile(p, []byte(`{not json`), 0o600)
	if _, err := loadConfig(p); err == nil || !strings.Contains(err.Error(), "not valid JSON") || !strings.Contains(err.Error(), p) {
		t.Fatalf("bad JSON error = %v", err)
	}

	// A directory cannot be read as a file.
	if _, err := loadConfig(dir); err == nil || !strings.Contains(err.Error(), "read config") {
		t.Fatalf("directory error = %v", err)
	}

	// save creates the parent directory, writes 0600 and round-trips.
	p = filepath.Join(dir, "nested", "deeper", "config.json")
	in := &Config{Current: "work", Profiles: map[string]*Profile{"work": {URL: "https://x.example", Key: "stk_k"}, "a": {URL: "http://l"}}}
	if err := in.save(p); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		st, _ := os.Stat(p)
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("config mode = %v", st.Mode().Perm())
		}
		dst, _ := os.Stat(filepath.Dir(p))
		if dst.Mode().Perm() != 0o700 {
			t.Fatalf("config dir mode = %v", dst.Mode().Perm())
		}
		if insecurePerms(p) {
			t.Fatal("0600 config reported as insecure")
		}
		os.Chmod(p, 0o644)
		if !insecurePerms(p) {
			t.Fatal("0644 config not reported as insecure")
		}
	}
	if insecurePerms(filepath.Join(dir, "nope.json")) {
		t.Fatal("missing file reported as insecure")
	}
	out, err := loadConfig(p)
	if err != nil || out.Current != "work" || out.Profiles["work"].Key != "stk_k" {
		t.Fatalf("round trip = %+v, %v", out, err)
	}
	if got := strings.Join(out.names(), ","); got != "a,work" {
		t.Fatalf("names = %s", got)
	}
	// No temp files left behind.
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Fatalf("leftover files: %v", entries)
	}

	// save fails when the parent "directory" is a file.
	blocker := filepath.Join(dir, "file")
	os.WriteFile(blocker, nil, 0o600)
	if err := in.save(filepath.Join(blocker, "config.json")); err == nil {
		t.Fatal("save under a file should fail")
	}
}

func TestConfigWarningsAndProfileResolution(t *testing.T) {
	f := newFake(t)
	f.on("GET /v1/posts", 200, map[string]any{"data": []any{}, "next_cursor": nil})

	// Insecure permissions trigger a warning but the command still runs.
	if runtime.GOOS != "windows" {
		p := filepath.Join(t.TempDir(), "config.json")
		os.WriteFile(p, []byte(`{"profiles":{"default":{"url":"`+f.srv.URL+`","key":"stk_secretkey1"}}}`), 0o644)
		r := f.runWith(runOpts{cfgPath: p, env: map[string]string{"STAQLY_API_URL": ""}}, "posts", "list").mustCode(t, 0)
		mustContain(t, r.err, "readable by other users", "chmod 600")
	}

	// Broken config file: a runtime error mentioning the file.
	p := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(p, []byte(`{`), 0o600)
	r := f.runWith(runOpts{cfgPath: p}, "posts", "list").mustCode(t, ExitError)
	mustContain(t, r.err, "not valid JSON")

	// STAQLY_PROFILE naming an unknown profile is a usage error.
	p = filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(p, []byte(`{"current":"work","profiles":{"work":{"url":"`+f.srv.URL+`","key":"stk_workkey99"}}}`), 0o600)
	r = f.runWith(runOpts{cfgPath: p, env: map[string]string{"STAQLY_PROFILE": "nope"}}, "posts", "list").mustCode(t, ExitUsage)
	mustContain(t, r.err, `profile "nope" not found`)

	// STAQLY_PROFILE selecting an existing profile uses its URL and key.
	f.runWith(runOpts{cfgPath: p, env: map[string]string{"STAQLY_PROFILE": "work", "STAQLY_API_URL": ""}}, "posts", "list").mustCode(t, 0)
	if got := f.last("GET", "/v1/posts").Header.Get("Authorization"); got != "Bearer stk_workkey99" {
		t.Fatalf("Authorization = %q", got)
	}

	// A non-http(s) URL is rejected before any request.
	r = f.runWith(runOpts{env: map[string]string{"STAQLY_API_URL": "ftp://x"}}, "posts", "list").mustCode(t, ExitUsage)
	mustContain(t, r.err, "must start with http:// or https://")
}

func TestRequestHeaders(t *testing.T) {
	f := newFake(t)
	f.on("GET /v1/posts", 200, map[string]any{"data": []any{}, "next_cursor": nil})
	f.runWith(runOpts{env: map[string]string{"STAQLY_API_KEY": "stk_headerkey"}}, "posts", "list").mustCode(t, 0)
	h := f.last("GET", "/v1/posts").Header
	if h.Get("Accept") != "application/json" || h.Get("User-Agent") != "staqly-cli/1.2.3" || h.Get("Authorization") != "Bearer stk_headerkey" {
		t.Fatalf("headers = %v", h)
	}
	if h.Get("Idempotency-Key") != "" || h.Get("Content-Type") != "" {
		t.Fatalf("GET must not carry Idempotency-Key/Content-Type: %v", h)
	}
}

func TestDecodeAPIError(t *testing.T) {
	resp := func(status int, hdr map[string]string) *http.Response {
		r := &http.Response{StatusCode: status, Header: http.Header{}}
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		return r
	}

	// Non-envelope body: cleaned and truncated to 200 runes.
	long := strings.Repeat("x", 250)
	e := decodeAPIError(resp(502, nil), []byte("\x1b[31m"+long))
	if e.Status != 502 || e.Code != "" || e.Message != strings.Repeat("x", 200)+"..." {
		t.Fatalf("long body = %+v", e)
	}
	// Empty body: status text.
	if e := decodeAPIError(resp(503, nil), nil); e.Message != "Service Unavailable" {
		t.Fatalf("empty body message = %q", e.Message)
	}
	// Envelope without code or message is treated as plain text.
	if e := decodeAPIError(resp(400, nil), []byte(`{"error":{}}`)); e.Message != `{"error":{}}` {
		t.Fatalf("empty envelope message = %q", e.Message)
	}
	// Control characters in fields are scrubbed (key and value).
	e = decodeAPIError(resp(422, nil), []byte(`{"error":{"code":"validation_failed","message":"bad","fields":{"na\u001b[2Jme":"is\nrequired"}}}`))
	if e.Fields["name"] != "is required" || len(e.Fields) != 1 {
		t.Fatalf("fields = %q", e.Fields)
	}
	// Retry-After as an HTTP date.
	when := time.Now().Add(90 * time.Second).UTC().Format(http.TimeFormat)
	e = decodeAPIError(resp(429, map[string]string{"Retry-After": when}), []byte(`{"error":{"code":"rate_limited","message":"slow"}}`))
	if e.RetryAfter < 80*time.Second || e.RetryAfter > 91*time.Second {
		t.Fatalf("RetryAfter = %v", e.RetryAfter)
	}
	// Garbage Retry-After is ignored.
	if e := decodeAPIError(resp(429, map[string]string{"Retry-After": "soon"}), nil); e.RetryAfter != 0 {
		t.Fatalf("RetryAfter = %v", e.RetryAfter)
	}
}

func TestAPIAndNetErrorStrings(t *testing.T) {
	if got := (&APIError{Status: 404, Code: "not_found", Message: "no item"}).Error(); got != "no item (404 not_found)" {
		t.Fatal(got)
	}
	if got := (&APIError{Status: 502, Message: "Bad Gateway"}).Error(); got != "Bad Gateway (HTTP 502)" {
		t.Fatal(got)
	}
	inner := errors.New("connection refused")
	ne := &NetError{URL: "http://x", Err: inner}
	if ne.Error() != "cannot reach http://x: connection refused" || !errors.Is(ne, inner) {
		t.Fatalf("NetError = %v", ne)
	}
}

func TestFriendlyAPIErrorVariants(t *testing.T) {
	cases := []struct {
		e    *APIError
		want []string
	}{
		{&APIError{Status: 403, Code: "csrf_failed", Message: "bad token"}, []string{"request rejected (CSRF): bad token", "API key"}},
		{&APIError{Status: 402, Code: "payment_required", Message: "upgrade"}, []string{"plan limit reached: upgrade", "staqly status"}},
		{&APIError{Status: 404, Code: "not_found", Message: "item not found"}, []string{"not found: item not found"}},
		{&APIError{Status: 413, Message: "file too big"}, []string{"too large: file too big"}},
		{&APIError{Status: 415, Message: "image/tiff"}, []string{"unsupported file type: image/tiff"}},
		{&APIError{Status: 400, Message: "bad cursor", Fields: map[string]string{"z": "last", "a": "first"}}, []string{"invalid request: bad cursor\n  a: first\n  z: last"}},
		{&APIError{Status: 429, Message: "slow down"}, []string{"rate limited: slow down"}},
		{&APIError{Status: 503, Message: "maintenance"}, []string{"server error: maintenance (HTTP 503)"}},
		{&APIError{Status: 418, Code: "teapot", Message: "short and stout"}, []string{"short and stout (418 teapot)"}},
		{&APIError{Status: 403, Code: "forbidden", Message: "missing required scope items:write"}, []string{"permission denied: missing required scope items:write", "staqly keys list"}},
	}
	for _, tc := range cases {
		got := friendlyAPIError(tc.e)
		mustContain(t, got, tc.want...)
		if tc.e.Status == 402 && strings.Contains(got, "limit:") {
			t.Errorf("402 without details printed a limit line: %q", got)
		}
		if tc.e.Status == 429 && strings.Contains(got, "Retry after") {
			t.Errorf("429 without Retry-After printed a hint: %q", got)
		}
	}
}

// A 403 that is not about scopes (an admin key asking for a dashboard-only
// action) must not send the user hunting for a missing scope.
func TestFriendlyAPIError403WithoutScope(t *testing.T) {
	got := friendlyAPIError(&APIError{Status: 403, Code: "forbidden", Message: "device links can only be created from the dashboard"})
	if got != "permission denied: device links can only be created from the dashboard" {
		t.Fatalf("got %q", got)
	}
}

func TestFriendlyError(t *testing.T) {
	if got := friendlyError(fmt.Errorf("wrap: %w", context.DeadlineExceeded)); got != "request timed out" {
		t.Fatal(got)
	}
	if got := friendlyError(errors.New("plain")); got != "plain" {
		t.Fatal(got)
	}
	ne := &NetError{URL: "http://127.0.0.1:1", Err: &url.Error{Op: "Get", URL: "http://127.0.0.1:1/v1", Err: errors.New("dial refused")}}
	got := friendlyError(ne)
	mustContain(t, got, "cannot reach the staqly API at http://127.0.0.1:1 (dial refused)", "STAQLY_API_URL")
	if got := friendlyError(fmt.Errorf("x: %w", &APIError{Status: 404, Message: "gone"})); got != "not found: gone" {
		t.Fatal(got)
	}
}

func TestIsTransientAndMultiFailure(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{&NetError{URL: "u", Err: errors.New("x")}, true},
		{&APIError{Status: 429}, true},
		{&APIError{Status: 500}, true},
		{&APIError{Status: 503}, true},
		{&APIError{Status: 404}, false},
		{&APIError{Status: 409}, false},
		{errors.New("other"), false},
	}
	for _, tc := range cases {
		if got := isTransient(tc.err); got != tc.want {
			t.Errorf("isTransient(%v) = %v", tc.err, got)
		}
	}

	c := &Ctx{}
	if err := multiFailure(c, 0, 3, nil); err != nil {
		t.Fatalf("no failures = %v", err)
	}
	err := multiFailure(c, 2, 3, &APIError{Status: 409, Message: "already publishing"})
	var ee *exitErr
	if !errors.As(err, &ee) || ee.code != ExitError || ee.Error() != "2 of 3 failed (last: conflict: already publishing)" {
		t.Fatalf("multiFailure = %#v", err)
	}
	c.JSON = true
	if err := multiFailure(c, 1, 1, errors.New("x")); !errors.As(err, &ee) || ee.msg != "" || ee.code != ExitError {
		t.Fatalf("json multiFailure = %#v", err)
	}
}

func TestSmallFormatters(t *testing.T) {
	if actorName(Actor{Type: "agent", Name: "writer"}) != "writer" || actorName(Actor{Type: "api_key"}) != "api_key" {
		t.Fatal("actorName")
	}
	empty := ""
	ts := "2026-10-13T10:00:00Z"
	if fmtOptTime(nil) != "" || fmtOptTime(&empty) != "" || fmtOptTime(&ts) != fmtTime(ts) {
		t.Fatal("fmtOptTime")
	}
	if fmtTime("") != "-" || fmtTime("yesterday") != "yesterday" || fmtTime("2026-10-13 10:00:00") == "2026-10-13 10:00:00" {
		t.Fatal("fmtTime")
	}
	if firstLine(" a\nb ") != "a" || firstLine("") != "" {
		t.Fatal("firstLine")
	}
	if publishExitCode("published") != ExitOK || publishExitCode("failed") != ExitFailed || publishExitCode("publishing") != ExitOther {
		t.Fatal("publishExitCode")
	}
	s := Schedule{Kind: "recurring", Rule: ScheduleRule{Times: []string{"09:00"}, Weekdays: []int{1, 5}, TZ: "Europe/Warsaw"}}
	if got := describeSchedule(s); got != "09:00 on mon,fri Europe/Warsaw" {
		t.Fatalf("describeSchedule = %q", got)
	}
	if got := describeSchedule(Schedule{Kind: "recurring", Rule: ScheduleRule{EveryMinutes: 360}}); got != "every 6h0m0s" {
		t.Fatalf("describeSchedule every = %q", got)
	}
	if d, err := parseWeekdays("mon,Tue,6"); err != nil || fmt.Sprint(d) != "[1 2 6]" {
		t.Fatalf("parseWeekdays = %v %v", d, err)
	}
	if _, err := parseWeekdays("funday"); err == nil {
		t.Fatal("parseWeekdays accepted garbage")
	}
}

func TestEventTypeColors(t *testing.T) {
	plain := &Ctx{}
	if got := eventType(plain, "post.published"); got != fmt.Sprintf("%-28s", "post.published") {
		t.Fatalf("no-color eventType = %q", got)
	}
	color := &Ctx{Color: true}
	for typ, code := range map[string]string{"post.published": green, "channel.needs_reconnect": red, "webhook.failed": red, "post.created": cyan, "post.scheduled": cyan} {
		if got := eventType(color, typ); !strings.HasPrefix(got, code) || !strings.Contains(got, typ) {
			t.Errorf("eventType(%s) = %q", typ, got)
		}
	}
	if got := eventType(color, "post.updated"); strings.Contains(got, "\x1b") {
		t.Errorf("uncoloured type got colour: %q", got)
	}
	if got := eventType(color, "post.\x1b[2Jpublished"); strings.Contains(got, "[2J") {
		t.Errorf("type not cleaned: %q", got)
	}
}

func TestEscapeSequencesStripped(t *testing.T) {
	cases := map[string]string{
		"a\x1bcb":            "ab",  // ESC c (reset)
		"a\x1b(Bb":           "ab",  // ESC ( B (charset)
		"a\x1b":              "a",   // lone trailing ESC
		"a\x1b\x01b":         "ab",  // lone ESC before a control char
		"a\x1b[31\x01mb":     "amb", // malformed CSI stops early
		"a\x1b[31":           "a",   // unterminated CSI
		"a\x1b]0;title\x07b": "ab",  // OSC with BEL
		"a\x1b]0;t\x1b\\b":   "ab",  // OSC with ESC \
		"a\x1b]0;t\u009cb":   "ab",  // OSC with C1 ST
		"a\x1b]0;t\x1b[1mb":  "ab",  // OSC interrupted by CSI
		"a\x1b_unterminated": "a",   // APC swallows the rest
	}
	for in, want := range cases {
		if got := clean(in); got != want {
			t.Errorf("clean(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPrintRawNonJSON(t *testing.T) {
	var out strings.Builder
	c := &Ctx{Out: &out}
	if err := c.printRaw(json.RawMessage("<html>\x1b[2Jbad gateway</html>")); err != nil {
		t.Fatal(err)
	}
	if out.String() != "<html>bad gateway</html>\n" {
		t.Fatalf("printRaw = %q", out.String())
	}
	out.Reset()
	if err := c.printRaw(nil); err != nil || out.String() != "{}\n" {
		t.Fatalf("printRaw(nil) = %q, %v", out.String(), err)
	}
}

func TestReadJSONObjectArgSources(t *testing.T) {
	f := newFake(t)
	f.on("PATCH /v1/alerts/nch_1", 200, alert("nch_1"))

	p := filepath.Join(t.TempDir(), "cfg.json")
	os.WriteFile(p, []byte(`{"topic":"from-file"}`), 0o600)
	f.run("alerts", "update", "nch_1", "--config-json", "@"+p).mustCode(t, 0)
	if cfg := f.last("PATCH", "/v1/alerts/nch_1").json(t)["config"].(map[string]any); cfg["topic"] != "from-file" {
		t.Fatalf("config = %v", cfg)
	}
	f.run("alerts", "update", "nch_1", "--config-json", `{"topic":"inline"}`).mustCode(t, 0)
	if cfg := f.last("PATCH", "/v1/alerts/nch_1").json(t)["config"].(map[string]any); cfg["topic"] != "inline" {
		t.Fatalf("config = %v", cfg)
	}

	n := len(f.find("PATCH", "/v1/alerts/nch_1"))
	r := f.run("alerts", "update", "nch_1", "--config-json", "@"+filepath.Join(t.TempDir(), "missing.json"))
	if r.code == 0 {
		t.Fatal("missing @file should fail")
	}
	mustContain(t, r.err, "read --config-json")
	if r := f.run("alerts", "update", "nch_1", "--config-json", "[1,2]"); r.code == 0 {
		t.Fatal("non-object JSON should fail")
	}
	if got := len(f.find("PATCH", "/v1/alerts/nch_1")); got != n {
		t.Fatalf("invalid input still sent %d requests", got-n)
	}
}

func TestMediaGetErrors(t *testing.T) {
	f := newFake(t)
	r := f.run("media", "get", "med_missing", "--output", filepath.Join(t.TempDir(), "x")).mustCode(t, ExitError)
	mustContain(t, r.err, "not found")
	if q := f.last("GET", "/v1/media/med_missing"); q.Header.Get("Accept") != "*/*" {
		t.Fatalf("Accept = %q", q.Header.Get("Accept"))
	}
}

func TestStreamKeepsTimeoutOffAndContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	hc := &http.Client{Timeout: 50 * time.Millisecond}
	cl := &Client{BaseURL: srv.URL, HTTP: hc}

	// Do respects the client timeout...
	if _, err := cl.Do(context.Background(), Request{Method: http.MethodGet, Path: "/"}); err == nil {
		t.Fatal("Do should time out")
	}
	// ...but Stream (NoTimeout) lifts it without mutating the shared client.
	resp, err := cl.Stream(context.Background(), "/", nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	resp.Body.Close()
	if hc.Timeout != 50*time.Millisecond {
		t.Fatalf("shared client timeout changed to %v", hc.Timeout)
	}

	// A cancelled context surfaces as the context error, not a NetError.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = cl.Do(ctx, Request{Method: http.MethodGet, Path: "/"})
	var ne *NetError
	if !errors.Is(err, context.Canceled) || errors.As(err, &ne) {
		t.Fatalf("cancelled Do = %v", err)
	}
}

func TestClientDecodeAndBodyEncoding(t *testing.T) {
	var gotCT, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 1024)
		n, _ := r.Body.Read(b)
		gotCT, gotBody = r.Header.Get("Content-Type"), string(b[:n])
		switch r.URL.Path {
		case "/empty":
			w.WriteHeader(204)
		case "/text":
			_, _ = w.Write([]byte("not json"))
		default:
			_, _ = w.Write([]byte(`{"name":"x\u001b[2Jy"}`))
		}
	}))
	defer srv.Close()
	cl := &Client{BaseURL: srv.URL + "/", Sanitize: true}
	ctx := context.Background()

	var out struct{ Name string }
	if _, err := cl.Post(ctx, "/obj", map[string]any{"a": 1}, &out); err != nil || out.Name != "xy" {
		t.Fatalf("Post = %+v, %v", out, err)
	}
	if gotCT != "application/json" || gotBody != `{"a":1}` {
		t.Fatalf("JSON body = %s %q", gotCT, gotBody)
	}
	if _, err := cl.Do(ctx, Request{Method: http.MethodPut, Path: "/obj", Body: strings.NewReader("raw"), ContentType: "text/plain"}); err != nil {
		t.Fatal(err)
	}
	if gotCT != "text/plain" || gotBody != "raw" {
		t.Fatalf("reader body = %s %q", gotCT, gotBody)
	}
	raw, err := cl.Patch(ctx, "/empty", map[string]any{}, &out)
	if err != nil || raw != nil {
		t.Fatalf("204 = %q, %v", raw, err)
	}
	raw, err = cl.Get(ctx, "/text", nil, &out)
	if err == nil || !strings.Contains(err.Error(), "unexpected response") || string(raw) != "not json" {
		t.Fatalf("non-JSON = %q, %v", raw, err)
	}
	if _, err := cl.Post(ctx, "/obj", map[string]any{"bad": make(chan int)}, nil); err == nil {
		t.Fatal("unencodable body should fail")
	}
}

func TestEnvSleepAndNowDefaults(t *testing.T) {
	e := &Env{}
	if err := e.sleep(context.Background(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := e.sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled sleep = %v", err)
	}
	if d := time.Since(e.now()); d < 0 || d > time.Minute {
		t.Fatalf("now drift = %v", d)
	}
}
