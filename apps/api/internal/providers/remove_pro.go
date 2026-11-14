package providers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

var (
	_ Remover = LinkedIn{}
	_ Remover = LinkedInPage{}
	_ Remover = YouTube{}
)

// removeDo sends a DELETE; nil on 2xx and on 404 (already gone), fail turns any other answer into a Failure.
func removeDo(ctx context.Context, name string, req *http.Request, fail func(status int, raw []byte) error) error {
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		// The error may quote the URL: name the host only.
		return fmt.Errorf("%s could not be reached (%s)", name, req.URL.Host)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 == 2 || resp.StatusCode == http.StatusNotFound {
		return nil
	}
	return fail(resp.StatusCode, raw)
}

// linkedinRemove deletes a post, or a comment ("urn:li:comment:(<post>,<id>)") as actor.
func linkedinRemove(ctx context.Context, token, actor, externalID string) error {
	endpoint := linkedinAPI + "/rest/posts/" + linkedinURN(externalID)
	if inner, ok := strings.CutPrefix(externalID, "urn:li:comment:("); ok {
		i := strings.LastIndex(inner, ",")
		if i <= 0 {
			return &Failure{Kind: ErrRejected, Msg: "LinkedIn: the comment has no id to delete"}
		}
		endpoint = linkedinAPI + "/rest/socialActions/" + linkedinURN(inner[:i]) + "/comments/" +
			linkedinURN(strings.TrimSuffix(inner[i+1:], ")")) + "?" + url.Values{"actor": {actor}}.Encode()
	}
	req, err := linkedinJSON(ctx, token, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	return removeDo(ctx, "LinkedIn", req, func(status int, raw []byte) error { return linkedinError(status, string(raw)) })
}

func (LinkedIn) Remove(ctx context.Context, c Credentials, externalID string) error {
	token, person := c["access_token"], c["person_id"]
	if token == "" || person == "" {
		return &Failure{Kind: ErrReauth, Msg: "LinkedIn: no account stored; reconnect the account"}
	}
	return linkedinRemove(ctx, token, "urn:li:person:"+person, externalID)
}

func (LinkedInPage) Remove(ctx context.Context, c Credentials, externalID string) error {
	token, org := c["access_token"], c["organization_id"]
	if token == "" || org == "" {
		return &Failure{Kind: ErrReauth, Msg: "LinkedIn: no page stored; reconnect the account"}
	}
	return linkedinRemove(ctx, token, "urn:li:organization:"+org, externalID)
}

// Remove deletes the video, or a comment ("comment:<video>:<id>"); both need the force-ssl scope.
func (YouTube) Remove(ctx context.Context, c Credentials, externalID string) error {
	token := c["access_token"]
	if token == "" {
		return &Failure{Kind: ErrReauth, Msg: "YouTube: no channel stored; reconnect the account"}
	}
	if s := c["scope"]; s != "" && !strings.Contains(s, "youtube.force-ssl") {
		return &Failure{Kind: ErrReauth, Msg: "YouTube: this sign-in may not delete; reconnect the account and allow everything Google asks for"}
	}
	endpoint := youtubeAPI + "/youtube/v3/videos?id=" + url.QueryEscape(externalID)
	if rest, ok := strings.CutPrefix(externalID, "comment:"); ok {
		_, id, _ := strings.Cut(rest, ":")
		endpoint = youtubeAPI + "/youtube/v3/comments?id=" + url.QueryEscape(id)
	}
	req, err := googleRequest(ctx, token, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	return removeDo(ctx, "YouTube", req, func(status int, raw []byte) error { return googleError("YouTube", status, raw) })
}
