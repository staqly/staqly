package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Instagram login (the Instagram app's own keys): the sign-in on instagram.com, the code traded for a 60-day token,
// the account from /me, and publishing on graph.instagram.com; an account from Unified login stays on graph.facebook.com.
func TestInstagramLogin(t *testing.T) {
	ctx := context.Background()
	ig, _ := Get("instagram")
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/oauth/access_token":
			if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("client_secret") != "ig-secret" {
				w.WriteHeader(400)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"access_token": "short", "user_id": "17841",
				"permissions": "instagram_business_basic,instagram_business_content_publish"}}})
		case r.URL.Path == "/access_token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "long", "expires_in": 5184000})
		case strings.HasSuffix(r.URL.Path, "/me"):
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "app-scoped", "user_id": "17841", "username": "mia", "name": "Mia"})
		case strings.HasSuffix(r.URL.Path, "/17841/media"):
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "c1"})
		case strings.HasSuffix(r.URL.Path, "/c1"):
			_ = json.NewEncoder(w).Encode(map[string]any{"status_code": "FINISHED"})
		case strings.HasSuffix(r.URL.Path, "/media_publish"):
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "1790"})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"permalink": "https://www.instagram.com/p/X/"})
		}
	}))
	defer srv.Close()
	host, fb := instagramGraph, facebookGraph
	instagramGraph, facebookGraph = srv.URL, "http://unused.invalid"
	defer func() { instagramGraph, facebookGraph = host, fb }()

	app := App{ClientID: "ig-app", ClientSecret: "ig-secret", InstagramLogin: true}
	auth, err := ig.Provider.(interface {
		AuthURL(App, string, string, map[string]string) (string, error)
	}).AuthURL(app, "https://app.example/channels/callback/instagram", "st", nil)
	u, _ := url.Parse(auth)
	if err != nil || !strings.HasPrefix(auth, "https://www.instagram.com/oauth/authorize?") || u.Query().Get("scope") != instagramScopes ||
		u.Query().Get("config_id") != "" {
		t.Fatalf("auth url %s %v", auth, err)
	}
	creds, _, err := ig.Provider.(interface {
		Exchange(context.Context, App, string, string, map[string]string) (Credentials, Profile, error)
	}).Exchange(ctx, app, "https://app.example/channels/callback/instagram", "code#_", nil)
	if err != nil || creds["access_token"] != "long" || creds["login"] != "instagram" || creds["no_comments"] != "true" {
		t.Fatalf("exchange %v %v", creds, err)
	}
	accounts, err := ig.Provider.(AccountLister).Accounts(ctx, creds)
	if err != nil || len(accounts) != 1 || accounts[0].ID != "17841" || accounts[0].Handle != "@mia" {
		t.Fatalf("accounts %+v %v", accounts, err)
	}
	creds, _, err = ig.Provider.(AccountLister).UseAccount(ctx, creds, "17841")
	if err != nil || creds["login"] != "instagram" || creds["user_id"] != "17841" {
		t.Fatalf("use account %v %v", creds, err)
	}
	creds["expires_at"] = time.Now().Add(50 * 24 * time.Hour).UTC().Format(time.RFC3339)
	res, err := ig.Publish(ctx, creds, PostContent{Text: "hi", Media: []MediaFile{jpeg("a.jpg")}}, Settings{})
	if err != nil || res.ExternalID != "1790" {
		t.Fatalf("publish %+v %v (%v)", res, err, paths)
	}
	if !strings.Contains(strings.Join(paths, " "), "POST /"+instagramVersion+"/17841/media_publish") {
		t.Fatalf("published elsewhere: %v", paths)
	}
}
