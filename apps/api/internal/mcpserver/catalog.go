package mcpserver

import (
	"fmt"
	"strings"

	"github.com/staqly/staqly/apps/api/internal/providers"
)

// The platforms named in agent-facing text come from the provider catalog, so the
// instructions never offer a platform that is still "soon".

// availablePlatforms lists the names of the available platforms (not your own automation) by how they
// connect: OAuth in the dashboard, or a token through connect_channel.
func availablePlatforms() (oauth, token []string) {
	for _, e := range providers.All() {
		if e.Status != providers.StatusAvailable || e.ID() == "custom" {
			continue
		}
		switch e.Auth() {
		case providers.AuthOAuth2:
			oauth = append(oauth, e.Name())
		case providers.AuthToken:
			token = append(token, e.Name())
		}
	}
	return oauth, token
}

// joinOr joins names into "A", "A or B", "A, B or C".
func joinOr(names []string) string {
	if len(names) <= 1 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
}

// availableText: "X" or "X, Y" (with "none yet" when the catalog has no platform available).
func availableText() string {
	oauth, token := availablePlatforms()
	all := append(append([]string{}, oauth...), token...)
	if len(all) == 0 {
		return "none yet"
	}
	return strings.Join(all, ", ")
}

// connectHint: how missing channels get connected, for the instructions' flow.
func connectHint() string {
	oauth, token := availablePlatforms()
	parts := []string{`connect_channel connects "your own automation" (provider custom)`}
	if len(token) > 0 {
		parts[0] += " and " + joinOr(token)
	}
	if len(oauth) > 0 {
		parts = append(parts, joinOr(oauth)+" is connected in the dashboard")
	}
	return strings.Join(parts, "; ")
}

func serverInstructions() string {
	return fmt.Sprintf(instructions, availableText(), connectHint())
}

func connectChannelDescription() string {
	oauth, token := availablePlatforms()
	d := "Connect your own automation (provider custom, no fields)"
	if len(token) > 0 {
		d += " or a channel on " + joinOr(token) + " (see list_providers credential_fields)"
	}
	d += "."
	if len(oauth) > 0 {
		d += " " + joinOr(oauth) + " (OAuth) is connected in the dashboard."
	}
	return d + " Platforms with status soon in list_providers can't be connected yet. Requires channels:manage."
}
