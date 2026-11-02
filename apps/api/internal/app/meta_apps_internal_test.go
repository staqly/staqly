package app

import (
	"context"
	"strings"
	"testing"
)

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

// With Unified login there is one Instagram tile; with Instagram login, "Instagram (Standalone)" and
// "Instagram (Facebook Business)".
func TestInstagramTiles(t *testing.T) {
	s, p, _ := publishTest(t)
	env := map[string]string{"PROVIDER_META_CLIENT_ID": "meta", "PROVIDER_META_CLIENT_SECRET": "meta-secret", "PROVIDER_INSTAGRAM_CONFIG_ID": "cfg"}
	s.Getenv = func(k string) string { return env[k] }
	tiles := func() map[string]string {
		list, err := s.ListProviders(context.Background(), p)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, i := range list {
			if strings.HasPrefix(i.ID, "instagram") {
				out[i.ID] = i.Name
			}
		}
		return out
	}
	if got := tiles(); len(got) != 1 || got["instagram"] != "Instagram" {
		t.Fatalf("unified: %v", got)
	}
	env["PROVIDER_INSTAGRAM_CLIENT_ID"], env["PROVIDER_INSTAGRAM_CLIENT_SECRET"] = "ig", "ig-secret"
	if got := tiles(); got["instagram"] != "Instagram (Standalone)" || got["instagram_business"] != "Instagram (Facebook Business)" {
		t.Fatalf("instagram login: %v", got)
	}
	list, _ := s.ListProviders(context.Background(), p)
	for _, i := range list {
		if strings.HasPrefix(i.ID, "instagram") && i.Status != "available" {
			t.Errorf("%s is %s (app configured %v)", i.ID, i.Status, i.AppConfigured)
		}
	}
	if app, ok := s.serverApp("instagram_business"); !ok || app.ClientID != "meta" || app.InstagramLogin {
		t.Fatalf("business uses the Meta app: %+v", app)
	}
}
