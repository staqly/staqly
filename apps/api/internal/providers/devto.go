package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode"
)

// DevTo publishes articles to dev.to over the Forem API (v1) with an API key the person makes in their settings.
// A post's text is the article in Markdown, its title setting the headline. Dev.to takes no uploads: a picture
// becomes the article's cover, which Dev.to downloads from a public link (MediaFile.SignedURL).
type DevTo struct{ Base }

// devtoAPI is Dev.to's API root, a variable so tests point it at httptest.
var devtoAPI = "https://dev.to/api"

func init() {
	limits := Limits{MaxChars: 0, MaxMedia: 1, MediaKinds: []string{"image"}, NeedsTitle: true}
	Register(DevTo{NewBase("devto", "Dev.to", AuthToken, limits).WithSettings(
		titleField(128), // Forem refuses longer titles
		Field{Key: "tags", Label: "Tags", Kind: "list", MaxItems: 4, Placeholder: "webdev",
			Help: "Up to 4 tags; Dev.to keeps letters and digits only."},
		Field{Key: "canonical_url", Label: "Original link", Kind: "url", Help: "Where the article first appeared, if anywhere."},
		Field{Key: "series", Label: "Series", Kind: "text", MaxLength: 128, Placeholder: "Learning Go",
			Help: "Articles with the same series name are shown together."},
	)}, StatusAvailable)
}

// Needs: Dev.to downloads a cover picture from a link.
func (DevTo) Needs() Needs { return Needs{PublicMedia: true} }

func (DevTo) CredentialFields() []Field {
	return []Field{{Key: "api_key", Label: "API key", Kind: "secret", Required: true,
		Help: "dev.to → Settings → Extensions → DEV Community API Keys: Generate API key."}}
}

func (d DevTo) Validate(p PostContent, s Settings) error {
	if err := CheckLimits(d.Limits(), p, s); err != nil {
		return err
	}
	if err := blogTitled(s); err != nil {
		return err
	}
	if err := blogImages("Dev.to", p.Media); err != nil {
		return err
	}
	for _, t := range ListValues(s["tags"]) {
		if devtoTag(t) == "" {
			return invalid("tags", "Dev.to tags are letters and digits ("+t+")")
		}
	}
	return CheckFields(d.SettingsFields(), s)
}

// devtoTag is a tag as Dev.to stores it: lowercase letters and digits only ("Web Dev" is "webdev").
func devtoTag(t string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(t) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Connect checks the key (GET /users/me).
func (DevTo) Connect(ctx context.Context, f map[string]string) (Credentials, Profile, error) {
	key := strings.TrimSpace(f["api_key"])
	if key == "" {
		return nil, Profile{}, invalid("api_key", "paste the API key")
	}
	var me struct {
		ID           int64  `json:"id"`
		Username     string `json:"username"`
		Name         string `json:"name"`
		ProfileImage string `json:"profile_image"`
	}
	if err := blogRetry(devtoCall(ctx, key, http.MethodGet, "/users/me", nil, &me)); err != nil {
		return nil, Profile{}, err
	}
	if me.Username == "" {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Dev.to did not say which account the key belongs to"}
	}
	name := me.Name
	if name == "" {
		name = me.Username
	}
	return Credentials{"api_key": key, "user_id": strconv.FormatInt(me.ID, 10), "username": me.Username},
		Profile{Handle: "@" + me.Username, Name: name, AvatarURL: me.ProfileImage, ProfileURL: "https://dev.to/" + me.Username}, nil
}

type devtoArticle struct {
	ID           int64  `json:"id"`
	Title        string `json:"title"`
	URL          string `json:"url"`
	BodyMarkdown string `json:"body_markdown"`
}

func (DevTo) Publish(ctx context.Context, c Credentials, p PostContent, s Settings) (Result, error) {
	key := c["api_key"]
	if key == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "Dev.to: no account stored; reconnect the account"}
	}
	cover, err := blogCover("Dev.to", p.Media)
	if err != nil {
		return Result{}, err
	}
	title := strings.TrimSpace(s["title"])
	article := map[string]any{
		"title":         title,
		"body_markdown": p.Text,
		"published":     true,
	}
	var tags []string
	for _, t := range ListValues(s["tags"]) {
		if t = devtoTag(t); t != "" {
			tags = append(tags, t)
		}
	}
	if len(tags) > 0 {
		article["tags"] = tags
	}
	if v := strings.TrimSpace(s["canonical_url"]); v != "" {
		article["canonical_url"] = v
	}
	if v := strings.TrimSpace(s["series"]); v != "" {
		article["series"] = v
	}
	if cover != "" {
		article["main_image"] = cover
	}
	var out devtoArticle
	err = devtoCall(ctx, key, http.MethodPost, "/articles", map[string]any{"article": article}, &out)
	if err != nil {
		// Dev.to refuses the same title and body from one author twice: after an earlier attempt whose answer was
		// lost (or a person posting it again), that article is this post, published.
		if strings.Contains(err.Error(), devtoDuplicateMsg) {
			if a, ok := devtoFind(ctx, key, title, p.Text); ok {
				return Result{ExternalID: strconv.FormatInt(a.ID, 10), URL: a.URL}, nil
			}
		}
		return Result{}, blogLost("Dev.to", "your Dev.to dashboard", err)
	}
	if out.ID == 0 {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Dev.to published the article but did not say its id; check your Dev.to dashboard"}
	}
	return Result{ExternalID: strconv.FormatInt(out.ID, 10), URL: out.URL}, nil
}

const devtoDuplicateMsg = "this article was already published"

// devtoFind looks for the author's recent article with this title and body.
func devtoFind(ctx context.Context, key, title, body string) (devtoArticle, bool) {
	var list []devtoArticle
	if err := devtoCall(ctx, key, http.MethodGet, "/articles/me/all?per_page=30", nil, &list); err != nil {
		return devtoArticle{}, false
	}
	for _, a := range list {
		if a.Title == title && strings.TrimSpace(a.BodyMarkdown) == strings.TrimSpace(body) && a.ID != 0 {
			return a, true
		}
	}
	return devtoArticle{}, false
}

func devtoCall(ctx context.Context, key, method, path string, body, out any) error {
	req, err := blogJSON(ctx, method, devtoAPI+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("api-key", key)
	req.Header.Set("Accept", "application/vnd.forem.api-v1+json")
	return blogDo(ctx, "Dev.to", req, out, devtoError)
}

// devtoError maps Forem's replies ({"error","status"}) to what staqly does next.
func devtoError(status int, raw []byte) error {
	var e struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &e)
	detail := e.Error
	if detail == "" {
		detail = blogSnippet(raw)
	}
	lower := strings.ToLower(detail)
	switch {
	case status == http.StatusUnauthorized:
		return &Failure{Kind: ErrReauth, Msg: "Dev.to no longer accepts this API key (" + detail + "); reconnect with a new one"}
	case status == http.StatusTooManyRequests:
		return &Failure{Msg: "Dev.to rate limit reached; staqly tries again in a few minutes"}
	case status >= 500:
		return &Failure{Msg: fmt.Sprintf("Dev.to is unavailable (%d); staqly tries again in a few minutes", status)}
	case strings.Contains(lower, "has already been taken"):
		return &Failure{Kind: ErrRejected, Msg: "Dev.to: " + devtoDuplicateMsg + " (" + detail + "); change the title or the text to post it again"}
	case status == http.StatusForbidden:
		return &Failure{Kind: ErrRejected, Msg: "Dev.to refused it (" + detail + ")"}
	}
	return &Failure{Kind: ErrRejected, Msg: "Dev.to: " + detail}
}
