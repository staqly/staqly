package providers

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

var _ Remover = Reddit{}

// Remove deletes the post or comment (POST /api/del); externalID is its fullname (t3_... or t1_...). A post whose
// id Reddit never gave back can't be found by staqly.
func (Reddit) Remove(ctx context.Context, c Credentials, externalID string) error {
	token := c["access_token"]
	if token == "" {
		return &Failure{Kind: ErrReauth, Msg: "Reddit: no account stored; reconnect the account"}
	}
	if externalID == "" {
		return &Failure{Kind: ErrManual, Msg: "Reddit did not say the post's id: delete it on Reddit"}
	}
	if !strings.HasPrefix(externalID, "t1_") && !strings.HasPrefix(externalID, "t3_") {
		externalID = "t3_" + externalID
	}
	req, err := redditRequest(ctx, http.MethodPost, redditOAuth+"/api/del", token, url.Values{"id": {externalID}})
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", redditUserAgent)
	return removeSend(req, "Reddit", redditError)
}

var _ Remover = Tumblr{}

// Remove deletes the post (POST /v2/blog/{blog}/post/delete) on the stored blog.
func (Tumblr) Remove(ctx context.Context, c Credentials, externalID string) error {
	token, blog := c["access_token"], c["blog"]
	if token == "" || blog == "" {
		return &Failure{Kind: ErrReauth, Msg: "Tumblr: no blog stored; reconnect the account"}
	}
	req, err := tumblrRequest(ctx, http.MethodPost, "/v2/blog/"+url.PathEscape(blog)+"/post/delete", token,
		strings.NewReader(url.Values{"id": {externalID}}.Encode()), "application/x-www-form-urlencoded")
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", tumblrUserAgent)
	return removeSend(req, "Tumblr", tumblrError)
}

var _ Remover = Pinterest{}

// Remove deletes the pin (DELETE /v5/pins/{id}).
func (Pinterest) Remove(ctx context.Context, c Credentials, externalID string) error {
	token := c["access_token"]
	if token == "" {
		return &Failure{Kind: ErrReauth, Msg: "Pinterest: no account stored; reconnect the account"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, pinterestAPI+"/v5/pins/"+url.PathEscape(externalID), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return removeSend(req, "Pinterest", pinterestError)
}
