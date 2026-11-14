package app

import "testing"

// The PKCE verifier is the server's: derived from the signed state with its key, the same at the start and
// the callback, different per sign-in, and a verifier a client slips into the query is replaced.
func TestPKCEVerifier(t *testing.T) {
	s := &Service{Cfg: Config{SecretKey: []byte("0123456789abcdef0123456789abcdef")}}
	a := oauthState{Provider: "x", Workspace: "ws_1", Nonce: "n1"}
	v := s.pkceVerifier(a)
	if len(v) != 43 || v != s.pkceVerifier(a) {
		t.Fatalf("verifier %q must be 43 characters and stable", v)
	}
	b := a
	b.Nonce = "n2"
	if s.pkceVerifier(b) == v {
		t.Fatal("another sign-in must get another verifier")
	}
	extra := map[string]string{"instance": "m.example", "code_verifier": "forged"}
	got := withVerifier(extra, v)
	if got["code_verifier"] != v || got["instance"] != "m.example" || extra["code_verifier"] != "forged" {
		t.Fatalf("withVerifier: %v (input %v)", got, extra)
	}
}
