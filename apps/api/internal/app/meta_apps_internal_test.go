package app

import "testing"

// Facebook and Instagram take the Meta app's keys unless they have their own; Instagram's own keys mean Instagram login.
func TestMetaPlatformsTakeTheirOwnKeys(t *testing.T) {
	s, _, _ := publishTest(t)
	env := map[string]string{"PROVIDER_META_CLIENT_ID": "meta", "PROVIDER_META_CLIENT_SECRET": "meta-secret", "PROVIDER_INSTAGRAM_CONFIG_ID": "cfg"}
	s.Getenv = func(k string) string { return env[k] }
	ig, ok := s.serverApp("instagram")
	if !ok || ig.ClientID != "meta" || ig.ConfigID != "cfg" || ig.InstagramLogin {
		t.Fatalf("unified: %+v", ig)
	}
	env["PROVIDER_INSTAGRAM_CLIENT_ID"], env["PROVIDER_INSTAGRAM_CLIENT_SECRET"] = "ig", "ig-secret"
	if ig, _ = s.serverApp("instagram"); ig.ClientID != "ig" || ig.ClientSecret != "ig-secret" || !ig.InstagramLogin || ig.ConfigID != "" {
		t.Fatalf("instagram login: %+v", ig)
	}
	if fb, _ := s.serverApp("facebook"); fb.ClientID != "meta" {
		t.Fatalf("facebook on the Meta app: %+v", fb)
	}
	env["PROVIDER_FACEBOOK_CLIENT_ID"], env["PROVIDER_FACEBOOK_CLIENT_SECRET"] = "fb", "fb-secret"
	if fb, _ := s.serverApp("facebook"); fb.ClientID != "fb" || fb.ClientSecret != "fb-secret" {
		t.Fatalf("facebook's own app: %+v", fb)
	}
	env["PROVIDER_INSTAGRAM_CLIENT_SECRET"] = ""
	if ig, _ = s.serverApp("instagram"); ig.InstagramLogin || ig.ClientID != "meta" {
		t.Fatalf("half a key pair is not Instagram login: %+v", ig)
	}
}
