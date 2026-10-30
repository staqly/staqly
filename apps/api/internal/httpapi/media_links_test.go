package httpapi_test

import (
	"strings"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// With PUBLIC_MEDIA_URL, platforms download a file through a signed link on this server: no credentials, that one
// file only, and nothing for a wrong signature or another workspace's id.
func TestMediaLinks(t *testing.T) {
	e := newEnv(t)
	var pic, other app.Media
	e.upload("/v1/media", e.key, nil, "launch.png", pngBytes, 201, &pic)
	e.upload("/v1/media", e.key, nil, "other.png", pngBytes, 201, &other)
	var ws string
	e.svc.DB.QueryRow("SELECT workspace_id FROM media WHERE id = ?", pic.ID).Scan(&ws)

	if link := e.svc.MediaLink(ws, pic.ID); link != "" {
		t.Fatalf("without PUBLIC_MEDIA_URL there is no link: %s", link)
	}
	e.svc.Cfg.PublicMediaURL = e.srv.URL
	link := e.svc.MediaLink(ws, pic.ID)
	path := strings.TrimPrefix(link, e.srv.URL)
	if !strings.HasPrefix(path, "/v1/media-links/"+ws+"/"+pic.ID+"/") {
		t.Fatalf("link: %s", link)
	}
	res := e.do(nil, "GET", path, "", nil, 200, nil)
	if res.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("type: %s", res.Header.Get("Content-Type"))
	}
	sig := path[strings.LastIndex(path, "/")+1:]
	e.do(nil, "GET", "/v1/media-links/"+ws+"/"+other.ID+"/"+sig, "", nil, 404, nil)
	e.do(nil, "GET", "/v1/media-links/ws_other/"+pic.ID+"/"+sig, "", nil, 404, nil)
	e.do(nil, "GET", path+"x", "", nil, 404, nil)
}
