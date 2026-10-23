package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// WordPress publishes posts to a self-hosted WordPress site (5.6 or newer) over its REST API (/wp-json/wp/v2), signed
// in with a username and an application password (HTTP Basic). WordPress.com sites take no application passwords and
// are not supported. A post's text is Markdown, turned into the HTML WordPress stores; its title setting is the
// headline, and a picture is uploaded to the media library and becomes the featured image.
type WordPress struct{ Base }

func init() {
	limits := Limits{MaxChars: 0, MaxMedia: 1, MediaKinds: []string{"image"}, NeedsTitle: true}
	Register(WordPress{NewBase("wordpress", "WordPress", AuthToken, limits).WithSettings(
		titleField(0),
		Field{Key: "status", Label: "Publish as", Kind: "select", Default: "publish", Options: []Option{
			{Value: "publish", Label: "Published post"},
			{Value: "draft", Label: "Draft"},
			{Value: "pending", Label: "Pending review"},
		}},
		Field{Key: "categories", Label: "Categories", Kind: "list", Placeholder: "News",
			Help: "Names of categories that exist on the site."},
		Field{Key: "tags", Label: "Tags", Kind: "list", Placeholder: "running",
			Help: "Tags the site doesn't have yet are created."},
	)}, StatusAvailable)
}

func (WordPress) CredentialFields() []Field {
	return []Field{
		{Key: "site_url", Label: "Site address", Kind: "url", Required: true, Placeholder: "https://blog.example.com"},
		{Key: "username", Label: "Username", Kind: "text", Required: true},
		{Key: "app_password", Label: "Application password", Kind: "secret", Required: true,
			Help: "WordPress → Users → Profile → Application passwords: add one named staqly. Not your login password."},
	}
}

func (w WordPress) Validate(p PostContent, s Settings) error {
	if err := CheckLimits(w.Limits(), p, s); err != nil {
		return err
	}
	if err := blogTitled(s); err != nil {
		return err
	}
	if err := blogImages("WordPress", p.Media); err != nil {
		return err
	}
	return CheckFields(w.SettingsFields(), s)
}

// wordpressSite is where a channel's REST API answers: "/wp-json" on sites with pretty links, "?rest_route=" on the
// others (plain permalinks).
type wordpressSite struct {
	base, user, password string
	plain                bool
}

func wordpressFrom(c Credentials) wordpressSite {
	return wordpressSite{base: strings.TrimRight(c["site_url"], "/"), user: c["username"], password: c["app_password"], plain: c["rest"] == "plain"}
}

func (s wordpressSite) url(route string, q url.Values) string {
	if s.plain {
		u := s.base + "/?rest_route=" + url.QueryEscape(route)
		if len(q) > 0 {
			u += "&" + q.Encode()
		}
		return u
	}
	u := s.base + "/wp-json" + route
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

func (s wordpressSite) call(ctx context.Context, method, route string, q url.Values, body, out any) error {
	req, err := blogJSON(ctx, method, s.url(route, q), body)
	if err != nil {
		return err
	}
	return wordpressDo(ctx, s, req, out)
}

func wordpressDo(ctx context.Context, s wordpressSite, req *http.Request, out any) error {
	req.SetBasicAuth(s.user, s.password)
	return blogDo(ctx, "WordPress", req, out, wordpressError)
}

type wordpressUser struct {
	ID           int64             `json:"id"`
	Name         string            `json:"name"`
	Slug         string            `json:"slug"`
	Link         string            `json:"link"`
	AvatarURLs   map[string]string `json:"avatar_urls"`
	Capabilities map[string]bool   `json:"capabilities"`
}

// Connect checks the application password (GET /wp/v2/users/me) and that the user may write posts.
func (WordPress) Connect(ctx context.Context, f map[string]string) (Credentials, Profile, error) {
	u, err := blogInstance("site_url", f["site_url"])
	if err != nil {
		return nil, Profile{}, err
	}
	user, pass := strings.TrimSpace(f["username"]), strings.TrimSpace(f["app_password"])
	if user == "" {
		return nil, Profile{}, invalid("username", "enter the WordPress username")
	}
	if pass == "" {
		return nil, Profile{}, invalid("app_password", "paste the application password")
	}
	site := wordpressSite{base: u.String(), user: user, password: pass}
	q := url.Values{"context": {"edit"}}
	var me wordpressUser
	err = site.call(ctx, http.MethodGet, "/wp/v2/users/me", q, nil, &me)
	if err != nil && !errors.Is(err, ErrReauth) {
		// Sites with plain permalinks answer at ?rest_route= only.
		plain := site
		plain.plain = true
		switch perr := plain.call(ctx, http.MethodGet, "/wp/v2/users/me", q, nil, &me); {
		case perr == nil:
			site, err = plain, nil
		case errors.Is(perr, ErrReauth):
			err = perr // the API is there, the sign-in is wrong
		}
	}
	if err != nil {
		if errors.Is(err, ErrReauth) {
			return nil, Profile{}, &Failure{Kind: ErrReauth, Msg: err.Error() + ". Check the username and the application password; on some hosts the web server drops the sign-in before WordPress sees it (see the WordPress page of staqly's docs)"}
		}
		return nil, Profile{}, blogRetry(err)
	}
	if me.ID == 0 {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "WordPress did not say which user signed in; check the site address"}
	}
	if caps := me.Capabilities; caps != nil && !caps["edit_posts"] {
		return nil, Profile{}, invalid("username", "this WordPress user may not write posts; use an Author, Editor or Administrator")
	}
	creds := Credentials{"site_url": site.base, "username": user, "app_password": pass}
	if site.plain {
		creds["rest"] = "plain"
	}
	name := html.UnescapeString(me.Name)
	if name == "" {
		name = user
	}
	return creds, Profile{
		Handle:     user + "@" + strings.TrimPrefix(strings.TrimPrefix(site.base, "https://"), "http://"),
		Name:       name,
		AvatarURL:  me.AvatarURLs["96"],
		ProfileURL: site.base,
	}, nil
}

func (WordPress) Publish(ctx context.Context, c Credentials, p PostContent, s Settings) (Result, error) {
	site := wordpressFrom(c)
	if site.base == "" || site.user == "" || site.password == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "WordPress: no site stored; reconnect the channel"}
	}
	content, err := markdownHTML(p.Text)
	if err != nil {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "WordPress: the text could not be turned into HTML (" + err.Error() + ")"}
	}
	status := s["status"]
	if status == "" {
		status = "publish"
	}
	post := map[string]any{"title": strings.TrimSpace(s["title"]), "content": content, "status": status}
	// Everything before the post itself publishes nothing, so its failures are retried.
	if names := ListValues(s["categories"]); len(names) > 0 {
		ids, err := site.terms(ctx, "categories", names, false)
		if err != nil {
			return Result{}, blogRetry(err)
		}
		post["categories"] = ids
	}
	if names := ListValues(s["tags"]); len(names) > 0 {
		ids, err := site.terms(ctx, "tags", names, true)
		if err != nil {
			return Result{}, blogRetry(err)
		}
		post["tags"] = ids
	}
	if len(p.Media) > 0 {
		id, err := site.upload(ctx, p.Media[0])
		if err != nil {
			return Result{}, blogRetry(err)
		}
		post["featured_media"] = id
	}
	var out struct {
		ID   int64  `json:"id"`
		Link string `json:"link"`
	}
	// WordPress has no idempotency key: a post whose answer never came may be out, and a retry would post it twice.
	if err := site.call(ctx, http.MethodPost, "/wp/v2/posts", nil, post, &out); err != nil {
		return Result{}, blogLost("WordPress", "the site's posts", err)
	}
	if out.ID == 0 {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "WordPress made the post but did not say its id; check the site's posts"}
	}
	link := out.Link
	if link == "" {
		link = site.base + "/?p=" + strconv.FormatInt(out.ID, 10)
	}
	return Result{ExternalID: strconv.FormatInt(out.ID, 10), URL: link}, nil
}

// terms turns category or tag names into their ids; create makes the missing ones (tags: any author may), otherwise
// a missing one fails the post.
func (s wordpressSite) terms(ctx context.Context, taxonomy string, names []string, create bool) ([]int64, error) {
	var ids []int64
	for _, name := range names {
		var found []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		}
		if err := s.call(ctx, http.MethodGet, "/wp/v2/"+taxonomy, url.Values{"search": {name}, "per_page": {"100"}}, nil, &found); err != nil {
			return nil, err
		}
		var id int64
		for _, t := range found {
			if strings.EqualFold(html.UnescapeString(t.Name), name) {
				id = t.ID
				break
			}
		}
		if id == 0 && create {
			var made struct {
				ID int64 `json:"id"`
			}
			err := s.call(ctx, http.MethodPost, "/wp/v2/"+taxonomy, nil, map[string]string{"name": name}, &made)
			var exists *wordpressTermExists
			if errors.As(err, &exists) && exists.id > 0 {
				made.ID, err = exists.id, nil // made by an earlier attempt, or a spelling the search missed
			}
			if err != nil {
				return nil, err
			}
			id = made.ID
		}
		if id == 0 {
			return nil, &Failure{Kind: ErrRejected, Msg: "WordPress has no category named " + name + "; create it on the site or remove it from the post"}
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// upload adds a picture to the media library and returns its id.
func (s wordpressSite) upload(ctx context.Context, m MediaFile) (int64, error) {
	r, err := m.Reader(ctx)
	if err != nil {
		return 0, fmt.Errorf("reading %s: %w", m.Name, err)
	}
	defer r.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url("/wp/v2/media", nil), r)
	if err != nil {
		return 0, err
	}
	if m.Size > 0 {
		req.ContentLength = m.Size
	}
	name := strings.NewReplacer(`"`, "", "\r", "", "\n", "", "/", "_", `\`, "_").Replace(m.Name)
	if name == "" {
		name = "image"
	}
	req.Header.Set("Content-Type", m.MimeType)
	req.Header.Set("Content-Disposition", `attachment; filename="`+name+`"`)
	req.Header.Set("Accept", "application/json")
	var out struct {
		ID int64 `json:"id"`
	}
	if err := wordpressDo(ctx, s, req, &out); err != nil {
		return 0, err
	}
	if out.ID == 0 {
		return 0, &Failure{Kind: ErrRejected, Msg: "WordPress took the picture but did not say its id"}
	}
	return out.ID, nil
}

// wordpressError maps WordPress's REST errors ({"code","message","data":{"status","term_id"}}) to what staqly does next.
func wordpressError(status int, raw []byte) error {
	var e struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Data    struct {
			TermID int64 `json:"term_id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(raw, &e)
	detail := html.UnescapeString(e.Message)
	if detail == "" {
		detail = blogSnippet(raw)
	}
	switch {
	case e.Code == "term_exists":
		return &wordpressTermExists{id: e.Data.TermID, f: &Failure{Kind: ErrRejected, Msg: "WordPress: " + detail}}
	case status == http.StatusUnauthorized, e.Code == "incorrect_password", e.Code == "invalid_username", e.Code == "invalid_email",
		e.Code == "rest_not_logged_in":
		return &Failure{Kind: ErrReauth, Msg: "WordPress did not accept the username and application password (" + detail + ")"}
	case status == http.StatusTooManyRequests:
		return &Failure{Msg: "WordPress rate limit reached; staqly tries again in a few minutes"}
	case status >= 500:
		return &Failure{Msg: fmt.Sprintf("WordPress is unavailable (%d: %s); staqly tries again in a few minutes", status, detail)}
	case e.Code == "application_passwords_disabled":
		return &Failure{Kind: ErrReauth, Msg: "this WordPress site has application passwords turned off (" + detail + "); turn them on, or ask its administrator"}
	case e.Code == "rest_cannot_create", e.Code == "rest_cannot_publish", e.Code == "rest_cannot_create_term",
		e.Code == "rest_cannot_assign_term", status == http.StatusForbidden:
		return &Failure{Kind: ErrRejected, Msg: "WordPress refused it (" + detail + "); the user's role may not allow it"}
	case status == http.StatusNotFound:
		return &Failure{Kind: ErrRejected, Msg: "WordPress's REST API did not answer at this address (" + detail + "); check the site address and that the REST API is on"}
	}
	return &Failure{Kind: ErrRejected, Msg: "WordPress: " + detail}
}

// wordpressTermExists: the tag staqly tried to create is there already (term_id names it).
type wordpressTermExists struct {
	id int64
	f  *Failure
}

func (e *wordpressTermExists) Error() string { return e.f.Error() }
func (e *wordpressTermExists) Unwrap() error { return e.f }
