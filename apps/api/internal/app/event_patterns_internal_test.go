package app

import "testing"

// Audit events reach a subscription only when it names their family.
func TestEventMatches(t *testing.T) {
	for _, c := range []struct {
		pat, typ string
		want     bool
	}{
		{"*", "item.created", true},
		{"item.*", "item.approved", true},
		{"item.*", "collection.created", false},
		{"*", "session.signed_in", false},
		{"*", "api_key.created", false},
		{"*.*", "session.signed_in", false},
		{"*", "device_link.created", false},
		{"session.*", "session.signed_in", true},
		{"api_key.revoked", "api_key.revoked", true},
		{"api_key.revoked", "api_key.created", false},
		{"session.*", "item.created", false},
	} {
		if got := eventMatches(c.pat, c.typ); got != c.want {
			t.Errorf("eventMatches(%q, %q) = %v, want %v", c.pat, c.typ, got, c.want)
		}
	}
}
