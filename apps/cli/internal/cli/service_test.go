package cli

import (
	"errors"
	"flag"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestServiceUnitFiles(t *testing.T) {
	spec := serviceSpec{Exe: "/opt/my apps/staqly", Args: []string{"--port", "43333", "--expose"},
		Env: map[string]string{"TAGGER_URL": "https://api.openai.com/v1", "STAQLY_X": `a"b&c`}, Log: "/tmp/s.log"}
	plist := launchdPlist(spec)
	for _, want := range []string{"<string>/opt/my apps/staqly</string>", "<string>serve</string>", "<string>--expose</string>",
		"<key>TAGGER_URL</key><string>https://api.openai.com/v1</string>", "a&#34;b&amp;c", "<key>KeepAlive</key><true/>", "<key>RunAtLoad</key><true/>"} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist lacks %q:\n%s", want, plist)
		}
	}
	unit := systemdUnitFile(spec)
	for _, want := range []string{`ExecStart="/opt/my apps/staqly" serve "--port" "43333" "--expose"`, `Environment="STAQLY_X=a\"b&c"`,
		"Restart=always", "WantedBy=default.target"} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit lacks %q:\n%s", want, unit)
		}
	}
	if p := unitPort(plist); p != 43333 {
		t.Errorf("plist port %d", p)
	}
	if p := unitPort(unit); p != 43333 {
		t.Errorf("unit port %d", p)
	}
	if p := unitPort(systemdUnitFile(serviceSpec{Exe: "/s"})); p != 3333 {
		t.Errorf("default port %d", p)
	}
	if u := systemdUnitFile(serviceSpec{Exe: "/s", Env: map[string]string{"STAQLY_X": "a\nExecStartPre=/bin/evil"}}); strings.Contains(u, "\nExecStartPre") {
		t.Fatalf("line break not escaped:\n%s", u)
	}
	env := serviceEnv([]string{"MAIL_SMTP_HOST=x", "HOME=/h", "PATH=/bin", "STAQLY_MODE=local", "TAGGER_API_KEY=y", "AI_DISABLED="})
	// PATH is kept so the service finds the AI CLI (claude, codex) for summaries; HOME and retired names are not.
	if len(env) != 3 || env["MAIL_SMTP_HOST"] != "x" || env["STAQLY_MODE"] != "local" || env["PATH"] != "/bin" {
		t.Fatalf("env %v", env)
	}
}

// systemd expands $VAR in ExecStart= ("$$" is a literal dollar) but not in Environment=, where "$$" stays two
// dollars: a password like pa$word reached the service as pa$$word (checked against a real systemd 252).
func TestSystemdDollarQuoting(t *testing.T) {
	u := systemdUnitFile(serviceSpec{Exe: "/opt/$x/staqly", Args: []string{"--data", "/d/$HOME"},
		Env: map[string]string{"SMTP_PASSWORD": "pa$word", "TAGGER_COMMAND": "run 100%"}})
	for _, want := range []string{`ExecStart="/opt/$$x/staqly" serve "--data" "/d/$$HOME"`, `Environment="SMTP_PASSWORD=pa$word"`,
		`Environment="TAGGER_COMMAND=run 100%%"`} {
		if !strings.Contains(u, want) {
			t.Errorf("unit lacks %q:\n%s", want, u)
		}
	}
}

// fakeServiceManager stands in for launchctl / systemctl: a job is loaded after bootstrap / restart and, like
// launchd, stays visible for a few polls after bootout while its process shuts down.
type fakeServiceManager struct {
	ran          []string
	loaded       bool
	stopping     int // polls left until a booted-out job is gone
	bootstrapErr int // failing bootstraps left
}

func (f *fakeServiceManager) run(name string, args ...string) (string, error) {
	cmd := name + " " + strings.Join(args, " ")
	f.ran = append(f.ran, cmd)
	switch {
	case strings.HasPrefix(cmd, "launchctl print"):
		if f.stopping > 0 {
			f.stopping--
			return "state = running", nil
		}
		if !f.loaded {
			return "Could not find service", errors.New("exit status 113")
		}
		return "state = running", nil
	case strings.HasPrefix(cmd, "launchctl bootout"):
		if f.loaded {
			f.loaded, f.stopping = false, 3
		}
	case strings.HasPrefix(cmd, "launchctl bootstrap"):
		if f.stopping > 0 || f.bootstrapErr > 0 {
			f.bootstrapErr--
			return "Bootstrap failed: 5: Input/output error", errors.New("exit status 5")
		}
		f.loaded = true
	case strings.Contains(cmd, "is-active"):
		if f.loaded {
			return "active", nil
		}
		return "inactive", errors.New("exit status 3")
	case strings.Contains(cmd, "restart"):
		f.loaded = true
	case strings.Contains(cmd, "stop"), strings.Contains(cmd, "disable"):
		f.loaded = false
	}
	return "", nil
}

func fakeService(t *testing.T) *fakeServiceManager {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("macOS and Linux only")
	}
	f := &fakeServiceManager{}
	oldRun, oldFree := runSystem, portFree
	runSystem, portFree = f.run, func(string) bool { return true }
	t.Cleanup(func() { runSystem, portFree = oldRun, oldFree })
	return f
}

// healthServer answers /health like a running staqly and returns its port.
func healthServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_, _ = w.Write([]byte(`{"status":"ok","version":"9.9.9"}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	return port
}

func unitPath(home string) string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist")
	}
	return filepath.Join(home, ".config", "systemd", "user", systemdUnit)
}

// install writes the unit, loads it with the platform's service manager and waits for /health; uninstall
// removes it.
func TestServiceInstallUninstall(t *testing.T) {
	f := fakeService(t)
	home, port := t.TempDir(), healthServer(t)
	run := func(args ...string) result {
		return runCLI(t, "http://127.0.0.1:1", runOpts{env: map[string]string{"HOME": home}}, args...)
	}

	r := run("service", "install", "--expose", "--port", port)
	if r.code != 0 {
		t.Fatalf("install: %d %s %s", r.code, r.out, r.err)
	}
	unit := unitPath(home)
	b, err := os.ReadFile(unit)
	if err != nil || !strings.Contains(string(b), "--expose") {
		t.Fatalf("unit %s: %v %s", unit, err, b)
	}
	// The unit keeps the shell's settings (API keys, SMTP passwords...): only its owner may read it, also
	// when an earlier install left it world-readable.
	if st, err := os.Stat(unit); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("unit mode %v: %v", st.Mode().Perm(), err)
	}
	if err := os.Chmod(unit, 0o644); err != nil {
		t.Fatal(err)
	}
	if r := run("service", "install", "--port", port); r.code != 0 {
		t.Fatalf("reinstall: %d %s", r.code, r.err)
	}
	if st, err := os.Stat(unit); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("unit mode after reinstall %v: %v", st.Mode().Perm(), err)
	}
	if !strings.Contains(strings.Join(f.ran, "\n"), map[string]string{"darwin": "launchctl bootstrap", "linux": "systemctl --user enable"}[runtime.GOOS]) {
		t.Fatalf("not loaded: %v", f.ran)
	}
	if r := run("service", "status"); r.code != 0 || !strings.Contains(r.out, "staqly 9.9.9 answers on http://127.0.0.1:"+port) {
		t.Fatalf("status: %d %s", r.code, r.out)
	}
	if r := run("service", "uninstall"); r.code != 0 {
		t.Fatalf("uninstall: %d %s", r.code, r.err)
	}
	if _, err := os.Stat(unit); !os.IsNotExist(err) {
		t.Fatalf("unit still there: %v", err)
	}
	if f.loaded || f.stopping > 0 {
		t.Fatalf("uninstall returned before the job was gone: loaded=%v stopping=%d", f.loaded, f.stopping)
	}
	if r := run("service", "status"); r.code != 1 || !strings.Contains(r.out, "Not installed") {
		t.Fatalf("status after uninstall: %d %s", r.code, r.out)
	}
}

// Reinstalling over a running service: launchctl bootout returns while the old process is still shutting
// down, and a bootstrap then fails with "5: Input/output error", which left the service unloaded until the
// next login. install now waits for the old job to be gone (and retries the bootstrap).
func TestServiceReinstallWaitsForOldJob(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("launchd only")
	}
	f := fakeService(t)
	home, port := t.TempDir(), healthServer(t)
	run := func(args ...string) result {
		return runCLI(t, "http://127.0.0.1:1", runOpts{env: map[string]string{"HOME": home}}, args...)
	}
	if r := run("service", "install", "--port", port); r.code != 0 {
		t.Fatalf("install: %d %s", r.code, r.err)
	}
	f.ran = nil
	f.bootstrapErr = 1 // and launchd still busy for one more attempt
	if r := run("service", "install", "--port", port); r.code != 0 {
		t.Fatalf("reinstall: %d %s %s", r.code, r.out, r.err)
	}
	// The fake refuses every bootstrap while the old job is visible, as launchd does.
	if !f.loaded {
		t.Fatalf("not loaded after reinstall: %v", f.ran)
	}
}

// A port held by another program (e.g. the staqly Docker container on 3333) is refused before anything is
// loaded, instead of installing a service that fails and restarts forever.
func TestServiceInstallPortInUse(t *testing.T) {
	f := fakeService(t)
	portFree = func(addr string) bool { return !strings.HasSuffix(addr, ":3333") }
	home := t.TempDir()
	r := runCLI(t, "http://127.0.0.1:1", runOpts{env: map[string]string{"HOME": home}}, "service", "install")
	if r.code == 0 || !strings.Contains(r.err, "port 3333 is already in use") {
		t.Fatalf("install on a busy port: %d %s %s", r.code, r.out, r.err)
	}
	if _, err := os.Stat(unitPath(home)); !os.IsNotExist(err) || f.loaded {
		t.Fatalf("unit written / loaded on a busy port: %v %v", err, f.ran)
	}
}

// A service that never answers /health is reported as a failure, not "runs in the background".
func TestServiceInstallUnhealthy(t *testing.T) {
	fakeService(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0") // a port nothing answers HTTP on once closed
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	_ = ln.Close()
	r := runCLI(t, "http://127.0.0.1:1", runOpts{env: map[string]string{"HOME": t.TempDir()}}, "service", "install", "--port", port)
	if r.code == 0 || !strings.Contains(r.err, "did not answer on http://127.0.0.1:"+port+"/health") {
		t.Fatalf("install of an unhealthy service: %d %s %s", r.code, r.out, r.err)
	}
}

func TestPortFree(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if portFree(addr) {
		t.Fatalf("%s reported free while listened on", addr)
	}
	_ = ln.Close()
	if !portFree(addr) {
		t.Fatalf("%s reported busy after close", addr)
	}
}

func TestConnectLinkCommand(t *testing.T) {
	for _, tc := range []struct {
		data string
		port int
		want string
	}{
		{"", 8080, "staqly serve connect-link"},
		{"/tmp/x", 9080, "staqly serve --data /tmp/x --dashboard-port 9080 connect-link"},
		{"/Users/me/My Data", 8080, "staqly serve --data '/Users/me/My Data' connect-link"},
	} {
		if got := connectLinkCommand(tc.data, tc.port); got != tc.want {
			t.Errorf("connectLinkCommand(%q, %d) = %q, want %q", tc.data, tc.port, got, tc.want)
		}
	}
}

// Operator commands after `staqly serve` keep their own flags: `set-plan ... --until`
// used to fail with "flag provided but not defined: -until" (exit 64).
func TestServeOperatorFlagsPassThrough(t *testing.T) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	data := fs.String("data", "", "")
	expose := fs.Bool("expose", false, "")
	port := fs.Int("port", 3333, "")
	pass, err := parseKnownArgs(fs, []string{"--data", "/d", "set-plan", "ws_1", "pro", "--until", "2026-12-31", "--expose", "--port=4333", "--", "--data"})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(pass, " "); got != "set-plan ws_1 pro --until 2026-12-31 --data" {
		t.Errorf("passed through %q", got)
	}
	if *data != "/d" || !*expose || *port != 4333 {
		t.Errorf("serve flags: data=%q expose=%v port=%d", *data, *expose, *port)
	}

	// end to end against an empty data directory: the operator command runs (unknown workspace: exit 1)
	for _, k := range []string{"DATA_DIR", "ADDR", "DASHBOARD_ADDR", "PUBLIC_API_URL", "LOG_FORMAT"} {
		t.Setenv(k, "")
	}
	r := runCLI(t, "http://127.0.0.1:1", runOpts{}, "serve", "--data", t.TempDir(), "set-plan", "ws_nope", "pro", "--until", "2020-01-01")
	if r.code != 1 || strings.Contains(r.err, "flag provided but not defined") {
		t.Fatalf("set-plan --until: exit %d, stderr %q", r.code, r.err)
	}
}

// The service keeps every variable the server reads (internal/config and Service.Env in apps/api), except
// the ones serve sets from its own flags; and no prefix is left that nothing reads any more.
func TestServiceEnvCoversServerConfig(t *testing.T) {
	api := filepath.Join("..", "..", "..", "api", "internal")
	names := map[string]bool{}
	read := func(path string, re *regexp.Regexp) {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			names[m[1]] = true
		}
	}
	read(filepath.Join(api, "config", "config.go"), regexp.MustCompile(`"([A-Z][A-Z0-9]*(?:_[A-Z0-9]+)+_?)"`))
	files, _ := filepath.Glob(filepath.Join(api, "app", "*.go"))
	for _, f := range files {
		if !strings.HasSuffix(f, "_test.go") {
			read(f, regexp.MustCompile(`(?:s\.Env|num)\("([A-Z][A-Z0-9_]+)"`))
		}
	}
	// PROVIDER_<ID>_ENABLED and PROVIDER_<ID>_CLIENT_ID/SECRET are built from the provider id.
	read(filepath.Join(api, "app", "channels.go"), regexp.MustCompile(`return "(PROVIDER_)"`))
	if len(names) < 40 {
		t.Fatalf("found only %d variables: %v", len(names), names)
	}
	fromFlags := map[string]bool{"ADDR": true, "DASHBOARD_ADDR": true, "DASHBOARD_PORT": true, "DASHBOARD_EXPOSED": true, "DATA_DIR": true}
	// Test hooks pointing the alert senders at fakes.
	testOnly := map[string]bool{"SLACK_API_BASE": true, "DISCORD_API_BASE": true, "TELEGRAM_API_BASE": true}
	for n := range names {
		if fromFlags[n] || testOnly[n] {
			continue
		}
		if _, ok := serviceEnv([]string{n + "=v"})[n]; !ok {
			t.Errorf("the service drops %s", n)
		}
	}
	for _, p := range serviceEnvPrefixes {
		used := false
		for n := range names {
			used = used || strings.HasPrefix(n, p)
		}
		if !used {
			t.Errorf("prefix %s matches nothing the server reads", p)
		}
	}
}
