package app

import (
	"context"
	"testing"
)

// Audit events reach a subscription only when it names their family.
func TestEventMatches(t *testing.T) {
	for _, c := range []struct {
		pat, typ string
		want     bool
	}{
		{"*", "post.created", true},
		{"post.*", "post.published", true},
		{"post.*", "collection.created", false},
		{"*", "session.signed_in", false},
		{"*", "api_key.created", false},
		{"*.*", "session.signed_in", false},
		{"*", "device_link.created", false},
		{"session.*", "session.signed_in", true},
		{"api_key.revoked", "api_key.revoked", true},
		{"api_key.revoked", "api_key.created", false},
		{"session.*", "post.created", false},
	} {
		if got := eventMatches(c.pat, c.typ); got != c.want {
			t.Errorf("eventMatches(%q, %q) = %v, want %v", c.pat, c.typ, got, c.want)
		}
	}
}

// Was a gap: member and invitation events were emitted but not known, so a subscription got them only through
// "*": "member.*" or "invitation.accepted" was refused as an unknown pattern.
func TestTeamEventsCanBeSubscribed(t *testing.T) {
	s := &Service{}
	p := Principal{Scopes: AllScopes}
	for _, pats := range [][]string{{"member.*"}, {"member.updated", "member.removed"}, {"invitation.*"},
		{"invitation.created", "invitation.resent", "invitation.revoked", "invitation.accepted"}} {
		if err := s.validateEventPatterns(context.Background(), p, pats); err != nil {
			t.Errorf("%v: %v", pats, err)
		}
	}
	if err := s.validateEventPatterns(context.Background(), p, []string{"member.joined"}); err == nil {
		t.Error("an unknown member event was accepted")
	}
}
