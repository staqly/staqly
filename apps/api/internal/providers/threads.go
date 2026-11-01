package providers

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Threads over the Threads API: OAuth 2.0 on threads.com, a short-lived token traded for a 60-day one and refreshed
// before it runs out, posts as containers that are made, processed and then published. Text alone is a post; Threads
// fetches pictures and videos itself from a link, so media needs a public address (MediaFile.SignedURL: S3-compatible
// storage). A post's thread goes out as replies, each under the one before.
type Threads struct{ Base }

// Endpoints, variables so tests point them at httptest.
var (
	threadsAuthorize = "https://threads.net/oauth/authorize"
	threadsToken     = "https://graph.threads.net/oauth/access_token"
	threadsGraph     = "https://graph.threads.net"
	// threadsPoll is how long to wait between checks of a container Threads is still processing.
	threadsPoll = 3 * time.Second
)

const threadsScopes = "threads_basic,threads_content_publish,threads_manage_replies,threads_manage_insights,threads_read_replies,threads_delete"

func init() {
	l := Limits{MaxChars: 500, MaxMedia: 10, MediaKinds: []string{"image", "video"}}
	Register(Threads{NewBase("threads", "Threads", AuthOAuth2, l).WithSettings(soonSettings("threads", l)...)}, StatusAvailable)
}

// Threads calls back over https only and downloads every picture and video itself.
func (Threads) Needs() Needs { return Needs{HTTPS: true, PublicMedia: true} }

// CheckApp asks Threads for an app token: it gives one only for the right Threads app ID and secret.
func (Threads) CheckApp(ctx context.Context, app App, _ string) (bool, error) {
	return metaCheckApp(ctx, thGraph, threadsToken, app)
}

func (t Threads) Validate(p PostContent, s Settings) error {
	if err := CheckLimits(t.Limits(), p, s); err != nil {
		return err
	}
	check := func(media []MediaFile) error {
		for _, m := range media {
			if !strings.HasPrefix(m.MimeType, "video/") && m.MimeType != "image/jpeg" && m.MimeType != "image/png" {
				return invalid("media", "Threads takes pictures as JPEG or PNG; save "+m.Name+" as one of them (open it in the design editor and save it)")
			}
		}
		return nil
	}
	if err := check(p.Media); err != nil {
		return err
	}
	for _, part := range p.Thread {
		if len(part.Media) > t.Limits().MaxMedia {
			return invalid("media", "a Threads reply takes at most 10 files")
		}
		if err := check(part.Media); err != nil {
			return err
		}
	}
	return CheckFields(t.SettingsFields(), s)
}

func (Threads) AuthURL(app App, redirect, state string, _ map[string]string) (string, error) {
	q := url.Values{
		"client_id":     {app.ClientID},
		"redirect_uri":  {redirect},
		"response_type": {"code"},
		"scope":         {threadsScopes},
		"state":         {state},
	}
	return threadsAuthorize + "?" + q.Encode(), nil
}

func (Threads) Exchange(ctx context.Context, app App, redirect, code string, _ map[string]string) (Credentials, Profile, error) {
	form := url.Values{
		"client_id":     {app.ClientID},
		"client_secret": {app.ClientSecret},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {redirect},
		"code":          {strings.TrimSuffix(code, "#_")},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, threadsToken, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, Profile{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var short struct {
		AccessToken string `json:"access_token"`
	}
	if err := thGraph.do(ctx, req, &short); err != nil {
		return nil, Profile{}, instagramStep(err, "trading the sign-in's code")
	}
	if short.AccessToken == "" {
		return nil, Profile{}, errors.New("Threads returned no access token")
	}
	// Trade the hour-long token for one that lasts 60 days.
	long, err := threadsLongToken(ctx, "/access_token", url.Values{"grant_type": {"th_exchange_token"}, "client_secret": {app.ClientSecret},
		"access_token": {short.AccessToken}})
	if err != nil {
		return nil, Profile{}, instagramStep(err, "trading the sign-in for the 60-day token")
	}
	var me struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Name     string `json:"name"`
		Picture  string `json:"threads_profile_picture_url"`
	}
	if err := thGraph.get(ctx, long.AccessToken, "/me", url.Values{"fields": {"id,username,name,threads_profile_picture_url"}}, &me); err != nil {
		return nil, Profile{}, instagramStep(err, "reading the account")
	}
	if me.ID == "" || me.Username == "" {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Threads did not say which account signed in"}
	}
	name := me.Name
	if name == "" {
		name = me.Username
	}
	return long.into(Credentials{"user_id": me.ID}, time.Now()), Profile{
		Handle:     "@" + me.Username,
		Name:       name,
		AvatarURL:  me.Picture,
		ProfileURL: "https://www.threads.com/@" + me.Username,
	}, nil
}

// threadsLongToken calls the long-lived token endpoints (/access_token, /refresh_access_token).
func threadsLongToken(ctx context.Context, path string, q url.Values) (instagramLong, error) {
	var t instagramLong
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, threadsGraph+path+"?"+q.Encode(), nil)
	if err != nil {
		return t, err
	}
	if err := thGraph.do(ctx, req, &t); err != nil {
		return t, err
	}
	if t.AccessToken == "" {
		return t, errors.New("Threads returned no access token")
	}
	return t, nil
}

// Refresh renews the 60-day token once it has less than a month left (Threads takes a refresh of a token at least a
// day old and still valid); a younger one is only checked, so a channel test finds a revoked sign-in.
func (Threads) Refresh(ctx context.Context, _ App, c Credentials) (Credentials, error) {
	token := c["access_token"]
	if token == "" {
		return nil, &Failure{Kind: ErrReauth, Msg: "Threads: no account stored; reconnect the account"}
	}
	exp, ok := credentialsExpiry(c)
	if ok && !exp.After(time.Now()) {
		return nil, &Failure{Kind: ErrReauth, Msg: "Threads' sign-in ran out; reconnect the account"}
	}
	if ok && exp.After(time.Now().Add(30*24*time.Hour)) {
		if err := thGraph.get(ctx, token, "/me", url.Values{"fields": {"id"}}, nil); err != nil {
			return nil, err
		}
		return c, nil
	}
	t, err := threadsLongToken(ctx, "/refresh_access_token", url.Values{"grant_type": {"th_refresh_token"}, "access_token": {token}})
	if err != nil {
		return nil, err
	}
	return t.into(c, time.Now()), nil
}

// Threads publishes a post's thread as replies, each under the one before.
var _ Replier = Threads{}

func (t Threads) Publish(ctx context.Context, c Credentials, p PostContent, s Settings) (Result, error) {
	form := url.Values{}
	if v := s["reply_control"]; v != "" {
		form.Set("reply_control", v)
	}
	return t.post(ctx, c, p, form)
}

// Reply publishes a part of the thread under parentExternalID: the post, or from the second part on the reply before.
func (t Threads) Reply(ctx context.Context, c Credentials, parentExternalID string, p PostContent, _ Settings) (Result, error) {
	if parentExternalID == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Threads: nothing to reply to (the post has no id)"}
	}
	return t.post(ctx, c, p, url.Values{"reply_to_id": {parentExternalID}})
}

// post makes the container (text, one file, or a carousel of up to ten), waits until Threads has processed it and
// publishes it. extra carries reply_control or reply_to_id.
func (Threads) post(ctx context.Context, c Credentials, p PostContent, extra url.Values) (Result, error) {
	token, user := c["access_token"], c["user_id"]
	if token == "" || user == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "Threads: no account stored; reconnect the account"}
	}
	for _, m := range p.Media {
		if m.SignedURL == "" {
			// For whoever runs the server: Threads downloads media itself, so files must be in S3-compatible
			// storage (STORAGE_DRIVER=s3) at an address it can reach.
			return Result{}, &Failure{Kind: ErrRejected, Msg: "Threads downloads a post's pictures and videos from a link, and this server keeps them where Threads can't reach; ask whoever runs it to store media in the cloud"}
		}
	}
	form := url.Values{"text": {p.Text}}
	for k, v := range extra {
		form[k] = v
	}
	switch len(p.Media) {
	case 0:
		form.Set("media_type", "TEXT")
	case 1:
		threadsMedia(form, p.Media[0])
	default:
		var children []string
		for _, m := range p.Media {
			child := url.Values{"is_carousel_item": {"true"}}
			threadsMedia(child, m)
			id, err := threadsContainer(ctx, token, user, child)
			if err != nil {
				return Result{}, err
			}
			children = append(children, id)
		}
		form.Set("media_type", "CAROUSEL")
		form.Set("children", strings.Join(children, ","))
	}
	container, err := threadsContainer(ctx, token, user, form)
	if err != nil {
		return Result{}, err
	}

	req, err := thGraph.post(ctx, token, "/"+user+"/threads_publish", url.Values{"creation_id": {container}})
	if err != nil {
		return Result{}, err
	}
	var out struct {
		ID string `json:"id"`
	}
	// Threads has no idempotency key and a new attempt makes new containers, so publishing again after an answer that
	// never came could post twice: say so instead of retrying.
	if err := thGraph.do(ctx, req, &out); err != nil {
		return Result{}, thGraph.lost(err)
	}
	if out.ID == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Threads published the post but did not say its id; check the profile"}
	}
	return Result{ExternalID: out.ID, URL: threadsPermalink(ctx, token, out.ID)}, nil
}

// threadsMedia sets the file's type and the link Threads fetches it from.
func threadsMedia(form url.Values, m MediaFile) {
	if strings.HasPrefix(m.MimeType, "video/") {
		form.Set("media_type", "VIDEO")
		form.Set("video_url", m.SignedURL)
	} else {
		form.Set("media_type", "IMAGE")
		form.Set("image_url", m.SignedURL)
	}
}

// threadsContainer creates a container and waits until Threads has fetched and processed it.
func threadsContainer(ctx context.Context, token, user string, form url.Values) (string, error) {
	req, err := thGraph.post(ctx, token, "/"+user+"/threads", form)
	if err != nil {
		return "", err
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := thGraph.do(ctx, req, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", &Failure{Msg: "Threads did not say the new post's id; staqly tries again in a few minutes"}
	}
	for {
		var st struct {
			Status       string `json:"status"`
			ErrorMessage string `json:"error_message"`
		}
		if err := thGraph.get(ctx, token, "/"+out.ID, url.Values{"fields": {"status,error_message"}}, &st); err != nil {
			return "", err
		}
		switch st.Status {
		case "FINISHED", "PUBLISHED":
			return out.ID, nil
		case "ERROR":
			why := st.ErrorMessage
			if why == "" || why == "UNKNOWN" {
				why = "check the file's format and size"
			}
			return "", &Failure{Kind: ErrRejected, Msg: "Threads could not use the media (" + why + ")"}
		case "EXPIRED":
			// Not published within a day: the next attempt makes a new container.
			return "", &Failure{Msg: "Threads' upload expired; staqly tries again in a few minutes"}
		}
		// The job's own deadline ends a video that never finishes; its next attempt starts again.
		if err := metaWait(ctx, threadsPoll); err != nil {
			return "", err
		}
	}
}

// threadsPermalink is the post's address, or the app's home when Threads doesn't say (the post is out anyway).
func threadsPermalink(ctx context.Context, token, id string) string {
	var out struct {
		Permalink string `json:"permalink"`
	}
	if err := thGraph.get(ctx, token, "/"+id, url.Values{"fields": {"permalink"}}, &out); err != nil || out.Permalink == "" {
		return "https://www.threads.com/"
	}
	return out.Permalink
}

// threadsSpecial: Threads' own error codes, before the ones Meta's platforms share.
func threadsSpecial(e metaErr) error {
	switch {
	case e.sub == 4279009:
		return &Failure{Msg: "Threads has not finished taking in the media yet; staqly tries again in a few minutes"}
	case e.sub == 4279013:
		return &Failure{Kind: ErrRejected, Msg: "Threads has restricted this account (" + e.detail + ")"}
	case strings.Contains(e.lower, "could not be fetched"):
		return &Failure{Kind: ErrRejected, Msg: "Threads could not download the media from its link (" + e.detail + "); check that the server's storage is reachable from the internet"}
	}
	return nil
}
