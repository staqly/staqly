package app

import (
	"testing"

	"github.com/staqly/staqly/apps/api/internal/providers"
)

// A personal account is the platform's own id where its credentials carry one (the same across the ways a platform
// connects), else the account URL (Mastodon) or the handle. A shared space is the person who authorized it, and
// nothing when no person did (a bot, a webhook).
func TestChannelIdentity(t *testing.T) {
	cases := []struct {
		provider string
		creds    providers.Credentials
		profile  providers.Profile
		kind, id string
	}{
		{"bluesky", providers.Credentials{"did": "did:plc:Abc"}, providers.Profile{Handle: "@jane.bsky.social"}, "account", "bluesky\x00did:plc:abc"},
		{"bluesky_app_password", providers.Credentials{"did": "did:plc:abc"}, providers.Profile{Handle: "@renamed.example"}, "account", "bluesky\x00did:plc:abc"},
		{"mastodon_token", providers.Credentials{}, providers.Profile{Handle: "@jane@social.example", ProfileURL: "https://social.example/@Jane/"}, "account", "mastodon\x00https://social.example/@jane"},
		{"mastodon", providers.Credentials{}, providers.Profile{ProfileURL: "https://social.example/@jane"}, "account", "mastodon\x00https://social.example/@jane"},
		{"x", providers.Credentials{"user_id": "99"}, providers.Profile{Handle: "@jane"}, "account", "x\x0099"},
		{"linkedin", providers.Credentials{"person_id": "p1"}, providers.Profile{}, "account", "linkedin\x00p1"},
		{"devto", providers.Credentials{"api_key": "k"}, providers.Profile{Handle: "@Jane"}, "account", "devto\x00jane"}, // no id: the handle
		// Shared spaces: the person, never the space.
		{"slack", providers.Credentials{"team_id": "T1", "user_id": "U1"}, providers.Profile{Handle: "acme"}, "person", "slack\x00t1:u1"},
		{"slack", providers.Credentials{"team_id": "T1"}, providers.Profile{Handle: "acme"}, "", ""}, // a bot only
		{"facebook", providers.Credentials{"page_id": "pg", "authorized_by": "fb1"}, providers.Profile{Handle: "page"}, "person", "facebook\x00fb1"},
		{"facebook", providers.Credentials{"page_id": "pg"}, providers.Profile{Handle: "page"}, "", ""},
		{"linkedin_page", providers.Credentials{"organization_id": "o1", "person_id": "p1"}, providers.Profile{}, "person", "linkedin_page\x00p1"},
		{"google_business", providers.Credentials{"location": "l1", "google_id": "g1"}, providers.Profile{}, "person", "google_business\x00g1"},
		{"mewe", providers.Credentials{"group_id": "g", "user_id": "u"}, providers.Profile{}, "person", "mewe\x00u"},
		{"mewe", providers.Credentials{"user_id": "u"}, providers.Profile{}, "account", "mewe\x00u"},
		{"discord", providers.Credentials{"guild_id": "42"}, providers.Profile{Handle: "42"}, "", ""},
		{"discord_webhook", providers.Credentials{"guild_id": "42", "webhook_id": "7"}, providers.Profile{Handle: "Hook (7)"}, "", ""},
		{"telegram", providers.Credentials{"chat_id": "-100123"}, providers.Profile{Handle: "-100123"}, "", ""},
		{"custom", providers.Credentials{}, providers.Profile{Name: "Your own automation"}, "", ""},
	}
	for _, c := range cases {
		kind, id := channelIdentity(c.provider, c.creds, c.profile)
		if kind != c.kind || id != c.id {
			t.Errorf("%s %v: %q %q, want %q %q", c.provider, c.creds, kind, id, c.kind, c.id)
		}
	}
}

// Fingerprints are keyed: the same value gives the same fingerprint with one key and another with a different
// key, and the value can't be read from it.
func TestFingerprintKeyed(t *testing.T) {
	a := &Service{Cfg: Config{SecretKey: make([]byte, 32)}}
	b := &Service{Cfg: Config{SecretKey: append(make([]byte, 31), 1)}}
	fa, fb := a.fingerprint(fingerprintCard, "fp_1"), b.fingerprint(fingerprintCard, "fp_1")
	if fa == "" || fa != a.fingerprint(fingerprintCard, "fp_1") || fa == fb || fa == a.fingerprint(fingerprintAccount, "fp_1") {
		t.Fatalf("fingerprints: %s %s", fa, fb)
	}
	if a.fingerprint(fingerprintCard, "") != "" {
		t.Fatal("no value, no fingerprint")
	}
}
