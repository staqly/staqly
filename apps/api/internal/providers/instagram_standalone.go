package providers

import (
	"context"
	"net/url"
	"strings"
)

// InstagramStandalone is Instagram with Instagram login, "Instagram (Standalone)": the person signs in on
// instagram.com with the Instagram account itself (the Instagram app's own keys, PROVIDER_INSTAGRAM_CLIENT_ID and
// _SECRET), with no Facebook account or business portfolio in between. It is offered when the server has those keys.
// A connected account says so in its credentials ("login": "instagram") and goes through graph.instagram.com;
// everything after the sign-in is Instagram's (embedded).
type InstagramStandalone struct{ Instagram }

func init() {
	Register(InstagramStandalone{Instagram{NewBase("instagram_standalone", "Instagram (Standalone)", AuthOAuth2,
		Limits{MaxChars: 2200, MaxMedia: 10, MediaKinds: []string{"image", "video"}, NeedsMedia: true, CommentsTextOnly: true})}}, StatusAvailable)
}

// KeepsPosts: Instagram login has no delete for posts (only for comments); see Instagram.Remove.
func (InstagramStandalone) KeepsPosts() {}

// CheckApp: Instagram login has no app token to ask for; a wrong key shows at the first sign-in.
func (InstagramStandalone) CheckApp(context.Context, App, string) (bool, error) { return true, nil }

// instagramScopes: what Instagram login asks for (the Instagram app's keys).
const instagramScopes = "instagram_business_basic,instagram_business_content_publish,instagram_business_manage_comments,instagram_business_manage_insights"

// AuthURL opens Instagram login, on instagram.com.
func (InstagramStandalone) AuthURL(app App, redirect, state string, _ map[string]string) (string, error) {
	q := url.Values{"client_id": {app.ClientID}, "redirect_uri": {redirect}, "response_type": {"code"},
		"scope": {instagramScopes}, "state": {state}, "enable_fb_login": {"0"}, "force_reauth": {"true"}}
	return instagramAuthorize + "?" + q.Encode(), nil
}

// Exchange trades the code for the 60-day token of the one account that signed in.
func (InstagramStandalone) Exchange(ctx context.Context, app App, redirect, code string, _ map[string]string) (Credentials, Profile, error) {
	return instagramLoginExchange(ctx, app, redirect, strings.TrimSuffix(code, "#_"))
}
