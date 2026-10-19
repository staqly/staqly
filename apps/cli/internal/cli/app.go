// Package cli implements the user-facing `staqly` command line client. It
// talks to the staqly API over HTTP only.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
)

// Exit codes (documented at https://docs.staqly.ai/cli).
const (
	ExitOK          = 0   // success; for --wait: published
	ExitError       = 1   // API, network or other runtime error
	ExitFailed      = 2   // --wait: the post failed on a target
	ExitTimeout     = 3   // --wait: timed out
	ExitOther       = 4   // --wait: the post settled in another status
	ExitUsage       = 64  // bad flags or arguments
	ExitInterrupted = 130 // Ctrl-C
)

// BuildInfo is injected by the linker (see Makefile / .goreleaser.yaml).
type BuildInfo struct {
	Version, Commit, Date string
}

// Env holds everything the CLI touches in the outside world so tests can
// replace it.
type Env struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	Getenv         func(string) string
	StdinTTY       bool
	StdoutTTY      bool
	StdinFd        int
	HTTP           *http.Client
	Build          BuildInfo
	// Sleep waits for d or until ctx is done (replaced in tests).
	Sleep func(ctx context.Context, d time.Duration) error
	// Now is the wall clock (replaced in tests).
	Now func() time.Time
}

// Main is the process entry point.
func Main(build BuildInfo, args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	env := &Env{
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, Getenv: os.Getenv,
		StdinTTY:  term.IsTerminal(int(os.Stdin.Fd())),
		StdoutTTY: term.IsTerminal(int(os.Stdout.Fd())),
		StdinFd:   int(os.Stdin.Fd()),
		HTTP:      &http.Client{Timeout: 5 * time.Minute},
		Build:     build,
	}
	return Run(ctx, env, args)
}

func (e *Env) sleep(ctx context.Context, d time.Duration) error {
	if e.Sleep != nil {
		return e.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (e *Env) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// ---- command tree ----

type runFunc func(c *Ctx, args []string) error

// Command is a node in the command tree. Leaf commands have Setup, which
// registers flags and returns the function to run.
type Command struct {
	Name     string
	Aliases  []string
	Args     string // usage of positional args, e.g. "<id>"
	Short    string
	Long     string
	Examples []string
	MinArgs  int
	MaxArgs  int // -1 = unlimited
	Setup    func(fs *flag.FlagSet) runFunc
	Subs     []*Command
	Hidden   bool
	// PassUnknownFlags: flags the command does not define are passed through
	// with the positional arguments instead of being an error. For `serve`,
	// whose operator commands have flags of their own (set-plan --until).
	PassUnknownFlags bool
	parent           *Command
}

func (c *Command) path() string {
	if c.parent == nil {
		return c.Name
	}
	return c.parent.path() + " " + c.Name
}

func (c *Command) find(name string) *Command {
	for _, s := range c.Subs {
		if s.Name == name {
			return s
		}
		for _, a := range s.Aliases {
			if a == name {
				return s
			}
		}
	}
	return nil
}

func link(c *Command) *Command {
	for _, s := range c.Subs {
		s.parent = c
		link(s)
	}
	return c
}

// ---- global flags ----

type globals struct {
	url, key, profile string
	json, noColor     bool
	help              bool
}

var globalValueFlags = map[string]bool{"url": true, "api-url": true, "key": true, "profile": true}

// addURLAlias registers --url as an alias of --api-url unless the command
// uses --url for something else (e.g. a webhook's URL).
func addURLAlias(fs *flag.FlagSet, g *globals) {
	if fs.Lookup("url") == nil {
		fs.StringVar(&g.url, "url", "", "API base URL (alias of --api-url)")
	}
}

func addGlobals(fs *flag.FlagSet) *globals {
	g := &globals{}
	fs.StringVar(&g.url, "api-url", "", "API base URL (env STAQLY_API_URL, default "+defaultURL+")")
	fs.StringVar(&g.key, "key", "", "API key stk_..., or the Community Edition's access token (env STAQLY_API_KEY). Every server needs one; for staqly serve on this computer the CLI reads the token from its data folder by itself")
	fs.StringVar(&g.profile, "profile", "", "config profile to use (env STAQLY_PROFILE)")
	fs.BoolVar(&g.json, "json", false, "machine-readable JSON output")
	fs.BoolVar(&g.noColor, "no-color", false, "disable colors (also NO_COLOR=1)")
	fs.BoolVar(&g.help, "help", false, "show help")
	fs.BoolVar(&g.help, "h", false, "show help")
	return g
}

var globalNames = map[string]bool{"api-url": true, "key": true, "profile": true, "json": true, "no-color": true, "help": true, "h": true}

// parseArgs parses flags interspersed with positional arguments.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return pos, nil
		}
		consumed := len(args) - len(rest)
		if consumed > 0 && args[consumed-1] == "--" {
			return append(pos, rest...), nil
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
}

// parseKnownArgs parses the flags fs defines wherever they appear and returns
// everything else in order: positional arguments and unknown flags with their
// values (Command.PassUnknownFlags). After "--" nothing is parsed.
func parseKnownArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var known, pass []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pass = append(pass, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			pass = append(pass, a)
			continue
		}
		name, _, hasValue := strings.Cut(strings.TrimLeft(a, "-"), "=")
		f := fs.Lookup(name)
		if f == nil {
			pass = append(pass, a)
			continue
		}
		known = append(known, a)
		if b, ok := f.Value.(interface{ IsBoolFlag() bool }); !hasValue && !(ok && b.IsBoolFlag()) && i+1 < len(args) {
			known = append(known, args[i+1])
			i++
		}
	}
	if err := fs.Parse(known); err != nil {
		return nil, err
	}
	return pass, nil
}

// route walks the tree; flags seen before the leaf command must be globals.
func route(root *Command, args []string) (*Command, []string, error) {
	cmd := root
	var rest []string
	for i := 0; i < len(args); i++ {
		if len(cmd.Subs) == 0 {
			rest = append(rest, args[i:]...)
			break
		}
		a := args[i]
		if a == "--" {
			rest = append(rest, args[i:]...)
			break
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			rest = append(rest, a)
			name := strings.TrimLeft(a, "-")
			if !strings.Contains(name, "=") && globalValueFlags[name] && i+1 < len(args) {
				rest = append(rest, args[i+1])
				i++
			}
			continue
		}
		sub := cmd.find(a)
		if sub == nil {
			if cmd.Setup != nil && cmd != root {
				rest = append(rest, args[i:]...)
				break
			}
			return cmd, nil, usageErrf("unknown command %q for %q", a, cmd.path())
		}
		cmd = sub
	}
	return cmd, rest, nil
}

// ---- errors ----

type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func usageErrf(format string, a ...any) error { return &usageError{fmt.Sprintf(format, a...)} }

// exitErr carries a specific exit code; quiet means the message was already shown.
type exitErr struct {
	code int
	msg  string
}

func (e *exitErr) Error() string { return e.msg }

func exitWith(code int) error { return &exitErr{code: code} }

// ---- context passed to commands ----

// Ctx is what a command sees: output streams, global options and lazily
// resolved config / client.
type Ctx struct {
	context.Context
	Env    *Env
	Out    io.Writer
	Stderr io.Writer
	JSON   bool
	Color  bool
	cmd    *Command
	g      *globals

	cfgPath string
	cfg     *Config
	client  *Client
	profile string
}

func (c *Ctx) config() (*Config, error) {
	if c.cfg != nil {
		return c.cfg, nil
	}
	cfg, err := loadConfig(c.cfgPath)
	if err != nil {
		return nil, err
	}
	if len(cfg.Profiles) > 0 && insecurePerms(c.cfgPath) {
		fmt.Fprintf(c.Stderr, "warning: %s is readable by other users; run chmod 600 on it\n", c.cfgPath)
	}
	c.cfg = cfg
	return cfg, nil
}

// profileName is --profile, STAQLY_PROFILE, the config's current profile or "default".
func (c *Ctx) profileName(cfg *Config) string {
	switch {
	case c.g.profile != "":
		return c.g.profile
	case c.Env.Getenv("STAQLY_PROFILE") != "":
		return c.Env.Getenv("STAQLY_PROFILE")
	case cfg != nil && cfg.Current != "":
		return cfg.Current
	}
	return defaultProfile
}

// Client resolves URL and key: flag > env > profile > default.
func (c *Ctx) Client() (*Client, error) {
	if c.client != nil {
		return c.client, nil
	}
	cfg, err := c.config()
	if err != nil {
		return nil, err
	}
	name := c.profileName(cfg)
	prof := cfg.Profiles[name]
	explicit := c.g.profile != "" || c.Env.Getenv("STAQLY_PROFILE") != ""
	if prof == nil && explicit {
		return nil, usageErrf("profile %q not found in %s (see `staqly profiles`)", name, c.cfgPath)
	}
	if prof == nil {
		prof = &Profile{}
	}
	base := firstNonEmpty(c.g.url, c.Env.Getenv("STAQLY_API_URL"), prof.URL, defaultURL)
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		return nil, usageErrf("API URL must start with http:// or https:// (got %q)", base)
	}
	key := firstNonEmpty(c.g.key, c.Env.Getenv("STAQLY_API_KEY"))
	if key == "" && prof.Key != "" {
		// A stored key belongs to the profile's server only: never send it to
		// a URL given by --api-url / STAQLY_API_URL.
		if sameAPIURL(base, firstNonEmpty(prof.URL, defaultURL)) {
			key = prof.Key
		} else {
			fmt.Fprintf(c.Stderr, "note: not sending the API key of profile %q: the API URL was overridden (%s instead of %s).\n"+
				"      Pass --key or STAQLY_API_KEY to authenticate against %s.\n", name, clean(base), clean(prof.URL), clean(base))
		}
	}
	if key == "" && !insecureKeyURL(base) && isLocalURL(base) {
		// The local server on this computer: its access token is in the data folder (staqly serve).
		key = localAccessToken(c)
	}
	if key != "" && insecureKeyURL(base) {
		fmt.Fprintf(c.Stderr, "warning: sending an API key over plain http to %s; anyone on the network path can read it. Use https://.\n", clean(base))
	}
	c.profile = name
	c.client = &Client{BaseURL: strings.TrimRight(base, "/"), Key: key, HTTP: c.Env.HTTP,
		UserAgent: "staqly-cli/" + c.Env.Build.Version, Sanitize: !c.JSON}
	return c.client, nil
}

// sameAPIURL compares two API base URLs by scheme, host, port (defaults
// filled in) and path, ignoring case in scheme/host and trailing slashes.
func sameAPIURL(a, b string) bool {
	ua, errA := url.Parse(strings.TrimRight(a, "/"))
	ub, errB := url.Parse(strings.TrimRight(b, "/"))
	if errA != nil || errB != nil {
		return false
	}
	norm := func(u *url.URL) string {
		scheme := strings.ToLower(u.Scheme)
		host, port := strings.ToLower(u.Hostname()), u.Port()
		if port == "" {
			port = map[string]string{"http": "80", "https": "443"}[scheme]
		}
		return scheme + "://" + net.JoinHostPort(host, port) + strings.TrimRight(u.EscapedPath(), "/")
	}
	return norm(ua) == norm(ub)
}

// isLocalURL reports whether base names this computer (localhost, a loopback address).
func isLocalURL(base string) bool {
	u, err := url.Parse(base)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// defaultDataDir is where `staqly serve` keeps its data: STAQLY_DATA_DIR, else ~/.local/share/staqly.
func defaultDataDir(c *Ctx) (string, error) {
	if d := c.Env.Getenv("STAQLY_DATA_DIR"); d != "" {
		return d, nil
	}
	home, err := homeDir(c)
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "staqly"), nil
}

// localTokenFile: the token's file in the data folder (the server's app.LocalTokenFile).
const localTokenFile = "access_token"

// localAccessToken reads the access token `staqly serve` keeps in its data folder ("" when there is none,
// e.g. the server runs in Docker: then pass it with STAQLY_API_KEY or `staqly login`).
func localAccessToken(c *Ctx) string {
	dir, err := defaultDataDir(c)
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(dir, localTokenFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// insecureKeyURL reports whether base is plain http to a non-loopback host.
func insecureKeyURL(base string) bool {
	u, err := url.Parse(base)
	if err != nil || !strings.EqualFold(u.Scheme, "http") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return false
	}
	ip := net.ParseIP(host)
	return ip == nil || !ip.IsLoopback()
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// ---- Run ----

// Run executes one CLI invocation and returns the exit code.
func Run(ctx context.Context, env *Env, args []string) int {
	if env.Getenv == nil {
		env.Getenv = func(string) string { return "" }
	}
	if env.Build.Version == "" {
		env.Build.Version = "dev"
	}
	root := link(rootCommand())
	cmd, rest, err := route(root, args)
	if err != nil {
		fmt.Fprintf(env.Stderr, "error: %v\nRun 'staqly --help' for usage.\n", err)
		return ExitUsage
	}

	fs := flag.NewFlagSet(cmd.path(), flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	g := addGlobals(fs)
	var run runFunc
	if cmd.Setup != nil {
		run = cmd.Setup(fs)
	}
	addURLAlias(fs, g)
	parse := parseArgs
	if cmd.PassUnknownFlags {
		parse = parseKnownArgs
	}
	pos, err := parse(fs, rest)
	if errors.Is(err, flag.ErrHelp) || (err == nil && g.help) {
		printHelp(env.Stdout, cmd, fs)
		return ExitOK
	}
	if err != nil {
		fmt.Fprintf(env.Stderr, "error: %v\nRun '%s --help' for usage.\n", err, cmd.path())
		return ExitUsage
	}
	if run == nil {
		if len(pos) > 0 {
			fmt.Fprintf(env.Stderr, "error: unknown command %q for %q\n", pos[0], cmd.path())
			return ExitUsage
		}
		printHelp(env.Stdout, cmd, fs)
		return ExitOK
	}
	if len(pos) < cmd.MinArgs || (cmd.MaxArgs >= 0 && len(pos) > cmd.MaxArgs) {
		fmt.Fprintf(env.Stderr, "error: %s expects %s\nRun '%s --help' for usage.\n", cmd.path(), argsDesc(cmd), cmd.path())
		return ExitUsage
	}

	c := &Ctx{
		Context: ctx, Env: env, Out: env.Stdout, Stderr: env.Stderr, JSON: g.json, cmd: cmd, g: g,
		cfgPath: configPath(env.Getenv),
	}
	c.Color = env.StdoutTTY && !g.noColor && env.Getenv("NO_COLOR") == "" && env.Getenv("TERM") != "dumb"
	return finish(c, run(c, pos))
}

func argsDesc(cmd *Command) string {
	if cmd.Args == "" {
		return "no arguments"
	}
	return "arguments " + cmd.Args
}

func finish(c *Ctx, err error) int {
	if err == nil {
		return ExitOK
	}
	var ee *exitErr
	if errors.As(err, &ee) {
		if ee.msg != "" {
			fmt.Fprintf(c.Stderr, "error: %s\n", clean(ee.msg))
		}
		return ee.code
	}
	if errors.Is(err, context.Canceled) {
		fmt.Fprintln(c.Stderr, "interrupted")
		return ExitInterrupted
	}
	var ue *usageError
	if errors.As(err, &ue) {
		fmt.Fprintf(c.Stderr, "error: %s\nRun '%s --help' for usage.\n", clean(ue.msg), c.cmd.path())
		return ExitUsage
	}
	if c.JSON {
		printJSONError(c.Stderr, err)
	} else {
		fmt.Fprintf(c.Stderr, "%s %s\n", c.paint(red, "error:"), clean(friendlyError(err)))
	}
	return ExitError
}

// ---- help ----

func printHelp(w io.Writer, cmd *Command, fs *flag.FlagSet) {
	if cmd.Long != "" {
		fmt.Fprintln(w, strings.TrimSpace(cmd.Long))
	} else if cmd.Short != "" {
		fmt.Fprintln(w, cmd.Short)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	if cmd.Setup != nil {
		fmt.Fprintf(w, "  %s%s [flags]\n", cmd.path(), prefixSpace(cmd.Args))
	}
	if len(cmd.Subs) > 0 {
		fmt.Fprintf(w, "  %s <command> [flags]\n", cmd.path())
		fmt.Fprintln(w, "\nCommands:")
		width := 0
		for _, s := range cmd.Subs {
			width = max(width, len(s.Name))
		}
		for _, s := range cmd.Subs {
			if !s.Hidden {
				fmt.Fprintf(w, "  %-*s  %s\n", width, s.Name, s.Short)
			}
		}
	}
	local, global := flagLines(fs)
	if len(local) > 0 {
		fmt.Fprintln(w, "\nFlags:")
		for _, l := range local {
			fmt.Fprintln(w, l)
		}
	}
	fmt.Fprintln(w, "\nGlobal flags:")
	for _, l := range global {
		fmt.Fprintln(w, l)
	}
	if len(cmd.Examples) > 0 {
		fmt.Fprintln(w, "\nExamples:")
		for _, e := range cmd.Examples {
			fmt.Fprintf(w, "  %s\n", e)
		}
	}
	if len(cmd.Subs) > 0 {
		fmt.Fprintf(w, "\nRun '%s <command> --help' for details.\n", cmd.path())
	}
}

func prefixSpace(s string) string {
	if s == "" {
		return ""
	}
	return " " + s
}

func flagLines(fs *flag.FlagSet) (local, global []string) {
	type row struct{ name, usage string }
	var lr, gr []row
	fs.VisitAll(func(f *flag.Flag) {
		if f.Name == "h" {
			return
		}
		typ, usage := flag.UnquoteUsage(f)
		name := "--" + f.Name
		if len(f.Name) == 1 {
			name = "-" + f.Name
		}
		if typ != "" {
			name += " " + typ
		}
		if f.DefValue != "" && f.DefValue != "false" && f.DefValue != "0" && f.DefValue != "[]" {
			usage += fmt.Sprintf(" (default %s)", f.DefValue)
		}
		if globalNames[f.Name] || (f.Name == "url" && strings.Contains(f.Usage, "alias of --api-url")) {
			gr = append(gr, row{name, usage})
		} else {
			lr = append(lr, row{name, usage})
		}
	})
	format := func(rows []row) []string {
		width := 0
		for _, r := range rows {
			width = max(width, len(r.name))
		}
		out := make([]string, len(rows))
		for i, r := range rows {
			out[i] = fmt.Sprintf("  %-*s  %s", width, r.name, r.usage)
		}
		return out
	}
	return format(lr), format(gr)
}
