package app

import "testing"

// Media links a platform can download from: public names and addresses only.
func TestPublicHost(t *testing.T) {
	for h, want := range map[string]bool{
		"dev-media.staqly.ai": true, "s3.eu-central-1.amazonaws.com": true, "203.0.113.7": true,
		"localhost": false, "maxiofs": false, "127.0.0.1": false, "192.168.1.23": false, "10.0.0.5": false,
		"nas.local": false, "staqly.localhost": false, "": false,
	} {
		if publicHost(h) != want {
			t.Errorf("%q: want %v", h, want)
		}
	}
}
