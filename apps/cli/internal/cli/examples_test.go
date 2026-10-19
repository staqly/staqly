package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Help examples and texts about channels, posts and schedules use only the platforms that can be connected
// today (your own automation and X); every other catalog entry is "soon" (apps/api internal/providers).
func TestHelpNamesOnlyAvailablePlatforms(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "api", "internal", "providers", "catalog.go"))
	if err != nil {
		t.Fatal(err)
	}
	available := map[string]bool{"custom": true, "x": true}
	soon := map[string]bool{}
	for _, m := range regexp.MustCompile(`\{"([a-z_]+)", "([^"]+)", Auth`).FindAllStringSubmatch(string(b), -1) {
		if !available[m[1]] {
			soon[m[1]], soon[strings.ToLower(m[2])] = true, true
		}
	}
	if len(soon) < 20 {
		t.Fatalf("catalog not parsed: %v", soon)
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
