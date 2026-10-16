package cli

import (
	"errors"
	"flag"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
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

// serviceEnvPrefixes: settings from the installing shell that the service keeps (models, URLs, logging...).
var serviceEnvPrefixes = []string{"STAQLY_", "TAGGER_", "PUBLIC_", "NOTIFY_", "WEBHOOK_", "WATCHER_", "LOG_",
	"SENTRY_", "SMTP_", "RETENTION_", "METRICS_", "LOCAL_", "MAX_UPLOAD_MB"}

type serviceSpec struct {
	Exe  string
	Args []string // after "serve"
	Env  map[string]string
	Log  string // macOS log file
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
			spec := serviceSpec{Exe: exe, Args: args, Env: serviceEnv(os.Environ())}
			if err := installService(c, spec); err != nil {
				return err
			}
			fmt.Fprintf(c.Out, "staqly runs in the background and starts with your computer.\n  dashboard  http://localhost:%d\n  API + MCP  http://localhost:%d (MCP at /mcp)\n",
				*dashboardPort, *port)
			if *expose {
				fmt.Fprintf(c.Out, "  The dashboard is open to your network: sign a browser in with `%s`.\n", connectLinkCommand(dataDir, *dashboardPort))
			}
			fmt.Fprintln(c.Out, "  `staqly service status` checks it, `staqly service logs` shows its log, `staqly service uninstall` removes it.")
			return nil
		}
	}, "staqly service install", "staqly service install --expose", "TAGGER_URL=http://localhost:11434/v1 staqly service install")
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
	switch runtime.GOOS {
	case "darwin":
		if err := os.MkdirAll(filepath.Dir(logFile), 0o755); err != nil {
			return err
		}
		spec.Log = logFile
		if err := writePrivate(unit, launchdPlist(spec)); err != nil {
			return err
		}
		domain := "gui/" + strconv.Itoa(os.Getuid())
		_, _ = runSystem("launchctl", "bootout", domain+"/"+launchdLabel) // replace a running one
		if out, err := runSystem("launchctl", "bootstrap", domain, unit); err != nil {
			return fmt.Errorf("launchctl bootstrap: %v: %s", err, out)
		}
	default:
		if err := writePrivate(unit, systemdUnitFile(spec)); err != nil {
			return err
		}
		for _, args := range [][]string{{"--user", "daemon-reload"}, {"--user", "enable", systemdUnit}, {"--user", "restart", systemdUnit}} {
			if out, err := runSystem("systemctl", args...); err != nil {
				return fmt.Errorf("systemctl %s: %v: %s", strings.Join(args, " "), err, out)
			}
		}
		// Start at boot, not only while you are logged in; best effort (some systems need sudo for this).
		if out, err := runSystem("loginctl", "enable-linger"); err != nil {
			fmt.Fprintf(c.Stderr, "note: could not enable lingering (%s); staqly starts when you log in. Run `sudo loginctl enable-linger %s` to start it at boot.\n",
				firstNonEmpty(out, err.Error()), c.Env.Getenv("USER"))
		}
	}
	return nil
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
	switch runtime.GOOS {
	case "darwin":
		_, _ = runSystem("launchctl", "bootout", "gui/"+strconv.Itoa(os.Getuid())+"/"+launchdLabel)
	default:
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
	if _, err := os.Stat(unit); err != nil {
		return "Not installed. Run `staqly service install`.", exitWith(1)
	}
	if runtime.GOOS == "darwin" {
		out, err := runSystem("launchctl", "print", "gui/"+strconv.Itoa(os.Getuid())+"/"+launchdLabel)
		if err != nil || !strings.Contains(out, "state = running") {
			return "Installed but not running. Log: " + logFile, exitWith(1)
		}
		return "Running (" + unit + "). Log: " + logFile, nil
	}
	out, err := runSystem("systemctl", "--user", "is-active", systemdUnit)
	if err != nil || out != "active" {
		return "Installed but not running (" + out + "). Log: journalctl --user -u staqly", exitWith(1)
	}
	return "Running (" + unit + "). Log: journalctl --user -u staqly", nil
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

// systemdQuote quotes one argument for ExecStart / Environment. Line breaks are escaped too: a raw one would
// end the directive and start another (e.g. an ExecStartPre= smuggled in through an environment value).
func systemdQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%", "$", "$$", "\n", `\n`, "\r", `\r`).Replace(s) + `"`
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
		b.WriteString("Environment=" + systemdQuote(k+"="+s.Env[k]) + "\n")
	}
	b.WriteString("Restart=always\nRestartSec=5\n\n[Install]\nWantedBy=default.target\n")
	return b.String()
}
