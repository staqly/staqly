package providers

import (
	"context"
	"net/url"
	"strings"
	"time"
)

// InstagramBusiness is an Instagram professional account linked to a Facebook Page, over the Instagram Graph API
// with Facebook Login: the person signs in on facebook.com, picks the Instagram accounts of their Pages
// (AccountLister) and each channel keeps the Page's token, which does not run out. Publishing is Instagram's (the
// same containers from public links, carousels, reels, stories and collaborators), on graph.facebook.com.
type InstagramBusiness struct{ Base }

// instagramBusinessScopes: list the Pages and their Instagram accounts (pages_show_list, pages_read_engagement,
// business_management for Pages a business portfolio owns, instagram_basic), publish (instagram_content_publish)
// and comment (instagram_manage_comments).
const instagramBusinessScopes = "instagram_basic,instagram_content_publish,instagram_manage_comments,pages_show_list,pages_read_engagement,business_management"

func init() {
	l := Limits{MaxChars: 2200, MaxMedia: 10, MediaKinds: []string{"image", "video"}, NeedsMedia: true}
	Register(InstagramBusiness{NewBase("instagram_business", "Instagram Business", AuthOAuth2, l).WithSettings(soonSettings("instagram_business", l)...)}, StatusAvailable)
}

// Instagram Business calls back over https only and downloads every picture and video itself.
func (InstagramBusiness) Needs() Needs { return Needs{HTTPS: true, PublicMedia: true} }

// CheckApp asks Facebook for an app token: the keys are the Meta app's, as for Facebook Pages.
func (InstagramBusiness) CheckApp(ctx context.Context, app App, _ string) (bool, error) {
	return metaCheckApp(ctx, igbGraph, igbGraph.url("/oauth/access_token"), app)
}

func (i InstagramBusiness) Validate(p PostContent, s Settings) error {
	return instagramValidate(i.Limits(), i.SettingsFields(), p, s)
}

func (InstagramBusiness) AuthURL(app App, redirect, state string, _ map[string]string) (string, error) {
	q := url.Values{
		"client_id":     {app.ClientID},
		"redirect_uri":  {redirect},
		"response_type": {"code"},
		"scope":         {instagramBusinessScopes},
		"state":         {state},
	}
	return facebookAuthorize + "?" + q.Encode(), nil
}

// Exchange signs the person in on Facebook; the Instagram accounts come from Accounts.
func (InstagramBusiness) Exchange(ctx context.Context, app App, redirect, code string, _ map[string]string) (Credentials, Profile, error) {
	long, err := metaUserToken(ctx, igbGraph, app, redirect, strings.TrimSuffix(code, "#_"))
	if err != nil {
		return nil, Profile{}, err
	}
	granted, err := metaGranted(ctx, igbGraph, long.AccessToken)
	if err != nil {
		return nil, Profile{}, err
	}
	if !granted["instagram_content_publish"] || !granted["pages_show_list"] || !granted["instagram_basic"] {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Facebook did not allow staqly to find your Instagram accounts and post to them: connect again and keep every permission ticked"}
	}
	var me struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := igbGraph.get(ctx, long.AccessToken, "/me", url.Values{"fields": {"id,name"}}, &me); err != nil {
		return nil, Profile{}, instagramStep(err, "reading the account")
	}
	creds := long.into(Credentials{"fb_user_id": me.ID}, time.Now())
	if !granted["instagram_manage_comments"] {
		creds["no_comments"] = "true"
	}
	return creds, Profile{Handle: me.ID, Name: me.Name, ProfileURL: "https://www.facebook.com/" + me.ID, NoComments: creds["no_comments"] == "true"}, nil
}

var _ AccountLister = InstagramBusiness{}

const instagramBusinessPageFields = "id,name,access_token,instagram_business_account{id,username,name,profile_picture_url}"

// Accounts lists the Instagram accounts linked to the person's Pages.
func (InstagramBusiness) Accounts(ctx context.Context, c Credentials) ([]Account, error) {
	pages, err := instagramBusinessPages(ctx, c)
	if err != nil {
		return nil, err
	}
	out := make([]Account, 0, len(pages))
	for _, p := range pages {
		ig := p.Instagram
		out = append(out, Account{ID: ig.ID, Name: instagramBusinessName(p), Handle: "@" + ig.Username, AvatarURL: ig.Picture,
			ProfileURL: "https://www.instagram.com/" + ig.Username + "/"})
	}
	return out, nil
}

// instagramBusinessPages are the Pages with an Instagram professional account and a token for this person, one per
// Instagram account.
func instagramBusinessPages(ctx context.Context, c Credentials) ([]metaPage, error) {
	token := c["access_token"]
	if token == "" {
		return nil, &Failure{Kind: ErrReauth, Msg: "Instagram: no sign-in stored; connect again"}
	}
	all, err := metaPages(ctx, igbGraph, token, instagramBusinessPageFields)
	if err != nil {
		return nil, err
	}
	var pages []metaPage
	seen := map[string]bool{}
	linked := 0
	for _, p := range all {
		if p.Instagram == nil || p.Instagram.ID == "" {
			continue
		}
		linked++
		if p.AccessToken != "" && !seen[p.Instagram.ID] {
			seen[p.Instagram.ID] = true
			pages = append(pages, p)
		}
	}
	switch {
	case len(pages) == 0 && linked > 0:
		return nil, &Failure{Kind: ErrRejected, Msg: "Your Facebook user can't post for the Pages these Instagram accounts are linked to: ask a Page admin for full control or content access, then connect again"}
	case len(pages) == 0 && len(all) > 0:
		return nil, &Failure{Kind: ErrRejected, Msg: "None of your Facebook Pages has an Instagram professional account linked: link one in the Page's settings (Linked accounts), or use the Instagram channel, which needs no Page"}
	case len(pages) == 0:
		return nil, &Failure{Kind: ErrRejected, Msg: "This Facebook account manages no Pages that staqly may use: connect again and choose the Pages your Instagram accounts are linked to when Facebook asks"}
	}
	return pages, nil
}

func instagramBusinessName(p metaPage) string {
	if p.Instagram.Name != "" {
		return p.Instagram.Name
	}
	return p.Instagram.Username
}

// UseAccount keeps the Page's token (it reaches the linked Instagram account and does not run out).
func (InstagramBusiness) UseAccount(ctx context.Context, c Credentials, accountID string) (Credentials, Profile, error) {
	pages, err := instagramBusinessPages(ctx, c)
	if err != nil {
		return nil, Profile{}, err
	}
	for _, p := range pages {
		ig := p.Instagram
		if ig.ID != accountID {
			continue
		}
		creds := Credentials{"access_token": p.AccessToken, "user_id": ig.ID, "page_id": p.ID}
		if c["no_comments"] == "true" {
			creds["no_comments"] = "true"
		}
		return creds, Profile{Handle: "@" + ig.Username, Name: instagramBusinessName(p), AvatarURL: ig.Picture,
			ProfileURL: "https://www.instagram.com/" + ig.Username + "/", NoComments: c["no_comments"] == "true"}, nil
	}
	return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Facebook no longer lists this Instagram account for your sign-in; connect again and choose its Page"}
}

// Refresh checks the token still reaches the account (a Page token does not run out, but the person may remove the
// app, lose the Page or unlink the account).
func (InstagramBusiness) Refresh(ctx context.Context, _ App, c Credentials) (Credentials, error) {
	if c["access_token"] == "" || c["user_id"] == "" {
		return nil, &Failure{Kind: ErrReauth, Msg: "Instagram: no account stored; reconnect the account"}
	}
	if err := igbGraph.get(ctx, c["access_token"], "/"+c["user_id"], url.Values{"fields": {"id"}}, nil); err != nil {
		return nil, err
	}
	return c, nil
}

// Instagram Business publishes a post's thread as comments under it.
var _ Replier = InstagramBusiness{}

func (InstagramBusiness) Publish(ctx context.Context, c Credentials, p PostContent, s Settings) (Result, error) {
	token, user := c["access_token"], c["user_id"]
	if token == "" || user == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "Instagram: no account stored; reconnect the account"}
	}
	return instagramPublish(ctx, igbGraph, token, user, p, s)
}

// Reply comments under the post, like Instagram's.
func (InstagramBusiness) Reply(ctx context.Context, c Credentials, parentExternalID string, p PostContent, _ Settings) (Result, error) {
	return instagramReply(ctx, igbGraph, c, parentExternalID, p)
}
