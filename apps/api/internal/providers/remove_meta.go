package providers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Facebook, Instagram and Threads delete a post (or a comment) with DELETE /{id}.
var (
	_ Remover = Facebook{}
	_ Remover = Instagram{}
	_ Remover = Threads{}
)

// del sends DELETE /{id}; nil also when Meta says the object is gone (404, or error 100 / subcode 33).
func (g metaGraph) del(ctx context.Context, token, id string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, g.url("/"+url.PathEscape(id))+"?"+url.Values{"access_token": {token}}.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		// The error may quote the URL, and with it the access token: name the host only.
		return fmt.Errorf("%s could not be reached (%s)", g.name, req.URL.Host)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	if e := parseMetaError(resp.StatusCode, raw); e.status == http.StatusNotFound || (e.code == 100 && e.sub == 33) {
		return nil
	}
	return g.fail(resp.StatusCode, raw)
}

// metaCommentID is the comment's own id from "comment:<post>:<id>", ok false for a post.
func metaCommentID(externalID string) (string, bool) {
	rest, ok := strings.CutPrefix(externalID, "comment:")
	if !ok {
		return "", false
	}
	_, id, _ := strings.Cut(rest, ":")
	return id, true
}

func (Facebook) Remove(ctx context.Context, c Credentials, externalID string) error {
	if id, ok := metaCommentID(externalID); ok {
		externalID = id
	}
	return fbGraph.del(ctx, c["access_token"], externalID)
}

// Remove deletes a comment of the thread, or the post itself: Meta deletes Instagram posts, reels, stories and whole
// carousels through Facebook Login only (graph.facebook.com, instagram_manage_contents), whatever comments others left.
// Instagram login (graph.instagram.com) has no delete, and a refusal (a permission not granted, an ad, a type Meta
// keeps) leaves it to the person.
func (Instagram) Remove(ctx context.Context, c Credentials, externalID string) error {
	if id, ok := metaCommentID(externalID); ok {
		return igFor(c).del(ctx, c["access_token"], id)
	}
	if c["login"] == "instagram" {
		return &Failure{Kind: ErrManual, Msg: "Instagram deletes posts through its API only for accounts connected with Facebook: delete it in the Instagram app"}
	}
	err := igGraph.del(ctx, c["access_token"], externalID)
	if errors.Is(err, ErrRejected) {
		return &Failure{Kind: ErrManual, Msg: err.Error() + "; delete it in the Instagram app (reconnecting the channel asks Facebook for the permission to delete)"}
	}
	return err
}

func (Threads) Remove(ctx context.Context, c Credentials, externalID string) error {
	return thGraph.del(ctx, c["access_token"], externalID)
}
