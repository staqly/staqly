package providers

import (
	"context"
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

// fakeMeWe answers like mewe.com/api/dev for the calls the provider makes.
type fakeMeWe struct {
	mu       sync.Mutex
	srv      *httptest.Server
	tokenHdr http.Header
	appID    string
	pending  bool
	posts    map[string]map[string]any // path -> body
	uploads  []string                  // "filename:content-type:body"
	postCode int
	postBody string
	drop     bool
}

func (f *fakeMeWe) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/api/dev/token" {
		f.tokenHdr = r.Header.Clone()
		if r.URL.Query().Get("loginRequestToken") != "lrt-1" || r.Header.Get("X-Api-Key") != "apikey" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"id":400,"message":"Invalid loginRequestToken"}`))
			return
		}
		if f.pending {
			_, _ = w.Write([]byte(`{"pending":true}`))
			return
		}
		_, _ = w.Write([]byte(`{"apiToken":"tok","expiresAt":"` + time.Now().Add(30*24*time.Hour).UTC().Format(time.RFC3339) + `"}`))
		return
	}
	f.appID = r.Header.Get("X-App-Id")
	if r.Header.Get("Authorization") == "Bearer revoked" {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"id":401,"message":"Unauthorized"}`))
		return
	}
	switch {
	case r.URL.Path == "/api/dev/me":
		_, _ = w.Write([]byte(`{"userId":"u1","name":"Jane Doe","firstName":"Jane","lastName":"Doe","handle":"janedoe","profilePhoto":{"href":"/api/v2/photo/profile/150x150/u1.jpg","small":""},"timezone":"UTC","locale":"en"}`))
	case r.URL.Path == "/api/dev/groups" && r.URL.Query().Get("nextId") == "":
		_, _ = w.Write([]byte(`{"groups":[{"groupId":"g1","name":"Runners","ownerId":"u2"}],"nextPage":"` + f.srv.URL + `/api/dev/groups?nextId=n2"}`))
	case r.URL.Path == "/api/dev/groups":
		_, _ = w.Write([]byte(`{"groups":[{"groupId":"g2","name":"Bakers","ownerId":"u3"}]}`))
	case r.URL.Path == "/api/dev/group/g2":
		_, _ = w.Write([]byte(`{"groupId":"g2","name":"Bakers","createdAt":1,"membersCount":3}`))
	case r.URL.Path == "/api/dev/photo/upload":
		file, hdr, err := r.FormFile("file")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		body, _ := io.ReadAll(file)
		f.uploads = append(f.uploads, hdr.Filename+":"+hdr.Header.Get("Content-Type")+":"+string(body))
		_, _ = w.Write([]byte(`{"id":"ph` + string(rune('0'+len(f.uploads))) + `","name":"x","type":"image","size":4}`))
	case strings.HasSuffix(r.URL.Path, "/post"):
		if f.drop {
			hj, _ := w.(http.Hijacker)
			c, _, _ := hj.Hijack()
			_ = c.Close()
			return
		}
		body := map[string]any{}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		f.posts[r.URL.Path] = body
		if f.postCode != 0 {
			w.WriteHeader(f.postCode)
			_, _ = w.Write([]byte(f.postBody))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"id":404,"message":"Not found"}`))
	}
}

func useFakeMeWe(t *testing.T) *fakeMeWe {
	t.Helper()
	f := &fakeMeWe{posts: map[string]map[string]any{}}
	f.srv = httptest.NewServer(f)
	host := meweHost
	meweHost = f.srv.URL
	t.Cleanup(func() {
		f.srv.Close()
		meweHost = host
	})
	return f
}

func meweCreds() Credentials {
	return Credentials{"access_token": "tok", "app_id": "app1", "user_id": "u1", "handle": "janedoe",
		"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}
}

func TestMeWeConnect(t *testing.T) {
	f := useFakeMeWe(t)
	mw, _ := Get("mewe")
	if mw.Status != StatusAvailable || mw.Auth() != AuthOAuth2 {
		t.Fatalf("mewe is %q/%q", mw.Status, mw.Auth())
	}
	app := App{ClientID: "app1", ClientSecret: "apikey"}
	u, err := mw.AuthURL(app, "https://app.example/cb", "st", nil)
	if err != nil {
		t.Fatal(err)
	}
	q, _ := url.Parse(u)
	if q.Path != "/login" || q.Query().Get("client_id") != "app1" || q.Query().Get("redirect_uri") != "https://app.example/cb" || q.Query().Get("state") != "st" {
		t.Fatalf("auth url %s", u)
	}
	ctx := context.Background()
	// MeWe sends the sign-in back as loginRequestToken.
	creds, profile, err := mw.Exchange(ctx, app, "https://app.example/cb", "", map[string]string{"loginRequestToken": "lrt-1"})
	if err != nil {
		t.Fatal(err)
	}
	if f.tokenHdr.Get("X-App-Id") != "app1" || creds["access_token"] != "tok" || creds["app_id"] != "app1" || creds["expires_at"] == "" || creds["handle"] != "janedoe" {
		t.Fatalf("creds %v", creds)
	}
	if profile.Handle != "@janedoe" || profile.Name != "Jane Doe" || profile.AvatarURL != f.srv.URL+"/api/v2/photo/profile/150x150/u1.jpg" || profile.ProfileURL != "https://mewe.com/i/janedoe" {
		t.Fatalf("profile %+v", profile)
	}
	// Or as the code, once the callback maps it.
	if _, _, err := mw.Exchange(ctx, app, "https://app.example/cb", "lrt-1", nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := mw.Exchange(ctx, app, "https://app.example/cb", "other", nil); !errors.Is(err, ErrRejected) {
		t.Fatalf("bad sign-in: %v", err)
	}
	f.pending = true
	if _, _, err := mw.Exchange(ctx, app, "https://app.example/cb", "lrt-1", nil); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "allow") {
		t.Fatalf("pending: %v", err)
	}
}

func TestMeWeAccounts(t *testing.T) {
	useFakeMeWe(t)
	mw, _ := Get("mewe")
	lister := mw.Provider.(AccountLister)
	ctx := context.Background()
	accounts, err := lister.Accounts(ctx, meweCreds())
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 3 || accounts[0].ID != meweTimeline || accounts[1].ID != "g1" || accounts[2].ID != "g2" || accounts[2].Name != "Bakers" {
		t.Fatalf("accounts %+v", accounts)
	}
	creds, profile, err := lister.UseAccount(ctx, meweCreds(), "g2")
	if err != nil {
		t.Fatal(err)
	}
	if creds["group_id"] != "g2" || creds["access_token"] != "tok" || profile.Handle != "group/g2" || profile.Name != "Bakers" || profile.ProfileURL != "https://mewe.com/group/g2" {
		t.Fatalf("group %v %+v", creds, profile)
	}
	creds, profile, err = lister.UseAccount(ctx, creds, meweTimeline)
	if err != nil || creds["group_id"] != "" || profile.Handle != "@janedoe" {
		t.Fatalf("timeline %v %+v %v", creds, profile, err)
	}
	if _, _, err := lister.UseAccount(ctx, meweCreds(), "gone"); !errors.Is(err, ErrRejected) {
		t.Fatalf("unknown group: %v", err)
	}
}

func TestMeWeRefresh(t *testing.T) {
	useFakeMeWe(t)
	mw, _ := Get("mewe")
	ctx := context.Background()
	if c, err := mw.Refresh(ctx, App{}, meweCreds()); err != nil || c["access_token"] != "tok" {
		t.Fatalf("valid: %v %v", c, err)
	}
	c := meweCreds()
	c["access_token"] = "revoked"
	if _, err := mw.Refresh(ctx, App{}, c); !errors.Is(err, ErrReauth) {
		t.Fatalf("revoked: %v", err)
	}
	c = meweCreds()
	c["expires_at"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	if _, err := mw.Refresh(ctx, App{}, c); !errors.Is(err, ErrReauth) {
		t.Fatalf("expired: %v", err)
	}
}

func meweFile(name, mime, body string) MediaFile {
	return MediaFile{Name: name, MimeType: mime, Size: int64(len(body)), Open: func(context.Context) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(body)), nil
	}}
}

func TestMeWePublish(t *testing.T) {
	f := useFakeMeWe(t)
	mw, _ := Get("mewe")
	ctx := context.Background()
	res, err := mw.Publish(ctx, meweCreds(), PostContent{Text: "Hello MeWe"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.URL != "https://mewe.com/i/janedoe" || f.posts["/api/dev/me/post"]["text"] != "Hello MeWe" || f.appID != "app1" {
		t.Fatalf("timeline post %+v %v", res, f.posts)
	}
	c := meweCreds()
	c["group_id"] = "g1"
	res, err = mw.Publish(ctx, c, PostContent{Text: "pics", Media: []MediaFile{meweFile("a.jpg", "image/jpeg", "jpg!"), meweFile("b.png", "image/png", "png!")}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.URL != "https://mewe.com/group/g1" || f.posts["/api/dev/group/g1/post"]["uploadedPhotoIds"] != "ph1,ph2" {
		t.Fatalf("group post %+v %v", res, f.posts)
	}
	if len(f.uploads) != 2 || f.uploads[0] != "a.jpg:image/jpeg:jpg!" {
		t.Fatalf("uploads %v", f.uploads)
	}

	for _, tc := range []struct {
		code int
		body string
		kind error
	}{
		{http.StatusForbidden, `{"id":403,"message":"Forbidden","requiredPermissions":["post_content"]}`, ErrRejected},
		{http.StatusBadRequest, `{"id":400,"message":"Bad request"}`, ErrRejected},
		{420, `{"id":420,"message":"Enhance Your Calm"}`, nil},
		{http.StatusServiceUnavailable, ``, nil},
		{http.StatusUnauthorized, `{"id":401,"message":"Unauthorized"}`, ErrReauth},
	} {
		f.postCode, f.postBody = tc.code, tc.body
		_, err := mw.Publish(ctx, meweCreds(), PostContent{Text: "x"}, nil)
		if err == nil || (tc.kind != nil && !errors.Is(err, tc.kind)) || (tc.kind == nil && (errors.Is(err, ErrRejected) || errors.Is(err, ErrReauth))) {
			t.Errorf("%d: %v", tc.code, err)
		}
	}
	f.postCode = 0
	if _, err := mw.Publish(ctx, meweCreds(), PostContent{Text: "x"}, nil); err != nil {
		t.Fatal(err)
	}
	f.drop = true
	if _, err := mw.Publish(ctx, meweCreds(), PostContent{Text: "x"}, nil); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "may have published") {
		t.Fatalf("lost answer: %v", err)
	}
}

func TestMeWeValidate(t *testing.T) {
	mw, _ := Get("mewe")
	if err := mw.Validate(PostContent{Text: "hi", Media: []MediaFile{{Name: "a.jpg", MimeType: "image/jpeg"}}}, nil); err != nil {
		t.Fatal(err)
	}
	var ve *ValidationError
	if err := mw.Validate(PostContent{Text: "hi", Media: []MediaFile{{Name: "a.mp4", MimeType: "video/mp4"}}}, nil); !errors.As(err, &ve) || ve.Field != "media" {
		t.Fatalf("video: %v", err)
	}
	if err := mw.Validate(PostContent{Text: strings.Repeat("a", 5001)}, nil); !errors.As(err, &ve) || ve.Field != "content" {
		t.Fatalf("too long: %v", err)
	}
}
