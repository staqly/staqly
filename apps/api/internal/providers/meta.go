package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Meta's platforms (Instagram, Facebook Pages, Threads) share one shape of API:
// the Graph API, with the token in the query or the form, errors as {"error":{"message","code","error_subcode",
// "error_user_msg"}}, and media containers that are made, processed and then published. metaGraph is one host of it.
type metaGraph struct {
	name    string  // what messages call the platform
	host    *string // a variable, so tests point it at httptest
	version string  // "v24.0" (instagramVersion), "v1.0" for Threads
	// special maps a platform's own error codes before the shared ones; nil when it has none.
	special func(e metaErr) error
}

var (
	igGraph = metaGraph{name: "Instagram", host: &facebookGraph, version: instagramVersion}
	fbGraph = metaGraph{name: "Facebook", host: &facebookGraph, version: instagramVersion, special: facebookSpecial}
	thGraph = metaGraph{name: "Threads", host: &threadsGraph, version: "v1.0", special: threadsSpecial}
)

func (g metaGraph) url(path string) string { return *g.host + "/" + g.version + path }

func (g metaGraph) get(ctx context.Context, token, path string, q url.Values, out any) error {
	if q == nil {
		q = url.Values{}
	}
	q.Set("access_token", token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.url(path)+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	return g.do(ctx, req, out)
}

func (g metaGraph) post(ctx context.Context, token, path string, form url.Values) (*http.Request, error) {
	return g.postTo(ctx, g.url(path), token, form)
}

// postTo posts a form to a full address (another host of the same platform).
func (g metaGraph) postTo(ctx context.Context, endpoint, token string, form url.Values) (*http.Request, error) {
	form.Set("access_token", token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req, nil
}

// multipart posts the fields and the file's bytes (as field file), streamed from the server's storage.
func (g metaGraph) multipart(ctx context.Context, endpoint, token string, form url.Values, file string, m MediaFile) (*http.Request, error) {
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	form.Set("access_token", token)
	go func() {
		err := func() error {
			for k, vs := range form {
				for _, v := range vs {
					if err := mw.WriteField(k, v); err != nil {
						return err
					}
				}
			}
			rc, err := m.Reader(ctx)
			if err != nil {
				return err
			}
			defer rc.Close()
			name := m.Name
			if name == "" {
				name = "file"
			}
			h := make(map[string][]string)
			h["Content-Disposition"] = []string{fmt.Sprintf(`form-data; name=%q; filename=%q`, file, name)}
			h["Content-Type"] = []string{m.MimeType}
			part, err := mw.CreatePart(h)
			if err != nil {
				return err
			}
			if _, err := io.Copy(part, rc); err != nil {
				return err
			}
			return mw.Close()
		}()
		_ = pw.CloseWithError(err)
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, pr)
	if err != nil {
		_ = pr.Close()
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req, nil
}

// do sends a request and decodes a 2xx reply into out; Meta's errors become messages a person can act on.
// A request that never got an answer is a plain error (not a Failure), so Publish can tell it apart.
func (g metaGraph) do(ctx context.Context, req *http.Request, out any) error {
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		// The error may quote the URL, and with it the access token: name the host only.
		return fmt.Errorf("%s could not be reached (%s)", g.name, req.URL.Host)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out != nil && len(raw) > 0 {
			return json.Unmarshal(raw, out)
		}
		return nil
	}
	return g.fail(resp.StatusCode, raw)
}

// lost turns a publish request whose answer never came into a final failure: Meta has no idempotency key, so
// sending it again could post twice.
func (g metaGraph) lost(err error) error {
	var f *Failure
	if errors.As(err, &f) {
		return err
	}
	return &Failure{Kind: ErrRejected, Msg: g.name + " may have published the post, but its answer was lost (" + err.Error() + "); check the profile before posting it again"}
}

// metaErr is one of Meta's error replies, read.
type metaErr struct {
	status, code, sub int
	detail, lower     string
	oauth             bool // the shape of Instagram's token endpoint ({"error_type","code","error_message"})
}

func parseMetaError(status int, raw []byte) metaErr {
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
	out := metaErr{status: status, code: e.Error.Code, sub: e.Error.Subcode, detail: e.Error.Message}
	if e.Error.UserMsg != "" {
		out.detail = e.Error.UserMsg
	}
	if out.detail == "" && e.ErrorMessage != "" {
		out.detail, out.oauth = e.ErrorMessage, true
	}
	if out.detail == "" {
		out.detail = string(raw)
		if len(out.detail) > 200 {
			out.detail = out.detail[:200] + "..."
		}
	}
	out.lower = strings.ToLower(out.detail)
	return out
}

// fail maps Meta's replies to what staqly does next: a sign-in problem needs reconnecting, a refused post is final,
// rate limits and outages are retried.
func (g metaGraph) fail(status int, raw []byte) error {
	e := parseMetaError(status, raw)
	if g.special != nil {
		if err := g.special(e); err != nil {
			return err
		}
	}
	name, detail := g.name, e.detail
	switch code, sub := e.code, e.sub; {
	// Instagram's token endpoint: the app's keys, the return address or the sign-in's code, not the account.
	case e.oauth && strings.Contains(e.lower, "redirect_uri"):
		// For whoever runs the server: the app's login settings need <dashboard>/channels/callback/<platform>.
		return &Failure{Kind: ErrRejected, Msg: name + " did not accept the return address (" + detail + ")"}
	case e.oauth && (strings.Contains(e.lower, "client") || strings.Contains(e.lower, "app")):
		// For whoever runs the server: PROVIDER_<PLATFORM>_CLIENT_ID and _SECRET are the app's ID and secret.
		return &Failure{Kind: ErrRejected, Msg: name + " did not accept this server's app (" + detail + ")"}
	case e.oauth:
		return &Failure{Kind: ErrRejected, Msg: name + " did not accept the sign-in (" + detail + "); connect again"}
	case strings.Contains(e.lower, "not an instagram business"):
		return &Failure{Kind: ErrReauth, Msg: "Instagram posts only from professional accounts: switch the account to Business or Creator in the Instagram app, then reconnect it"}
	case code == 190, code == 102:
		return &Failure{Kind: ErrReauth, Msg: name + " no longer accepts this account's sign-in (" + detail + "); reconnect it"}
	case code == 4, code == 17, code == 32, code == 613, code == 80001, code == 80002, status == http.StatusTooManyRequests:
		return &Failure{Msg: name + " rate limit reached; staqly tries again in a few minutes"}
	case sub == 2207052, sub == 2207003:
		// The platform could not download the file in time: often a slow first fetch.
		return &Failure{Msg: name + " could not download the media (" + detail + "); staqly tries again in a few minutes"}
	case sub == 2207042:
		return &Failure{Kind: ErrRejected, Msg: name + "'s limit of posts a day for this account is reached (" + detail + "); schedule it for tomorrow"}
	case sub == 2207050, sub == 2207051:
		return &Failure{Kind: ErrRejected, Msg: name + " has restricted this account (" + detail + ")"}
	case code == 1, code == 2, status >= 500:
		return &Failure{Msg: fmt.Sprintf("%s is unavailable (%s); staqly tries again in a few minutes", name, detail)}
	case code == 10, code == 200, status == http.StatusForbidden:
		return &Failure{Kind: ErrRejected, Msg: name + " refused it (" + detail + ")"}
	case code == 9004, strings.Contains(e.lower, "media"):
		return &Failure{Kind: ErrRejected, Msg: name + " could not use the media (" + detail + ")"}
	}
	return &Failure{Kind: ErrRejected, Msg: name + ": " + detail}
}

// metaUserToken is a Facebook Login sign-in: the code traded for a user token, that one for a 60-day one. Facebook
// Pages and Instagram accounts linked to them sign in this way.
func metaUserToken(ctx context.Context, g metaGraph, app App, redirect, code string) (instagramLong, error) {
	var short instagramLong
	q := url.Values{"client_id": {app.ClientID}, "client_secret": {app.ClientSecret}, "redirect_uri": {redirect}, "code": {code}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.url("/oauth/access_token")+"?"+q.Encode(), nil)
	if err != nil {
		return short, err
	}
	if err := g.do(ctx, req, &short); err != nil {
		return short, instagramStep(err, "trading the sign-in's code")
	}
	if short.AccessToken == "" {
		return short, errors.New(g.name + " returned no access token")
	}
	var long instagramLong
	q = url.Values{"grant_type": {"fb_exchange_token"}, "client_id": {app.ClientID}, "client_secret": {app.ClientSecret}, "fb_exchange_token": {short.AccessToken}}
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, g.url("/oauth/access_token")+"?"+q.Encode(), nil)
	if err != nil {
		return long, err
	}
	if err := g.do(ctx, req, &long); err != nil {
		return long, instagramStep(err, "trading the sign-in for the 60-day token")
	}
	if long.AccessToken == "" {
		return long, errors.New(g.name + " returned no access token")
	}
	return long, nil
}

// metaGranted lists the permissions the person allowed (Facebook Login lets them untick some).
func metaGranted(ctx context.Context, g metaGraph, token string) (map[string]bool, error) {
	var out struct {
		Data []struct {
			Permission string `json:"permission"`
			Status     string `json:"status"`
		} `json:"data"`
	}
	if err := g.get(ctx, token, "/me/permissions", nil, &out); err != nil {
		return nil, instagramStep(err, "reading the permissions")
	}
	granted := map[string]bool{}
	for _, p := range out.Data {
		if p.Status == "granted" {
			granted[p.Permission] = true
		}
	}
	return granted, nil
}

// metaPage is a Facebook Page as /me/accounts lists it.
type metaPage struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Username    string `json:"username"`
	AccessToken string `json:"access_token"`
	Picture     struct {
		Data struct {
			URL string `json:"url"`
		} `json:"data"`
	} `json:"picture"`
	Instagram *struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Name     string `json:"name"`
		Picture  string `json:"profile_picture_url"`
	} `json:"instagram_business_account"`
}

// metaPages lists the Pages the sign-in may manage: the ones the person allowed in the dialog (/me/accounts), then
// the ones their business portfolios own or manage for clients (business_management), each once.
func metaPages(ctx context.Context, g metaGraph, token, fields string) ([]metaPage, error) {
	var pages []metaPage
	seen := map[string]bool{}
	add := func(path string, strict bool) error {
		err := metaEach(ctx, g, token, path, url.Values{"fields": {fields}, "limit": {"100"}}, func(raw json.RawMessage) {
			var p metaPage
			if json.Unmarshal(raw, &p) == nil && p.ID != "" && !seen[p.ID] {
				seen[p.ID] = true
				pages = append(pages, p)
			}
		})
		if err != nil && !strict {
			return nil // a portfolio the person may not read: the others still count
		}
		return err
	}
	if err := add("/me/accounts", true); err != nil {
		return nil, instagramStep(err, "listing the Pages")
	}
	var businesses []string
	_ = metaEach(ctx, g, token, "/me/businesses", url.Values{"fields": {"id"}, "limit": {"100"}}, func(raw json.RawMessage) {
		var b struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &b) == nil && b.ID != "" {
			businesses = append(businesses, b.ID)
		}
	})
	for _, b := range businesses {
		_ = add("/"+b+"/owned_pages", false)
		_ = add("/"+b+"/client_pages", false)
	}
	return pages, nil
}

// metaEach walks a paged list, following "paging.next" while it stays on the Graph host (at most 20 pages).
func metaEach(ctx context.Context, g metaGraph, token, path string, q url.Values, each func(json.RawMessage)) error {
	var out struct {
		Data   []json.RawMessage `json:"data"`
		Paging struct {
			Next string `json:"next"`
		} `json:"paging"`
	}
	if err := g.get(ctx, token, path, q, &out); err != nil {
		return err
	}
	host, _ := url.Parse(*g.host)
	for i := 0; ; i++ {
		for _, d := range out.Data {
			each(d)
		}
		next, err := url.Parse(out.Paging.Next)
		if out.Paging.Next == "" || err != nil || host == nil || next.Host != host.Host || next.Scheme != host.Scheme || i >= 20 {
			return nil
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, next.String(), nil)
		if err != nil {
			return err
		}
		out.Data, out.Paging.Next = nil, ""
		if err := g.do(ctx, req, &out); err != nil {
			return err
		}
	}
}

// metaWait sleeps between checks of something the platform is still processing, or ends with ctx.
func metaWait(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// metaCheckApp asks the Graph API for an app token (client_credentials): Facebook apps and Threads apps get one with
// good keys, and Meta names the key it did not accept otherwise.
func metaCheckApp(ctx context.Context, g metaGraph, endpoint string, app App) (bool, error) {
	q := url.Values{"client_id": {app.ClientID}, "client_secret": {app.ClientSecret}, "grant_type": {"client_credentials"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return false, err
	}
	status, body, err := probe(ctx, req, g.name)
	if err != nil {
		return false, err
	}
	if status == http.StatusOK && strings.Contains(body, "access_token") {
		return true, nil
	}
	e := parseMetaError(status, []byte(body))
	if e.code == 1 || e.code == 101 || strings.Contains(e.lower, "client") || strings.Contains(e.lower, "app") {
		return false, fmt.Errorf("%s did not accept the app keys: check the App ID and App secret (%s)", g.name, e.detail)
	}
	return false, unexpected(g.name, status, body)
}
