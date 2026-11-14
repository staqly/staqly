package httpapi_test

import (
	"net/url"
	"testing"
)

// A new person's first workspace takes the zone the browser sent with the sign-in (Google or an email code); an
// unknown zone falls back to UTC without failing the sign-in, and an invitation leaves the workspace's zone alone.
func TestSignInBrowserZone(t *testing.T) {
	e, _ := newEmailEnv(t, "")
	zone := func(p *person) string { return e.session(p).Workspace.Timezone }

	owner := e.mustSignIn(ana, "timezone="+url.QueryEscape("Europe/Warsaw"))
	if z := zone(owner); z != "Europe/Warsaw" {
		t.Fatalf("google: %q", z)
	}
	if z := zone(e.mustSignIn(eve, "timezone=Nowhere%2FLand")); z != "UTC" {
		t.Fatalf("google, unknown zone: %q", z)
	}
	_, tok := e.invite(owner, bob.Email, "editor")
	if z := zone(e.mustSignIn(bob, "invite="+url.QueryEscape(tok)+"&timezone=Asia%2FTokyo")); z != "Europe/Warsaw" {
		t.Fatalf("invited: %q", z)
	}

	if z := zone(e.emailSignIn("zoe@example.com", map[string]any{"timezone": "America/New_York"})); z != "America/New_York" {
		t.Fatalf("email: %q", z)
	}
	if z := zone(e.emailSignIn("yan@example.com", map[string]any{"timezone": "Local"})); z != "UTC" {
		t.Fatalf("email, unknown zone: %q", z)
	}
}
