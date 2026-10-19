package app

import "testing"

// safeClient is asked for on every publish: it hands back one client per policy (connections are reused),
// and a changed policy gets a client of its own.
func TestSafeClientReused(t *testing.T) {
	s := &Service{Cfg: Config{Webhook: WebhookConfig{AllowPrivate: true}}}
	a, b := s.safeClient(), s.safeClient()
	if a != b {
		t.Fatal("a new client per call")
	}
	s.Cfg.Webhook.AllowLoopback = true
	if s.safeClient() == a {
		t.Fatal("the client of another policy")
	}
}
