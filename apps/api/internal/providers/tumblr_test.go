package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeTumblr answers like api.tumblr.com for the calls the provider makes.
type fakeTumblr struct {
	mu        sync.Mutex
	srv       *httptest.Server
	tokenForm url.Values
	post      map[string]any    // the post's JSON (body or "json" part)
	parts     map[string]string // multipart file parts: name -> content type
	posts     int
	postCode  int
	postBody  string
	dropPost  bool
	listed    bool
	noBlogs   bool
}

func (f *fakeTumblr) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/v2/oauth2/token":
		_ = r.ParseForm()
		f.tokenForm = r.PostForm
		switch {
		case r.PostForm.Get("client_secret") != "secret":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_client","error_description":"Client authentication failed","message":"Client authentication failed"}`))
		case r.PostForm.Get("code") == checkCode, r.PostForm.Get("refresh_token") == "used":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"The refresh token is invalid."}`))
		default:
			_, _ = w.Write([]byte(`{"access_token":"tat-` + r.PostForm.Get("grant_type") + `","expires_in":2520,"id_token":false,"refresh_token":"trt-2","scope":"basic write offline_access","token_type":"bearer"}`))
		}
	case r.Header.Get("Authorization") == "Bearer revoked":
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"meta":{"status":401,"msg":"Unauthorized"},"response":[],"errors":[{"title":"Unauthorized","code":1016,"detail":"Unable to authorize"}]}`))
	case r.URL.Path == "/v2/user/info":
		blogs := `[{"name":"janedoe","title":"Jane runs","url":"https://janedoe.tumblr.com/","uuid":"t:abc","primary":true,"avatar":[{"width":64,"url":"https://64.media.example/a64.png"},{"width":512,"url":"https://64.media.example/a512.png"}]},{"name":"janedoe-art","title":"","url":"https://janedoe-art.tumblr.com/","uuid":"t:def","primary":false}]`
		if f.noBlogs {
			blogs = `[]`
		}
		_, _ = w.Write([]byte(`{"meta":{"status":200,"msg":"OK"},"response":{"user":{"name":"janedoe","blogs":` + blogs + `}}}`))
	case r.Method == http.MethodGet && r.URL.Path == "/v2/blog/t:abc/posts":
		posts := `{"id_string":"100","timestamp":1500000000,"content":[{"type":"text","text":"Splits"}]}`
		if f.listed {
			posts = `{"id_string":"7001","timestamp":` + strconv.FormatInt(time.Now().Unix(), 10) + `,"content":[{"type":"text","text":"Splits"}]},` + posts
		}
		_, _ = w.Write([]byte(`{"meta":{"status":200,"msg":"OK"},"response":{"posts":[` + posts + `]}}`))
	case r.Method == http.MethodPost && r.URL.Path == "/v2/blog/t:abc/posts":
		f.posts++
		f.post, f.parts = nil, map[string]string{}
		mt, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if mt == "multipart/form-data" {
			mr := multipart.NewReader(r.Body, params["boundary"])
			for {
				p, err := mr.NextPart()
				if err != nil {
					break
				}
				b, _ := io.ReadAll(p)
				if p.FormName() == "json" {
					_ = json.Unmarshal(b, &f.post)
					continue
				}
				f.parts[p.FormName()] = p.Header.Get("Content-Type") + ":" + strconv.Itoa(len(b))
			}
		} else {
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &f.post)
		}
		switch {
		case f.dropPost:
			hj, _ := w.(http.Hijacker)
			c, _, _ := hj.Hijack()
			_ = c.Close()
		case f.postCode != 0:
			w.WriteHeader(f.postCode)
			_, _ = w.Write([]byte(f.postBody))
		default:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"meta":{"msg":"Created","status":201},"response":{"id":"7000"}}`))
		}
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func useFakeTumblr(t *testing.T) *fakeTumblr {
	t.Helper()
	f := &fakeTumblr{}
	f.srv = httptest.NewServer(f)
	api, auth := tumblrAPI, tumblrAuthorize
	tumblrAPI, tumblrAuthorize = f.srv.URL, f.srv.URL+"/oauth2/authorize"
	t.Cleanup(func() {
		f.srv.Close()
		tumblrAPI, tumblrAuthorize = api, auth
	})
	return f
}

func tumblrCreds() Credentials {
	return Credentials{"access_token": "tat", "refresh_token": "trt-1", "user": "janedoe", "blog": "t:abc", "blog_name": "janedoe"}
}

func tumblrPicture(w, h int) MediaFile {
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewGray(image.Rect(0, 0, w, h)))
	data := b.Bytes()
	return MediaFile{Name: "a.png", MimeType: "image/png", Size: int64(len(data)),
		Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }}
}

func TestTumblrConnect(t *testing.T) {
	f := useFakeTumblr(t)
	tb, _ := Get("tumblr")
	if tb.Status != StatusAvailable {
		t.Fatalf("tumblr is %q", tb.Status)
	}
	app := App{ClientID: "ckey", ClientSecret: "secret"}
	u, _ := tb.AuthURL(app, "https://app.example/cb", "st", nil)
	if q, _ := url.Parse(u); q.Query().Get("scope") != tumblrScopes || q.Query().Get("redirect_uri") != "https://app.example/cb" {
		t.Fatalf("auth url %s", u)
	}
	ctx := context.Background()
	creds, profile, err := tb.Exchange(ctx, app, "https://app.example/cb", "code1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.tokenForm.Get("client_id") != "ckey" || f.tokenForm.Get("code") != "code1" || creds["refresh_token"] != "trt-2" || creds["expires_at"] == "" {
		t.Fatalf("token %v %v", f.tokenForm, creds)
	}
	if profile.Handle != "janedoe" || profile.AvatarURL != "https://64.media.example/a512.png" {
		t.Fatalf("person %+v", profile)
	}

	// The person picks blogs; each is its own channel.
	al := tb.Provider.(AccountLister)
	accounts, err := al.Accounts(ctx, creds)
	if err != nil || len(accounts) != 2 || accounts[1].ID != "t:def" || accounts[1].Name != "janedoe-art" || accounts[0].Name != "Jane runs" {
		t.Fatalf("accounts %+v %v", accounts, err)
	}
	ch, p, err := al.UseAccount(ctx, creds, "t:def")
	if err != nil || ch["blog"] != "t:def" || ch["blog_name"] != "janedoe-art" || ch["refresh_token"] != "trt-2" || p.Handle != "janedoe-art" || p.ProfileURL != "https://janedoe-art.tumblr.com/" {
		t.Fatalf("use account %v %+v %v", ch, p, err)
	}
	if _, _, err := al.UseAccount(ctx, creds, "t:gone"); !errors.Is(err, ErrRejected) {
		t.Fatalf("gone blog: %v", err)
	}
	f.noBlogs = true
	if _, err := al.Accounts(ctx, creds); !errors.Is(err, ErrRejected) {
		t.Fatalf("no blogs: %v", err)
	}

	// Refresh stores the new refresh token; a used one needs reconnecting.
	c, err := tb.Refresh(ctx, app, tumblrCreds())
	if err != nil || c["access_token"] != "tat-refresh_token" || c["refresh_token"] != "trt-2" || c["blog"] != "t:abc" {
		t.Fatalf("refresh %v %v", c, err)
	}
	used := tumblrCreds()
	used["refresh_token"] = "used"
	if _, err := tb.Refresh(ctx, app, used); !errors.Is(err, ErrReauth) {
		t.Fatalf("used refresh token: %v", err)
	}
	if ok, err := (Tumblr{}).CheckApp(ctx, app, "https://app.example/cb"); !ok || err != nil {
		t.Fatalf("good keys: %v %v", ok, err)
	}
	if _, err := (Tumblr{}).CheckApp(ctx, App{ClientID: "ckey", ClientSecret: "wrong"}, ""); err == nil {
		t.Fatal("bad keys passed")
	}
}

func TestTumblrPublish(t *testing.T) {
	ctx := context.Background()
	tb, _ := Get("tumblr")

	t.Run("text with tags", func(t *testing.T) {
		f := useFakeTumblr(t)
		res, err := tb.Publish(ctx, tumblrCreds(), PostContent{Text: "Splits\n\nNegative, all of them.", Tags: []string{"#running", "marathon"}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		content := f.post["content"].([]any)
		if len(content) != 2 || content[1].(map[string]any)["text"] != "Negative, all of them." || f.post["tags"] != "running,marathon" || f.post["state"] != "published" {
			t.Fatalf("post %v", f.post)
		}
		if res.ExternalID != "7000" || res.URL != "https://www.tumblr.com/janedoe/7000" {
			t.Fatalf("result %+v", res)
		}
	})

	t.Run("pictures and a video in one request", func(t *testing.T) {
		f := useFakeTumblr(t)
		video := MediaFile{Name: "v.mp4", MimeType: "video/mp4", Size: 7,
			Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("MP4DATA")), nil }}
		if _, err := tb.Publish(ctx, tumblrCreds(), PostContent{Text: "Race day", Media: []MediaFile{tumblrPicture(640, 480), video}}, nil); err != nil {
			t.Fatal(err)
		}
		content := f.post["content"].([]any)
		img := content[1].(map[string]any)["media"].([]any)[0].(map[string]any)
		vid := content[2].(map[string]any)
		if img["identifier"] != "media-0" || img["width"] != 640.0 || img["height"] != 480.0 || vid["type"] != "video" || vid["media"].(map[string]any)["identifier"] != "media-1" {
			t.Fatalf("content %v", content)
		}
		if !strings.HasPrefix(f.parts["media-0"], "image/png:") || f.parts["media-1"] != "video/mp4:7" {
			t.Fatalf("parts %v", f.parts)
		}
	})

	t.Run("refused, limited, unavailable, reauth", func(t *testing.T) {
		f := useFakeTumblr(t)
		f.postCode, f.postBody = http.StatusForbidden, `{"meta":{"status":403,"msg":"Forbidden"},"response":[],"errors":[{"title":"Forbidden","code":8023,"detail":"daily posting limit"}]}`
		if _, err := tb.Publish(ctx, tumblrCreds(), PostContent{Text: "x"}, nil); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "daily post limit") {
			t.Fatalf("refused: %v", err)
		}
		f.postCode, f.postBody = http.StatusServiceUnavailable, `{"meta":{"status":503,"msg":"Service Unavailable"},"response":[]}`
		_, err := tb.Publish(ctx, tumblrCreds(), PostContent{Text: "x"}, nil)
		if err == nil || errors.Is(err, ErrRejected) || errors.Is(err, ErrReauth) {
			t.Fatalf("outage is retried: %v", err)
		}
		c := tumblrCreds()
		c["access_token"] = "revoked"
		if _, err := tb.Publish(ctx, c, PostContent{Text: "x"}, nil); !errors.Is(err, ErrReauth) {
			t.Fatalf("revoked: %v", err)
		}
	})

	t.Run("lost answer", func(t *testing.T) {
		f := useFakeTumblr(t)
		f.dropPost = true
		if _, err := tb.Publish(ctx, tumblrCreds(), PostContent{Text: "Splits"}, nil); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "may have published") {
			t.Fatalf("lost answer: %v", err)
		}
		f.listed = true
		if res, err := tb.Publish(ctx, tumblrCreds(), PostContent{Text: "Splits"}, nil); err != nil || res.ExternalID != "7001" {
			t.Fatalf("found after a lost answer: %+v %v", res, err)
		}
	})
}

func TestTumblrValidate(t *testing.T) {
	tb, _ := Get("tumblr")
	two := []MediaFile{{MimeType: "video/mp4"}, {MimeType: "video/mp4"}}
	var ve *ValidationError
	if err := tb.Validate(PostContent{Media: two}, nil); !errors.As(err, &ve) || ve.Field != "media" {
		t.Fatalf("two videos: %v", err)
	}
	if err := tb.Validate(PostContent{Media: []MediaFile{{MimeType: "application/pdf"}}}, nil); !errors.As(err, &ve) {
		t.Fatalf("a pdf: %v", err)
	}
	if err := tb.Validate(PostContent{Text: "x", Media: []MediaFile{{MimeType: "image/png"}, {MimeType: "video/mp4"}}}, nil); err != nil {
		t.Fatal(err)
	}
	if got := tumblrText(strings.Repeat("a", tumblrBlockMax+5)); len(got) != 2 {
		t.Fatalf("long paragraph in %d blocks", len(got))
	}
}
