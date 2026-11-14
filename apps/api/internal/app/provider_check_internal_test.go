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

// Facebook and Instagram sign in through the server's one Meta app; Threads keeps its own keys.
func TestMetaAppKeys(t *testing.T) {
	env := map[string]string{"PROVIDER_META_CLIENT_ID": "meta", "PROVIDER_META_CLIENT_SECRET": "shh",
		"PROVIDER_FACEBOOK_CLIENT_ID": "old", "PROVIDER_INSTAGRAM_CONFIG_ID": "cfg", "PROVIDER_THREADS_CLIENT_ID": "threads"}
	s := &Service{Getenv: func(k string) string { return env[k] }}
	for _, id := range []string{"facebook", "instagram"} {
		if app, ok := s.serverApp(id); !ok || app.ClientID != "meta" || app.ClientSecret != "shh" {
			t.Errorf("%s: %+v %v", id, app, ok)
		}
	}
	if app, _ := s.serverApp("instagram"); app.ConfigID != "cfg" {
		t.Errorf("config id: %q", app.ConfigID)
	}
	if app, ok := s.serverApp("threads"); !ok || app.ClientID != "threads" {
		t.Errorf("threads: %+v %v", app, ok)
	}
}
