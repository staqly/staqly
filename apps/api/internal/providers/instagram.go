package providers

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Instagram (a professional account, Business or Creator) over the Instagram API with Unified Login: the person signs in
// on facebook.com with Instagram or Facebook (Facebook Login for Business with a configuration, config_id), shares
// one or more Instagram accounts (AccountLister) and the code is traded for a business integration system user token
// that reaches all of them; no Facebook Page is needed. Posts go through the Content Publishing API on
// graph.facebook.com. Instagram fetches every picture and video itself from a link, so media needs a public address
// (MediaFile.SignedURL: S3-compatible storage). Comments are the post's thread.
type Instagram struct{ Base }

var (
	// instagramPoll is how long to wait between checks of a container Instagram is still processing.
	instagramPoll = 3 * time.Second
)

// instagramVersion is the Graph API version every call names. ponytail: Meta keeps a version about two years;
// bump it once a year (https://developers.facebook.com/docs/graph-api/changelog).
const instagramVersion = "v24.0"

func init() {
	Register(Instagram{NewBase("instagram", "Instagram", AuthOAuth2,
		Limits{MaxChars: 2200, MaxMedia: 10, MediaKinds: []string{"image", "video"}, NeedsMedia: true})}, StatusAvailable)
}

func (Instagram) SettingsFields() []Field { return instagramFields }

func (i Instagram) Validate(p PostContent, s Settings) error {
	if err := CheckLimits(i.Limits(), p, s); err != nil {
		return err
	}
	videos := 0
	for _, m := range p.Media {
		switch {
		case strings.HasPrefix(m.MimeType, "video/"):
			videos++
		case m.MimeType != "image/jpeg":
			return invalid("media", "Instagram takes pictures as JPEG; save "+m.Name+" as JPEG (open it in the design editor and save it: photos save as JPEG)")
		}
	}
	switch s["post_type"] {
	case "reel":
		if len(p.Media) != 1 || videos != 1 {
			return invalid("media", "a reel is one video")
		}
	case "story":
		if len(p.Media) != 1 {
			return invalid("media", "a story is one picture or one video")
		}
	}
	return CheckFields(i.SettingsFields(), s)
}

// Instagram calls back over https only and downloads every picture and video itself.
func (Instagram) Needs() Needs { return Needs{HTTPS: true, PublicMedia: true} }

// CheckApp asks Facebook for an app token: the keys are the Meta app's, as for Facebook Pages.
func (Instagram) CheckApp(ctx context.Context, app App, _ string) (bool, error) {
	if app.InstagramLogin {
		return true, nil // Instagram login has no app token to ask for; a wrong key shows at the first sign-in
	}
	return metaCheckApp(ctx, igGraph, igGraph.url("/oauth/access_token"), app)
}

// instagramScopes: what Instagram login asks for (the Instagram app's keys).
const instagramScopes = "instagram_business_basic,instagram_business_content_publish,instagram_business_manage_comments,instagram_business_manage_insights"

// igFor: the Graph host of a connected account: graph.instagram.com for Instagram login, else graph.facebook.com.
func igFor(c Credentials) metaGraph {
	if c["login"] == "instagram" {
		return igLoginGraph
	}
	return igGraph
}

// AuthURL opens the sign-in. With the Instagram app's own keys (app.InstagramLogin: PROVIDER_INSTAGRAM_CLIENT_ID and
// _SECRET) it is Instagram login, on instagram.com with the Instagram account itself; otherwise Facebook Login for
// Business with the Meta app's Instagram configuration (Unified login), where the configuration, not a scope, says
// which permissions and which kind of token the sign-in gives.
func (Instagram) AuthURL(app App, redirect, state string, _ map[string]string) (string, error) {
	if app.InstagramLogin {
		q := url.Values{"client_id": {app.ClientID}, "redirect_uri": {redirect}, "response_type": {"code"},
			"scope": {instagramScopes}, "state": {state}, "enable_fb_login": {"0"}, "force_reauth": {"true"}}
		return instagramAuthorize + "?" + q.Encode(), nil
	}
	if app.ConfigID == "" {
		// For whoever runs the server: PROVIDER_INSTAGRAM_CONFIG_ID is the configuration made under the Meta app's
		// Instagram use case, API setup with Unified login (or the Instagram app's own keys for Instagram login).
		return "", &Failure{Kind: ErrRejected, Msg: "Instagram needs the Meta app's login configuration: whoever runs this server sets PROVIDER_INSTAGRAM_CONFIG_ID"}
	}
	q := url.Values{
		"client_id":    {app.ClientID},
		"config_id":    {app.ConfigID},
		"redirect_uri": {redirect},
		// Otherwise an existing Facebook session skips the Instagram login choice.
		"prompt":        {"login"},
		"response_type": {"code"},
		"state":         {state},
	}
	return facebookAuthorize + "?" + q.Encode(), nil
}

// Exchange trades the code for the token; the Instagram accounts it reaches come from Accounts.
func (Instagram) Exchange(ctx context.Context, app App, redirect, code string, _ map[string]string) (Credentials, Profile, error) {
	if app.InstagramLogin {
		return instagramLoginExchange(ctx, app, redirect, strings.TrimSuffix(code, "#_"))
	}
	q := url.Values{"client_id": {app.ClientID}, "client_secret": {app.ClientSecret}, "redirect_uri": {redirect},
		"code": {strings.TrimSuffix(code, "#_")}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, igGraph.url("/oauth/access_token")+"?"+q.Encode(), nil)
	if err != nil {
		return nil, Profile{}, err
	}
	var t instagramLong
	if err := igGraph.do(ctx, req, &t); err != nil {
		return nil, Profile{}, instagramStep(err, "trading the sign-in's code")
	}
	if t.AccessToken == "" {
		return nil, Profile{}, errors.New("Instagram returned no access token")
	}
	// The permissions come from the app's configuration; one left out there is missing here too.
	granted, err := metaGranted(ctx, igGraph, t.AccessToken)
	if err != nil {
		return nil, Profile{}, err
	}
	if !granted["instagram_content_publish"] || !granted["instagram_basic"] {
		// For whoever runs the server: the configuration needs instagram_basic and instagram_content_publish.
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Instagram did not allow staqly to post for this account: the app's login configuration must include publishing; connect again once it does"}
	}
	creds := t.into(Credentials{}, time.Now())
	if !granted["instagram_manage_comments"] {
		creds["no_comments"] = "true"
	}
	return creds, Profile{NoComments: creds["no_comments"] == "true"}, nil
}

var _ AccountLister = Instagram{}

// instagramAccount is an Instagram account the sign-in reaches, as /me/assigned_instagram_accounts lists it.
type instagramAccount struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
	Picture  string `json:"profile_picture_url"`
}

func (a instagramAccount) profile() Profile {
	return Profile{Handle: "@" + a.Username, Name: cmp.Or(a.Name, a.Username), AvatarURL: a.Picture,
		ProfileURL: "https://www.instagram.com/" + a.Username + "/"}
}

// instagramAccounts lists the Instagram accounts the person shared when they signed in (the token's /me is the
// business integration's system user, not an Instagram account).
func instagramAccounts(ctx context.Context, c Credentials) ([]instagramAccount, error) {
	token := c["access_token"]
	if token == "" {
		return nil, &Failure{Kind: ErrReauth, Msg: "Instagram: no sign-in stored; connect again"}
	}
	if c["login"] == "instagram" {
		a, err := instagramMe(ctx, token)
		if err != nil {
			return nil, err
		}
		return []instagramAccount{a}, nil
	}
	var out []instagramAccount
	err := metaEach(ctx, igGraph, token, "/me/assigned_instagram_accounts", url.Values{"fields": {"id,username,name,profile_picture_url"}, "limit": {"100"}},
		func(raw json.RawMessage) {
			var a instagramAccount
			if json.Unmarshal(raw, &a) == nil && a.ID != "" {
				out = append(out, a)
			}
		})
	if err != nil {
		return nil, instagramStep(err, "listing the Instagram accounts")
	}
	if len(out) == 0 {
		return nil, &Failure{Kind: ErrRejected, Msg: "This sign-in shared no Instagram account: connect again and choose the account when Instagram asks (it must be a Business or Creator account)"}
	}
	return out, nil
}

func (Instagram) Accounts(ctx context.Context, c Credentials) ([]Account, error) {
	accounts, err := instagramAccounts(ctx, c)
	if err != nil {
		return nil, err
	}
	out := make([]Account, 0, len(accounts))
	for _, a := range accounts {
		p := a.profile()
		out = append(out, Account{ID: a.ID, Name: p.Name, Handle: p.Handle, AvatarURL: p.AvatarURL, ProfileURL: p.ProfileURL})
	}
	return out, nil
}

// UseAccount keeps the sign-in's token (it reaches every shared account) with the account picked.
func (Instagram) UseAccount(ctx context.Context, c Credentials, accountID string) (Credentials, Profile, error) {
	accounts, err := instagramAccounts(ctx, c)
	if err != nil {
		return nil, Profile{}, err
	}
	for _, a := range accounts {
		if a.ID != accountID {
			continue
		}
		creds := Credentials{"user_id": a.ID}
		for _, k := range []string{"access_token", "expires_at", "no_comments", "login"} {
			if c[k] != "" {
				creds[k] = c[k]
			}
		}
		p := a.profile()
		p.NoComments = c["no_comments"] == "true"
		return creds, p, nil
	}
	return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "This sign-in no longer reaches that Instagram account; connect again and choose it"}
}

// instagramStep says which step of connecting an error comes from (Meta's own messages rarely do).
func instagramStep(err error, step string) error {
	var f *Failure
	if errors.As(err, &f) {
		return &Failure{Kind: f.Kind, Msg: f.Msg + " (" + step + ")"}
	}
	return fmt.Errorf("%w (%s)", err, step)
}

type instagramLong struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
}

func (t instagramLong) into(c Credentials, now time.Time) Credentials {
	out := Credentials{}
	for k, v := range c {
		out[k] = v
	}
	out["access_token"] = t.AccessToken
	if t.ExpiresIn > 0 {
		out["expires_at"] = now.Add(time.Duration(t.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)
	}
	return out
}

// Refresh checks the token still reaches the account: the system user token can't be renewed (a configuration
// whose tokens never expire is the one to use), and the person may remove the app or move the account to another
// business portfolio.
func (Instagram) Refresh(ctx context.Context, app App, c Credentials) (Credentials, error) {
	if c["access_token"] == "" || c["user_id"] == "" {
		return nil, &Failure{Kind: ErrReauth, Msg: "Instagram: no account stored; reconnect the account"}
	}
	if exp, ok := credentialsExpiry(c); ok && !exp.After(time.Now()) {
		return nil, &Failure{Kind: ErrReauth, Msg: "Instagram's sign-in ran out; reconnect the account"}
	}
	if c["login"] == "instagram" {
		return instagramLoginRefresh(ctx, c)
	}
	if err := igGraph.get(ctx, c["access_token"], "/"+c["user_id"], url.Values{"fields": {"id"}}, nil); err != nil {
		return nil, err
	}
	c = maps.Clone(c)
	if err := metaPermissions(ctx, igGraph, app, c["access_token"], c["user_id"], "instagram_content_publish", "instagram_manage_comments", c); err != nil {
		return nil, err
	}
	return c, nil
}

// Instagram publishes a post's thread as comments under it.
var _ Replier = Instagram{}

func (Instagram) Publish(ctx context.Context, c Credentials, p PostContent, s Settings) (Result, error) {
	token, user := c["access_token"], c["user_id"]
	if token == "" || user == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "Instagram: no account stored; reconnect the account"}
	}
	return instagramPublish(ctx, igFor(c), token, user, p, s)
}

// instagramPublish publishes a post to an Instagram account through g (graph.facebook.com, or graph.instagram.com
// for Instagram login: the same Content Publishing API).
func instagramPublish(ctx context.Context, g metaGraph, token, user string, p PostContent, s Settings) (Result, error) {
	for _, m := range p.Media {
		if m.SignedURL == "" {
			// For whoever runs the server: Instagram downloads media itself, so files must be in S3-compatible
			// storage (STORAGE_DRIVER=s3) at an address it can reach.
			return Result{}, &Failure{Kind: ErrRejected, Msg: "Instagram downloads a post's pictures and videos from a link, and this server keeps them where Instagram can't reach; ask whoever runs it to store media in the cloud"}
		}
	}
	if len(p.Media) == 0 {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Instagram needs at least one picture or video"}
	}
	base := url.Values{"caption": {p.Text}}
	if names := instagramCollaborators(s["collaborators"]); names != "" {
		base.Set("collaborators", names)
	}
	var container string
	var err error
	m := p.Media[0]
	video := strings.HasPrefix(m.MimeType, "video/")
	switch {
	case s["post_type"] == "story":
		// Stories take no caption.
		form := url.Values{"media_type": {"STORIES"}}
		instagramMediaURL(form, m)
		container, err = instagramContainer(ctx, g, token, user, form)
	case len(p.Media) > 1:
		var children []string
		for _, m := range p.Media {
			form := url.Values{"is_carousel_item": {"true"}}
			instagramMediaURL(form, m)
			if strings.HasPrefix(m.MimeType, "video/") {
				form.Set("media_type", "VIDEO")
			}
			id, err := instagramContainer(ctx, g, token, user, form)
			if err != nil {
				return Result{}, err
			}
			children = append(children, id)
		}
		base.Set("media_type", "CAROUSEL")
		base.Set("children", strings.Join(children, ","))
		container, err = instagramContainer(ctx, g, token, user, base)
	case video:
		// A single video is a reel: Instagram publishes no other kind of video post.
		base.Set("media_type", "REELS")
		base.Set("video_url", m.SignedURL)
		share := "true"
		if s["post_type"] == "reel" && s["share_to_feed"] == "false" {
			share = "false"
		}
		base.Set("share_to_feed", share)
		container, err = instagramContainer(ctx, g, token, user, base)
	default:
		base.Set("image_url", m.SignedURL)
		container, err = instagramContainer(ctx, g, token, user, base)
	}
	if err != nil {
		return Result{}, err
	}

	var out struct {
		ID string `json:"id"`
	}
	req, err := g.post(ctx, token, "/"+user+"/media_publish", url.Values{"creation_id": {container}})
	if err != nil {
		return Result{}, err
	}
	// Instagram has no idempotency key and a new attempt makes new containers, so publishing again after an answer
	// that never came could post twice: say so instead of retrying.
	if err := g.do(ctx, req, &out); err != nil {
		return Result{}, g.lost(err)
	}
	if out.ID == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Instagram published the post but did not say its id; check the profile"}
	}
	return Result{ExternalID: out.ID, URL: instagramPermalink(ctx, g, token, out.ID)}, nil
}

// instagramMediaURL sets the link Instagram fetches the file from.
func instagramMediaURL(form url.Values, m MediaFile) {
	if strings.HasPrefix(m.MimeType, "video/") {
		form.Set("video_url", m.SignedURL)
	} else {
		form.Set("image_url", m.SignedURL)
	}
}

// instagramCollaborators turns "@ana, ben" into the JSON list Instagram takes (["ana","ben"]), or "" for none.
func instagramCollaborators(v string) string {
	var names []string
	for _, n := range ListValues(v) {
		names = append(names, strings.TrimPrefix(n, "@"))
	}
	if len(names) == 0 {
		return ""
	}
	b, _ := json.Marshal(names)
	return string(b)
}

// instagramContainer creates a media container and waits until Instagram has fetched and processed it.
func instagramContainer(ctx context.Context, g metaGraph, token, user string, form url.Values) (string, error) {
	req, err := g.post(ctx, token, "/"+user+"/media", form)
	if err != nil {
		return "", err
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := g.do(ctx, req, &out); err != nil {
		return "", err
	}
	for {
		var st struct {
			StatusCode string `json:"status_code"`
			Status     string `json:"status"`
		}
		if err := g.get(ctx, token, "/"+out.ID, url.Values{"fields": {"status_code,status"}}, &st); err != nil {
			return "", err
		}
		switch st.StatusCode {
		case "FINISHED", "PUBLISHED":
			return out.ID, nil
		case "ERROR":
			return "", &Failure{Kind: ErrRejected, Msg: "Instagram could not use the media (" + st.Status + ")"}
		case "EXPIRED":
			// Not published within a day: the next attempt makes a new container.
			return "", &Failure{Msg: "Instagram's upload expired; staqly tries again in a few minutes"}
		}
		// The job's own deadline ends a video that never finishes; its next attempt starts again.
		if err := metaWait(ctx, instagramPoll); err != nil {
			return "", err
		}
	}
}

// instagramPermalink is the post's address, or the app's home when Instagram doesn't say (the post is out anyway).
func instagramPermalink(ctx context.Context, g metaGraph, token, id string) string {
	var out struct {
		Permalink string `json:"permalink"`
	}
	if err := g.get(ctx, token, "/"+id, url.Values{"fields": {"permalink"}}, &out); err != nil || out.Permalink == "" {
		return "https://www.instagram.com/"
	}
	return out.Permalink
}

// Reply comments under the post. parentExternalID is the post or, from the second part on, the comment before
// ("comment:<post>:<comment>"); every part becomes a comment on the post itself, in order.
func (Instagram) Reply(ctx context.Context, c Credentials, parentExternalID string, p PostContent, _ Settings) (Result, error) {
	return instagramReply(ctx, igFor(c), c, parentExternalID, p)
}

func instagramReply(ctx context.Context, g metaGraph, c Credentials, parentExternalID string, p PostContent) (Result, error) {
	token := c["access_token"]
	if token == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "Instagram: no account stored; reconnect the account"}
	}
	post := parentExternalID
	if rest, ok := strings.CutPrefix(parentExternalID, "comment:"); ok {
		post, _, _ = strings.Cut(rest, ":")
	}
	if post == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Instagram: nothing to comment on (the post has no id)"}
	}
	if len(p.Media) > 0 {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Instagram comments take text only"}
	}
	req, err := g.post(ctx, token, "/"+post+"/comments", url.Values{"message": {p.Text}})
	if err != nil {
		return Result{}, err
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := g.do(ctx, req, &out); err != nil {
		return Result{}, err
	}
	return Result{ExternalID: "comment:" + post + ":" + out.ID, URL: instagramPermalink(ctx, g, token, post)}, nil
}

// ---- Instagram login (the Instagram app's own keys) ----

// instagramMe is the account an Instagram login token belongs to. user_id is the professional account's id, the one
// the Content Publishing API takes (id is app-scoped).
func instagramMe(ctx context.Context, token string) (instagramAccount, error) {
	var me struct {
		ID       string `json:"id"`
		UserID   string `json:"user_id"`
		Username string `json:"username"`
		Name     string `json:"name"`
		Picture  string `json:"profile_picture_url"`
	}
	if err := igLoginGraph.get(ctx, token, "/me", url.Values{"fields": {"user_id,username,name,profile_picture_url"}}, &me); err != nil {
		return instagramAccount{}, instagramStep(err, "reading the account")
	}
	id := cmp.Or(me.UserID, me.ID)
	if id == "" {
		return instagramAccount{}, errors.New("Instagram did not say which account signed in")
	}
	return instagramAccount{ID: id, Username: me.Username, Name: me.Name, Picture: me.Picture}, nil
}

// instagramLoginExchange trades the code at api.instagram.com for a short token, then for the 60-day one.
func instagramLoginExchange(ctx context.Context, app App, redirect, code string) (Credentials, Profile, error) {
	form := url.Values{"client_id": {app.ClientID}, "client_secret": {app.ClientSecret}, "grant_type": {"authorization_code"},
		"redirect_uri": {redirect}, "code": {code}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, instagramTokenURL(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, Profile{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// Instagram answers {"access_token","user_id","permissions"} or the same inside "data": [...].
	var short struct {
		AccessToken string          `json:"access_token"`
		Permissions json.RawMessage `json:"permissions"`
		Data        []struct {
			AccessToken string          `json:"access_token"`
			Permissions json.RawMessage `json:"permissions"`
		} `json:"data"`
	}
	if err := igLoginGraph.do(ctx, req, &short); err != nil {
		return nil, Profile{}, instagramStep(err, "trading the sign-in's code")
	}
	token, perms := short.AccessToken, short.Permissions
	if token == "" && len(short.Data) > 0 {
		token, perms = short.Data[0].AccessToken, short.Data[0].Permissions
	}
	if token == "" {
		return nil, Profile{}, errors.New("Instagram returned no access token")
	}
	long, err := instagramLoginLong(ctx, "/access_token", url.Values{"grant_type": {"ig_exchange_token"},
		"client_secret": {app.ClientSecret}, "access_token": {token}})
	if err != nil {
		return nil, Profile{}, instagramStep(err, "trading the sign-in for the 60-day token")
	}
	creds := long.into(Credentials{"login": "instagram"}, time.Now())
	granted := instagramPermissions(perms)
	if granted != nil && !granted["instagram_business_content_publish"] {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Instagram did not allow staqly to post for this account: connect again and allow publishing"}
	}
	if granted != nil && !granted["instagram_business_manage_comments"] {
		creds["no_comments"] = "true"
	}
	return creds, Profile{NoComments: creds["no_comments"] == "true"}, nil
}

// instagramPermissions reads the granted scopes ("a,b" or ["a","b"]); nil when Instagram didn't list them.
func instagramPermissions(raw json.RawMessage) map[string]bool {
	var list []string
	if json.Unmarshal(raw, &list) != nil {
		var s string
		if json.Unmarshal(raw, &s) != nil || s == "" {
			return nil
		}
		list = strings.Split(s, ",")
	}
	out := map[string]bool{}
	for _, p := range list {
		out[strings.TrimSpace(p)] = true
	}
	return out
}

func instagramTokenURL() string {
	if instagramGraph != "https://graph.instagram.com" {
		return instagramGraph + "/oauth/access_token" // tests: one fake host for everything
	}
	return instagramToken
}

// instagramLoginLong asks graph.instagram.com for a 60-day token (ig_exchange_token) or a renewed one (ig_refresh_token).
func instagramLoginLong(ctx context.Context, path string, q url.Values) (instagramLong, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, instagramGraph+path+"?"+q.Encode(), nil)
	if err != nil {
		return instagramLong{}, err
	}
	var t instagramLong
	if err := igLoginGraph.do(ctx, req, &t); err != nil {
		return instagramLong{}, err
	}
	if t.AccessToken == "" {
		return instagramLong{}, errors.New("Instagram returned no access token")
	}
	return t, nil
}

// instagramLoginRefresh renews the 60-day token once fewer than 30 days are left (Instagram renews a token at
// least a day old), and otherwise checks it still reaches the account.
func instagramLoginRefresh(ctx context.Context, c Credentials) (Credentials, error) {
	exp, ok := credentialsExpiry(c)
	if ok && exp.After(time.Now().Add(30*24*time.Hour)) {
		if err := igLoginGraph.get(ctx, c["access_token"], "/me", url.Values{"fields": {"user_id"}}, nil); err != nil {
			return nil, err
		}
		return c, nil
	}
	t, err := instagramLoginLong(ctx, "/refresh_access_token", url.Values{"grant_type": {"ig_refresh_token"}, "access_token": {c["access_token"]}})
	if err != nil {
		return nil, err
	}
	return t.into(c, time.Now()), nil
}
