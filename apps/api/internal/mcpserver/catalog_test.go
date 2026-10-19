package mcpserver

import (
	"strings"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/providers"
)

// The instructions and connect_channel offer only platforms that are available: every
// available one is named, no "soon" one is.
func TestAgentTextNamesOnlyAvailablePlatforms(t *testing.T) {
	text := serverInstructions() + "\n" + connectChannelDescription()
	for _, e := range providers.All() {
		named := strings.Contains(text, e.Name())
		switch {
		case e.ID() == "custom":
		case e.Status == providers.StatusSoon && named:
			t.Errorf("names %s, which is %s", e.Name(), e.Status)
		case e.Status == providers.StatusAvailable && !named:
			t.Errorf("does not name %s, which is available", e.Name())
		}
	}
	if strings.Contains(text, "%!") {
		t.Fatalf("formatting: %s", text)
	}
}
