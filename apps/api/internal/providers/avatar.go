package providers

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"time"
)

// AvatarMax is the most a channel's own copy of its picture weighs (as Telegram's, kept inline in avatar_url).
const AvatarMax = 64 << 10

// OwnAvatar downloads a channel's picture once and returns it as a data: URL (a JPEG or PNG of at most AvatarMax,
// shrunk when bigger), so it outlives the platform's link: Meta's and TikTok's CDN links stop working after a few
// days and a channel that needs reconnecting can't ask for a new one. Anything else (no link, a failed download,
// not a picture) keeps the link as it was.
func OwnAvatar(ctx context.Context, link string) string {
	if !strings.HasPrefix(link, "https://") && !strings.HasPrefix(link, "http://") {
		return link
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return link
	}
	res, err := httpClient(ctx).Do(req)
	if err != nil {
		return link
	}
	defer res.Body.Close()
	const most = 8 << 20
	b, err := io.ReadAll(io.LimitReader(res.Body, most+1))
	if err != nil || res.StatusCode != http.StatusOK || len(b) > most {
		return link
	}
	mime := http.DetectContentType(b)
	if len(b) > AvatarMax || (mime != "image/jpeg" && mime != "image/png") {
		var ok bool
		if b, ok = shrinkJPEG(b, AvatarMax); !ok {
			return link
		}
		mime = "image/jpeg"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b)
}
