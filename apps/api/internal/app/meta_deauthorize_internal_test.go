package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/providers"
)

func signedRequest(secret, payload string) string {
	p := base64.RawURLEncoding.EncodeToString([]byte(payload))
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(p))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) + "." + p
}

// Meta's deauthorize marks the person's channel for reconnecting; a data deletion request wipes what came from the
// account and gives a status page; a forged request changes nothing.
func TestMetaAccountCallbacks(t *testing.T) {
	s, _, _, _, chans := publishedOn(t, "fakemetadeauth")
	ctx := context.Background()
	env := map[string]string{"PROVIDER_INSTAGRAM_CLIENT_SECRET": "ig-secret", "PUBLIC_DASHBOARD_URL": "https://app.example"}
	s.Getenv = func(k string) string { return env[k] }
	sealed, err := s.seal(providers.Credentials{"access_token": "t", "user_id": "17841", "app_user_id": "555", "login": "instagram"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`UPDATE channels SET provider = 'instagram', credentials = ?, avatar_url = 'https://pic' WHERE id = ?`, sealed, chans[0]); err != nil {
		t.Fatal(err)
	}
	status := func() (string, string) {
		var st, creds string
		if err := s.DB.QueryRow(`SELECT status, credentials FROM channels WHERE id = ?`, chans[0]).Scan(&st, &creds); err != nil {
			t.Fatal(err)
		}
		return st, creds
	}
	if _, err := s.MetaDeauthorize(ctx, signedRequest("other", `{"user_id":"555"}`)); err == nil {
		t.Fatal("a forged request must be refused")
	}
	if n, err := s.MetaDeauthorize(ctx, signedRequest("ig-secret", `{"algorithm":"HMAC-SHA256","user_id":"555"}`)); err != nil || n != 1 {
		t.Fatalf("deauthorize: %d %v", n, err)
	}
	if st, _ := status(); st != "needs_reconnect" {
		t.Fatalf("status %s", st)
	}
	url, code, err := s.MetaDataDeletion(ctx, signedRequest("ig-secret", `{"user_id":"17841"}`))
	if err != nil || code == "" || url != "https://app.example/api/v1/meta/data-deletion/"+code {
		t.Fatalf("deletion: %s %s %v", url, code, err)
	}
	if _, creds := status(); creds != "" {
		t.Fatal("the sign-in must be gone")
	}
	if text, err := s.MetaDataDeletionStatus(ctx, code); err != nil || !strings.Contains(text, code) {
		t.Fatalf("status page: %q %v", text, err)
	}
	if _, err := s.MetaDataDeletionStatus(ctx, "nope"); err == nil {
		t.Fatal("an unknown code has no page")
	}
}
