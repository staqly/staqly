package app

import (
	"context"
	"strings"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// Instagram (Unified login) always takes the Meta app's keys; the Instagram app's own keys are Instagram login's
// (instagram_standalone). Facebook takes the Meta app's keys unless it has its own.
func TestMetaPlatformsTakeTheirOwnKeys(t *testing.T) {
	s, _, _ := publishTest(t)
	env := map[string]string{"PROVIDER_META_CLIENT_ID": "meta", "PROVIDER_META_CLIENT_SECRET": "meta-secret", "PROVIDER_INSTAGRAM_CONFIG_ID": "cfg"}
	s.Getenv = func(k string) string { return env[k] }
	if ig, ok := s.serverApp("instagram"); !ok || ig.ClientID != "meta" || ig.ConfigID != "cfg" {
		t.Fatalf("unified: %+v", ig)
	}
	if _, ok := s.serverApp("instagram_standalone"); ok {
		t.Fatal("Instagram login needs the Instagram app's keys")
	}
	env["PROVIDER_INSTAGRAM_CLIENT_ID"], env["PROVIDER_INSTAGRAM_CLIENT_SECRET"] = "ig", "ig-secret"
	if alone, ok := s.serverApp("instagram_standalone"); !ok || alone.ClientID != "ig" || alone.ClientSecret != "ig-secret" || alone.ConfigID != "" {
		t.Fatalf("instagram login: %+v", alone)
	}
	if ig, ok := s.serverApp("instagram"); !ok || ig.ClientID != "meta" || ig.ClientSecret != "meta-secret" || ig.ConfigID != "cfg" {
		t.Fatalf("unified stays on the Meta app next to Instagram login: %+v", ig)
	}
	if fb, _ := s.serverApp("facebook"); fb.ClientID != "meta" {
		t.Fatalf("facebook on the Meta app: %+v", fb)
	}
	env["PROVIDER_FACEBOOK_CLIENT_ID"], env["PROVIDER_FACEBOOK_CLIENT_SECRET"] = "fb", "fb-secret"
	if fb, _ := s.serverApp("facebook"); fb.ClientID != "fb" || fb.ClientSecret != "fb-secret" {
		t.Fatalf("facebook's own app: %+v", fb)
	}
	env["PROVIDER_INSTAGRAM_CLIENT_SECRET"] = ""
	if _, ok := s.serverApp("instagram_standalone"); ok {
		t.Fatal("half a key pair is not Instagram login")
	}
}

// Without the Instagram app's keys there is one Instagram tile (Unified login). With them it is "Instagram
// (Standalone)", and Unified login joins it as "Instagram (Professional)" only when PROVIDER_INSTAGRAM_UNIFIED_ENABLED.
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
				if i.Status != "available" {
					t.Errorf("%s is %s (app configured %v)", i.ID, i.Status, i.AppConfigured)
				}
				out[i.ID] = i.Name
			}
		}
		return out
	}
	if got := tiles(); len(got) != 1 || got["instagram"] != "Instagram" {
		t.Fatalf("unified: %v", got)
	}
	env["PROVIDER_INSTAGRAM_CLIENT_ID"], env["PROVIDER_INSTAGRAM_CLIENT_SECRET"] = "ig", "ig-secret"
	if got := tiles(); len(got) != 1 || got["instagram_standalone"] != "Instagram (Standalone)" {
		t.Fatalf("instagram login: %v", got)
	}
	env["PROVIDER_INSTAGRAM_UNIFIED_ENABLED"] = "true"
	if got := tiles(); len(got) != 2 || got["instagram"] != "Instagram (Professional)" || got["instagram_standalone"] != "Instagram (Standalone)" {
		t.Fatalf("both: %v", got)
	}
}

// Channels connected with Instagram login before it had its own provider move to instagram_standalone at start;
// those from Unified login stay.
func TestMoveInstagramLoginChannels(t *testing.T) {
	s, p, _ := publishTest(t)
	ctx := context.Background()
	for id, creds := range map[string]providers.Credentials{
		"chn_login":   {"access_token": "a", "user_id": "1", "login": "instagram"},
		"chn_unified": {"access_token": "b", "user_id": "2"},
	} {
		sealed, err := s.seal(creds)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.W.CreateChannel(ctx, db.CreateChannelParams{ID: id, WorkspaceID: p.WorkspaceID, Provider: "instagram", Name: id,
			Status: "connected", Credentials: sealed, Settings: "{}", CreatedAt: s.now(), UpdatedAt: s.now()}); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 { // a second start changes nothing
		if err := s.MoveInstagramLoginChannels(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for id, want := range map[string]string{"chn_login": "instagram_standalone", "chn_unified": "instagram"} {
		if ch, err := s.Q.GetChannelByID(ctx, id); err != nil || ch.Provider != want {
			t.Errorf("%s: %q %v, want %s", id, ch.Provider, err, want)
		}
	}
}
