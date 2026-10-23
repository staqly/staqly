package providers

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeDribbble answers like dribbble.com/oauth and api.dribbble.com for the calls the provider makes.
type fakeDribbble struct {
	mu        sync.Mutex
	srv       *httptest.Server
	tokenForm url.Values
	shot      map[string][]string // form values of the last POST /v2/shots (the image as its size)
	shots     int
	shotCode  int
	shotBody  string
	dropShot  bool
	listed    bool
	canUpload string
}

func (f *fakeDribbble) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/oauth/token":
		_ = r.ParseForm()
		f.tokenForm = r.PostForm
		if r.PostForm.Get("client_secret") != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_client","error_description":"Client authentication failed."}`))
			return
		}
		if r.PostForm.Get("code") == checkCode {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"The provided authorization grant is invalid."}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"dt-1","token_type":"bearer","scope":"public upload"}`))
	case r.Header.Get("Authorization") == "Bearer revoked":
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Bad credentials."}`))
	case r.URL.Path == "/v2/user":
		can := f.canUpload
		if can == "" {
			can = "true"
		}
		_, _ = w.Write([]byte(`{"id":1,"name":"Jane Doe","login":"janedoe","html_url":"https://dribbble.com/janedoe","avatar_url":"https://cdn.dribbble.example/jane.png","can_upload_shot":` + can + `}`))
	case r.URL.Path == "/v2/user/shots":
		shots := `{"id":1,"title":"Long run","published_at":"2020-01-01T10:00:00Z"}`
		if f.listed {
			shots = `{"id":471756,"title":"Long run","published_at":"` + time.Now().UTC().Format(time.RFC3339) + `"},` + shots
		}
		_, _ = w.Write([]byte(`[` + shots + `]`))
	case r.URL.Path == "/v2/shots":
		f.shots++
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		mr := multipart.NewReader(r.Body, params["boundary"])
		f.shot = map[string][]string{}
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			b, _ := io.ReadAll(p)
			v := string(b)
			if p.FileName() != "" {
				v = p.FileName()
			}
			f.shot[p.FormName()] = append(f.shot[p.FormName()], v)
		}
		switch {
		case f.dropShot:
			hj, _ := w.(http.Hijacker)
			c, _, _ := hj.Hijack()
			_ = c.Close()
		case f.shotCode != 0:
			w.WriteHeader(f.shotCode)
			_, _ = w.Write([]byte(f.shotBody))
		default:
			w.Header().Set("Location", "https://api.dribbble.com/v2/shots/471756")
			w.WriteHeader(http.StatusAccepted)
		}
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func useFakeDribbble(t *testing.T) *fakeDribbble {
	t.Helper()
	f := &fakeDribbble{}
	f.srv = httptest.NewServer(f)
	api, oauth, poll := dribbbleAPI, dribbbleOAuth, dribbblePoll
	dribbbleAPI, dribbbleOAuth, dribbblePoll = f.srv.URL, f.srv.URL+"/oauth", time.Millisecond
	t.Cleanup(func() {
		f.srv.Close()
		dribbbleAPI, dribbbleOAuth, dribbblePoll = api, oauth, poll
	})
	return f
}

func dribbbleShot(w, h int) MediaFile {
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewGray(image.Rect(0, 0, w, h)))
	data := b.Bytes()
	return MediaFile{Name: "shot.png", MimeType: "image/png", Size: int64(len(data)),
		Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }}
}

func TestDribbbleConnect(t *testing.T) {
	f := useFakeDribbble(t)
	db, _ := Get("dribbble")
	if db.Status != StatusAvailable {
		t.Fatalf("dribbble is %q", db.Status)
	}
	app := App{ClientID: "cid", ClientSecret: "secret"}
	u, _ := db.AuthURL(app, "https://app.example/cb", "st", nil)
	if q, _ := url.Parse(u); q.Query().Get("scope") != "public upload" || q.Query().Get("state") != "st" {
		t.Fatalf("auth url %s", u)
	}
	creds, profile, err := db.Exchange(context.Background(), app, "https://app.example/cb", "code1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.tokenForm.Get("code") != "code1" || creds["access_token"] != "dt-1" || profile.Handle != "@janedoe" || profile.ProfileURL != "https://dribbble.com/janedoe" {
		t.Fatalf("connect %v %v %+v", f.tokenForm, creds, profile)
	}
	f.canUpload = "false"
	if _, _, err := db.Exchange(context.Background(), app, "https://app.example/cb", "code2", nil); !errors.Is(err, ErrRejected) {
		t.Fatalf("an account that can't upload: %v", err)
	}
	if _, err := db.Refresh(context.Background(), app, Credentials{"access_token": "revoked"}); !errors.Is(err, ErrReauth) {
		t.Fatalf("revoked: %v", err)
	}
	if ok, err := (Dribbble{}).CheckApp(context.Background(), app, "https://app.example/cb"); !ok || err != nil {
		t.Fatalf("good keys: %v %v", ok, err)
	}
	if _, err := (Dribbble{}).CheckApp(context.Background(), App{ClientID: "cid", ClientSecret: "wrong"}, ""); err == nil {
		t.Fatal("bad keys passed")
	}
}

func TestDribbblePublish(t *testing.T) {
	ctx := context.Background()
	db, _ := Get("dribbble")
	c := Credentials{"access_token": "dt-1", "username": "janedoe"}
	s := Settings{"title": "Long run"}

	t.Run("shot", func(t *testing.T) {
		f := useFakeDribbble(t)
		res, err := db.Publish(ctx, c, PostContent{Text: "Poster for the club", Tags: []string{"#poster", "running"}, Media: []MediaFile{dribbbleShot(800, 600)}}, s)
		if err != nil {
			t.Fatal(err)
		}
		if f.shot["title"][0] != "Long run" || f.shot["description"][0] != "Poster for the club" || strings.Join(f.shot["tags[]"], ",") != "poster,running" || f.shot["image"][0] != "shot.png" {
			t.Fatalf("shot %v", f.shot)
		}
		if res.ExternalID != "471756" || res.URL != "https://dribbble.com/shots/471756" {
			t.Fatalf("result %+v", res)
		}
	})

	t.Run("wrong size never leaves", func(t *testing.T) {
		f := useFakeDribbble(t)
		if _, err := db.Publish(ctx, c, PostContent{Media: []MediaFile{dribbbleShot(1000, 750)}}, s); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "1000x750") || f.shots != 0 {
			t.Fatalf("wrong size: %v", err)
		}
	})

	t.Run("refused, rate limited, reauth", func(t *testing.T) {
		f := useFakeDribbble(t)
		f.shotCode, f.shotBody = http.StatusUnprocessableEntity, `{"message":"Validation failed.","errors":[{"attribute":"title","message":"has already been taken"}]}`
		if _, err := db.Publish(ctx, c, PostContent{Media: []MediaFile{dribbbleShot(400, 300)}}, s); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "title has already been taken") {
			t.Fatalf("refused: %v", err)
		}
		f.shotCode, f.shotBody = http.StatusTooManyRequests, `{"message":"API rate limit exceeded."}`
		_, err := db.Publish(ctx, c, PostContent{Media: []MediaFile{dribbbleShot(400, 300)}}, s)
		if err == nil || errors.Is(err, ErrRejected) || errors.Is(err, ErrReauth) {
			t.Fatalf("rate limit is retried: %v", err)
		}
		if _, err := db.Publish(ctx, Credentials{"access_token": "revoked"}, PostContent{Media: []MediaFile{dribbbleShot(400, 300)}}, s); !errors.Is(err, ErrReauth) {
			t.Fatalf("revoked: %v", err)
		}
	})

	t.Run("lost answer", func(t *testing.T) {
		f := useFakeDribbble(t)
		f.dropShot = true
		if _, err := db.Publish(ctx, c, PostContent{Media: []MediaFile{dribbbleShot(400, 300)}}, s); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "may have published") {
			t.Fatalf("lost answer: %v", err)
		}
		f.listed = true
		if res, err := db.Publish(ctx, c, PostContent{Media: []MediaFile{dribbbleShot(400, 300)}}, s); err != nil || res.ExternalID != "471756" {
			t.Fatalf("found after a lost answer: %+v %v", res, err)
		}
	})
}

func TestDribbbleValidate(t *testing.T) {
	db, _ := Get("dribbble")
	pic := []MediaFile{{MimeType: "image/png"}}
	for name, tc := range map[string]struct {
		p     PostContent
		s     Settings
		field string
	}{
		"no picture": {PostContent{Text: "x"}, Settings{"title": "t"}, "media"},
		"no title":   {PostContent{Media: pic}, Settings{}, "title"},
		"webp":       {PostContent{Media: []MediaFile{{MimeType: "image/webp"}}}, Settings{"title": "t"}, "media"},
		"too big":    {PostContent{Media: []MediaFile{{MimeType: "image/png", Size: 9 << 20}}}, Settings{"title": "t"}, "media"},
		"13 tags":    {PostContent{Media: pic, Tags: strings.Split("a,b,c,d,e,f,g,h,i,j,k,l,m", ",")}, Settings{"title": "t"}, "tags"},
	} {
		var ve *ValidationError
		if err := db.Validate(tc.p, tc.s); !errors.As(err, &ve) || ve.Field != tc.field {
			t.Errorf("%s: %v, want a %s error", name, err, tc.field)
		}
	}
	if err := db.Validate(PostContent{Media: pic}, Settings{"title": "t"}); err != nil {
		t.Fatal(err)
	}
	if l := db.Limits(); l.Image == nil || l.Image.MaxBytes != 8<<20 {
		t.Fatalf("image rules %+v", l.Image)
	}
}
