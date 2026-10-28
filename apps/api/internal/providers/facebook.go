package providers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Facebook Pages over the Graph API with Facebook Login: the person signs in on facebook.com, the code is traded for
// a 60-day user token, the person picks the Pages to connect (AccountLister) and each channel keeps its Page's own
// token, which does not run out. Posts go to the Page's feed (text, up to ten pictures, or one video), as a reel or
// as a story. staqly uploads every file's bytes itself, so no public media link is needed; a video kept in
// S3-compatible storage is handed over as a link instead (Facebook fetches it). Comments are the post's thread.
type Facebook struct{ Base }

// Endpoints, variables so tests point them at httptest.
var (
	facebookAuthorize = "https://www.facebook.com/" + instagramVersion + "/dialog/oauth"
	facebookGraph     = "https://graph.facebook.com"
	facebookVideo     = "https://graph-video.facebook.com" // video posts (Facebook takes their bytes on this host)
	// facebookPoll is how long to wait between checks of a video Facebook is still taking in.
	facebookPoll = 3 * time.Second
)

// facebookScopes: list the Pages (pages_show_list, and business_management for Pages a business portfolio owns),
// publish as them (pages_manage_posts, pages_read_engagement) and comment as them (pages_manage_engagement).
const facebookScopes = "pages_show_list,pages_manage_posts,pages_read_engagement,pages_manage_engagement,business_management"

func init() {
	l := Limits{MaxChars: 63206, MaxMedia: 10, MediaKinds: []string{"image", "video"}}
	Register(Facebook{NewBase("facebook", "Facebook Page", AuthOAuth2, l).WithSettings(soonSettings("facebook", l)...)}, StatusAvailable)
}

// Facebook calls back over https only; it takes uploads, so media needs no public link.
func (Facebook) Needs() Needs { return Needs{HTTPS: true} }

// CheckApp asks Facebook for an app token: it gives one only for the right App ID and App secret.
func (Facebook) CheckApp(ctx context.Context, app App, _ string) (bool, error) {
	return metaCheckApp(ctx, fbGraph, fbGraph.url("/oauth/access_token"), app)
}

func (f Facebook) Validate(p PostContent, s Settings) error {
	if err := CheckLimits(f.Limits(), p, s); err != nil {
		return err
	}
	videos := 0
	for _, m := range p.Media {
		switch {
		case strings.HasPrefix(m.MimeType, "video/"):
			videos++
		case !strings.HasPrefix(m.MimeType, "image/"):
			return invalid("media", "Facebook takes pictures and videos; "+m.Name+" is neither")
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
	default:
		if videos > 0 && len(p.Media) > 1 {
			return invalid("media", "a Facebook post takes up to 10 pictures or one video, not both")
		}
	}
	for _, part := range p.Thread {
		if len(part.Media) > 1 || (len(part.Media) == 1 && !strings.HasPrefix(part.Media[0].MimeType, "image/")) {
			return invalid("media", "a Facebook comment takes text and at most one picture")
		}
	}
	return CheckFields(f.SettingsFields(), s)
}

func (Facebook) AuthURL(app App, redirect, state string, _ map[string]string) (string, error) {
	q := url.Values{
		"client_id":     {app.ClientID},
		"redirect_uri":  {redirect},
		"response_type": {"code"},
		"scope":         {facebookScopes},
		"state":         {state},
	}
	return facebookAuthorize + "?" + q.Encode(), nil
}

// Exchange signs the person in; the Pages they may publish as come from Accounts.
func (Facebook) Exchange(ctx context.Context, app App, redirect, code string, _ map[string]string) (Credentials, Profile, error) {
	long, err := metaUserToken(ctx, fbGraph, app, redirect, strings.TrimSuffix(code, "#_"))
	if err != nil {
		return nil, Profile{}, err
	}
	granted, err := metaGranted(ctx, fbGraph, long.AccessToken)
	if err != nil {
		return nil, Profile{}, err
	}
	if !granted["pages_manage_posts"] || !granted["pages_show_list"] {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Facebook did not allow staqly to list your Pages and post to them: connect again and keep every permission ticked"}
	}
	var me struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Picture struct {
			Data struct {
				URL string `json:"url"`
			} `json:"data"`
		} `json:"picture"`
	}
	if err := fbGraph.get(ctx, long.AccessToken, "/me", url.Values{"fields": {"id,name,picture"}}, &me); err != nil {
		return nil, Profile{}, instagramStep(err, "reading the account")
	}
	creds := long.into(Credentials{"user_id": me.ID}, time.Now())
	if !granted["pages_manage_engagement"] {
		creds["no_comments"] = "true"
	}
	return creds, Profile{Handle: me.ID, Name: me.Name, AvatarURL: me.Picture.Data.URL, ProfileURL: "https://www.facebook.com/" + me.ID,
		NoComments: creds["no_comments"] == "true"}, nil
}

var _ AccountLister = Facebook{}

const facebookPageFields = "id,name,username,access_token,picture.type(large)"

// Accounts lists the Pages the person may post to.
func (Facebook) Accounts(ctx context.Context, c Credentials) ([]Account, error) {
	pages, err := facebookPages(ctx, c)
	if err != nil {
		return nil, err
	}
	out := make([]Account, 0, len(pages))
	for _, p := range pages {
		out = append(out, Account{ID: p.ID, Name: p.Name, Handle: facebookHandle(p), AvatarURL: p.Picture.Data.URL, ProfileURL: facebookPageURL(p)})
	}
	return out, nil
}

// facebookPages are the Pages with a token for this person: a Page listed through a business portfolio without a
// role on it comes without one, and can't be posted to.
func facebookPages(ctx context.Context, c Credentials) ([]metaPage, error) {
	token := c["access_token"]
	if token == "" {
		return nil, &Failure{Kind: ErrReauth, Msg: "Facebook: no sign-in stored; connect again"}
	}
	all, err := metaPages(ctx, fbGraph, token, facebookPageFields)
	if err != nil {
		return nil, err
	}
	var pages []metaPage
	for _, p := range all {
		if p.AccessToken != "" {
			pages = append(pages, p)
		}
	}
	switch {
	case len(pages) == 0 && len(all) > 0:
		return nil, &Failure{Kind: ErrRejected, Msg: "Your Facebook user can't post to these Pages: ask a Page admin for full control or content access to the Page, then connect again"}
	case len(pages) == 0:
		return nil, &Failure{Kind: ErrRejected, Msg: "This Facebook account manages no Pages that staqly may use: create a Page, or connect again and choose your Pages when Facebook asks"}
	}
	return pages, nil
}

func facebookHandle(p metaPage) string {
	if p.Username != "" {
		return "@" + p.Username
	}
	return p.ID // a Page without a username: its id is the one stable name it has
}

func facebookPageURL(p metaPage) string {
	if p.Username != "" {
		return "https://www.facebook.com/" + p.Username
	}
	return "https://www.facebook.com/" + p.ID
}

// UseAccount keeps the Page's own token: one made from a 60-day sign-in does not run out.
func (Facebook) UseAccount(ctx context.Context, c Credentials, accountID string) (Credentials, Profile, error) {
	pages, err := facebookPages(ctx, c)
	if err != nil {
		return nil, Profile{}, err
	}
	for _, p := range pages {
		if p.ID != accountID {
			continue
		}
		creds := Credentials{"access_token": p.AccessToken, "page_id": p.ID}
		if c["user_id"] != "" { // the Facebook user who connected the Page (a Page has many admins)
			creds["authorized_by"] = c["user_id"]
		}
		if c["no_comments"] == "true" {
			creds["no_comments"] = "true"
		}
		return creds, Profile{Handle: facebookHandle(p), Name: p.Name, AvatarURL: p.Picture.Data.URL, ProfileURL: facebookPageURL(p),
			NoComments: c["no_comments"] == "true"}, nil
	}
	return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Facebook no longer lists this Page for your sign-in; connect again and choose it"}
}

// Refresh checks the Page token (it does not run out, but the person may remove the app or lose the Page).
func (Facebook) Refresh(ctx context.Context, _ App, c Credentials) (Credentials, error) {
	if c["access_token"] == "" || c["page_id"] == "" {
		return nil, &Failure{Kind: ErrReauth, Msg: "Facebook: no Page stored; reconnect the channel"}
	}
	if err := fbGraph.get(ctx, c["access_token"], "/"+c["page_id"], url.Values{"fields": {"id"}}, nil); err != nil {
		return nil, err
	}
	return c, nil
}

// Facebook publishes a post's thread as comments under it.
var _ Replier = Facebook{}

func (f Facebook) Publish(ctx context.Context, c Credentials, p PostContent, s Settings) (Result, error) {
	token, page := c["access_token"], c["page_id"]
	if token == "" || page == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "Facebook: no Page stored; reconnect the channel"}
	}
	video := len(p.Media) == 1 && strings.HasPrefix(p.Media[0].MimeType, "video/")
	switch {
	case s["post_type"] == "story":
		return f.story(ctx, token, page, p)
	case s["post_type"] == "reel":
		if !video {
			return Result{}, &Failure{Kind: ErrRejected, Msg: "a reel is one video"}
		}
		return f.reel(ctx, token, page, p)
	case video:
		return f.video(ctx, token, page, p)
	}
	return f.feed(ctx, token, page, p)
}

// feed posts text and pictures: each picture is uploaded unpublished first, then the post attaches them all.
func (f Facebook) feed(ctx context.Context, token, page string, p PostContent) (Result, error) {
	form := url.Values{"message": {p.Text}}
	for i, m := range p.Media {
		id, err := f.photo(ctx, token, page, m)
		if err != nil {
			return Result{}, err
		}
		form.Set(fmt.Sprintf("attached_media[%d]", i), `{"media_fbid":"`+id+`"}`)
	}
	req, err := fbGraph.post(ctx, token, "/"+page+"/feed", form)
	if err != nil {
		return Result{}, err
	}
	var out struct {
		ID string `json:"id"`
	}
	// Facebook has no idempotency key: after an answer that never came, publishing again could post twice.
	if err := fbGraph.do(ctx, req, &out); err != nil {
		return Result{}, fbGraph.lost(err)
	}
	if out.ID == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Facebook published the post but did not say its id; check the Page"}
	}
	return Result{ExternalID: out.ID, URL: facebookPermalink(ctx, token, out.ID)}, nil
}

// photo uploads a picture without publishing it and returns its id.
func (Facebook) photo(ctx context.Context, token, page string, m MediaFile) (string, error) {
	req, err := fbGraph.multipart(ctx, fbGraph.url("/"+page+"/photos"), token, url.Values{"published": {"false"}}, "source", m)
	if err != nil {
		return "", err
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := fbGraph.do(ctx, req, &out); err != nil {
		var f *Failure
		if !errors.As(err, &f) {
			// Nothing is published yet: the next attempt uploads again.
			return "", &Failure{Msg: "Facebook could not take the picture " + m.Name + " (" + err.Error() + "); staqly tries again in a few minutes"}
		}
		return "", err
	}
	if out.ID == "" {
		return "", &Failure{Msg: "Facebook did not say the uploaded picture's id; staqly tries again in a few minutes"}
	}
	return out.ID, nil
}

// video posts one video to the feed: the upload is the post, so an answer that never came is not retried.
func (Facebook) video(ctx context.Context, token, page string, p PostContent) (Result, error) {
	m := p.Media[0]
	form := url.Values{"description": {p.Text}, "published": {"true"}}
	endpoint := facebookVideo + "/" + instagramVersion + "/" + page + "/videos"
	var req *http.Request
	var err error
	if m.SignedURL != "" {
		form.Set("file_url", m.SignedURL)
		req, err = fbGraph.postTo(ctx, endpoint, token, form)
	} else {
		req, err = fbGraph.multipart(ctx, endpoint, token, form, "source", m)
	}
	if err != nil {
		return Result{}, err
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := fbGraph.do(ctx, req, &out); err != nil {
		return Result{}, fbGraph.lost(err)
	}
	if out.ID == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Facebook published the video but did not say its id; check the Page"}
	}
	return Result{ExternalID: out.ID, URL: "https://www.facebook.com/" + page + "/videos/" + out.ID}, nil
}

// reel: start an upload, send the bytes (or the link), wait until Facebook has them, then publish.
func (f Facebook) reel(ctx context.Context, token, page string, p PostContent) (Result, error) {
	id, err := f.upload(ctx, token, "/"+page+"/video_reels", p.Media[0])
	if err != nil {
		return Result{}, err
	}
	req, err := fbGraph.post(ctx, token, "/"+page+"/video_reels", url.Values{"upload_phase": {"finish"}, "video_id": {id},
		"video_state": {"PUBLISHED"}, "description": {p.Text}})
	if err != nil {
		return Result{}, err
	}
	var out struct {
		Success bool `json:"success"`
	}
	if err := fbGraph.do(ctx, req, &out); err != nil {
		return Result{}, fbGraph.lost(err)
	}
	if !out.Success {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Facebook did not publish the reel; check the Page before posting it again"}
	}
	return Result{ExternalID: id, URL: "https://www.facebook.com/reel/" + id}, nil
}

// story: a picture is uploaded unpublished and then shared as a story; a video goes through the story upload.
func (f Facebook) story(ctx context.Context, token, page string, p PostContent) (Result, error) {
	if len(p.Media) != 1 {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "a Facebook story is one picture or one video"}
	}
	m := p.Media[0]
	var req *http.Request
	var err error
	if strings.HasPrefix(m.MimeType, "video/") {
		id, uerr := f.upload(ctx, token, "/"+page+"/video_stories", m)
		if uerr != nil {
			return Result{}, uerr
		}
		req, err = fbGraph.post(ctx, token, "/"+page+"/video_stories", url.Values{"upload_phase": {"finish"}, "video_id": {id}})
	} else {
		id, perr := f.photo(ctx, token, page, m)
		if perr != nil {
			return Result{}, perr
		}
		req, err = fbGraph.post(ctx, token, "/"+page+"/photo_stories", url.Values{"photo_id": {id}})
	}
	if err != nil {
		return Result{}, err
	}
	var out struct {
		Success bool   `json:"success"`
		PostID  string `json:"post_id"`
	}
	if err := fbGraph.do(ctx, req, &out); err != nil {
		return Result{}, fbGraph.lost(err)
	}
	if out.PostID == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Facebook did not say it published the story; check the Page before posting it again"}
	}
	return Result{ExternalID: out.PostID, URL: "https://www.facebook.com/" + page}, nil
}

// upload starts a reel or story video upload (edge), sends the file to the address Facebook gives and waits until
// Facebook has it all. Nothing is published yet, so every failure here is retried.
func (Facebook) upload(ctx context.Context, token, edge string, m MediaFile) (string, error) {
	req, err := fbGraph.post(ctx, token, edge, url.Values{"upload_phase": {"start"}})
	if err != nil {
		return "", err
	}
	var start struct {
		VideoID   string `json:"video_id"`
		UploadURL string `json:"upload_url"`
	}
	if err := fbGraph.do(ctx, req, &start); err != nil {
		return "", facebookBeforePost(err)
	}
	if start.VideoID == "" || start.UploadURL == "" {
		return "", &Failure{Msg: "Facebook did not start the video upload; staqly tries again in a few minutes"}
	}

	var body io.Reader
	size := m.Size
	if m.SignedURL == "" {
		rc, err := m.Reader(ctx)
		if err != nil {
			return "", err
		}
		defer rc.Close()
		body = rc
		if size <= 0 {
			raw, err := readMedia(ctx, m, 0)
			if err != nil {
				return "", err
			}
			body, size = bytes.NewReader(raw), int64(len(raw))
		}
	}
	// The Page token goes to the address Graph answered with: only ever Facebook's own upload host, over https.
	if !platformHost(start.UploadURL, facebookGraph, "facebook.com") {
		return "", &Failure{Msg: "Facebook answered with an unexpected upload address; staqly tries again in a few minutes"}
	}
	up, err := http.NewRequestWithContext(ctx, http.MethodPost, start.UploadURL, body)
	if err != nil {
		return "", err
	}
	up.Header.Set("Authorization", "OAuth "+token)
	if m.SignedURL != "" {
		up.Header.Set("file_url", m.SignedURL) // Facebook fetches the file itself
	} else {
		up.ContentLength = size
		up.Header.Set("offset", "0")
		up.Header.Set("file_size", strconv.FormatInt(size, 10))
		up.Header.Set("Content-Type", "application/octet-stream")
	}
	if err := fbGraph.do(ctx, up, nil); err != nil {
		return "", facebookBeforePost(err)
	}

	for {
		var st struct {
			Status struct {
				VideoStatus string `json:"video_status"`
				Uploading   struct {
					Status string `json:"status"`
				} `json:"uploading_phase"`
				Processing struct {
					Status string `json:"status"`
					Errors []struct {
						Message string `json:"message"`
					} `json:"errors"`
				} `json:"processing_phase"`
			} `json:"status"`
		}
		if err := fbGraph.get(ctx, token, "/"+start.VideoID, url.Values{"fields": {"status"}}, &st); err != nil {
			return "", facebookBeforePost(err)
		}
		s := st.Status
		switch {
		case s.VideoStatus == "error" || s.Processing.Status == "error":
			why := "the video's format or length"
			if len(s.Processing.Errors) > 0 {
				why = s.Processing.Errors[0].Message
			}
			return "", &Failure{Kind: ErrRejected, Msg: "Facebook could not use the video (" + why + ")"}
		case s.Uploading.Status == "complete", s.VideoStatus == "upload_complete", s.VideoStatus == "ready":
			return start.VideoID, nil
		}
		if err := metaWait(ctx, facebookPoll); err != nil {
			return "", err
		}
	}
}

// facebookBeforePost: a step before the post itself that got no answer is safe to retry.
func facebookBeforePost(err error) error {
	var f *Failure
	if errors.As(err, &f) {
		return err
	}
	return &Failure{Msg: err.Error() + "; staqly tries again in a few minutes"}
}

// facebookPermalink is the post's address, or the post's id on facebook.com when Facebook doesn't say.
func facebookPermalink(ctx context.Context, token, id string) string {
	var out struct {
		Permalink string `json:"permalink_url"`
	}
	if err := fbGraph.get(ctx, token, "/"+id, url.Values{"fields": {"permalink_url"}}, &out); err != nil || out.Permalink == "" {
		return "https://www.facebook.com/" + id
	}
	return out.Permalink
}

// Reply comments under the post as the Page. parentExternalID is the post or, from the second part on, the comment
// before ("comment:<post>:<comment>"); every part becomes a comment on the post itself, in order.
func (Facebook) Reply(ctx context.Context, c Credentials, parentExternalID string, p PostContent, s Settings) (Result, error) {
	token := c["access_token"]
	if token == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "Facebook: no Page stored; reconnect the channel"}
	}
	if s["post_type"] == "story" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Facebook stories take no comments"}
	}
	post := parentExternalID
	if rest, ok := strings.CutPrefix(parentExternalID, "comment:"); ok {
		post, _, _ = strings.Cut(rest, ":")
	}
	if post == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Facebook: nothing to comment on (the post has no id)"}
	}
	var req *http.Request
	var err error
	switch {
	case len(p.Media) > 1, len(p.Media) == 1 && !strings.HasPrefix(p.Media[0].MimeType, "image/"):
		return Result{}, &Failure{Kind: ErrRejected, Msg: "a Facebook comment takes text and at most one picture"}
	case len(p.Media) == 1:
		req, err = fbGraph.multipart(ctx, fbGraph.url("/"+post+"/comments"), token, url.Values{"message": {p.Text}}, "source", p.Media[0])
	default:
		req, err = fbGraph.post(ctx, token, "/"+post+"/comments", url.Values{"message": {p.Text}})
	}
	if err != nil {
		return Result{}, err
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := fbGraph.do(ctx, req, &out); err != nil {
		return Result{}, fbGraph.lost(err)
	}
	return Result{ExternalID: "comment:" + post + ":" + out.ID, URL: facebookPermalink(ctx, token, post)}, nil
}

// facebookSpecial: Facebook's own error codes, before the ones Meta's platforms share.
func facebookSpecial(e metaErr) error {
	switch {
	case e.sub == 1390008:
		return &Failure{Msg: "Facebook says the Page is posting too fast; staqly tries again in a few minutes"}
	case e.code == 368:
		return &Failure{Kind: ErrRejected, Msg: "Facebook has blocked the Page from posting for a while (" + e.detail + ")"}
	case e.sub == 492:
		return &Failure{Kind: ErrReauth, Msg: "Your Facebook user no longer has a role on this Page: ask a Page admin for one, then reconnect the channel"}
	case e.sub == 1404078, strings.Contains(e.lower, "must be granted before impersonating"),
		strings.Contains(e.lower, "posted to a page as the page itself"):
		return &Failure{Kind: ErrReauth, Msg: "Facebook no longer lets staqly post as this Page (" + e.detail + "); reconnect the channel and allow every permission"}
	case e.sub == 4854002, e.sub == 459, e.sub == 1404006:
		return &Failure{Kind: ErrRejected, Msg: "Facebook asks you to confirm your identity or pass a security check first: open facebook.com or the Facebook app, follow its steps, then post again"}
	case e.code == 506, e.sub == 1366051:
		return &Failure{Kind: ErrRejected, Msg: "Facebook already has this exact post on the Page (" + e.detail + ")"}
	case e.sub == 1366046:
		return &Failure{Kind: ErrRejected, Msg: "Facebook did not take a picture (" + e.detail + "): save it as JPEG or PNG under 4 MB"}
	case e.sub == 1346003, e.sub == 1404102:
		return &Failure{Kind: ErrRejected, Msg: "Facebook refused the post under its Community Standards (" + e.detail + ")"}
	case e.sub == 1609008:
		return &Failure{Kind: ErrRejected, Msg: "Facebook does not take links to facebook.com in a post (" + e.detail + ")"}
	case e.code == 100 && strings.Contains(e.lower, "does not exist"):
		return &Failure{Kind: ErrRejected, Msg: "Facebook can't find the Page or the post (" + e.detail + "); reconnect the channel if the Page moved"}
	}
	return nil
}
