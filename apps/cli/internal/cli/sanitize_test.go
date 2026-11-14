package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// Hostile payloads a server (or anyone who can create an item) could send.
const (
	osc52       = "\x1b]52;c;Y3VybCBldmlsLnNoIHwgc2g=\x07" // set the clipboard
	osc52ST     = "\x1b]52;c;ZXZpbA==\x1b\\"               // same, ST-terminated
	csiClear    = "\x1b[2J\x1b[H"                          // clear screen, cursor home
	c1CSI       = "\u009b2J"                               // 8-bit CSI clear screen
	c1OSC       = "\u009d0;title\u009c"                    // 8-bit OSC set title
	bidiRLO     = "\u202e"                                 // right-to-left override
	bidiIsolate = "\u2066\u2069"
	hyperlink   = "\x1b]8;;https://evil.example\x1b\\click\x1b]8;;\x1b\\"
)

func assertNoTerminalControls(t *testing.T, where, s string) {
	t.Helper()
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t':
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) || isBidiControl(r):
			t.Fatalf("%s contains control character %U:\n%q", where, r, s)
		}
	}
}

func TestClean(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain text", "plain text"},
		{"multi\nline\ttab", "multi\nline\ttab"},
		{"a" + osc52 + "b", "ab"},
		{"a" + osc52ST + "b", "ab"},
		{csiClear + "title", "title"},
		{"x" + c1CSI + "y", "xy"},
		{"x" + c1OSC + "y", "xy"},
		{"admin" + bidiRLO + "gpj.exe", "admingpj.exe"},
		{"a" + bidiIsolate + "b", "ab"},
		{hyperlink, "click"},
		{"\x1b[31mred\x1b[0m", "red"},
		{"bell\x07 bs\x08 cr\r del\x7f", "bell bs cr del"},
		{"esc at end\x1b", "esc at end"},
		{"unterminated " + "\x1b]52;c;abc", "unterminated "},
		{"bad utf8 \xff", "bad utf8 \ufffd"},
		{"zażółć 🎉", "zażółć 🎉"},
	}
	for _, tc := range cases {
		if got := clean(tc.in); got != tc.want {
			t.Errorf("clean(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if got := cleanLine("a\nb\tc"); got != "a b c" {
		t.Errorf("cleanLine = %q", got)
	}
}

func TestCleanCellKeepsOwnColors(t *testing.T) {
	c := &Ctx{Color: true}
	cell := c.status("pending")
	if got := cleanCell(cell); got != cell {
		t.Fatalf("own color stripped: %q", got)
	}
	if got := cleanCell("x" + csiClear + osc52 + "\ty"); got != "x y" {
		t.Fatalf("cleanCell = %q", got)
	}
	// paint cleans what it wraps.
	if got := c.paint(bold, "a"+osc52+"b"); got != bold+"ab"+reset {
		t.Fatalf("paint = %q", got)
	}
}

func TestScrubDecodedResponse(t *testing.T) {
	var p Post
	raw := `{"id":"pst_1","content":"t\u001b]52;c;eA==\u0007","metadata":{"k\u202e":["v\u001b[2J",{"n":"\u009b2J"}]},"media_items":[{"original_name":"a\u001b[Hb"}]}`
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	scrub(&p)
	b, _ := json.Marshal(p)
	if strings.Contains(string(b), `\u001b`) || strings.Contains(string(b), `\u009b`) || strings.Contains(string(b), "\u202e") {
		t.Fatalf("scrub left controls: %s", b)
	}
	if p.Content != "t" || p.MediaItems[0].OriginalName != "ab" || p.Metadata["k"] == nil {
		t.Fatalf("scrubbed post = %+v", p)
	}
}

func TestSafeJSON(t *testing.T) {
	in := []byte(`{"t":"a` + "\u009b2J\u202e" + `b"}`)
	out := safeJSON(in)
	assertNoTerminalControls(t, "json", string(out))
	var m map[string]string
	if err := json.Unmarshal(out, &m); err != nil || m["t"] != "a\u009b2J\u202eb" {
		t.Fatalf("safeJSON must stay lossless JSON: %v %q", err, m)
	}
}

func hostilePost(id string) map[string]any {
	p := post(id, "draft")
	p["content"] = "Launch" + osc52 + csiClear + bidiRLO + "post\n" + csiClear + "line two" + c1CSI + hyperlink
	p["collection_name"] = "Blog" + c1OSC
	p["tags"] = []any{"tag" + osc52ST}
	p["metadata"] = map[string]any{"note" + bidiRLO: "x" + csiClear}
	return p
}

func TestHumanOutputIsSanitized(t *testing.T) {
	f := newFake(t)
	f.on("GET /v1/posts/pst_1", 200, hostilePost("pst_1"))
	f.on("GET /v1/posts", 200, map[string]any{"data": []any{hostilePost("pst_1")}})
	f.on("GET /v1/collections", 200, map[string]any{"data": []any{collection("col_1", "Blog"+osc52)}})
	e := ev(1, "post.created"+csiClear)
	e["data"] = map[string]any{"status": "evil" + osc52 + bidiRLO, "error": "x\x1b[2Jy"}
	e["actor"] = map[string]any{"type": "local", "id": "local", "name": "Lo" + c1CSI + "cal"}
	f.on("GET /v1/events", 200, map[string]any{"data": []any{e}})
	f.on("POST /v1/collections", 422, map[string]any{"error": map[string]any{"code": "validation_failed" + csiClear,
		"message": "bad" + osc52 + "\x1b[1A\x1b[2K", "fields": map[string]any{"name" + bidiRLO: "is " + c1CSI + "required"}}})

	for _, args := range [][]string{
		{"posts", "get", "pst_1"},
		{"posts", "list"},
		{"schedule", "--all"},
		{"events", "tail"},
		{"collections", "create", "X"},
	} {
		for _, tty := range []bool{false, true} {
			r := f.runWith(runOpts{stdoutTTY: tty}, args...)
			out := r.out + r.err
			if tty {
				// Only the CLI's own SGR colors may remain.
				for _, code := range ownColors {
					out = strings.ReplaceAll(out, code, "")
				}
			}
			assertNoTerminalControls(t, strings.Join(args, " "), out)
		}
	}
	r := f.run("posts", "get", "pst_1").mustCode(t, 0)
	mustContain(t, r.out, "Launchpost", "line two")
	r = f.run("events", "tail").mustCode(t, 0)
	mustContain(t, r.out, "status=evil", "error=xy")
	r = f.run("collections", "create", "X").mustCode(t, ExitError)
	mustContain(t, r.err, "invalid request: bad", "name: is required")
}

func TestJSONOutputIsEscapedButLossless(t *testing.T) {
	f := newFake(t)
	f.on("GET /v1/posts/pst_1", 200, hostilePost("pst_1"))
	f.on("GET /v1/posts", 200, map[string]any{"data": []any{hostilePost("pst_1")}})
	r := f.run("posts", "get", "pst_1", "--json").mustCode(t, 0)
	assertNoTerminalControls(t, "posts get --json", r.out)
	if m := mustJSON(t, r.out); m["content"] != hostilePost("x")["content"] {
		t.Fatalf("--json must keep data verbatim, got %q", m["content"])
	}
	e := ev(1, "post.created"+csiClear)
	f.on("GET /v1/events", 200, map[string]any{"data": []any{e}})
	r = f.run("events", "tail", "--json").mustCode(t, 0)
	assertNoTerminalControls(t, "events tail --json", r.out)
	if m := mustJSON(t, strings.TrimSpace(r.out)); m["type"] != e["type"] {
		t.Fatalf("NDJSON must keep data verbatim, got %q", m["type"])
	}
}

// ---- M13: stored keys are bound to their profile's URL ----

func TestProfileKeyNotSentToOverriddenURL(t *testing.T) {
	f := statusFake(t)
	other := statusFake(t)
	cfg := filepath.Join(t.TempDir(), "config.json")
	noEnv := runOpts{cfgPath: cfg, env: map[string]string{"STAQLY_API_URL": ""}}
	f.runWith(noEnv, "login", "--url", f.srv.URL+"/", "--key", "stk_profilekey123", "--profile", "work").mustCode(t, 0)

	// Same server (trailing slash ignored): key is sent.
	same := f.srv.URL + "/"
	runCLI(t, same, runOpts{cfgPath: cfg}, "posts", "list").mustCode(t, 0)
	if got := f.last("GET", "/v1/posts").Header.Get("Authorization"); got != "Bearer stk_profilekey123" {
		t.Fatalf("profile key not used for its own URL: %q", got)
	}

	// STAQLY_API_URL points elsewhere: no key, plus a note.
	r := runCLI(t, other.srv.URL, runOpts{cfgPath: cfg}, "posts", "list").mustCode(t, 0)
	if got := other.last("GET", "/v1/posts").Header.Get("Authorization"); got != "" {
		t.Fatalf("profile key leaked to an overridden URL: %q", got)
	}
	mustContain(t, r.err, "not sending the API key", `"work"`, "--key")

	// --api-url likewise.
	r = runCLI(t, "", noEnv, "posts", "list", "--api-url", other.srv.URL).mustCode(t, 0)
	if got := other.last("GET", "/v1/posts").Header.Get("Authorization"); got != "" {
		t.Fatalf("profile key leaked to --api-url: %q", got)
	}
	mustContain(t, r.err, "not sending the API key")

	// An explicit key is always sent, with no note.
	r = runCLI(t, other.srv.URL, runOpts{cfgPath: cfg, env: map[string]string{"STAQLY_API_KEY": "stk_explicit"}}, "posts", "list").mustCode(t, 0)
	if got := other.last("GET", "/v1/posts").Header.Get("Authorization"); got != "Bearer stk_explicit" {
		t.Fatalf("explicit key not used: %q", got)
	}
	if strings.Contains(r.err, "not sending") {
		t.Fatalf("unexpected note: %s", r.err)
	}
}

func TestPlainHTTPKeyWarning(t *testing.T) {
	for _, tc := range []struct {
		url  string
		warn bool
	}{
		{"http://127.0.0.1:3333", false},
		{"http://localhost:3333", false},
		{"http://[::1]:3333", false},
		{"https://staqly.example.com", false},
		{"http://staqly.example.com", true},
		{"http://192.168.1.5:3333", true},
	} {
		if got := insecureKeyURL(tc.url); got != tc.warn {
			t.Errorf("insecureKeyURL(%q) = %v, want %v", tc.url, got, tc.warn)
		}
	}
	// The warning goes to stderr when the client is resolved.
	c := &Ctx{Context: context.Background(), Env: &Env{Getenv: func(k string) string {
		return map[string]string{"STAQLY_API_URL": "http://staqly.example.com", "STAQLY_API_KEY": "stk_x"}[k]
	}}, g: &globals{}, cfgPath: filepath.Join(t.TempDir(), "c.json")}
	var errb strings.Builder
	c.Stderr = &errb
	if _, err := c.Client(); err != nil {
		t.Fatal(err)
	}
	mustContain(t, errb.String(), "plain http", "https://")
	c2 := &Ctx{Context: context.Background(), Env: &Env{Getenv: func(k string) string {
		return map[string]string{"STAQLY_API_URL": "http://staqly.example.com"}[k]
	}}, g: &globals{}, cfgPath: filepath.Join(t.TempDir(), "c.json")}
	var errb2 strings.Builder
	c2.Stderr = &errb2
	if _, err := c2.Client(); err != nil || errb2.Len() != 0 {
		t.Fatalf("keyless requests need no warning: %v %q", err, errb2.String())
	}
}

func TestSameAPIURL(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"http://localhost:3333", "http://localhost:3333/", true},
		{"https://Staqly.Example.com", "https://staqly.example.com:443", true},
		{"http://localhost:3333", "http://127.0.0.1:3333", false},
		{"https://a.example", "http://a.example", false},
		{"https://a.example/api", "https://a.example", false},
		{"https://a.example.evil.com", "https://a.example", false},
	} {
		if got := sameAPIURL(tc.a, tc.b); got != tc.want {
			t.Errorf("sameAPIURL(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
