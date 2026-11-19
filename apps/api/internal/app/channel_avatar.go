package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/url"
	"strings"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// A channel keeps its own copy of its picture as a data: URL in avatar_url (providers.OwnAvatar). Mail apps
// (Gmail) show no data: URLs, so emails link it through a public route instead: the hash of the stored picture is
// the credential, and a link stops working once the picture changes.

func avatarHash(avatarURL string) string {
	sum := sha256.Sum256([]byte(avatarURL))
	return hex.EncodeToString(sum[:16])
}

// ChannelAvatar is a channel's own copy of its picture: its type (image/jpeg or image/png) and bytes. Anything
// else, a wrong hash included, is 404.
func (s *Service) ChannelAvatar(ctx context.Context, workspaceID, channelID, hash string) (string, []byte, error) {
	row, err := s.Q.GetChannel(ctx, db.GetChannelParams{WorkspaceID: workspaceID, ID: channelID})
	if isNoRows(err) {
		return "", nil, notFound("channel")
	}
	if err != nil {
		return "", nil, err
	}
	if !hmac.Equal([]byte(hash), []byte(avatarHash(row.AvatarUrl))) {
		return "", nil, notFound("channel")
	}
	for _, mime := range []string{"image/jpeg", "image/png"} {
		if b64, ok := strings.CutPrefix(row.AvatarUrl, "data:"+mime+";base64,"); ok {
			if b, err := base64.StdEncoding.DecodeString(b64); err == nil {
				return mime, b, nil
			}
		}
	}
	return "", nil, notFound("channel")
}

// emailAvatar is where an email shows a channel's picture from ("" = none): its own copy through the public route,
// or the platform's link unless that is a signed one that expires (Meta's oe=, TikTok's x-expires): a stale one is
// a broken image, and channels connected before the copy existed still have one.
func (s *Service) emailAvatar(workspaceID string, ch map[string]any) string {
	avatar, _ := ch["avatar_url"].(string)
	id, _ := ch["id"].(string)
	if strings.HasPrefix(avatar, "data:") {
		return s.webURL() + "/api/v1/channel-avatars/" + url.PathEscape(workspaceID) + "/" + url.PathEscape(id) + "/" + avatarHash(avatar)
	}
	u, err := url.Parse(avatar)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Query().Has("oe") || u.Query().Has("x-expires") {
		return ""
	}
	return avatar
}
