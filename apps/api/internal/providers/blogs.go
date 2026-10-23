package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

// Helpers the article platforms share (Medium, Dev.to, Hashnode, WordPress, listmonk): a post's text is the
// article's body in Markdown and its "title" setting the headline.

// blogDo sends a request and decodes a 2xx reply into out; anything else goes through mapErr. A request that never
// got an answer is a plain error (not a Failure) naming the host only, so Publish can tell a lost answer apart.
func blogDo(ctx context.Context, name string, req *http.Request, out any, mapErr func(status int, raw []byte) error) error {
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "staqly")
	}
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%s could not be reached (%s)", name, req.URL.Host)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out != nil && len(raw) > 0 {
			if err := json.Unmarshal(raw, out); err != nil {
				return &Failure{Kind: ErrRejected, Msg: name + " answered with something other than its API's JSON (" + blogSnippet(raw) + ")"}
			}
		}
		return nil
	}
	return mapErr(resp.StatusCode, raw)
}

// blogJSON builds a request with a JSON body.
func blogJSON(ctx context.Context, method, endpoint string, body any) (*http.Request, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, r)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

// blogLost turns an error from the request that creates the article into what staqly does next: the platform's
// own answers stay as they are, an answer that never came may mean the article is out, and a retry could publish
// it twice.
func blogLost(name, where string, err error) error {
	var f *Failure
	if errors.As(err, &f) || errors.Is(err, context.Canceled) {
		return err
	}
	return &Failure{Kind: ErrRejected, Msg: name + " may have published the post, but its answer was lost (" + err.Error() + "); check " + where + " before posting it again"}
}

// blogRetry makes an unanswered request (one that published nothing) a retryable Failure; others pass through.
func blogRetry(err error) error {
	var f *Failure
	if err == nil || errors.As(err, &f) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return &Failure{Msg: err.Error() + "; staqly tries again in a few minutes"}
}

// blogSnippet is a platform's reply cut to a readable length.
func blogSnippet(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	if s == "" {
		s = "an empty answer"
	}
	return s
}

// blogInstance checks a self-hosted instance's address (WordPress, listmonk) and returns it without a trailing slash.
func blogInstance(field, raw string) (*url.URL, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw != "" && !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, invalid(field, "enter the address as https://example.com")
	}
	u.User = nil
	return u, nil
}

// blogCover is the public link a platform downloads a cover picture from (Dev.to, Hashnode), or the reason it can't.
func blogCover(name string, media []MediaFile) (string, error) {
	if len(media) == 0 {
		return "", nil
	}
	if media[0].SignedURL == "" {
		// For whoever runs the server: the platform downloads the cover itself, so files must be in S3-compatible
		// storage (STORAGE_DRIVER=s3) at an address it can reach.
		return "", &Failure{Kind: ErrRejected, Msg: name + " downloads a cover picture from a link, and this server keeps media where " + name + " can't reach; remove the picture or ask whoever runs it to store media in the cloud"}
	}
	return media[0].SignedURL, nil
}

// blogTitled checks the title the article platforms need (CheckLimits already asks for one; this catches blanks).
func blogTitled(s Settings) error {
	if strings.TrimSpace(s["title"]) == "" {
		return invalid("title", "this channel needs a title")
	}
	return nil
}

// blogImages checks that every attachment is a picture (the article platforms take one, as a cover or lead image).
func blogImages(name string, media []MediaFile) error {
	for _, m := range media {
		if !strings.HasPrefix(m.MimeType, "image/") {
			return invalid("media", name+" takes a picture only; remove "+m.Name)
		}
	}
	return nil
}

var blogMarkdown = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	// The author's own HTML (embeds, figures) passes through; WordPress filters it by the account's role.
	goldmark.WithRendererOptions(html.WithUnsafe()),
)

// markdownHTML renders a post's Markdown as HTML (WordPress stores posts as HTML).
func markdownHTML(md string) (string, error) {
	var b bytes.Buffer
	if err := blogMarkdown.Convert([]byte(md), &b); err != nil {
		return "", err
	}
	return b.String(), nil
}
