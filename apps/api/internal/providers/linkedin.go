package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// LinkedIn (a member's own profile) over the versioned REST API: OAuth 2.0 with the self-serve products
// "Share on LinkedIn" and "Sign In with LinkedIn using OpenID Connect" (openid profile w_member_social),
// media through the Images and Videos APIs, posts through POST /rest/posts, comments through
// /rest/socialActions. Access tokens last 60 days; LinkedIn gives refresh tokens only to approved partners, so
// without one the channel is reconnected when its token runs out.
type LinkedIn struct{ Base }

// Endpoints, variables so tests point them at httptest.
var (
	linkedinAPI   = "https://api.linkedin.com"
	linkedinOAuth = "https://www.linkedin.com/oauth/v2"
	// linkedinPoll is how long to wait between video processing checks.
	linkedinPoll = 3 * time.Second
	// linkedinImageWait: a member's token may upload images but not read their status, so the post waits this
	// long for LinkedIn to process them. A post sent too early fails retryably, and its next attempt uploads again.
	linkedinImageWait = 3 * time.Second
)

const (
	linkedinScopes = "openid profile w_member_social"
	// linkedinVersion is the API version every REST call names (YYYYMM). ponytail: LinkedIn supports a version
	// for about a year; bump it once a year (https://learn.microsoft.com/linkedin/marketing/versioning).
	linkedinVersion = "202609"
)

func init() {
	Register(LinkedIn{NewBase("linkedin", "LinkedIn", AuthOAuth2,
		Limits{MaxChars: 3000, MaxMedia: 9, MediaKinds: []string{"image", "gif", "video"}})}, StatusAvailable)
}

func (LinkedIn) SettingsFields() []Field { return []Field{linkedinVisibility} }

func (l LinkedIn) Validate(p PostContent, s Settings) error {
	if err := CheckLimits(l.Limits(), p, s); err != nil {
		return err
	}
	for _, m := range p.Media {
		if strings.HasPrefix(m.MimeType, "video/") && len(p.Media) > 1 {
			return invalid("media", "LinkedIn takes up to 9 pictures or one video")
		}
	}
	switch s["visibility"] {
	case "", "public", "connections":
	default:
		return invalid("visibility", "choose who can see it")
	}
	return CheckFields(l.SettingsFields(), s)
}

func (LinkedIn) AuthURL(app App, redirect, state string, _ map[string]string) (string, error) {
	q := url.Values{
		"response_type": {"code"},
		"client_id":     {app.ClientID},
		"redirect_uri":  {redirect},
		"scope":         {linkedinScopes},
		"state":         {state},
	}
	return linkedinOAuth + "/authorization?" + q.Encode(), nil
}

type linkedinToken struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

func (LinkedIn) token(ctx context.Context, app App, form url.Values) (linkedinToken, error) {
	var t linkedinToken
	form.Set("client_id", app.ClientID)
	form.Set("client_secret", app.ClientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, linkedinOAuth+"/accessToken", strings.NewReader(form.Encode()))
	if err != nil {
		return t, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if _, err := linkedinDo(ctx, req, &t); err != nil {
		return t, err
	}
	if t.AccessToken == "" {
		return t, errors.New("LinkedIn returned no access token")
	}
	return t, nil
}

func (t linkedinToken) into(c Credentials, now time.Time) Credentials {
	out := Credentials{}
	for k, v := range c {
		out[k] = v
	}
	out["access_token"] = t.AccessToken
	if t.RefreshToken != "" {
		out["refresh_token"] = t.RefreshToken
	}
	if t.ExpiresIn > 0 {
		out["expires_at"] = now.Add(time.Duration(t.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)
	}
	return out
}

func (l LinkedIn) Exchange(ctx context.Context, app App, redirect, code string, _ map[string]string) (Credentials, Profile, error) {
	t, err := l.token(ctx, app, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect}})
	if err != nil {
		return nil, Profile{}, err
	}
	if t.Scope != "" && !strings.Contains(t.Scope, "w_member_social") {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "LinkedIn did not allow posting: add the \"Share on LinkedIn\" product to the app and connect again"}
	}
	var me struct {
		Sub     string `json:"sub"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, linkedinAPI+"/v2/userinfo", nil)
	if err != nil {
		return nil, Profile{}, err
	}
	req.Header.Set("Authorization", "Bearer "+t.AccessToken)
	if _, err := linkedinDo(ctx, req, &me); err != nil {
		return nil, Profile{}, err
	}
	if me.Sub == "" {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "LinkedIn did not say who signed in: add the \"Sign In with LinkedIn using OpenID Connect\" product to the app"}
	}
	creds := t.into(Credentials{}, time.Now())
	creds["person_id"] = me.Sub
	// The profile's public address needs a permission only partners get, so the channel shows the name.
	return creds, Profile{Name: me.Name, AvatarURL: me.Picture}, nil
}

// Refresh renews the token with a refresh token when LinkedIn gave one (approved partner apps). Without one the
// token stays as it is until it runs out; then the channel needs reconnecting.
func (l LinkedIn) Refresh(ctx context.Context, app App, c Credentials) (Credentials, error) {
	if c["refresh_token"] == "" {
		exp, ok := credentialsExpiry(c)
		if !ok || !exp.After(time.Now().Add(2*time.Minute)) {
			return nil, &Failure{Kind: ErrReauth, Msg: "LinkedIn signs apps out after 60 days; reconnect the account"}
		}
		// Still in date: ask LinkedIn whether it still takes it (the member may have removed the app), so a
		// channel test or a scheduled check finds out before a post does.
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, linkedinAPI+"/v2/userinfo", nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c["access_token"])
		if _, err := linkedinDo(ctx, req, nil); err != nil {
			return nil, err
		}
		return c, nil
	}
	t, err := l.token(ctx, app, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {c["refresh_token"]}})
	if err != nil {
		return nil, err
	}
	return t.into(c, time.Now()), nil
}

// credentialsExpiry reads "expires_at" (RFC3339).
func credentialsExpiry(c Credentials) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, c["expires_at"])
	return t, err == nil
}

// LinkedIn publishes a post's thread as comments under it.
var _ Replier = LinkedIn{}

func (l LinkedIn) Publish(ctx context.Context, c Credentials, p PostContent, s Settings) (Result, error) {
	token, person := c["access_token"], c["person_id"]
	if token == "" || person == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "LinkedIn: no account stored; reconnect the account"}
	}
	author := "urn:li:person:" + person
	var ids []string
	images := false
	for _, m := range p.Media {
		id, err := l.upload(ctx, token, author, m)
		if err != nil {
			return Result{}, err
		}
		images = images || !strings.HasPrefix(m.MimeType, "video/")
		ids = append(ids, id)
	}
	if images {
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-time.After(linkedinImageWait):
		}
	}
	visibility := "PUBLIC"
	if s["visibility"] == "connections" {
		visibility = "CONNECTIONS"
	}
	body := map[string]any{
		"author":                    author,
		"commentary":                linkedinText(p.Text),
		"visibility":                visibility,
		"distribution":              map[string]any{"feedDistribution": "MAIN_FEED", "targetEntities": []string{}, "thirdPartyDistributionChannels": []string{}},
		"lifecycleState":            "PUBLISHED",
		"isReshareDisabledByAuthor": false,
	}
	switch {
	case len(ids) == 1:
		body["content"] = map[string]any{"media": map[string]any{"id": ids[0]}}
	case len(ids) > 1:
		list := make([]map[string]string, len(ids))
		for i, id := range ids {
			list[i] = map[string]string{"id": id}
		}
		body["content"] = map[string]any{"multiImage": map[string]any{"images": list}}
	}
	req, err := linkedinJSON(ctx, token, http.MethodPost, linkedinAPI+"/rest/posts", body)
	if err != nil {
		return Result{}, err
	}
	// LinkedIn has no idempotency key. A retry after a lost answer is answered "duplicate of urn:li:share:...",
	// which linkedinDo turns into that post's id, so a retry never publishes twice.
	h, err := linkedinDo(ctx, req, nil)
	if err != nil {
		return Result{}, err
	}
	urn := h.Get("X-Restli-Id")
	if urn == "" {
		urn = h.Get("X-LinkedIn-Id")
	}
	if urn == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "LinkedIn published the post but did not say its id; check the profile"}
	}
	return Result{ExternalID: urn, URL: linkedinPostURL(urn)}, nil
}

// Reply comments under the post. parentExternalID is the post or, from the second part on, the comment before;
// every part becomes a comment on the post itself, in order.
func (l LinkedIn) Reply(ctx context.Context, c Credentials, parentExternalID string, p PostContent, _ Settings) (Result, error) {
	token, person := c["access_token"], c["person_id"]
	if token == "" || person == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "LinkedIn: no account stored; reconnect the account"}
	}
	post := linkedinCommentPost(parentExternalID)
	if post == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "LinkedIn: nothing to comment on (the post has no id)"}
	}
	if len(p.Media) > 0 {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "LinkedIn comments take text only"}
	}
	req, err := linkedinJSON(ctx, token, http.MethodPost, linkedinAPI+"/rest/socialActions/"+linkedinURN(post)+"/comments",
		map[string]any{"actor": "urn:li:person:" + person, "object": post, "message": map[string]string{"text": p.Text}})
	if err != nil {
		return Result{}, err
	}
	var out struct {
		URN string `json:"$URN"`
		ID  string `json:"id"`
	}
	h, err := linkedinDo(ctx, req, &out)
	if err != nil {
		return Result{}, err
	}
	urn := out.URN
	if urn == "" {
		id := out.ID
		if id == "" {
			id = h.Get("X-Restli-Id")
		}
		urn = "urn:li:comment:(" + post + "," + id + ")"
	}
	return Result{ExternalID: urn, URL: linkedinPostURL(post)}, nil
}

// linkedinCommentPost is the post a comment URN (urn:li:comment:(urn:li:activity:1,2)) belongs to, or the URN
// itself when it is a post.
func linkedinCommentPost(urn string) string {
	if inner, ok := strings.CutPrefix(urn, "urn:li:comment:("); ok {
		if i := strings.LastIndex(inner, ","); i > 0 {
			return inner[:i]
		}
		return ""
	}
	return urn
}

// linkedinURN is a URN as a path segment: LinkedIn wants its colons encoded (urn%3Ali%3Ashare%3A1), and answers
// "Syntax exception in path variables" to urn:li:share:1, which url.PathEscape leaves as it is.
func linkedinURN(urn string) string { return url.QueryEscape(urn) }

func linkedinPostURL(urn string) string { return "https://www.linkedin.com/feed/update/" + urn + "/" }

// A hashtag starts the text or follows a space or an opening parenthesis: docs#install in a link is no hashtag.
var linkedinHashtag = regexp.MustCompile(`(^|\s|\\\()\\#((?:[\p{L}\p{N}]|\\_)+)`)

// linkedinText writes plain text in LinkedIn's "little text" format: its reserved characters are escaped (or
// LinkedIn cuts the post at the first one) and hashtags become hashtag templates, so they stay links.
func linkedinText(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(`\|{}@[]()<>#*_~`, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	// "\#tag" -> "{hashtag|\#|tag}"; the tag's own escaped underscores ("\_") stay escaped inside the template.
	return linkedinHashtag.ReplaceAllString(b.String(), `$1{hashtag|\#|$2}`)
}

// upload sends one file and returns its URN: pictures in one PUT, videos in the parts LinkedIn asks for, then
// waits until LinkedIn has processed a video.
func (l LinkedIn) upload(ctx context.Context, token, owner string, m MediaFile) (string, error) {
	f, err := m.Reader(ctx)
	if err != nil {
		return "", fmt.Errorf("LinkedIn: cannot read %s: %w", m.Name, err)
	}
	defer f.Close()
	if !strings.HasPrefix(m.MimeType, "video/") {
		var init struct {
			Value struct {
				UploadURL string `json:"uploadUrl"`
				Image     string `json:"image"`
			} `json:"value"`
		}
		req, err := linkedinJSON(ctx, token, http.MethodPost, linkedinAPI+"/rest/images?action=initializeUpload",
			map[string]any{"initializeUploadRequest": map[string]string{"owner": owner}})
		if err != nil {
			return "", err
		}
		if _, err := linkedinDo(ctx, req, &init); err != nil {
			return "", err
		}
		if err := linkedinPut(ctx, token, init.Value.UploadURL, f, m.MimeType); err != nil {
			return "", err
		}
		return init.Value.Image, nil
	}

	var init struct {
		Value struct {
			Video              string `json:"video"`
			UploadToken        string `json:"uploadToken"`
			UploadInstructions []struct {
				UploadURL string `json:"uploadUrl"`
				FirstByte int64  `json:"firstByte"`
				LastByte  int64  `json:"lastByte"`
			} `json:"uploadInstructions"`
		} `json:"value"`
	}
	req, err := linkedinJSON(ctx, token, http.MethodPost, linkedinAPI+"/rest/videos?action=initializeUpload",
		map[string]any{"initializeUploadRequest": map[string]any{"owner": owner, "fileSizeBytes": m.Size, "uploadCaptions": false, "uploadThumbnail": false}})
	if err != nil {
		return "", err
	}
	if _, err := linkedinDo(ctx, req, &init); err != nil {
		return "", err
	}
	v := init.Value
	var parts []string
	for _, in := range v.UploadInstructions {
		buf := make([]byte, in.LastByte-in.FirstByte+1)
		if _, err := io.ReadFull(f, buf); err != nil {
			return "", fmt.Errorf("LinkedIn: cannot read %s: %w", m.Name, err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, in.UploadURL, bytes.NewReader(buf))
		if err != nil {
			return "", err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/octet-stream")
		h, err := linkedinDo(ctx, req, nil)
		if err != nil {
			return "", err
		}
		parts = append(parts, h.Get("Etag"))
	}
	req, err = linkedinJSON(ctx, token, http.MethodPost, linkedinAPI+"/rest/videos?action=finalizeUpload",
		map[string]any{"finalizeUploadRequest": map[string]any{"video": v.Video, "uploadToken": v.UploadToken, "uploadedPartIds": parts}})
	if err != nil {
		return "", err
	}
	if _, err := linkedinDo(ctx, req, nil); err != nil {
		return "", err
	}
	for {
		var st struct {
			Status                  string `json:"status"`
			ProcessingFailureReason string `json:"processingFailureReason"`
		}
		req, err := linkedinJSON(ctx, token, http.MethodGet, linkedinAPI+"/rest/videos/"+linkedinURN(v.Video), nil)
		if err != nil {
			return "", err
		}
		if _, err := linkedinDo(ctx, req, &st); err != nil {
			return "", err
		}
		switch st.Status {
		case "AVAILABLE":
			return v.Video, nil
		case "PROCESSING_FAILED":
			return "", &Failure{Kind: ErrRejected, Msg: "LinkedIn could not process " + m.Name + ": " + st.ProcessingFailureReason}
		}
		// The job's own deadline ends a video that never finishes; its next attempt uploads it again.
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(linkedinPoll):
		}
	}
}

// linkedinPut sends a picture to the upload URL LinkedIn gave.
func linkedinPut(ctx context.Context, token, uploadURL string, r io.Reader, mime string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", mime)
	_, err = linkedinDo(ctx, req, nil)
	return err
}

// linkedinJSON builds a versioned REST request (a nil body sends none).
func linkedinJSON(ctx context.Context, token, method, endpoint string, body any) (*http.Request, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("LinkedIn-Version", linkedinVersion)
	req.Header.Set("X-Restli-Protocol-Version", "2.0.0")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

var linkedinDuplicate = regexp.MustCompile(`duplicate of (urn:li:(?:share|ugcPost|activity):\d+)`)

// linkedinDo sends a request, decodes a 2xx reply into out and returns its headers; LinkedIn's errors become
// messages a person can act on. A post refused as a duplicate of one already on LinkedIn answers with that
// post's id (X-Restli-Id), as if it had just been created.
func linkedinDo(ctx context.Context, req *http.Request, out any) (http.Header, error) {
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		return nil, fmt.Errorf("LinkedIn could not be reached: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out != nil && len(raw) > 0 {
			if err := json.Unmarshal(raw, out); err != nil {
				return nil, err
			}
		}
		return resp.Header, nil
	}
	if m := linkedinDuplicate.FindStringSubmatch(string(raw)); m != nil && strings.HasSuffix(req.URL.Path, "/rest/posts") {
		return http.Header{"X-Restli-Id": {m[1]}}, nil
	}
	return nil, linkedinError(resp.StatusCode, string(raw))
}

// linkedinError maps LinkedIn's replies to what staqly does next: a sign-in problem needs reconnecting, a
// refused post is final, the rest is retried.
func linkedinError(status int, body string) error {
	var e struct {
		Message          string `json:"message"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	_ = json.Unmarshal([]byte(body), &e)
	detail := e.Message
	if detail == "" {
		detail = e.ErrorDescription
	}
	if detail == "" {
		detail = e.Error
	}
	if detail == "" {
		detail = body
		if len(detail) > 200 {
			detail = detail[:200] + "..."
		}
	}
	switch {
	// The token endpoint: the app's keys (a wrong Client ID or Secret) or the sign-in's code, not the account.
	case e.Error == "invalid_client", e.Error == "unauthorized_client":
		// For whoever runs the server: PROVIDER_LINKEDIN_CLIENT_ID and _SECRET don't match the app's Auth tab.
		return &Failure{Kind: ErrRejected, Msg: "LinkedIn did not accept this server's app (" + detail + ")"}
	case e.Error == "invalid_redirect_uri", strings.Contains(detail, "redirect_uri"):
		// For whoever runs the server: the app's Authorized redirect URLs need <dashboard>/channels/callback/linkedin.
		return &Failure{Kind: ErrRejected, Msg: "LinkedIn did not accept the return address (" + detail + ")"}
	case e.Error == "invalid_request", e.Error == "invalid_grant" && status == http.StatusBadRequest:
		// LinkedIn answers a sign-in code it doesn't know (used, expired) with 401 invalid_request.
		return &Failure{Kind: ErrRejected, Msg: "LinkedIn did not accept the sign-in (" + detail + "); connect again"}
	case status == http.StatusUnauthorized, e.Error == "invalid_grant", strings.Contains(body, "REVOKED_ACCESS_TOKEN"), strings.Contains(body, "EXPIRED_ACCESS_TOKEN"):
		return &Failure{Kind: ErrReauth, Msg: "LinkedIn no longer accepts this account's sign-in (" + detail + "); reconnect it"}
	case status == http.StatusTooManyRequests:
		return &Failure{Msg: "LinkedIn rate limit reached; staqly tries again in a few minutes"}
	case status >= 500:
		return &Failure{Msg: fmt.Sprintf("LinkedIn is unavailable (%d); staqly tries again in a few minutes", status)}
	case status == http.StatusForbidden && strings.Contains(detail, "partnerApiSocialActions"):
		// For whoever runs the server: "Share on LinkedIn" posts, but comments need the app approved for
		// LinkedIn's Community Management API.
		return &Failure{Kind: ErrRejected, Msg: "LinkedIn does not let this server's app comment yet (" + detail + ")"}
	case status == http.StatusForbidden:
		// For whoever runs the server: the app needs the "Share on LinkedIn" product.
		return &Failure{Kind: ErrRejected, Msg: "LinkedIn refused it (" + detail + ")"}
	case strings.Contains(body, "processing"), strings.Contains(body, "not ready"):
		return &Failure{Msg: "LinkedIn is still processing the media; staqly tries again in a few minutes"}
	case status >= 400:
		return &Failure{Kind: ErrRejected, Msg: "LinkedIn: " + detail}
	}
	return &Failure{Msg: fmt.Sprintf("LinkedIn answered %d: %s", status, detail)}
}
