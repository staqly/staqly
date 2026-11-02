package providers

import (
	"cmp"
	"context"
	"net/url"
	"strings"
	"time"
)

// InstagramBusiness is Instagram through Facebook Login (the Meta app's keys, the classic dialog with scopes): an
// Instagram Business or Creator account linked to a Facebook Page the person manages. It is offered next to
// Instagram login ("Instagram (Standalone)") when the server has the Instagram app's own keys; with Unified login
// (one "Instagram" tile) it stays hidden. Once connected it is an Instagram account like any other: posts, comments,
// numbers and removal go through graph.facebook.com (Instagram's methods, embedded).
type InstagramBusiness struct{ Instagram }

// instagramBusinessScopes: publishing, comments and numbers on the account, and the Pages that lead to it.
const instagramBusinessScopes = "instagram_basic,instagram_content_publish,instagram_manage_comments,instagram_manage_insights," +
	"pages_show_list,pages_read_engagement,business_management"

func init() {
	Register(InstagramBusiness{Instagram{NewBase("instagram_business", "Instagram (Facebook Business)", AuthOAuth2,
		Limits{MaxChars: 2200, MaxMedia: 10, MediaKinds: []string{"image", "video"}, NeedsMedia: true, CommentsTextOnly: true})}}, StatusAvailable)
}

// CheckApp asks Facebook for an app token: the keys are the Meta app's.
func (InstagramBusiness) CheckApp(ctx context.Context, app App, _ string) (bool, error) {
	return metaCheckApp(ctx, igGraph, igGraph.url("/oauth/access_token"), app)
}

// AuthURL opens Facebook Login with the scopes (no configuration).
func (InstagramBusiness) AuthURL(app App, redirect, state string, _ map[string]string) (string, error) {
	q := url.Values{"client_id": {app.ClientID}, "redirect_uri": {redirect}, "response_type": {"code"},
		"scope": {instagramBusinessScopes}, "state": {state}}
	return facebookAuthorize + "?" + q.Encode(), nil
}

// Exchange signs the person in (a 60-day user token); the Instagram accounts of their Pages come from Accounts.
func (InstagramBusiness) Exchange(ctx context.Context, app App, redirect, code string, _ map[string]string) (Credentials, Profile, error) {
	long, err := metaUserToken(ctx, igGraph, app, redirect, strings.TrimSuffix(code, "#_"))
	if err != nil {
		return nil, Profile{}, err
	}
	granted, err := metaGranted(ctx, igGraph, long.AccessToken)
	if err != nil {
		return nil, Profile{}, err
	}
	if !granted["instagram_content_publish"] || !granted["instagram_basic"] || !granted["pages_show_list"] {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Facebook did not allow staqly to post to Instagram: connect again and keep every permission ticked"}
	}
	var me struct {
		ID string `json:"id"`
	}
	if err := igGraph.get(ctx, long.AccessToken, "/me", url.Values{"fields": {"id"}}, &me); err != nil {
		return nil, Profile{}, instagramStep(err, "reading the account")
	}
	creds := long.into(Credentials{"authorized_by": me.ID}, time.Now())
	if !granted["instagram_manage_comments"] {
		creds["no_comments"] = "true"
	}
	return creds, Profile{NoComments: creds["no_comments"] == "true"}, nil
}

var _ AccountLister = InstagramBusiness{}

// businessAccounts: the Instagram accounts linked to the Pages the sign-in reaches, each with its Page's token.
func businessAccounts(ctx context.Context, c Credentials) ([]metaPage, error) {
	token := c["access_token"]
	if token == "" {
		return nil, &Failure{Kind: ErrReauth, Msg: "Instagram: no sign-in stored; connect again"}
	}
	pages, err := metaPages(ctx, igGraph, token, "id,name,access_token,instagram_business_account{id,username,name,profile_picture_url}")
	if err != nil {
		return nil, err
	}
	var out []metaPage
	for _, p := range pages {
		if p.Instagram != nil && p.Instagram.ID != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil, &Failure{Kind: ErrRejected, Msg: "None of your Facebook Pages has an Instagram Business or Creator account linked: link one to a Page (Instagram → Settings → Account type and tools), then connect again"}
	}
	return out, nil
}

func (InstagramBusiness) Accounts(ctx context.Context, c Credentials) ([]Account, error) {
	pages, err := businessAccounts(ctx, c)
	if err != nil {
		return nil, err
	}
	out := make([]Account, 0, len(pages))
	for _, p := range pages {
		a := instagramAccount{ID: p.Instagram.ID, Username: p.Instagram.Username, Name: p.Instagram.Name, Picture: p.Instagram.Picture}.profile()
		out = append(out, Account{ID: p.Instagram.ID, Name: a.Name, Handle: a.Handle, AvatarURL: a.AvatarURL, ProfileURL: a.ProfileURL})
	}
	return out, nil
}

// UseAccount keeps the Page's token (it doesn't expire while the app keeps its permissions) with the account.
func (InstagramBusiness) UseAccount(ctx context.Context, c Credentials, accountID string) (Credentials, Profile, error) {
	pages, err := businessAccounts(ctx, c)
	if err != nil {
		return nil, Profile{}, err
	}
	for _, p := range pages {
		if p.Instagram.ID != accountID {
			continue
		}
		creds := Credentials{"user_id": p.Instagram.ID, "page_id": p.ID, "access_token": cmp.Or(p.AccessToken, c["access_token"]),
			"authorized_by": c["authorized_by"]}
		if c["no_comments"] != "" {
			creds["no_comments"] = c["no_comments"]
		}
		prof := instagramAccount{ID: p.Instagram.ID, Username: p.Instagram.Username, Name: p.Instagram.Name, Picture: p.Instagram.Picture}.profile()
		prof.NoComments = c["no_comments"] == "true"
		return creds, prof, nil
	}
	return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "This sign-in no longer reaches that Instagram account; connect again and choose it"}
}
