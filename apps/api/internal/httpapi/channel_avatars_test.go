package httpapi_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// A channel's own picture is served without credentials for emails: only with the hash of the stored picture,
// only for that workspace's channel, and never a picture the channel only links to.
func TestChannelAvatarRoute(t *testing.T) {
	e := newEnv(t)
	var ch app.Channel
	e.do(nil, "POST", "/v1/channels", e.admin, map[string]any{"provider": "custom", "name": "My n8n"}, 201, &ch)
	var ws string
	e.svc.DB.QueryRow("SELECT workspace_id FROM channels WHERE id = ?", ch.ID).Scan(&ws)
	hash := func(v string) string {
		sum := sha256.Sum256([]byte(v))
		return hex.EncodeToString(sum[:16])
	}
	set := func(v string) {
		if _, err := e.svc.DB.Exec("UPDATE channels SET avatar_url = ? WHERE id = ?", v, ch.ID); err != nil {
			t.Fatal(err)
		}
	}
	own := "data:image/png;base64,iVBORw0KGgo="
	set(own)
	res := e.do(nil, "GET", "/v1/channel-avatars/"+ws+"/"+ch.ID+"/"+hash(own), "", nil, 200, nil)
	if res.Header.Get("Content-Type") != "image/png" || res.Header.Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("headers: %v", res.Header)
	}
	e.do(nil, "GET", "/v1/channel-avatars/"+ws+"/"+ch.ID+"/"+hash("other"), "", nil, 404, nil)
	e.do(nil, "GET", "/v1/channel-avatars/ws_other/"+ch.ID+"/"+hash(own), "", nil, 404, nil)
	link := "https://cdn.example/a.jpg"
	set(link)
	e.do(nil, "GET", "/v1/channel-avatars/"+ws+"/"+ch.ID+"/"+hash(link), "", nil, 404, nil)
}
