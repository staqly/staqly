package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---- service ----
//
// `staqly serve` as a background service of the signed-in user, started at login (macOS LaunchAgent) or boot
// (Linux systemd --user with lingering), restarted if it stops. No root, no Docker.

const (
	launchdLabel = "ai.staqly.serve"
	systemdUnit  = "staqly.service"
)

// runSystem runs launchctl / systemctl / loginctl (replaced in tests).
var runSystem = func(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// serviceEnvPrefixes: settings from the installing shell that the service keeps: every variable the server
// reads (apps/api internal/config and Service.Env; TestServiceEnvCoversServerConfig compares), except the
// ones serve sets from its flags (ADDR, DASHBOARD_ADDR, DASHBOARD_PORT, DASHBOARD_EXPOSED, DATA_DIR).
var serviceEnvPrefixes = []string{"STAQLY_", "PUBLIC_", "NOTIFY_", "WEBHOOK_", "LOG_", "SENTRY_", "MAIL_", "AI_",
	"RETENTION_", "LOCAL_", "RATE_LIMIT_", "PROVIDER_", "STORAGE_", "S3_", "GOOGLE_", "STRIPE_", "MAX_UPLOAD_MB",
	"UPLOAD_DIR", "DATABASE_PATH", "DATABASE_URL", "JOB_CONCURRENCY", "PROCESSING_TIMEOUT", "CORS_ALLOWED_ORIGINS",
	"TRUST_PROXY", "COOKIE_SECURE", "SESSION_TTL", "SETUP_TOKEN", "AUTH_DEV_SIGNIN", "TRIAL_PLAN"}

type serviceSpec struct {
	Exe  string
	Args []string // after "serve"
	Env  map[string]string
	Log  string // macOS log file

	Port          int    // API port, checked free before and healthy after the install
	DashboardAddr string // dashboard listen address, checked free before the install
}

// How long install / uninstall wait for the old process to exit (its graceful shutdown takes up to 20s, then
// the service manager kills it) and for the new one to answer /health.
var (
	serviceStopWait  = 30 * time.Second
	serviceStartWait = 20 * time.Second
)

// portFree reports whether addr can be listened on (replaced in tests).
var portFree = func(addr string) bool {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

func serviceCommand() *Command {
	install := leaf("install", "", "Run staqly serve in the background, started at login and restarted if it stops", 0, 0, func(fs *flag.FlagSet) runFunc {
		data := fs.String("data", "", "data directory (default ~/.local/share/staqly)")
		port := fs.Int("port", 3333, "API and MCP port")
		dashboardPort := fs.Int("dashboard-port", 8080, "dashboard port")
		expose := fs.Bool("expose", false, "serve the dashboard on all interfaces (phones, tablets)")
		return func(c *Ctx, _ []string) error {
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			if exe, err = filepath.EvalSymlinks(exe); err != nil {
				return err
			}
			args := []string{"--port", strconv.Itoa(*port), "--dashboard-port", strconv.Itoa(*dashboardPort)}
			dataDir := ""
			if *data != "" {
				if dataDir, err = filepath.Abs(*data); err != nil {
					return err
				}
				args = append(args, "--data", dataDir)
			}
			if *expose {
				args = append(args, "--expose")
			}
			dashHost := "127.0.0.1"
			if *expose {
				dashHost = ""
			}
			spec := serviceSpec{Exe: exe, Args: args, Env: serviceEnv(os.Environ()),
				Port: *port, DashboardAddr: net.JoinHostPort(dashHost, strconv.Itoa(*dashboardPort))}
			if err := installService(c, spec); err != nil {
				return err
			}
			fmt.Fprintf(c.Out, "Staqly runs in the background and starts with your computer.\n  dashboard  http://localhost:%d\n  API + MCP  http://localhost:%d (MCP at /mcp)\n",
				*dashboardPort, *port)
			if *expose {
				fmt.Fprintf(c.Out, "  The dashboard is open to your network: sign a browser in with `%s`.\n", connectLinkCommand(dataDir, *dashboardPort))
			}
			fmt.Fprintf(c.Out, "  `%s` prints the access token and a link that signs your browser in.\n",
				serveSubcommand(dataDir, *dashboardPort, "token"))
			fmt.Fprintln(c.Out, "  `staqly service status` checks it, `staqly service logs` shows its log, `staqly service uninstall` removes it.")
			return nil
		}
	}, "staqly service install", "staqly service install --expose", "LOG_LEVEL=debug staqly service install")
	install.Long = `Install staqly serve as a background service of your user:
macOS: a LaunchAgent (~/Library/LaunchAgents/` + launchdLabel + `.plist), started at login.
Linux: a systemd user unit (~/.config/systemd/user/` + systemdUnit + `), with lingering
enabled so it also starts at boot without a login.

It restarts if it stops. Settings in your environment when you run this (STAQLY_*,
TAGGER_*, PUBLIC_*, LOG_*...) are kept in the service. Running install again
replaces the service, e.g. after an upgrade or to change the flags.`

	uninstall := leaf("uninstall", "", "Stop the background service and remove it (your data is kept)", 0, 0, func(fs *flag.FlagSet) runFunc {
		return func(c *Ctx, _ []string) error {
			if err := uninstallService(c); err != nil {
				return err
			}
			fmt.Fprintln(c.Out, "Background service removed. Your data is kept.")
			return nil
		}
	}, "staqly service uninstall")

	status := leaf("status", "", "Show whether the background service is installed and running", 0, 0, func(fs *flag.FlagSet) runFunc {
		return func(c *Ctx, _ []string) error {
			out, err := serviceStatus(c)
			fmt.Fprintln(c.Out, out)
			return err
		}
	}, "staqly service status")

	logs := leaf("logs", "", "Print the background service's log (follow with -f)", 0, 0, func(fs *flag.FlagSet) runFunc {
		follow := fs.Bool("f", false, "keep printing new lines")
		return func(c *Ctx, _ []string) error {
			return serviceLogs(c, *follow)
		}
	}, "staqly service logs", "staqly service logs -f")

	return &Command{Name: "service", Short: "Run staqly in the background at login or boot (macOS, Linux)",
		Subs: []*Command{install, uninstall, status, logs}}
}

// serviceEnv keeps the staqly settings of an environment, sorted.
func serviceEnv(environ []string) map[string]string {
	out := map[string]string{}
	for _, kv := range environ {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || v == "" {
			continue
		}
		// PATH too: a service starts with a minimal one and would not find the user's AI CLI (claude,
		// codex) for summaries.
		if k == "PATH" {
			out[k] = v
			continue
		}
		for _, p := range serviceEnvPrefixes {
			if strings.HasPrefix(k, p) {
				out[k] = v
				break
			}
		}
	}
	return out
}

func homeDir(c *Ctx) (string, error) {
	if h := c.Env.Getenv("HOME"); h != "" {
		return h, nil
	}
	return os.UserHomeDir()
}

func servicePaths(c *Ctx) (unit, logFile string, err error) {
	home, err := homeDir(c)
	if err != nil {
		return "", "", err
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist"),
			filepath.Join(home, "Library", "Logs", "staqly", "staqly.log"), nil
	case "linux":
		return filepath.Join(home, ".config", "systemd", "user", systemdUnit), "", nil
	}
	return "", "", fmt.Errorf("staqly service supports macOS and Linux; on %s run `staqly serve` from your own service manager", runtime.GOOS)
}

func installService(c *Ctx, spec serviceSpec) error {
	unit, logFile, err := servicePaths(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(unit), 0o755); err != nil {
		return err
	}
	// Stop a running one first and wait until it has exited: it holds the ports, and launchd refuses to load
	// a job again while the old process is still shutting down ("Bootstrap failed: 5: Input/output error"),
	// which left the service unloaded until the next login.
	_, oldErr := os.Stat(unit)
	if err := stopService(c); err != nil {
		return err
	}
	for _, addr := range []string{net.JoinHostPort("127.0.0.1", strconv.Itoa(spec.Port)), spec.DashboardAddr} {
		if addr != "" && !portFree(addr) {
			if oldErr == nil {
				_ = loadService(c, unit) // put the previous service back, best effort
			}
			_, p, _ := net.SplitHostPort(addr)
			return fmt.Errorf("port %s is already in use by another program (another staqly, e.g. the Docker container?); choose free ones with --port and --dashboard-port", p)
		}
	}
	content := systemdUnitFile(spec)
	if runtime.GOOS == "darwin" {
		if err := os.MkdirAll(filepath.Dir(logFile), 0o755); err != nil {
			return err
		}
		spec.Log = logFile
		content = launchdPlist(spec)
	}
	if err := writePrivate(unit, content); err != nil {
		return err
	}
	if err := loadService(c, unit); err != nil {
		return err
	}
	if runtime.GOOS == "linux" {
		// Start at boot, not only while you are logged in; best effort (some systems need sudo for this).
		name := c.Env.Getenv("USER")
		if u, err := user.Current(); name == "" && err == nil {
			name = u.Username
		}
		if out, _ := runSystem("loginctl", "show-user", name, "--property=Linger", "--value"); out != "yes" {
			if out, err := runSystem("loginctl", "enable-linger"); err != nil {
				fmt.Fprintf(c.Stderr, "note: could not enable lingering (%s); staqly starts when you log in. Run `sudo loginctl enable-linger %s` to start it at boot.\n",
					firstNonEmpty(out, err.Error()), name)
			}
		}
	}
	// A service that exits at once (bad flag, unreadable data directory...) is restarted forever by the
	// service manager: say so now instead of reporting success.
	if _, err := waitHealthy(c, spec.Port, serviceStartWait); err != nil {
		return fmt.Errorf("the service is installed but staqly did not answer on http://127.0.0.1:%d/health within %s (%v); see `staqly service logs`",
			spec.Port, serviceStartWait, err)
	}
	return nil
}

func launchdTarget() string { return "gui/" + strconv.Itoa(os.Getuid()) + "/" + launchdLabel }

// loadService starts the unit file with the platform's service manager.
func loadService(c *Ctx, unit string) error {
	if runtime.GOOS == "darwin" {
		var out string
		var err error
		// Retried: launchd can still be tearing down the previous job for a moment after it exited.
		for i := 0; i < 5; i++ {
			if out, err = runSystem("launchctl", "bootstrap", "gui/"+strconv.Itoa(os.Getuid()), unit); err == nil {
				return nil
			}
			if err := c.Env.sleep(c, time.Second); err != nil {
				return err
			}
		}
		return fmt.Errorf("launchctl bootstrap: %v: %s", err, out)
	}
	for _, args := range [][]string{{"--user", "daemon-reload"}, {"--user", "enable", systemdUnit}, {"--user", "restart", systemdUnit}} {
		if out, err := runSystem("systemctl", args...); err != nil {
			return fmt.Errorf("systemctl %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
	return nil
}

// stopService stops the service if it runs and waits until its process has exited. launchctl bootout returns
// while the process is still in its graceful shutdown; systemctl stop waits by itself.
func stopService(c *Ctx) error {
	if runtime.GOOS != "darwin" {
		_, _ = runSystem("systemctl", "--user", "stop", systemdUnit)
		return nil
	}
	if _, err := runSystem("launchctl", "print", launchdTarget()); err != nil {
		return nil // not loaded
	}
	_, _ = runSystem("launchctl", "bootout", launchdTarget())
	deadline := c.Env.now().Add(serviceStopWait)
	for {
		if _, err := runSystem("launchctl", "print", launchdTarget()); err != nil {
			return nil
		}
		if c.Env.now().After(deadline) {
			return fmt.Errorf("the running staqly service did not stop within %s; check `launchctl print %s`", serviceStopWait, launchdTarget())
		}
		if err := c.Env.sleep(c, 250*time.Millisecond); err != nil {
			return err
		}
	}
}

// waitHealthy polls the API's /health until it answers 200, returning the version it reports.
func waitHealthy(c *Ctx, port int, wait time.Duration) (string, error) {
	deadline := c.Env.now().Add(wait)
	for {
		v, err := apiHealth(c, port)
		if err == nil || c.Env.now().After(deadline) {
			return v, err
		}
		if err := c.Env.sleep(c, 500*time.Millisecond); err != nil {
			return "", err
		}
	}
}

func apiHealth(c *Ctx, port int) (string, error) {
	ctx, cancel := context.WithTimeout(c, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", "http://"+net.JoinHostPort("127.0.0.1", strconv.Itoa(port))+"/health", nil)
	if err != nil {
		return "", err
	}
	resp, err := c.Env.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var body struct{ Version string }
	_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return body.Version, nil
}

// unitPort is the API port an installed unit file runs staqly with.
var unitPortRe = regexp.MustCompile(`--port(?:</string>\s*<string>|"\s+"|\s+)(\d+)`)

func unitPort(content string) int {
	if m := unitPortRe.FindStringSubmatch(content); m != nil {
		if p, err := strconv.Atoi(m[1]); err == nil {
			return p
		}
	}
	return 3333
}

// writePrivate writes a unit readable by its owner only: it carries the installing shell's settings, which
// may include secrets (TAGGER_API_KEY, SMTP passwords, NOTIFY_SECRET_KEY...). The chmod also fixes a file
// left world-readable by an earlier install.
func writePrivate(path, content string) error {
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func uninstallService(c *Ctx) error {
	unit, _, err := servicePaths(c)
	if err != nil {
		return err
	}
	if runtime.GOOS == "darwin" {
		if err := stopService(c); err != nil {
			return err
		}
	} else {
		_, _ = runSystem("systemctl", "--user", "disable", "--now", systemdUnit)
	}
	if err := os.Remove(unit); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if runtime.GOOS == "linux" {
		_, _ = runSystem("systemctl", "--user", "daemon-reload")
	}
	return nil
}

func serviceStatus(c *Ctx) (string, error) {
	unit, logFile, err := servicePaths(c)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(unit)
	if err != nil {
		return "Not installed. Run `staqly service install`.", exitWith(1)
	}
	logHint := "journalctl --user -u staqly"
	if runtime.GOOS == "darwin" {
		logHint = logFile
		out, err := runSystem("launchctl", "print", launchdTarget())
		if err != nil {
			return "Installed (" + unit + ") but not loaded: run `staqly service install` again. Log: " + logHint, exitWith(1)
		}
		if !strings.Contains(out, "state = running") {
			return "Installed but not running (it is restarted every few seconds while it fails). Log: " + logHint, exitWith(1)
		}
	} else if out, err := runSystem("systemctl", "--user", "is-active", systemdUnit); err != nil || out != "active" {
		return "Installed but not running (" + out + "). Log: " + logHint, exitWith(1)
	}
	port := unitPort(string(b))
	v, err := apiHealth(c, port)
	if err != nil {
		return fmt.Sprintf("Running (%s), but the API on http://127.0.0.1:%d does not answer (%v). Log: %s", unit, port, err, logHint), exitWith(1)
	}
	return fmt.Sprintf("Running (%s), staqly %s answers on http://127.0.0.1:%d. Log: %s", unit, v, port, logHint), nil
}

func serviceLogs(c *Ctx, follow bool) error {
	_, logFile, err := servicePaths(c)
	if err != nil {
		return err
	}
	var cmd *exec.Cmd
	switch {
	case runtime.GOOS == "darwin" && follow:
		cmd = exec.Command("tail", "-n", "100", "-f", logFile)
	case runtime.GOOS == "darwin":
		cmd = exec.Command("tail", "-n", "200", logFile)
	case follow:
		cmd = exec.Command("journalctl", "--user", "-u", "staqly", "-n", "100", "-f")
	default:
		cmd = exec.Command("journalctl", "--user", "-u", "staqly", "-n", "200", "--no-pager")
	}
	cmd.Stdout, cmd.Stderr = c.Out, c.Stderr
	return cmd.Run()
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// launchdPlist: RunAtLoad starts it at login, KeepAlive restarts it whenever it exits.
func launchdPlist(s serviceSpec) string {
	var b strings.Builder
	esc := html.EscapeString
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>` + launchdLabel + `</string>
  <key>ProgramArguments</key>
  <array>
    <string>` + esc(s.Exe) + `</string>
    <string>serve</string>
`)
	for _, a := range s.Args {
		b.WriteString("    <string>" + esc(a) + "</string>\n")
	}
	b.WriteString("  </array>\n")
	if len(s.Env) > 0 {
		b.WriteString("  <key>EnvironmentVariables</key>\n  <dict>\n")
		for _, k := range sortedKeys(s.Env) {
			b.WriteString("    <key>" + esc(k) + "</key><string>" + esc(s.Env[k]) + "</string>\n")
		}
		b.WriteString("  </dict>\n")
	}
	b.WriteString(`  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>10</integer>
  <key>StandardOutPath</key><string>` + esc(s.Log) + `</string>
  <key>StandardErrorPath</key><string>` + esc(s.Log) + `</string>
</dict>
</plist>
`)
	return b.String()
}

// systemdQuote quotes one argument for ExecStart, where systemd expands $VAR (so "$" is written "$$").
func systemdQuote(s string) string {
	return systemdEnvQuote(strings.ReplaceAll(s, "$", "$$"))
}

// systemdEnvQuote quotes one Environment= assignment. "$" has no special meaning there ("$$" would stay two
// dollars, breaking e.g. a password), but specifiers do ("%" is written "%%"). Line breaks are escaped too: a
// raw one would end the directive and start another (e.g. an ExecStartPre= smuggled in through a value).
func systemdEnvQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%", "\n", `\n`, "\r", `\r`).Replace(s) + `"`
}

func systemdUnitFile(s serviceSpec) string {
	var b strings.Builder
	b.WriteString("[Unit]\nDescription=staqly (API, MCP and dashboard)\nAfter=network-online.target\nWants=network-online.target\n\n[Service]\n")
	exec := []string{systemdQuote(s.Exe), "serve"}
	for _, a := range s.Args {
		exec = append(exec, systemdQuote(a))
	}
	b.WriteString("ExecStart=" + strings.Join(exec, " ") + "\n")
	for _, k := range sortedKeys(s.Env) {
		b.WriteString("Environment=" + systemdEnvQuote(k+"="+s.Env[k]) + "\n")
	}
	b.WriteString("Restart=always\nRestartSec=5\n\n[Install]\nWantedBy=default.target\n")
	return b.String()
}
