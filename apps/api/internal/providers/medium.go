package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"unicode/utf8"
)

// Medium over its REST API (api.medium.com/v1) with an integration token the person pastes. Medium stopped issuing
// new integration tokens, so only accounts that already have one can connect. A post's text is the story in
// Markdown, its title setting the headline; a picture is uploaded to Medium and leads the story. The story goes to
// the person's profile, or to a publication they write for (the optional "publication" credential).
type Medium struct{ Base }

// mediumAPI is Medium's API root, a variable so tests point it at httptest.
var mediumAPI = "https://api.medium.com/v1"

// mediumImageTypes are the picture formats Medium's images endpoint takes.
var mediumImageTypes = []string{"image/jpeg", "image/png", "image/gif", "image/tiff"}

func init() {
	limits := Limits{MaxChars: 0, MaxMedia: 1, MediaKinds: []string{"image"}, NeedsTitle: true,
		Image: &ImageRules{Formats: mediumImageTypes}}
	Register(Medium{NewBase("medium", "Medium", AuthToken, limits).WithSettings(
		titleField(100),
		// Medium's API uses the first three tags and ignores longer ones than 25 characters.
		Field{Key: "tags", Label: "Tags", Kind: "list", MaxItems: 3, Placeholder: "productivity",
			Help: "Up to 3 tags of at most 25 characters."},
		Field{Key: "canonical_url", Label: "Original link", Kind: "url", Help: "Where the article first appeared, if anywhere."},
		Field{Key: "publish_status", Label: "Publish as", Kind: "select", Default: "public", Options: []Option{
			{Value: "public", Label: "Public"},
			{Value: "unlisted", Label: "Unlisted (only people with the link)"},
			{Value: "draft", Label: "Draft"},
		}},
	)}, StatusAvailable)
}

func (Medium) CredentialFields() []Field {
	return []Field{
		{Key: "token", Label: "Integration token", Kind: "secret", Required: true,
			Help: "Medium → Settings → Security and apps → Integration tokens. Medium no longer issues new ones: only an account that already has a token can connect."},
		{Key: "publication", Label: "Publication", Kind: "text", Placeholder: "https://medium.com/your-publication",
			Help: "Optional: publish to a publication you write for (its link or id) instead of your profile."},
	}
}

func (m Medium) Validate(p PostContent, s Settings) error {
	if err := CheckLimits(m.Limits(), p, s); err != nil {
		return err
	}
	if err := blogTitled(s); err != nil {
		return err
	}
	for _, f := range p.Media {
		ok := false
		for _, t := range mediumImageTypes {
			ok = ok || f.MimeType == t
		}
		if !ok {
			return invalid("media", "Medium takes JPEG, PNG, GIF or TIFF pictures; save "+f.Name+" as JPEG or PNG")
		}
	}
	for _, t := range ListValues(s["tags"]) {
		if utf8.RuneCountInString(t) > 25 {
			return invalid("tags", "Medium tags are at most 25 characters ("+t+")")
		}
	}
	return CheckFields(m.SettingsFields(), s)
}

type mediumUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	ImageURL string `json:"imageUrl"`
}

type mediumPublication struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Connect checks the token (GET /me) and, when a publication is named, that the person writes for it.
func (Medium) Connect(ctx context.Context, f map[string]string) (Credentials, Profile, error) {
	token := strings.TrimSpace(f["token"])
	if token == "" {
		return nil, Profile{}, invalid("token", "paste the integration token")
	}
	var me struct {
		Data mediumUser `json:"data"`
	}
	if err := mediumGet(ctx, token, "/me", &me); err != nil {
		return nil, Profile{}, err
	}
	if me.Data.ID == "" {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Medium did not say which account the token belongs to"}
	}
	creds := Credentials{"token": token, "user_id": me.Data.ID, "username": me.Data.Username}
	profile := Profile{Handle: "@" + me.Data.Username, Name: me.Data.Name, AvatarURL: me.Data.ImageURL, ProfileURL: me.Data.URL}
	if profile.Name == "" {
		profile.Name = me.Data.Username
	}
	if want := strings.TrimSpace(f["publication"]); want != "" {
		var pubs struct {
			Data []mediumPublication `json:"data"`
		}
		if err := mediumGet(ctx, token, "/users/"+url.PathEscape(me.Data.ID)+"/publications", &pubs); err != nil {
			return nil, Profile{}, err
		}
		pub, ok := mediumFindPublication(pubs.Data, want)
		if !ok {
			if len(pubs.Data) == 0 {
				return nil, Profile{}, invalid("publication", "this account writes for no publication on Medium; leave Publication empty to publish to your profile")
			}
			var names []string
			for _, p := range pubs.Data {
				names = append(names, p.Name+" ("+p.URL+")")
			}
			return nil, Profile{}, invalid("publication", "this account writes for no publication like that; choose one of: "+strings.Join(names, ", "))
		}
		creds["publication"] = pub.ID
		slug := strings.TrimRight(pub.URL, "/")
		slug = slug[strings.LastIndex(slug, "/")+1:]
		profile.Handle = "@" + me.Data.Username + " in " + slug
		profile.Name = pub.Name
		profile.ProfileURL = pub.URL
	}
	return creds, profile, nil
}

// mediumFindPublication matches a publication by id, link (with or without https://) or name.
func mediumFindPublication(pubs []mediumPublication, want string) (mediumPublication, bool) {
	norm := func(s string) string {
		s = strings.ToLower(strings.TrimRight(strings.TrimSpace(s), "/"))
		s = strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://")
		return strings.TrimPrefix(s, "www.")
	}
	w := norm(want)
	for _, p := range pubs {
		if p.ID == strings.TrimSpace(want) || norm(p.URL) == w || strings.EqualFold(p.Name, strings.TrimSpace(want)) {
			return p, true
		}
	}
	return mediumPublication{}, false
}

func (m Medium) Publish(ctx context.Context, c Credentials, p PostContent, s Settings) (Result, error) {
	token, user := c["token"], c["user_id"]
	if token == "" || user == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "Medium: no account stored; reconnect the account"}
	}
	content := "# " + strings.TrimSpace(s["title"]) + "\n\n"
	// Medium shows the title only when the story's content starts with it (the title field is for listings).
	if len(p.Media) > 0 {
		link, err := mediumUpload(ctx, token, p.Media[0])
		if err != nil {
			return Result{}, err
		}
		content += "![](" + link + ")\n\n"
	}
	content += p.Text
	status := s["publish_status"]
	if status == "" {
		status = "public"
	}
	body := map[string]any{
		"title":         strings.TrimSpace(s["title"]),
		"contentFormat": "markdown",
		"content":       content,
		"publishStatus": status,
	}
	if tags := ListValues(s["tags"]); len(tags) > 0 {
		body["tags"] = tags
	}
	if v := strings.TrimSpace(s["canonical_url"]); v != "" {
		body["canonicalUrl"] = v
	}
	path := "/users/" + url.PathEscape(user) + "/posts"
	if pub := c["publication"]; pub != "" {
		path = "/publications/" + url.PathEscape(pub) + "/posts"
	}
	req, err := blogJSON(ctx, http.MethodPost, mediumAPI+path, body)
	if err != nil {
		return Result{}, err
	}
	var out struct {
		Data struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		} `json:"data"`
	}
	// Medium has no idempotency key: a post whose answer never came may be out, and a retry would post it twice.
	if err := mediumDo(ctx, token, req, &out); err != nil {
		return Result{}, blogLost("Medium", "your Medium profile", err)
	}
	if out.Data.ID == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Medium published the post but did not say its id; check your Medium profile"}
	}
	link := out.Data.URL
	if link == "" {
		link = "https://medium.com/p/" + out.Data.ID
	}
	return Result{ExternalID: out.Data.ID, URL: link}, nil
}

// mediumUpload sends a picture to Medium's images endpoint and returns the link it is served from.
func mediumUpload(ctx context.Context, token string, m MediaFile) (string, error) {
	r, err := m.Reader(ctx)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", m.Name, err)
	}
	defer r.Close()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	h := textproto.MIMEHeader{}
	name := strings.NewReplacer(`"`, "", "\r", "", "\n", "").Replace(m.Name)
	if name == "" {
		name = "image"
	}
	h.Set("Content-Disposition", `form-data; name="image"; filename="`+name+`"`)
	h.Set("Content-Type", m.MimeType)
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, mediumAPI+"/images", &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	var out struct {
		Data struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := mediumDo(ctx, token, req, &out); err != nil {
		return "", blogRetry(err) // nothing is published yet
	}
	if out.Data.URL == "" {
		return "", &Failure{Kind: ErrRejected, Msg: "Medium took the picture but did not say where it is"}
	}
	return out.Data.URL, nil
}

func mediumGet(ctx context.Context, token, path string, out any) error {
	req, err := blogJSON(ctx, http.MethodGet, mediumAPI+path, nil)
	if err != nil {
		return err
	}
	return blogRetry(mediumDo(ctx, token, req, out))
}

func mediumDo(ctx context.Context, token string, req *http.Request, out any) error {
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept-Charset", "utf-8")
	return blogDo(ctx, "Medium", req, out, mediumError)
}

// mediumError maps Medium's replies ({"errors":[{"message","code"}]}) to what staqly does next.
func mediumError(status int, raw []byte) error {
	var e struct {
		Errors []struct {
			Message string `json:"message"`
			Code    int    `json:"code"`
		} `json:"errors"`
	}
	_ = json.Unmarshal(raw, &e)
	detail := blogSnippet(raw)
	code := 0
	if len(e.Errors) > 0 {
		detail, code = e.Errors[0].Message, e.Errors[0].Code
	}
	switch {
	case status == http.StatusUnauthorized, code == 6000, code == 6001, code == 6003:
		return &Failure{Kind: ErrReauth, Msg: "Medium no longer accepts this integration token (" + detail + "); reconnect with a valid one"}
	case status == http.StatusTooManyRequests:
		return &Failure{Msg: "Medium rate limit reached; staqly tries again in a few minutes"}
	case status >= 500:
		return &Failure{Msg: fmt.Sprintf("Medium is unavailable (%d); staqly tries again in a few minutes", status)}
	case status == http.StatusForbidden:
		return &Failure{Kind: ErrRejected, Msg: "Medium refused it (" + detail + "); check that the account may publish here"}
	case status == http.StatusNotFound:
		return &Failure{Kind: ErrRejected, Msg: "Medium could not find the account or publication (" + detail + "); reconnect the channel"}
	}
	return &Failure{Kind: ErrRejected, Msg: "Medium: " + detail}
}
