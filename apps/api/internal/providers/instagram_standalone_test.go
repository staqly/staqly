package providers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestInstagramStandaloneSignIn(t *testing.T) {
	e, ok := Get("instagram_standalone")
	if !ok || e.Status != StatusAvailable || e.Auth() != AuthOAuth2 || e.Name() != "Instagram (Standalone)" {
		t.Fatalf("entry %+v", e)
	}
	l := e.Limits()
	if l.MaxChars != 2200 || l.MaxMedia != 10 || !l.NeedsMedia || !l.CommentsTextOnly {
		t.Fatalf("limits %+v", l)
	}
	ig := InstagramStandalone{}

	// Instagram login has no app token to check keys with: any keys pass, without a request.
	if ok, err := ig.CheckApp(context.Background(), App{}, ""); !ok || err != nil {
		t.Fatalf("check %v %v", ok, err)
	}

	raw, err := ig.AuthURL(App{ClientID: "ig-app", ClientSecret: "ig-secret"}, "https://app.example/cb", "st", nil)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(raw)
	q := u.Query()
	for k, v := range map[string]string{"client_id": "ig-app", "redirect_uri": "https://app.example/cb", "response_type": "code",
		"scope": instagramScopes, "state": "st", "enable_fb_login": "0", "force_reauth": "true"} {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
	if u.Scheme+"://"+u.Host+u.Path != instagramAuthorize || strings.Contains(raw, "ig-secret") {
		t.Errorf("auth url %s", raw)
	}
	for _, s := range []string{"instagram_business_basic", "instagram_business_content_publish", "instagram_business_manage_comments"} {
		if !hasScope(instagramScopes, s) {
			t.Errorf("scopes lack %s", s)
		}
	}

	// Instagram appends "#_" to the code in the return address: it is not part of the code.
	var codes []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.URL.Path == "/oauth/access_token" {
			codes = append(codes, r.PostForm.Get("code"))
			_, _ = io.WriteString(w, `{"access_token":"short","permissions":["instagram_business_basic","instagram_business_content_publish","instagram_business_manage_comments"]}`)
			return
		}
		_, _ = io.WriteString(w, `{"access_token":"long","expires_in":5184000}`)
	}))
	defer srv.Close()
	host := instagramGraph
	instagramGraph = srv.URL
	defer func() { instagramGraph = host }()
	for _, code := range []string{"abc#_", "abc"} {
		creds, prof, err := ig.Exchange(context.Background(), App{ClientID: "ig-app", ClientSecret: "ig-secret"}, "https://app.example/cb", code, nil)
		if err != nil || creds["access_token"] != "long" || creds["login"] != "instagram" || creds["no_comments"] != "" || prof.NoComments {
			t.Fatalf("%s: %v %+v %v", code, creds, prof, err)
		}
	}
	if strings.Join(codes, ",") != "abc,abc" {
		t.Fatalf("codes sent %v", codes)
	}
}
