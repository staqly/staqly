package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Lemmy over its HTTP API (v3, Lemmy 0.19): the person signs in once with their username and password (and a
// two-factor code when they use one); staqly keeps only the login token Lemmy hands back, which lasts until the person
// logs out everywhere or changes the password. A post goes to one community with a title, an optional link and one
// picture (uploaded to the instance's pict-rs); the post's thread becomes comments, each under the one before.
type Lemmy struct{ Base }

func init() {
	l := Limits{MaxChars: 10000, MaxMedia: 1, MediaKinds: []string{"image"}, NeedsTitle: true}
	Register(Lemmy{NewBase("lemmy", "Lemmy", AuthToken, l).WithSettings(soonSettings("lemmy", l)...)}, StatusAvailable)
}

func (Lemmy) CredentialFields() []Field {
	return []Field{
		{Key: "instance", Label: "Server", Kind: "url", Required: true, Placeholder: "https://lemmy.world",
			Help: "The Lemmy server your account is on."},
		{Key: "username", Label: "Username or email", Kind: "text", Required: true, Placeholder: "janedoe"},
		{Key: "password", Label: "Password", Kind: "secret", Required: true,
			Help: "Used once to sign in; staqly keeps only the login token Lemmy gives back."},
		{Key: "totp", Label: "Two-factor code", Kind: "text", Placeholder: "123456",
			Help: "Only if your account uses two-factor authentication."},
	}
}

func (l Lemmy) Validate(p PostContent, s Settings) error {
	if err := CheckLimits(l.Limits(), p, s); err != nil {
		return err
	}
	for _, m := range p.Media {
		if !strings.HasPrefix(m.MimeType, "image/") {
			return invalid("media", "Lemmy takes one picture")
		}
	}
	if _, _, err := lemmyCommunity(s["community"]); err != nil && strings.TrimSpace(s["community"]) != "" {
		return err
	}
	return CheckFields(l.SettingsFields(), s)
}

// lemmyCommunity reads "technology", "technology@lemmy.world" or "!technology@lemmy.world".
func lemmyCommunity(v string) (name, host string, err error) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "!")
	v = strings.TrimPrefix(v, "c/")
	name, host, _ = strings.Cut(v, "@")
	if name == "" || strings.ContainsAny(name, " /") || strings.ContainsAny(host, " /") {
		return "", "", invalid("community", "write the community as name@server, for example technology@lemmy.world")
	}
	return name, host, nil
}

type lemmyPerson struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Avatar      string `json:"avatar"`
	ActorID     string `json:"actor_id"`
}

func (Lemmy) Connect(ctx context.Context, f map[string]string) (Credentials, Profile, error) {
	instance, err := fediInstance(f["instance"], "https://lemmy.world", "instance")
	if err != nil {
		return nil, Profile{}, err
	}
	jwt := f["jwt"] // a stored channel being checked again
	if jwt == "" {
		user, pass := strings.TrimSpace(f["username"]), f["password"]
		if user == "" {
			return nil, Profile{}, invalid("username", "enter your username or email")
		}
		if pass == "" {
			return nil, Profile{}, invalid("password", "enter your password")
		}
		body := map[string]string{"username_or_email": user, "password": pass}
		if code := strings.TrimSpace(f["totp"]); code != "" {
			body["totp_2fa_token"] = code
		}
		var out struct {
			JWT string `json:"jwt"`
		}
		if err := lemmyCall(ctx, http.MethodPost, instance, "", "/api/v3/user/login", body, &out); err != nil {
			return nil, Profile{}, err
		}
		if out.JWT == "" {
			// Lemmy answers without a token while the email address or the registration still awaits approval.
			return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Lemmy did not sign the account in: confirm its email address or wait for the server to approve it, then try again"}
		}
		jwt = out.JWT
	}
	var site struct {
		MyUser *struct {
			LocalUserView struct {
				Person lemmyPerson `json:"person"`
			} `json:"local_user_view"`
		} `json:"my_user"`
	}
	if err := lemmyCall(ctx, http.MethodGet, instance, jwt, "/api/v3/site", nil, &site); err != nil {
		return nil, Profile{}, err
	}
	if site.MyUser == nil {
		return nil, Profile{}, &Failure{Kind: ErrReauth, Msg: "Lemmy no longer accepts this sign-in; reconnect the account"}
	}
	me := site.MyUser.LocalUserView.Person
	host := strings.TrimPrefix(strings.TrimPrefix(instance, "https://"), "http://")
	return Credentials{"instance": instance, "jwt": jwt}, Profile{
		Handle: "@" + me.Name + "@" + host, Name: fediFirst(me.DisplayName, me.Name), AvatarURL: me.Avatar,
		ProfileURL: fediFirst(me.ActorID, instance+"/u/"+me.Name),
	}, nil
}

// Lemmy publishes a post's thread as comments.
var _ Replier = Lemmy{}

func (Lemmy) Publish(ctx context.Context, c Credentials, p PostContent, s Settings) (Result, error) {
	instance, jwt := c["instance"], c["jwt"]
	if instance == "" || jwt == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "Lemmy: no account stored; reconnect the account"}
	}
	community, err := lemmyFindCommunity(ctx, instance, jwt, s["community"])
	if err != nil {
		return Result{}, err
	}
	body := map[string]any{"name": strings.TrimSpace(s["title"]), "community_id": community}
	if t := strings.TrimSpace(p.Text); t != "" {
		body["body"] = p.Text
	}
	link := strings.TrimSpace(s["link"])
	if link != "" {
		body["url"] = link
	}
	if len(p.Media) > 0 {
		pic, err := lemmyUpload(ctx, instance, jwt, p.Media[0])
		if err != nil {
			return Result{}, err
		}
		if link == "" {
			body["url"] = pic // an image post
		} else {
			body["custom_thumbnail"] = pic // a link post shown with the picture
		}
	}
	var out struct {
		PostView struct {
			Post struct {
				ID    int64  `json:"id"`
				ApID  string `json:"ap_id"`
				Title string `json:"name"`
			} `json:"post"`
		} `json:"post_view"`
	}
	// Lemmy has no idempotency key: after an answer that never came, a new attempt could post twice.
	if err := lemmyCall(ctx, http.MethodPost, instance, jwt, "/api/v3/post", body, &out); err != nil {
		if errors.Is(err, errLemmyNoAnswer) {
			return Result{}, &Failure{Kind: ErrRejected, Msg: "Lemmy may have published the post, but its answer was lost (" + err.Error() + "); check the community before posting it again"}
		}
		return Result{}, err
	}
	id := out.PostView.Post.ID
	if id == 0 {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Lemmy published the post but did not say its id; check the community"}
	}
	return Result{ExternalID: strconv.FormatInt(id, 10), URL: fmt.Sprintf("%s/post/%d", instance, id)}, nil
}

// Reply comments on the post: parentExternalID is the post ("123") or the comment before ("comment:123:456"), and
// each comment answers the one before it.
func (Lemmy) Reply(ctx context.Context, c Credentials, parentExternalID string, p PostContent, _ Settings) (Result, error) {
	instance, jwt := c["instance"], c["jwt"]
	if instance == "" || jwt == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "Lemmy: no account stored; reconnect the account"}
	}
	post, parent := parentExternalID, ""
	if rest, ok := strings.CutPrefix(parentExternalID, "comment:"); ok {
		post, parent, _ = strings.Cut(rest, ":")
	}
	postID, err := strconv.ParseInt(post, 10, 64)
	if err != nil {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Lemmy: nothing to comment on (the post has no id)"}
	}
	if len(p.Media) > 0 {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Lemmy comments take text only"}
	}
	body := map[string]any{"content": p.Text, "post_id": postID}
	if parent != "" {
		if n, err := strconv.ParseInt(parent, 10, 64); err == nil {
			body["parent_id"] = n
		}
	}
	var out struct {
		CommentView struct {
			Comment struct {
				ID int64 `json:"id"`
			} `json:"comment"`
		} `json:"comment_view"`
	}
	if err := lemmyCall(ctx, http.MethodPost, instance, jwt, "/api/v3/comment", body, &out); err != nil {
		if errors.Is(err, errLemmyNoAnswer) {
			return Result{}, &Failure{Kind: ErrRejected, Msg: "Lemmy may have published the comment, but its answer was lost (" + err.Error() + "); check the post before posting it again"}
		}
		return Result{}, err
	}
	id := out.CommentView.Comment.ID
	return Result{ExternalID: fmt.Sprintf("comment:%d:%d", postID, id), URL: fmt.Sprintf("%s/comment/%d", instance, id)}, nil
}

// lemmyFindCommunity looks the community up on the account's server, and has the server fetch it from its own
// server when it doesn't know it yet.
func lemmyFindCommunity(ctx context.Context, instance, jwt, v string) (int64, error) {
	name, host, err := lemmyCommunity(v)
	if err != nil {
		return 0, &Failure{Kind: ErrRejected, Msg: "Lemmy: " + err.(*ValidationError).Msg}
	}
	full := name
	if host != "" {
		full += "@" + host
	}
	var out struct {
		CommunityView struct {
			Community struct {
				ID int64 `json:"id"`
			} `json:"community"`
		} `json:"community_view"`
	}
	err = lemmyCall(ctx, http.MethodGet, instance, jwt, "/api/v3/community?name="+url.QueryEscape(full), nil, &out)
	if err == nil && out.CommunityView.Community.ID != 0 {
		return out.CommunityView.Community.ID, nil
	}
	if err != nil && !strings.Contains(err.Error(), "couldnt_find_community") {
		return 0, err
	}
	if host == "" {
		return 0, &Failure{Kind: ErrRejected, Msg: "Lemmy has no community " + name + " on this server; write it as name@server"}
	}
	var res struct {
		Community *struct {
			Community struct {
				ID int64 `json:"id"`
			} `json:"community"`
		} `json:"community"`
	}
	if err := lemmyCall(ctx, http.MethodGet, instance, jwt, "/api/v3/resolve_object?q="+url.QueryEscape("!"+full), nil, &res); err != nil || res.Community == nil {
		if err != nil && !strings.Contains(err.Error(), "couldnt_find") {
			return 0, err
		}
		return 0, &Failure{Kind: ErrRejected, Msg: "Lemmy could not find the community " + full + "; check its name and server"}
	}
	return res.Community.Community.ID, nil
}

// lemmyUpload sends a picture to the server's pict-rs and returns its address.
func lemmyUpload(ctx context.Context, instance, jwt string, m MediaFile) (string, error) {
	r, err := m.Reader(ctx)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", m.Name, err)
	}
	defer r.Close()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	h := make(map[string][]string)
	h["Content-Disposition"] = []string{fmt.Sprintf(`form-data; name="images[]"; filename=%q`, m.Name)}
	h["Content-Type"] = []string{m.MimeType}
	part, err := w.CreatePart(h)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, r); err != nil {
		return "", fmt.Errorf("reading %s: %w", m.Name, err)
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, instance+"/pictrs/image", &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.AddCookie(&http.Cookie{Name: "jwt", Value: jwt}) // older servers read the token from the cookie only
	var out struct {
		Msg   string `json:"msg"`
		Files []struct {
			File string `json:"file"`
		} `json:"files"`
	}
	if err := lemmyDo(ctx, req, &out); err != nil {
		if errors.Is(err, errLemmyNoAnswer) {
			return "", &Failure{Msg: err.Error() + "; staqly tries again in a few minutes"}
		}
		return "", err
	}
	if len(out.Files) == 0 || out.Files[0].File == "" {
		return "", &Failure{Kind: ErrRejected, Msg: "Lemmy did not take " + m.Name + " (" + fediFirst(out.Msg, "no file returned") + ")"}
	}
	return instance + "/pictrs/image/" + out.Files[0].File, nil
}

var errLemmyNoAnswer = errors.New("Lemmy could not be reached")

// lemmyCall sends a JSON request (body nil for GET) and decodes a 2xx reply into out.
func lemmyCall(ctx context.Context, method, instance, jwt, path string, body any, out any) error {
	var r io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		r = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, instance+path, r)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if jwt != "" {
		req.Header.Set("Authorization", "Bearer "+jwt)
	}
	err = lemmyDo(ctx, req, out)
	if errors.Is(err, errLemmyNoAnswer) && method == http.MethodGet {
		return &Failure{Msg: err.Error() + "; staqly tries again in a few minutes"}
	}
	return err
}

// lemmyDo sends a request; one that never got an answer wraps errLemmyNoAnswer, so a publish can tell it apart.
func lemmyDo(ctx context.Context, req *http.Request, out any) error {
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w (%s)", errLemmyNoAnswer, req.URL.Host)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out != nil && len(raw) > 0 {
			if err := json.Unmarshal(raw, out); err != nil {
				return &Failure{Kind: ErrRejected, Msg: "the server " + req.URL.Host + " did not answer like Lemmy; check its address"}
			}
		}
		return nil
	}
	return lemmyError(resp.StatusCode, raw)
}

// lemmyError maps Lemmy's answers ({"error": "snake_case_reason"}) to what staqly does next.
func lemmyError(status int, raw []byte) error {
	var e struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &e)
	code := e.Error
	detail := fediFirst(e.Error, e.Message, fediSnippet(raw))
	switch {
	case code == "incorrect_login":
		return &Failure{Kind: ErrReauth, Msg: "Lemmy did not accept the username or password (incorrect_login)"}
	case code == "missing_totp_token", code == "incorrect_totp_token":
		return &Failure{Kind: ErrReauth, Msg: "Lemmy asks for a current two-factor code (" + code + "); connect again with the code from your app"}
	case code == "email_not_verified", code == "registration_application_is_pending", code == "registration_denied":
		return &Failure{Kind: ErrRejected, Msg: "Lemmy will not sign this account in yet (" + code + ")"}
	case code == "not_logged_in", status == http.StatusUnauthorized:
		return &Failure{Kind: ErrReauth, Msg: "Lemmy no longer accepts this sign-in (" + detail + "); reconnect the account"}
	case code == "site_ban", code == "banned_from_community", code == "deleted", code == "person_is_banned_from_site":
		return &Failure{Kind: ErrRejected, Msg: "Lemmy refused the account (" + code + ")"}
	case code == "rate_limit_error", status == http.StatusTooManyRequests:
		return &Failure{Msg: "Lemmy rate limit reached; staqly tries again in a few minutes"}
	case status >= 500:
		return &Failure{Msg: fmt.Sprintf("Lemmy is unavailable (%d); staqly tries again in a few minutes", status)}
	case code == "only_mods_can_post_in_community":
		return &Failure{Kind: ErrRejected, Msg: "only moderators may post in this community"}
	case code == "couldnt_find_community":
		return &Failure{Kind: ErrRejected, Msg: "Lemmy could not find the community (couldnt_find_community)"}
	}
	return &Failure{Kind: ErrRejected, Msg: "Lemmy refused it (" + detail + ")"}
}
