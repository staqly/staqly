package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Instagram (a professional account, Business or Creator, without a Facebook Page) over the Instagram API with
// Instagram Login: OAuth 2.0 on instagram.com, a short-lived token traded for a 60-day one and refreshed before it
// runs out, posts through the Content Publishing API. Instagram fetches every picture and video itself from a link,
// so media needs a public address (MediaFile.SignedURL: S3-compatible storage). Comments are the post's thread.
type Instagram struct{ Base }

// Endpoints, variables so tests point them at httptest.
var (
	instagramAuthorize = "https://www.instagram.com/oauth/authorize"
	instagramToken     = "https://api.instagram.com/oauth/access_token"
	instagramGraph     = "https://graph.instagram.com"
	// instagramPoll is how long to wait between checks of a container Instagram is still processing.
	instagramPoll = 3 * time.Second
)

const (
	instagramScopes = "instagram_business_basic,instagram_business_content_publish,instagram_business_manage_comments"
	// instagramVersion is the Graph API version every call names. ponytail: Meta keeps a version about two years;
	// bump it once a year (https://developers.facebook.com/docs/graph-api/changelog).
	instagramVersion = "v24.0"
)

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
			return invalid("media", "Instagram takes pictures as JPEG; save "+m.Name+" as JPEG (the media editor does it)")
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

func (Instagram) AuthURL(app App, redirect, state string, _ map[string]string) (string, error) {
	q := url.Values{
		"enable_fb_login": {"0"}, // Instagram's own sign-in, not "Continue with Facebook"
		"client_id":       {app.ClientID},
		"redirect_uri":    {redirect},
		"response_type":   {"code"},
		"scope":           {instagramScopes},
		"state":           {state},
	}
	return instagramAuthorize + "?" + q.Encode(), nil
}

func (i Instagram) Exchange(ctx context.Context, app App, redirect, code string, _ map[string]string) (Credentials, Profile, error) {
	// Instagram appends "#_" to the code it sends back.
	code = strings.TrimSuffix(code, "#_")
	form := url.Values{
		"client_id":     {app.ClientID},
		"client_secret": {app.ClientSecret},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {redirect},
		"code":          {code},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, instagramToken, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, Profile{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	type short struct {
		AccessToken string `json:"access_token"`
		Permissions any    `json:"permissions"` // a comma-separated string, or a list
	}
	// The token comes alone or inside "data", depending on the API's age.
	var out struct {
		short
		Data []short `json:"data"`
	}
	if err := instagramDo(ctx, req, &out); err != nil {
		return nil, Profile{}, err
	}
	t := out.short
	if t.AccessToken == "" && len(out.Data) > 0 {
		t = out.Data[0]
	}
	if t.AccessToken == "" {
		return nil, Profile{}, errors.New("Instagram returned no access token")
	}
	perms := fmt.Sprint(t.Permissions)
	if t.Permissions != nil && !strings.Contains(perms, "instagram_business_content_publish") {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Instagram did not allow posting: allow publishing when you connect, then try again"}
	}

	// Trade the hour-long token for one that lasts 60 days.
	long, err := instagramLongToken(ctx, url.Values{"grant_type": {"ig_exchange_token"}, "client_id": {app.ClientID}, "client_secret": {app.ClientSecret},
		"access_token": {t.AccessToken}}, "/access_token")
	if err != nil {
		// Instagram signs in any account but then refuses its token on every call ("Unsupported request", code 100)
		// when the app may not serve it: a personal account, or one without a role on an app not yet reviewed.
		var f *Failure
		if errors.As(err, &f) && strings.Contains(f.Msg, "Unsupported request") {
			return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Instagram signed this account in but won't let the app use it (" + f.Msg + "): it must be a Business or Creator account, and while the app is in testing, one added as a tester. Check the account you are signed in to on instagram.com"}
		}
		return nil, Profile{}, instagramStep(err, "trading the sign-in for the 60-day token")
	}
	var me struct {
		ID          string `json:"id"`
		UserID      string `json:"user_id"`
		Username    string `json:"username"`
		Name        string `json:"name"`
		Picture     string `json:"profile_picture_url"`
		AccountType string `json:"account_type"`
	}
	if err := instagramGet(ctx, long.AccessToken, "/me", url.Values{"fields": {"id,user_id,username,name,profile_picture_url,account_type"}}, &me); err != nil {
		return nil, Profile{}, instagramStep(err, "reading the account")
	}
	user := me.UserID
	if user == "" {
		user = me.ID
	}
	if user == "" {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Instagram did not say which account signed in"}
	}
	creds := long.into(Credentials{"user_id": user}, time.Now())
	name := me.Name
	if name == "" {
		name = me.Username
	}
	return creds, Profile{
		Handle:     "@" + me.Username,
		Name:       name,
		AvatarURL:  me.Picture,
		ProfileURL: "https://www.instagram.com/" + me.Username + "/",
		NoComments: t.Permissions != nil && !strings.Contains(perms, "instagram_business_manage_comments"),
	}, nil
}

// instagramStep says which step of connecting an error comes from (Instagram's own messages rarely do).
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

// instagramLongToken calls the long-lived token endpoints (/access_token, /refresh_access_token).
func instagramLongToken(ctx context.Context, q url.Values, path string) (instagramLong, error) {
	var t instagramLong
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, instagramGraph+path+"?"+q.Encode(), nil)
	if err != nil {
		return t, err
	}
	if err := instagramDo(ctx, req, &t); err != nil {
		return t, err
	}
	if t.AccessToken == "" {
		return t, errors.New("Instagram returned no access token")
	}
	return t, nil
}

// Refresh renews the 60-day token once it has less than a month left (Instagram takes a refresh of a token at least
// a day old and still valid); a younger one is only checked, so a channel test finds a revoked sign-in.
func (Instagram) Refresh(ctx context.Context, _ App, c Credentials) (Credentials, error) {
	token := c["access_token"]
	if token == "" {
		return nil, &Failure{Kind: ErrReauth, Msg: "Instagram: no account stored; reconnect the account"}
	}
	exp, ok := credentialsExpiry(c)
	if ok && !exp.After(time.Now()) {
		return nil, &Failure{Kind: ErrReauth, Msg: "Instagram's sign-in ran out; reconnect the account"}
	}
	if ok && exp.After(time.Now().Add(30*24*time.Hour)) {
		if err := instagramGet(ctx, token, "/me", url.Values{"fields": {"id"}}, nil); err != nil {
			return nil, err
		}
		return c, nil
	}
	t, err := instagramLongToken(ctx, url.Values{"grant_type": {"ig_refresh_token"}, "access_token": {token}}, "/refresh_access_token")
	if err != nil {
		return nil, err
	}
	return t.into(c, time.Now()), nil
}

// Instagram publishes a post's thread as comments under it.
var _ Replier = Instagram{}

func (i Instagram) Publish(ctx context.Context, c Credentials, p PostContent, s Settings) (Result, error) {
	token, user := c["access_token"], c["user_id"]
	if token == "" || user == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "Instagram: no account stored; reconnect the account"}
	}
	for _, m := range p.Media {
		if m.SignedURL == "" {
			// For whoever runs the server: Instagram downloads media itself, so files must be in S3-compatible
			// storage (STORAGE_DRIVER=s3) at an address it can reach.
			return Result{}, &Failure{Kind: ErrRejected, Msg: "Instagram downloads a post's pictures and videos from a link, and this server keeps them where Instagram can't reach; ask whoever runs it to store media in the cloud"}
		}
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
		container, err = i.container(ctx, token, user, form)
	case len(p.Media) > 1:
		var children []string
		for _, m := range p.Media {
			form := url.Values{"is_carousel_item": {"true"}}
			instagramMediaURL(form, m)
			if strings.HasPrefix(m.MimeType, "video/") {
				form.Set("media_type", "VIDEO")
			}
			id, err := i.container(ctx, token, user, form)
			if err != nil {
				return Result{}, err
			}
			children = append(children, id)
		}
		base.Set("media_type", "CAROUSEL")
		base.Set("children", strings.Join(children, ","))
		container, err = i.container(ctx, token, user, base)
	case video:
		// A single video is a reel: Instagram publishes no other kind of video post.
		base.Set("media_type", "REELS")
		base.Set("video_url", m.SignedURL)
		share := "true"
		if s["post_type"] == "reel" && s["share_to_feed"] == "false" {
			share = "false"
		}
		base.Set("share_to_feed", share)
		container, err = i.container(ctx, token, user, base)
	default:
		base.Set("image_url", m.SignedURL)
		container, err = i.container(ctx, token, user, base)
	}
	if err != nil {
		return Result{}, err
	}

	var out struct {
		ID string `json:"id"`
	}
	req, err := instagramPost(ctx, token, "/"+user+"/media_publish", url.Values{"creation_id": {container}})
	if err != nil {
		return Result{}, err
	}
	// Instagram has no idempotency key and a new attempt makes new containers, so publishing again after an answer
	// that never came could post twice: say so instead of retrying.
	if err := instagramDo(ctx, req, &out); err != nil {
		var f *Failure
		if !errors.As(err, &f) {
			return Result{}, &Failure{Kind: ErrRejected, Msg: "Instagram may have published the post, but its answer was lost (" + err.Error() + "); check the profile before posting it again"}
		}
		return Result{}, err
	}
	if out.ID == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Instagram published the post but did not say its id; check the profile"}
	}
	return Result{ExternalID: out.ID, URL: instagramPermalink(ctx, token, out.ID)}, nil
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

// container creates a media container and waits until Instagram has fetched and processed it.
func (Instagram) container(ctx context.Context, token, user string, form url.Values) (string, error) {
	req, err := instagramPost(ctx, token, "/"+user+"/media", form)
	if err != nil {
		return "", err
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := instagramDo(ctx, req, &out); err != nil {
		return "", err
	}
	for {
		var st struct {
			StatusCode string `json:"status_code"`
			Status     string `json:"status"`
		}
		if err := instagramGet(ctx, token, "/"+out.ID, url.Values{"fields": {"status_code,status"}}, &st); err != nil {
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
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(instagramPoll):
		}
	}
}

// instagramPermalink is the post's address, or the app's home when Instagram doesn't say (the post is out anyway).
func instagramPermalink(ctx context.Context, token, id string) string {
	var out struct {
		Permalink string `json:"permalink"`
	}
	if err := instagramGet(ctx, token, "/"+id, url.Values{"fields": {"permalink"}}, &out); err != nil || out.Permalink == "" {
		return "https://www.instagram.com/"
	}
	return out.Permalink
}

// Reply comments under the post. parentExternalID is the post or, from the second part on, the comment before
// ("comment:<post>:<comment>"); every part becomes a comment on the post itself, in order.
func (Instagram) Reply(ctx context.Context, c Credentials, parentExternalID string, p PostContent, _ Settings) (Result, error) {
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
	req, err := instagramPost(ctx, token, "/"+post+"/comments", url.Values{"message": {p.Text}})
	if err != nil {
		return Result{}, err
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := instagramDo(ctx, req, &out); err != nil {
		return Result{}, err
	}
	return Result{ExternalID: "comment:" + post + ":" + out.ID, URL: instagramPermalink(ctx, token, post)}, nil
}

func instagramGet(ctx context.Context, token, path string, q url.Values, out any) error {
	if q == nil {
		q = url.Values{}
	}
	q.Set("access_token", token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, instagramGraph+"/"+instagramVersion+path+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	return instagramDo(ctx, req, out)
}

func instagramPost(ctx context.Context, token, path string, form url.Values) (*http.Request, error) {
	form.Set("access_token", token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, instagramGraph+"/"+instagramVersion+path, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req, nil
}

// instagramDo sends a request and decodes a 2xx reply into out; Meta's errors become messages a person can act on.
// A request that never got an answer is a plain error (not a Failure), so Publish can tell it apart.
func instagramDo(ctx context.Context, req *http.Request, out any) error {
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		// The error may quote the URL, and with it the access token: name the host only.
		return fmt.Errorf("Instagram could not be reached (%s)", req.URL.Host)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out != nil && len(raw) > 0 {
			return json.Unmarshal(raw, out)
		}
		return nil
	}
	return instagramError(resp.StatusCode, raw)
}

// instagramError maps Meta's replies to what staqly does next: a sign-in problem needs reconnecting, a refused post
// is final, rate limits and outages are retried. Graph errors are {"error":{"message","code","error_subcode",
// "error_user_msg"}}; the token endpoint answers {"error_type","code","error_message"}.
func instagramError(status int, raw []byte) error {
	var e struct {
		Error struct {
			Message string `json:"message"`
			Code    int    `json:"code"`
			Subcode int    `json:"error_subcode"`
			UserMsg string `json:"error_user_msg"`
		} `json:"error"`
		ErrorType    string `json:"error_type"`
		Code         int    `json:"code"`
		ErrorMessage string `json:"error_message"`
	}
	_ = json.Unmarshal(raw, &e)
	code, detail := e.Error.Code, e.Error.Message
	if e.Error.UserMsg != "" {
		detail = e.Error.UserMsg
	}
	if detail == "" {
		detail = e.ErrorMessage
	}
	if detail == "" {
		detail = string(raw)
		if len(detail) > 200 {
			detail = detail[:200] + "..."
		}
	}
	lower := strings.ToLower(detail)
	sub := e.Error.Subcode
	switch {
	// The token endpoint: the app's keys, the return address or the sign-in's code, not the account.
	case e.ErrorMessage != "" && strings.Contains(lower, "redirect_uri"):
		// For whoever runs the server: the app's Business login settings need <dashboard>/channels/callback/instagram.
		return &Failure{Kind: ErrRejected, Msg: "Instagram did not accept the return address (" + detail + ")"}
	case e.ErrorMessage != "" && (strings.Contains(lower, "client") || strings.Contains(lower, "app")):
		// For whoever runs the server: PROVIDER_INSTAGRAM_CLIENT_ID and _SECRET are the Instagram App ID and secret.
		return &Failure{Kind: ErrRejected, Msg: "Instagram did not accept this server's app (" + detail + ")"}
	case e.ErrorMessage != "":
		return &Failure{Kind: ErrRejected, Msg: "Instagram did not accept the sign-in (" + detail + "); connect again"}
	case strings.Contains(lower, "not an instagram business"):
		return &Failure{Kind: ErrReauth, Msg: "Instagram posts only from professional accounts: switch the account to Business or Creator in the Instagram app, then reconnect it"}
	case code == 190, code == 102:
		return &Failure{Kind: ErrReauth, Msg: "Instagram no longer accepts this account's sign-in (" + detail + "); reconnect it"}
	case code == 4, code == 17, code == 32, code == 613, code == 80002, status == http.StatusTooManyRequests:
		return &Failure{Msg: "Instagram rate limit reached; staqly tries again in a few minutes"}
	case sub == 2207052, sub == 2207003:
		// Instagram could not download the file in time: often a slow first fetch.
		return &Failure{Msg: "Instagram could not download the media (" + detail + "); staqly tries again in a few minutes"}
	case sub == 2207042:
		return &Failure{Kind: ErrRejected, Msg: "Instagram's limit of posts a day for this account is reached (" + detail + "); schedule it for tomorrow"}
	case sub == 2207050:
		return &Failure{Kind: ErrRejected, Msg: "Instagram has restricted this account (" + detail + ")"}
	case code == 1, code == 2, status >= 500:
		return &Failure{Msg: fmt.Sprintf("Instagram is unavailable (%s); staqly tries again in a few minutes", detail)}
	case code == 10, code == 200, status == http.StatusForbidden:
		return &Failure{Kind: ErrRejected, Msg: "Instagram refused it (" + detail + ")"}
	case code == 9004, strings.Contains(lower, "media"):
		return &Failure{Kind: ErrRejected, Msg: "Instagram could not use the media (" + detail + ")"}
	}
	return &Failure{Kind: ErrRejected, Msg: "Instagram: " + detail}
}
