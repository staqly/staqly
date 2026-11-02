package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Instagram (Facebook Business): Facebook Login with scopes, the Instagram accounts of the person's Pages, the Page's
// token kept with the account; publishing is Instagram's on graph.facebook.com.
func TestInstagramBusiness(t *testing.T) {
	ctx := context.Background()
	e, ok := Get("instagram_business")
	if !ok || e.Name() != "Instagram (Facebook Business)" {
		t.Fatal("not registered")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth/access_token"):
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "user-long", "expires_in": 5184000})
		case strings.HasSuffix(r.URL.Path, "/me/permissions"):
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{
				{"permission": "instagram_basic", "status": "granted"}, {"permission": "instagram_content_publish", "status": "granted"},
				{"permission": "pages_show_list", "status": "granted"}, {"permission": "instagram_manage_comments", "status": "granted"}}})
		case strings.HasSuffix(r.URL.Path, "/me/accounts"):
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{
				{"id": "page1", "name": "Shop", "access_token": "page-token", "instagram_business_account": map[string]string{"id": "1784", "username": "shop"}},
				{"id": "page2", "name": "No IG", "access_token": "x"}}})
		case strings.HasSuffix(r.URL.Path, "/me/businesses"):
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
		case strings.HasSuffix(r.URL.Path, "/me"):
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "fbuser"})
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	fb := facebookGraph
	facebookGraph = srv.URL
	defer func() { facebookGraph = fb }()

	app := App{ClientID: "meta", ClientSecret: "meta-secret"}
	auth, _ := e.Provider.(interface {
		AuthURL(App, string, string, map[string]string) (string, error)
	}).AuthURL(app, "https://app.example/channels/callback/instagram_business", "st", nil)
	u, _ := url.Parse(auth)
	if u.Query().Get("scope") != instagramBusinessScopes || u.Query().Get("config_id") != "" {
		t.Fatalf("auth url %s", auth)
	}
	creds, _, err := e.Provider.(interface {
		Exchange(context.Context, App, string, string, map[string]string) (Credentials, Profile, error)
	}).Exchange(ctx, app, "https://app.example/cb", "code", nil)
	if err != nil || creds["authorized_by"] != "fbuser" || creds["no_comments"] != "" {
		t.Fatalf("exchange %v %v", creds, err)
	}
	accounts, err := e.Provider.(AccountLister).Accounts(ctx, creds)
	if err != nil || len(accounts) != 1 || accounts[0].ID != "1784" || accounts[0].Handle != "@shop" {
		t.Fatalf("accounts %+v %v", accounts, err)
	}
	got, _, err := e.Provider.(AccountLister).UseAccount(ctx, creds, "1784")
	if err != nil || got["user_id"] != "1784" || got["access_token"] != "page-token" || got["page_id"] != "page1" || got["login"] != "" {
		t.Fatalf("use account %v %v", got, err)
	}
}
