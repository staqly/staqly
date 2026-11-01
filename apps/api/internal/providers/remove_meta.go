package providers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Facebook and Threads delete a post (or a comment) with DELETE /{id}; Instagram only its comments.
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

func (Instagram) Remove(ctx context.Context, c Credentials, externalID string) error {
	id, ok := metaCommentID(externalID)
	if !ok {
		return &Failure{Kind: ErrManual, Msg: "Instagram has no way to delete a post through its API: delete it in the Instagram app"}
	}
	return igGraph.del(ctx, c["access_token"], id)
}

func (Threads) Remove(ctx context.Context, c Credentials, externalID string) error {
	return thGraph.del(ctx, c["access_token"], externalID)
}
