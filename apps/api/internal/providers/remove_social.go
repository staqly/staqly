package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// removeSend sends a delete request. A 2xx and a 404 (the item is already gone) are success; any other answer goes
// through fail, which is the provider's own error mapping. The error text names the host, never the request.
func removeSend(req *http.Request, name string, fail func(status int, raw []byte) error) error {
	resp, err := httpClient(req.Context()).Do(req)
	if err != nil {
		if req.Context().Err() != nil {
			return req.Context().Err()
		}
		return &Failure{Msg: name + " (" + req.URL.Host + ") could not be reached; staqly tries again in a few minutes"}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 == 2 || resp.StatusCode == http.StatusNotFound {
		return nil
	}
	return fail(resp.StatusCode, raw)
}

var _ Remover = X{}

// Remove deletes the post (DELETE /2/tweets/{id}).
func (X) Remove(ctx context.Context, c Credentials, externalID string) error {
	if c["access_token"] == "" {
		return &Failure{Kind: ErrReauth, Msg: "X: no account stored; reconnect the account"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, xAPI+"/2/tweets/"+url.PathEscape(externalID), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c["access_token"])
	return removeSend(req, "X", func(status int, raw []byte) error { return xError(status, string(raw)) })
}

var _ Remover = Mastodon{}

// Remove deletes the status (DELETE /api/v1/statuses/{id}) on the stored server.
func (Mastodon) Remove(ctx context.Context, c Credentials, externalID string) error {
	instance, token := c["instance"], c["access_token"]
	if instance == "" || token == "" {
		return &Failure{Kind: ErrReauth, Msg: "Mastodon: no account stored; reconnect the account"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, instance+"/api/v1/statuses/"+url.PathEscape(externalID), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	return removeSend(req, "Mastodon", mastodonError)
}

var _ Remover = Bluesky{}

// Remove deletes the record the id's first field (the at:// uri: at://<did>/<collection>/<rkey>) names; a reply's
// id carries its parent and root after it, which do not matter here.
func (Bluesky) Remove(ctx context.Context, c Credentials, externalID string) error {
	uri, _, _ := strings.Cut(externalID, "|")
	f := strings.Split(strings.TrimPrefix(uri, "at://"), "/")
	if !strings.HasPrefix(uri, "at://") || len(f) != 3 || f[0] == "" || f[1] == "" || f[2] == "" {
		return &Failure{Kind: ErrRejected, Msg: "Bluesky: the post has an id staqly can't read"}
	}
	sess, err := blueskyOpen(ctx, c)
	if err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]string{"repo": f[0], "collection": f[1], "rkey": f[2]})
	err = sess.call(ctx, http.MethodPost, "com.atproto.repo.deleteRecord", bytes.NewReader(body), "application/json", nil)
	if blueskyNotFound(err) {
		return nil
	}
	return err
}
