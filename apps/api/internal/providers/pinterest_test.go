package providers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakePinterest answers like api.pinterest.com for the calls the provider makes.
type fakePinterest struct {
	mu        sync.Mutex
	srv       *httptest.Server
	calls     []string
	tokenForm url.Values
	basicUser string
	pin       map[string]any // the last POST /v5/pins body
	pins      int
	pinCode   int    // status for POST /v5/pins (0 = 201)
	pinBody   string // its body then
	dropPin   bool   // close the connection on POST /v5/pins
	onBoard   bool   // the board's pins list the pin
}

func (f *fakePinterest) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	raw, _ := io.ReadAll(r.Body)
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/v5/oauth/token":
		f.tokenForm, _ = url.ParseQuery(string(raw))
		f.basicUser, _, _ = r.BasicAuth()
		if f.tokenForm.Get("refresh_token") == "expired" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":1,"message":"Invalid refresh token"}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"pina_1","refresh_token":"pinr_2","response_type":"authorization_code","token_type":"bearer","expires_in":2592000,"refresh_token_expires_in":31536000,"scope":"boards:read,pins:read,pins:write,user_accounts:read"}`))
	case r.Header.Get("Authorization") == "Bearer revoked":
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":2,"message":"Authentication failed."}`))
	case r.URL.Path == "/v5/user_account":
		_, _ = w.Write([]byte(`{"account_type":"BUSINESS","username":"janedoe","business_name":"Jane Doe Running","profile_image":"https://i.pinimg.example/jane.jpg"}`))
	case r.URL.Path == "/v5/boards" && r.URL.Query().Get("bookmark") == "":
		_, _ = w.Write([]byte(`{"items":[{"id":"1001","name":"Recipes"}],"bookmark":"b2"}`))
	case r.URL.Path == "/v5/boards":
		_, _ = w.Write([]byte(`{"items":[{"id":"1002","name":"Marathon training"}],"bookmark":null}`))
	case r.URL.Path == "/v5/boards/1002/pins":
		items := `{"id":"old","title":"Long run","link":"https://example.com/run","created_at":"2020-01-01T10:00:00"}`
		if f.onBoard {
			items = `{"id":"9001","title":"Long run","link":"https://example.com/run","created_at":"` + time.Now().UTC().Format("2006-01-02T15:04:05") + `"},` + items
		}
		_, _ = w.Write([]byte(`{"items":[` + items + `],"bookmark":null}`))
	case r.Method == http.MethodPost && r.URL.Path == "/v5/pins":
		f.pins++
		_ = json.Unmarshal(raw, &f.pin)
		switch {
		case f.dropPin:
			hj, _ := w.(http.Hijacker)
			c, _, _ := hj.Hijack()
			_ = c.Close()
		case f.pinCode != 0:
			w.WriteHeader(f.pinCode)
			_, _ = w.Write([]byte(f.pinBody))
		default:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"8001","board_id":"1002"}`))
		}
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func useFakePinterest(t *testing.T) *fakePinterest {
	t.Helper()
	f := &fakePinterest{}
	f.srv = httptest.NewServer(f)
	api, auth := pinterestAPI, pinterestAuthorize
	pinterestAPI, pinterestAuthorize = f.srv.URL, f.srv.URL+"/oauth/"
	t.Cleanup(func() {
		f.srv.Close()
		pinterestAPI, pinterestAuthorize = api, auth
	})
	return f
}

func pinterestCreds() Credentials {
	return Credentials{"access_token": "pina_1", "refresh_token": "pinr_1", "username": "janedoe"}
}

func pinterestPicture() MediaFile {
	return MediaFile{Name: "a.png", MimeType: "image/png", Size: 3,
		Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("PNG")), nil }}
}

func TestPinterestConnect(t *testing.T) {
	f := useFakePinterest(t)
	pt, _ := Get("pinterest")
	if pt.Status != StatusAvailable {
		t.Fatalf("pinterest is %q", pt.Status)
	}
	app := App{ClientID: "cid", ClientSecret: "secret"}
	u, _ := pt.AuthURL(app, "https://app.example/cb", "st", nil)
	if q, _ := url.Parse(u); q.Query().Get("scope") != pinterestScopes || q.Query().Get("response_type") != "code" {
		t.Fatalf("auth url %s", u)
	}
	creds, profile, err := pt.Exchange(context.Background(), app, "https://app.example/cb", "code1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.basicUser != "cid" || f.tokenForm.Get("code") != "code1" {
		t.Fatalf("token request %v", f.tokenForm)
	}
	if creds["access_token"] != "pina_1" || creds["refresh_token"] != "pinr_2" || creds["expires_at"] == "" || creds["refresh_expires_at"] == "" {
		t.Fatalf("creds %v", creds)
	}
	if profile.Handle != "@janedoe" || profile.Name != "Jane Doe Running" || profile.ProfileURL != "https://www.pinterest.com/janedoe/" {
		t.Fatalf("profile %+v", profile)
	}
	if c, err := pt.Refresh(context.Background(), app, pinterestCreds()); err != nil || c["refresh_token"] != "pinr_2" {
		t.Fatalf("refresh %v %v", c, err)
	}
	expired := pinterestCreds()
	expired["refresh_token"] = "expired"
	if _, err := pt.Refresh(context.Background(), app, expired); !errors.Is(err, ErrReauth) {
		t.Fatalf("expired refresh: %v", err)
	}
}

func TestPinterestPublish(t *testing.T) {
	ctx := context.Background()
	pt, _ := Get("pinterest")
	s := Settings{"title": "Long run", "board": "marathon training", "link": "https://example.com/run", "alt_text": "A runner at dawn"}

	t.Run("pin on a board found by name", func(t *testing.T) {
		f := useFakePinterest(t)
		res, err := pt.Publish(ctx, pinterestCreds(), PostContent{Text: "20 miles", Media: []MediaFile{pinterestPicture()}}, s)
		if err != nil {
			t.Fatal(err)
		}
		src := f.pin["media_source"].(map[string]any)
		if f.pin["board_id"] != "1002" || f.pin["title"] != "Long run" || f.pin["description"] != "20 miles" || f.pin["link"] != "https://example.com/run" || f.pin["alt_text"] != "A runner at dawn" {
			t.Fatalf("pin %v", f.pin)
		}
		if src["source_type"] != "image_base64" || src["content_type"] != "image/png" || src["data"] != base64.StdEncoding.EncodeToString([]byte("PNG")) {
			t.Fatalf("media %v", src)
		}
		if res.ExternalID != "8001" || res.URL != "https://www.pinterest.com/pin/8001/" {
			t.Fatalf("result %+v", res)
		}
		// A board id works too.
		if _, err := pt.Publish(ctx, pinterestCreds(), PostContent{Media: []MediaFile{pinterestPicture()}}, Settings{"title": "t", "board": "1001", "link": "https://example.com"}); err != nil || f.pin["board_id"] != "1001" {
			t.Fatalf("by id: %v %v", err, f.pin)
		}
	})

	t.Run("unknown board lists the boards", func(t *testing.T) {
		f := useFakePinterest(t)
		bad := Settings{"title": "t", "board": "Gear", "link": "https://example.com"}
		_, err := pt.Publish(ctx, pinterestCreds(), PostContent{Media: []MediaFile{pinterestPicture()}}, bad)
		if !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "Recipes, Marathon training") || f.pins != 0 {
			t.Fatalf("unknown board: %v", err)
		}
	})

	t.Run("refused, rate limited, reauth", func(t *testing.T) {
		f := useFakePinterest(t)
		f.pinCode, f.pinBody = http.StatusBadRequest, `{"code":1,"message":"Invalid parameters: link must be a valid URL"}`
		if _, err := pt.Publish(ctx, pinterestCreds(), PostContent{Media: []MediaFile{pinterestPicture()}}, s); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "valid URL") {
			t.Fatalf("refused: %v", err)
		}
		f.pinCode, f.pinBody = http.StatusTooManyRequests, `{"code":8,"message":"Rate limit exceeded"}`
		_, err := pt.Publish(ctx, pinterestCreds(), PostContent{Media: []MediaFile{pinterestPicture()}}, s)
		if err == nil || errors.Is(err, ErrRejected) || errors.Is(err, ErrReauth) {
			t.Fatalf("rate limit is retried: %v", err)
		}
		c := pinterestCreds()
		c["access_token"] = "revoked"
		if _, err := pt.Publish(ctx, c, PostContent{Media: []MediaFile{pinterestPicture()}}, s); !errors.Is(err, ErrReauth) {
			t.Fatalf("revoked: %v", err)
		}
	})

	t.Run("lost answer", func(t *testing.T) {
		f := useFakePinterest(t)
		f.dropPin = true
		if _, err := pt.Publish(ctx, pinterestCreds(), PostContent{Media: []MediaFile{pinterestPicture()}}, s); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "may have published") {
			t.Fatalf("lost answer: %v", err)
		}
		f.onBoard = true
		res, err := pt.Publish(ctx, pinterestCreds(), PostContent{Media: []MediaFile{pinterestPicture()}}, s)
		if err != nil || res.ExternalID != "9001" {
			t.Fatalf("found after a lost answer: %+v %v", res, err)
		}
	})
}

func TestPinterestValidate(t *testing.T) {
	pt, _ := Get("pinterest")
	pic := []MediaFile{{MimeType: "image/jpeg"}}
	ok := Settings{"title": "t", "board": "b", "link": "https://example.com"}
	for name, tc := range map[string]struct {
		p     PostContent
		s     Settings
		field string
	}{
		"no picture": {PostContent{Text: "x"}, ok, "media"},
		"video":      {PostContent{Media: []MediaFile{{MimeType: "video/mp4"}}}, ok, "media"},
		"gif":        {PostContent{Media: []MediaFile{{MimeType: "image/gif"}}}, ok, "media"},
		"no board":   {PostContent{Media: pic}, Settings{"title": "t", "link": "https://example.com"}, "board"},
		"no link":    {PostContent{Media: pic}, Settings{"title": "t", "board": "b"}, "link"},
		"no title":   {PostContent{Media: pic}, Settings{"board": "b", "link": "https://example.com"}, "title"},
		"too long":   {PostContent{Text: strings.Repeat("a", 501), Media: pic}, ok, "content"},
	} {
		var ve *ValidationError
		if err := pt.Validate(tc.p, tc.s); !errors.As(err, &ve) || ve.Field != tc.field {
			t.Errorf("%s: %v, want a %s error", name, err, tc.field)
		}
	}
	if err := pt.Validate(PostContent{Media: pic}, ok); err != nil {
		t.Fatal(err)
	}
}

func TestPinterestCheckApp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, secret, _ := r.BasicAuth(); secret != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":2,"message":"Authentication failed."}`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":1,"message":"Invalid authorization code"}`))
	}))
	defer srv.Close()
	old := pinterestAPI
	pinterestAPI = srv.URL
	defer func() { pinterestAPI = old }()
	if ok, err := (Pinterest{}).CheckApp(context.Background(), App{ClientID: "cid", ClientSecret: "secret"}, "https://app.example/cb"); ok || err != nil {
		t.Fatalf("good keys: %v %v", ok, err)
	}
	if _, err := (Pinterest{}).CheckApp(context.Background(), App{ClientID: "cid", ClientSecret: "wrong"}, "https://app.example/cb"); err == nil {
		t.Fatal("bad keys passed")
	}
}
