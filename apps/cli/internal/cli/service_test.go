package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestServiceUnitFiles(t *testing.T) {
	spec := serviceSpec{Exe: "/opt/my apps/staqly", Args: []string{"--port", "3333", "--expose"},
		Env: map[string]string{"TAGGER_URL": "http://localhost:11434/v1", "STAQLY_X": `a"b&c`}, Log: "/tmp/s.log"}
	plist := launchdPlist(spec)
	for _, want := range []string{"<string>/opt/my apps/staqly</string>", "<string>serve</string>", "<string>--expose</string>",
		"<key>TAGGER_URL</key><string>http://localhost:11434/v1</string>", "a&#34;b&amp;c", "<key>KeepAlive</key><true/>", "<key>RunAtLoad</key><true/>"} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist lacks %q:\n%s", want, plist)
		}
	}
	unit := systemdUnitFile(spec)
	for _, want := range []string{`ExecStart="/opt/my apps/staqly" serve "--port" "3333" "--expose"`, `Environment="STAQLY_X=a\"b&c"`,
		"Restart=always", "WantedBy=default.target"} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit lacks %q:\n%s", want, unit)
		}
	}
	if u := systemdUnitFile(serviceSpec{Exe: "/s", Env: map[string]string{"STAQLY_X": "a\nExecStartPre=/bin/evil"}}); strings.Contains(u, "\nExecStartPre") {
		t.Fatalf("line break not escaped:\n%s", u)
	}
	env := serviceEnv([]string{"TAGGER_URL=x", "HOME=/h", "PATH=/bin", "STAQLY_MODE=local", "TAGGER_API_KEY="})
	// PATH is kept so the service finds the AI CLI (claude, codex) for summaries; HOME is not.
	if len(env) != 3 || env["TAGGER_URL"] != "x" || env["STAQLY_MODE"] != "local" || env["PATH"] != "/bin" {
		t.Fatalf("env %v", env)
	}
}

// install writes the unit and loads it with the platform's service manager; uninstall removes it.
func TestServiceInstallUninstall(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("macOS and Linux only")
	}
	var ran []string
	old := runSystem
	runSystem = func(name string, args ...string) (string, error) {
		ran = append(ran, name+" "+strings.Join(args, " "))
		if name == "systemctl" && len(args) > 1 && args[1] == "is-active" {
			return "active", nil
		}
		return "state = running", nil
	}
	t.Cleanup(func() { runSystem = old })
	home := t.TempDir()

	r := runCLI(t, "http://127.0.0.1:1", runOpts{env: map[string]string{"HOME": home}}, "service", "install", "--expose")
	if r.code != 0 {
		t.Fatalf("install: %d %s %s", r.code, r.out, r.err)
	}
	unit := filepath.Join(home, ".config", "systemd", "user", systemdUnit)
	if runtime.GOOS == "darwin" {
		unit = filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist")
	}
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
	if r := runCLI(t, "http://127.0.0.1:1", runOpts{env: map[string]string{"HOME": home}}, "service", "install"); r.code != 0 {
		t.Fatalf("reinstall: %d %s", r.code, r.err)
	}
	if st, err := os.Stat(unit); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("unit mode after reinstall %v: %v", st.Mode().Perm(), err)
	}
	if !strings.Contains(strings.Join(ran, "\n"), map[string]string{"darwin": "launchctl bootstrap", "linux": "systemctl --user enable"}[runtime.GOOS]) {
		t.Fatalf("not loaded: %v", ran)
	}
	if r := runCLI(t, "http://127.0.0.1:1", runOpts{env: map[string]string{"HOME": home}}, "service", "status"); r.code != 0 || !strings.Contains(r.out, "Running") {
		t.Fatalf("status: %d %s", r.code, r.out)
	}
	if r := runCLI(t, "http://127.0.0.1:1", runOpts{env: map[string]string{"HOME": home}}, "service", "uninstall"); r.code != 0 {
		t.Fatalf("uninstall: %d %s", r.code, r.err)
	}
	if _, err := os.Stat(unit); !os.IsNotExist(err) {
		t.Fatalf("unit still there: %v", err)
	}
	if r := runCLI(t, "http://127.0.0.1:1", runOpts{env: map[string]string{"HOME": home}}, "service", "status"); r.code != 1 || !strings.Contains(r.out, "Not installed") {
		t.Fatalf("status after uninstall: %d %s", r.code, r.out)
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
