package httpapi_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
)

func pngDataURL(t *testing.T, side int) string {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, side, side))); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes())
}

func TestWorkspaceLogo(t *testing.T) {
	e := newEnv(t)
	var ws app.Workspace
	e.do(nil, "GET", "/v1/workspace", e.admin, nil, 200, &ws)
	if ws.Logo != "" {
		t.Fatalf("default logo %q", ws.Logo)
	}
	logo := pngDataURL(t, 128)
	e.do(nil, "PATCH", "/v1/workspace", e.key, map[string]any{"logo": logo}, 403, nil)
	svg := "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(`<svg onload="alert(1)"/>`))
	notPNG := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("GIF89a"))
	huge := "data:image/png;base64," + strings.Repeat("A", 100<<10)
	for name, bad := range map[string]string{
		"svg": svg, "not png": notPNG, "too big": huge, "too wide": pngDataURL(t, 600), "url": "https://example.com/a.png",
	} {
		var errBody struct{ Error app.Error }
		e.do(nil, "PATCH", "/v1/workspace", e.admin, map[string]any{"logo": bad}, 422, &errBody)
		if errBody.Error.Fields["logo"] == "" {
			t.Fatalf("%s: %+v", name, errBody.Error)
		}
	}
	e.do(nil, "PATCH", "/v1/workspace", e.admin, map[string]any{"logo": logo}, 200, &ws)
	if ws.Logo != logo || ws.Name == "" {
		t.Fatalf("patched: %q", ws.Logo[:min(40, len(ws.Logo))])
	}
	// The name alone keeps the logo; '' brings staqly's back.
	e.do(nil, "PATCH", "/v1/workspace", e.admin, map[string]any{"name": "Renamed"}, 200, &ws)
	if ws.Logo != logo {
		t.Fatal("rename dropped the logo")
	}
	e.do(nil, "PATCH", "/v1/workspace", e.admin, map[string]any{"logo": ""}, 200, &ws)
	if ws.Logo != "" {
		t.Fatal("logo not removed")
	}
	// The event says a logo was set, never the image.
	var evs struct{ Data []app.Event }
	e.do(nil, "GET", "/v1/events?type=workspace.updated", e.admin, nil, 200, &evs)
	b, _ := json.Marshal(evs.Data)
	if !strings.Contains(string(b), `"logo":true`) || strings.Contains(string(b), "base64") {
		t.Fatalf("events: %s", b)
	}
}

func TestAccountPicture(t *testing.T) {
	e := newAccountsEnv(t, "")
	p, _ := e.signIn(ana, "", nil)
	google := e.session(p).User.AvatarURL
	if google == "" {
		t.Fatal("no Google picture")
	}
	pic := pngDataURL(t, 128)
	var me app.User
	var errBody struct{ Error app.Error }
	e.as(p, "PATCH", "/v1/account", map[string]any{"avatar": "https://example.com/b.png"}, 422, &errBody)
	if errBody.Error.Fields["avatar"] == "" {
		t.Fatalf("url: %+v", errBody.Error)
	}
	e.as(p, "PATCH", "/v1/account", map[string]any{}, 422, nil)
	e.as(p, "PATCH", "/v1/account", map[string]any{"avatar": pic}, 200, &me)
	if me.AvatarURL != pic || me.Name != ana.Name {
		t.Fatalf("picture: %+v", me.Name)
	}
	// Signing in with Google again keeps the uploaded picture; dropping it brings Google's back at the next one.
	p, _ = e.signIn(ana, "", nil)
	if got := e.session(p).User.AvatarURL; got != pic {
		t.Fatalf("after sign-in: %.40s", got)
	}
	e.as(p, "PATCH", "/v1/account", map[string]any{"avatar": ""}, 200, &me)
	p, _ = e.signIn(ana, "", nil)
	if got := e.session(p).User.AvatarURL; got != google {
		t.Fatalf("after drop: %q", got)
	}
}
