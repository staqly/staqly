package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Each platform's answer to a made-up sign-in code (or app token request) with good and with bad app keys, as
// recorded against the real ones.
func TestCheckApp(t *testing.T) {
	good := App{ClientID: "cid", ClientSecret: "secret"}
	bad := App{ClientID: "cid", ClientSecret: "wrong"}
	answer := func(goodBody, badBody string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = r.ParseForm()
			_, secret, _ := r.BasicAuth()
			if r.Form.Get("client_secret") == "secret" || secret == "secret" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(goodBody))
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(badBody))
		}))
	}
	ctx := context.Background()

	srv := answer(`{"error":"invalid_request","error_description":"Value passed for the authorization code was invalid."}`,
		`{"error":"unauthorized_client","error_description":"Missing valid authorization header"}`)
	defer srv.Close()
	xAPI, linkedinOAuth, instagramToken = srv.URL, srv.URL, srv.URL
	t.Cleanup(func() {
		xAPI, linkedinOAuth, instagramToken = "https://api.x.com", "https://www.linkedin.com/oauth/v2", "https://api.instagram.com/oauth/access_token"
	})
	if ok, err := (X{}).CheckApp(ctx, good, "https://app.example/cb"); !ok || err != nil {
		t.Fatalf("X good keys: %v %v", ok, err)
	}
	if _, err := (X{}).CheckApp(ctx, bad, "https://app.example/cb"); err == nil {
		t.Fatal("X bad keys passed")
	}

	srv2 := answer(`{"error":"access_denied","error_description":"This application is not allowed to create application tokens"}`,
		`{"error":"invalid_client","error_description":"Client authentication failed"}`)
	defer srv2.Close()
	linkedinOAuth = srv2.URL
	if ok, err := (LinkedIn{}).CheckApp(ctx, good, ""); !ok || err != nil {
		t.Fatalf("LinkedIn good keys: %v %v", ok, err)
	}
	if _, err := (LinkedIn{}).CheckApp(ctx, bad, ""); err == nil {
		t.Fatal("LinkedIn bad keys passed")
	}

	// Instagram looks at the code first: it answers the same either way, so the check only proves it answers.
	srv3 := answer(`{"error_type":"OAuthException","code":400,"error_message":"Invalid authorization code"}`,
		`{"error_type":"OAuthException","code":400,"error_message":"Invalid authorization code"}`)
	defer srv3.Close()
	instagramToken = srv3.URL
	for _, app := range []App{good, bad} {
		if ok, err := (Instagram{}).CheckApp(ctx, app, "https://app.example/cb"); ok || err != nil {
			t.Fatalf("Instagram: %v %v", ok, err)
		}
	}
}
