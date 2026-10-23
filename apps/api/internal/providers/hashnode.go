package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unicode"
)

// Hashnode publishes articles to a Hashnode blog ("publication") over its GraphQL API with a personal access token.
// A post's text is the article in Markdown, its title setting the headline. Hashnode takes no uploads: a picture
// becomes the cover, which Hashnode downloads from a public link (MediaFile.SignedURL).
type Hashnode struct{ Base }

// hashnodeAPI is Hashnode's GraphQL endpoint, a variable so tests point it at httptest.
var hashnodeAPI = "https://gql.hashnode.com"

func init() {
	limits := Limits{MaxChars: 0, MaxMedia: 1, MediaKinds: []string{"image"}, NeedsTitle: true}
	Register(Hashnode{NewBase("hashnode", "Hashnode", AuthToken, limits).WithSettings(
		titleField(0),
		Field{Key: "subtitle", Label: "Subtitle", Kind: "text", MaxLength: 150},
		Field{Key: "tags", Label: "Tags", Kind: "list", MaxItems: 5, Placeholder: "javascript"},
		Field{Key: "canonical_url", Label: "Original link", Kind: "url", Help: "Where the article first appeared, if anywhere."},
	)}, StatusAvailable)
}

// Needs: Hashnode downloads a cover picture from a link.
func (Hashnode) Needs() Needs { return Needs{PublicMedia: true} }

func (Hashnode) CredentialFields() []Field {
	return []Field{
		{Key: "token", Label: "Personal access token", Kind: "secret", Required: true,
			Help: "hashnode.com → Account settings → Developer: Generate new token."},
		{Key: "publication", Label: "Blog", Kind: "text", Placeholder: "blog.example.com",
			Help: "The blog's address or id; needed only when the account has several blogs."},
	}
}

func (h Hashnode) Validate(p PostContent, s Settings) error {
	if err := CheckLimits(h.Limits(), p, s); err != nil {
		return err
	}
	if err := blogTitled(s); err != nil {
		return err
	}
	if err := blogImages("Hashnode", p.Media); err != nil {
		return err
	}
	for _, t := range ListValues(s["tags"]) {
		if hashnodeSlug(t) == "" {
			return invalid("tags", "Hashnode tags need letters or digits ("+t+")")
		}
	}
	return CheckFields(h.SettingsFields(), s)
}

// hashnodeSlug is a tag's slug: "Web Development" is "web-development".
func hashnodeSlug(t string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimPrefix(strings.TrimSpace(t), "#")) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
		default:
			dash = true
		}
	}
	return b.String()
}

type hashnodePublication struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

const hashnodeMe = `query Me { me { id username name profilePicture publications(first: 50) { edges { node { id title url } } } } }`

// Connect checks the token and picks the blog: the one named, or the account's only one.
func (Hashnode) Connect(ctx context.Context, f map[string]string) (Credentials, Profile, error) {
	token := strings.TrimSpace(f["token"])
	if token == "" {
		return nil, Profile{}, invalid("token", "paste the personal access token")
	}
	var out struct {
		Me *struct {
			ID             string `json:"id"`
			Username       string `json:"username"`
			Name           string `json:"name"`
			ProfilePicture string `json:"profilePicture"`
			Publications   struct {
				Edges []struct {
					Node hashnodePublication `json:"node"`
				} `json:"edges"`
			} `json:"publications"`
		} `json:"me"`
	}
	if err := blogRetry(hashnodeCall(ctx, token, hashnodeMe, nil, &out)); err != nil {
		return nil, Profile{}, err
	}
	if out.Me == nil || out.Me.ID == "" {
		return nil, Profile{}, &Failure{Kind: ErrReauth, Msg: "Hashnode did not accept the token; generate a new one and paste it"}
	}
	var pubs []hashnodePublication
	for _, e := range out.Me.Publications.Edges {
		pubs = append(pubs, e.Node)
	}
	want := strings.TrimSpace(f["publication"])
	var pub hashnodePublication
	switch {
	case len(pubs) == 0:
		return nil, Profile{}, invalid("publication", "this Hashnode account has no blog yet: create one on hashnode.com, then connect again")
	case want == "" && len(pubs) == 1:
		pub = pubs[0]
	default:
		found := false
		for _, p := range pubs {
			if want != "" && (p.ID == want || hashnodeHost(p.URL) == hashnodeHost(want)) {
				pub, found = p, true
				break
			}
		}
		if !found {
			var names []string
			for _, p := range pubs {
				names = append(names, hashnodeHost(p.URL))
			}
			msg := "this account has several blogs; enter one of: "
			if want != "" {
				msg = "this account has no blog at " + want + "; enter one of: "
			}
			return nil, Profile{}, invalid("publication", msg+strings.Join(names, ", "))
		}
	}
	name := pub.Title
	if name == "" {
		name = out.Me.Name
	}
	return Credentials{"token": token, "publication": pub.ID, "username": out.Me.Username},
		Profile{Handle: hashnodeHost(pub.URL), Name: name, AvatarURL: out.Me.ProfilePicture, ProfileURL: pub.URL}, nil
}

// hashnodeHost is a blog address without scheme, "www." or trailing slash ("blog.example.com").
func hashnodeHost(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if u, err := url.Parse(v); err == nil && u.Host != "" {
		v = u.Host + strings.TrimRight(u.Path, "/")
	}
	return strings.TrimPrefix(strings.TrimRight(v, "/"), "www.")
}

const hashnodePublish = `mutation PublishPost($input: PublishPostInput!) { publishPost(input: $input) { post { id url } } }`

func (Hashnode) Publish(ctx context.Context, c Credentials, p PostContent, s Settings) (Result, error) {
	token, pub := c["token"], c["publication"]
	if token == "" || pub == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "Hashnode: no account stored; reconnect the account"}
	}
	cover, err := blogCover("Hashnode", p.Media)
	if err != nil {
		return Result{}, err
	}
	input := map[string]any{
		"title":           strings.TrimSpace(s["title"]),
		"publicationId":   pub,
		"contentMarkdown": p.Text,
	}
	if v := strings.TrimSpace(s["subtitle"]); v != "" {
		input["subtitle"] = v
	}
	if v := strings.TrimSpace(s["canonical_url"]); v != "" {
		input["originalArticleURL"] = v
	}
	if cover != "" {
		input["coverImageOptions"] = map[string]any{"coverImageURL": cover}
	}
	tags := []map[string]string{}
	for _, t := range ListValues(s["tags"]) {
		if slug := hashnodeSlug(t); slug != "" {
			tags = append(tags, map[string]string{"slug": slug, "name": strings.TrimPrefix(t, "#")})
		}
	}
	input["tags"] = tags
	var out struct {
		PublishPost struct {
			Post *struct {
				ID  string `json:"id"`
				URL string `json:"url"`
			} `json:"post"`
		} `json:"publishPost"`
	}
	// Hashnode has no idempotency key and makes a new slug for the same title: never post again after a lost answer.
	if err := hashnodeCall(ctx, token, hashnodePublish, map[string]any{"input": input}, &out); err != nil {
		return Result{}, blogLost("Hashnode", "your blog", err)
	}
	if out.PublishPost.Post == nil || out.PublishPost.Post.ID == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Hashnode published the article but did not say its id; check your blog"}
	}
	return Result{ExternalID: out.PublishPost.Post.ID, URL: out.PublishPost.Post.URL}, nil
}

// hashnodeCall runs a GraphQL query; GraphQL errors (sent with status 200) become Failures like HTTP ones.
func hashnodeCall(ctx context.Context, token, query string, vars map[string]any, out any) error {
	body := map[string]any{"query": query}
	if vars != nil {
		body["variables"] = vars
	}
	req, err := blogJSON(ctx, http.MethodPost, hashnodeAPI, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", token) // Hashnode takes the bare token
	var resp struct {
		Data   json.RawMessage    `json:"data"`
		Errors []hashnodeGQLError `json:"errors"`
	}
	if err := blogDo(ctx, "Hashnode", req, &resp, hashnodeError); err != nil {
		return err
	}
	if len(resp.Errors) > 0 {
		return hashnodeFailure(http.StatusOK, resp.Errors)
	}
	if out != nil && len(resp.Data) > 0 && string(resp.Data) != "null" {
		return json.Unmarshal(resp.Data, out)
	}
	return nil
}

type hashnodeGQLError struct {
	Message    string `json:"message"`
	Extensions struct {
		Code string `json:"code"`
	} `json:"extensions"`
}

// hashnodeError maps an HTTP error reply (often still GraphQL-shaped) to what staqly does next.
func hashnodeError(status int, raw []byte) error {
	var e struct {
		Errors []hashnodeGQLError `json:"errors"`
	}
	_ = json.Unmarshal(raw, &e)
	if len(e.Errors) == 0 {
		e.Errors = []hashnodeGQLError{{Message: blogSnippet(raw)}}
	}
	return hashnodeFailure(status, e.Errors)
}

func hashnodeFailure(status int, errs []hashnodeGQLError) error {
	var msgs []string
	code := ""
	for _, e := range errs {
		msgs = append(msgs, e.Message)
		if code == "" {
			code = e.Extensions.Code
		}
	}
	detail := strings.Join(msgs, "; ")
	lower := strings.ToLower(detail)
	switch {
	case status == http.StatusUnauthorized, code == "UNAUTHENTICATED", strings.Contains(lower, "invalid token"):
		return &Failure{Kind: ErrReauth, Msg: "Hashnode no longer accepts this token (" + detail + "); reconnect with a new one"}
	case status == http.StatusTooManyRequests, code == "TOO_MANY_REQUESTS":
		return &Failure{Msg: "Hashnode rate limit reached; staqly tries again in a few minutes"}
	case status >= 500, code == "INTERNAL_SERVER_ERROR":
		return &Failure{Msg: fmt.Sprintf("Hashnode is unavailable (%s); staqly tries again in a few minutes", detail)}
	case code == "FORBIDDEN", status == http.StatusForbidden:
		return &Failure{Kind: ErrRejected, Msg: "Hashnode refused it (" + detail + "); check that the account may publish to this blog"}
	}
	return &Failure{Kind: ErrRejected, Msg: "Hashnode: " + detail}
}
