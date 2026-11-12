package cli

import (
	"os"
	"strings"
	"testing"
)

// The release config (apps/cli/.goreleaser.yaml), which only a release itself exercises:
//   - a pre-release tag (v0.2.0-rc.1) must not become GitHub's "latest" release, which scripts/install.sh
//     downloads from;
//   - hooks run with the release token (contents: write) in their environment, so nothing that installs or
//     runs dependencies belongs there (release.yml does that in steps without the token).
func TestReleaseConfig(t *testing.T) {
	b, err := os.ReadFile("../../.goreleaser.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg := string(b)
	if !strings.Contains(cfg, "\nrelease:\n") || !strings.Contains(cfg, "\n  prerelease: auto\n") {
		t.Error("release.prerelease must be auto")
	}
	for _, cmd := range []string{"make ", "pnpm ", "go test", "go run", "go generate", "npm ", "npx "} {
		for _, line := range strings.Split(cfg, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "- ") && strings.Contains(line, cmd) {
				t.Errorf("hook %q would run with the release token", strings.TrimSpace(line))
			}
		}
	}
}
