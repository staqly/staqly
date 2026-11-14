package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Help examples and texts about channels, posts and schedules use only the platforms that can be connected
// today: those with an implementation that registers itself; the catalog's other entries are "soon"
// (apps/api internal/providers).
func TestHelpNamesOnlyAvailablePlatforms(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "api", "internal", "providers")
	b, err := os.ReadFile(filepath.Join(dir, "catalog.go"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	available := map[string]bool{}
	registered := regexp.MustCompile(`Register\(\w+\{NewBase\("([a-z_]+)"`)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || strings.HasSuffix(f, "catalog.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range registered.FindAllStringSubmatch(string(src), -1) {
			available[m[1]] = true
		}
	}
	if !available["custom"] || !available["x"] {
		t.Fatalf("implementations not parsed: %v", available)
	}
	soon := map[string]bool{}
	catalog := regexp.MustCompile(`\{"([a-z_]+)", "([^"]+)", Auth`).FindAllStringSubmatch(string(b), -1)
	for _, m := range catalog {
		if !available[m[1]] {
			soon[m[1]], soon[strings.ToLower(m[2])] = true, true
		}
	}
	if len(catalog) < 20 {
		t.Fatalf("catalog not parsed: %d entries", len(catalog))
	}
	word := regexp.MustCompile(`[a-z_.]+`)
	var walk func(path string, c *Command)
	walk = func(path string, c *Command) {
		path = strings.TrimSpace(path + " " + c.Name)
		if path == "staqly alerts" || strings.HasPrefix(path, "staqly alerts ") {
			return // alerts go to Telegram, Slack...: not channels
		}
		for _, text := range append([]string{c.Short}, c.Examples...) {
			for _, w := range word.FindAllString(strings.ToLower(text), -1) {
				if soon[w] {
					t.Errorf("%s names %q, which is not available yet: %s", path, w, text)
				}
			}
		}
		for _, sub := range c.Subs {
			walk(path, sub)
		}
	}
	walk("", rootCommand())
}
