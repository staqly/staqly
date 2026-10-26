package providers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeBluesky answers like a PDS for the XRPC calls the provider makes.
type fakeBluesky struct {
	mu         sync.Mutex
	scope      string                    // the session token's scope (default an app password's)
	records    map[string]map[string]any // rkey -> record
	creates    int
	blobs      int
	createCode int // answer to createRecord (0 = 200)
	createBody string
	drop       bool // store the record, then close the connection on createRecord
	srv        *httptest.Server
}

func blueskyTestJWT(scope string) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"ES256K"}`)) + "." + enc([]byte(`{"scope":"`+scope+`","sub":"did:plc:jane"}`)) + ".sig"
}

func (f *fakeBluesky) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	nsid := strings.TrimPrefix(r.URL.Path, "/xrpc/")
	if nsid != "com.atproto.server.createSession" && r.Header.Get("Authorization") != "Bearer "+blueskyTestJWT(f.scopeOr()) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"AuthenticationRequired","message":"Authentication Required"}`))
		return
	}
	raw, _ := io.ReadAll(r.Body)
	switch nsid {
	case "com.atproto.server.createSession":
		var in map[string]string
		_ = json.Unmarshal(raw, &in)
		if in["password"] != "app-pass" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"AuthenticationRequired","message":"Invalid identifier or password"}`))
			return
		}
		_, _ = w.Write([]byte(`{"accessJwt":"` + blueskyTestJWT(f.scopeOr()) + `","refreshJwt":"r","did":"did:plc:jane","handle":"jane.example.com"}`))
	case "app.bsky.actor.getProfile":
		_, _ = w.Write([]byte(`{"did":"did:plc:jane","handle":"jane.example.com","displayName":"Jane Doe","avatar":"https://example.com/a.jpg"}`))
	case "com.atproto.identity.resolveHandle":
		if r.URL.Query().Get("handle") != "ben.example.com" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"InvalidRequest","message":"Unable to resolve handle"}`))
			return
		}
		_, _ = w.Write([]byte(`{"did":"did:plc:ben"}`))
	case "com.atproto.repo.getRecord":
		rkey := r.URL.Query().Get("rkey")
		if _, ok := f.records[rkey]; !ok {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"RecordNotFound","message":"Could not locate record"}`))
			return
		}
		_, _ = w.Write([]byte(`{"uri":"at://did:plc:jane/app.bsky.feed.post/` + rkey + `","cid":"cid-` + rkey + `","value":{}}`))
	case "com.atproto.repo.uploadBlob":
		f.blobs++
		_, _ = w.Write([]byte(`{"blob":{"$type":"blob","ref":{"$link":"bafy"},"mimeType":"` + r.Header.Get("Content-Type") + `","size":` + jsonInt(len(raw)) + `}}`))
	case "com.atproto.repo.createRecord":
		f.creates++
		if f.createCode != 0 {
			w.WriteHeader(f.createCode)
			_, _ = w.Write([]byte(f.createBody))
			return
		}
		var in struct {
			Rkey   string         `json:"rkey"`
			Record map[string]any `json:"record"`
		}
		_ = json.Unmarshal(raw, &in)
		f.records[in.Rkey] = in.Record
		if f.drop {
			f.drop = false
			hj, _ := w.(http.Hijacker)
			c, _, _ := hj.Hijack()
			_ = c.Close()
			return
		}
		_, _ = w.Write([]byte(`{"uri":"at://did:plc:jane/app.bsky.feed.post/` + in.Rkey + `","cid":"cid-` + in.Rkey + `"}`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func jsonInt(n int) string { b, _ := json.Marshal(n); return string(b) }

func (f *fakeBluesky) scopeOr() string {
	if f.scope == "" {
		return "com.atproto.appPass"
	}
	return f.scope
}

func useFakeBluesky(t *testing.T) (*fakeBluesky, Credentials) {
	t.Helper()
	f := &fakeBluesky{records: map[string]map[string]any{}}
	f.srv = httptest.NewServer(f)
	t.Cleanup(f.srv.Close)
	return f, Credentials{"identifier": "jane.example.com", "app_password": "app-pass", "service": f.srv.URL}
}

func blueskyPNG(t *testing.T, w, h int) MediaFile {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	return MediaFile{Name: "a.png", MimeType: "image/png", Size: int64(len(b)), Open: func(context.Context) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(b)), nil
	}}
}

func TestBlueskyConnect(t *testing.T) {
	f, creds := useFakeBluesky(t)
	e, _ := Get("bluesky_app_password")
	if e.Status != StatusAvailable {
		t.Fatalf("bluesky is %q", e.Status)
	}
	got, prof, err := e.Connect(context.Background(), map[string]string{"identifier": "@jane.example.com", "app_password": "app-pass", "service": f.srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if got["did"] != "did:plc:jane" || got["app_password"] != "app-pass" || prof.Handle != "@jane.example.com" || prof.Name != "Jane Doe" {
		t.Fatalf("creds %v profile %+v", got, prof)
	}
	if _, _, err := e.Connect(context.Background(), map[string]string(got)); err != nil {
		t.Fatalf("checking again: %v", err)
	}
	creds["app_password"] = "wrong"
	if _, _, err := e.Connect(context.Background(), map[string]string(creds)); !errors.Is(err, ErrReauth) {
		t.Fatalf("a wrong password: %v", err)
	}
	// The account password (a full-access session) is refused.
	f.scope = "com.atproto.access"
	creds["app_password"] = "app-pass"
	if _, _, err := e.Connect(context.Background(), map[string]string(creds)); err == nil || !strings.Contains(err.Error(), "app password") {
		t.Fatalf("the account password: %v", err)
	}
}

func TestBlueskyPublish(t *testing.T) {
	f, creds := useFakeBluesky(t)
	e, _ := Get("bluesky_app_password")
	text := "Hi @ben.example.com and @nobody.example.com see example.com/x #golang and https://example.com/#frag café"
	res, err := e.Publish(context.Background(), creds, PostContent{PostID: "p1", ChannelID: "c1", Text: text, Media: []MediaFile{blueskyPNG(t, 40, 20)}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.creates != 1 || f.blobs != 1 || len(f.records) != 1 {
		t.Fatalf("creates %d blobs %d", f.creates, f.blobs)
	}
	var rec map[string]any
	var rkey string
	for k, v := range f.records {
		rkey, rec = k, v
	}
	if len(rkey) != 13 || res.URL != blueskyApp+"/profile/jane.example.com/post/"+rkey || res.ExternalID != "at://did:plc:jane/app.bsky.feed.post/"+rkey+"|cid-"+rkey {
		t.Fatalf("result %+v rkey %q", res, rkey)
	}
	facets := rec["facets"].([]any)
	kinds := map[string]string{}
	for _, fc := range facets {
		m := fc.(map[string]any)
		idx := m["index"].(map[string]any)
		feat := m["features"].([]any)[0].(map[string]any)
		kinds[text[int(idx["byteStart"].(float64)):int(idx["byteEnd"].(float64))]] = feat["$type"].(string)
	}
	want := map[string]string{
		"@ben.example.com":          "app.bsky.richtext.facet#mention",
		"example.com/x":             "app.bsky.richtext.facet#link",
		"#golang":                   "app.bsky.richtext.facet#tag",
		"https://example.com/#frag": "app.bsky.richtext.facet#link",
	}
	if len(kinds) != len(want) {
		t.Fatalf("facets %v", kinds)
	}
	for k, v := range want {
		if kinds[k] != v {
			t.Fatalf("facets %v, want %s as %s", kinds, k, v)
		}
	}
	img := rec["embed"].(map[string]any)["images"].([]any)[0].(map[string]any)
	if ar := img["aspectRatio"].(map[string]any); ar["width"] != 40.0 || ar["height"] != 20.0 {
		t.Fatalf("image %v", img)
	}

	// A reply names the post as root and parent; the next one keeps the root.
	r1, err := Bluesky{}.Reply(context.Background(), creds, res.ExternalID, PostContent{PostID: "p1", ChannelID: "c1", Text: "two"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := Bluesky{}.Reply(context.Background(), creds, r1.ExternalID, PostContent{PostID: "p1", ChannelID: "c1", Text: "three"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	parent, root, ok := blueskyParent(r2.ExternalID)
	if !ok || root.URI != strings.Split(res.ExternalID, "|")[0] || parent.URI == root.URI {
		t.Fatalf("reply ids %q %q", r1.ExternalID, r2.ExternalID)
	}
	for _, rec := range f.records {
		if rec["text"] == "three" {
			reply := rec["reply"].(map[string]any)
			if reply["root"].(map[string]any)["uri"] != root.URI || reply["parent"].(map[string]any)["uri"] != strings.Split(r1.ExternalID, "|")[0] {
				t.Fatalf("reply refs %v", reply)
			}
		}
	}
}

func TestBlueskyRetryFindsThePost(t *testing.T) {
	f, creds := useFakeBluesky(t)
	e, _ := Get("bluesky_app_password")
	post := PostContent{PostID: "p1", ChannelID: "c1", Text: "Hello"}
	f.drop = true
	_, err := e.Publish(context.Background(), creds, post, nil)
	if err == nil || errors.Is(err, ErrRejected) || errors.Is(err, ErrReauth) {
		t.Fatalf("a lost answer should be retried: %v", err)
	}
	res, err := e.Publish(context.Background(), creds, post, nil)
	if err != nil || f.creates != 1 || res.ExternalID == "" {
		t.Fatalf("the retry posted again: %v creates %d", err, f.creates)
	}
	// Another post or channel gets another record key.
	if blueskyRecordKey(fediKey("bluesky", "p1", "c1", "")) == blueskyRecordKey(fediKey("bluesky", "p1", "c2", "")) {
		t.Fatal("same record key for two channels")
	}
}

func TestBlueskyErrors(t *testing.T) {
	f, creds := useFakeBluesky(t)
	e, _ := Get("bluesky_app_password")
	post := PostContent{PostID: "p1", ChannelID: "c1", Text: "Hello"}
	for _, c := range []struct {
		code int
		body string
		kind error
	}{
		{400, `{"error":"InvalidRequest","message":"Invalid app.bsky.feed.post record: Record/text must not be longer than 300 graphemes"}`, ErrRejected},
		{429, `{"error":"RateLimitExceeded","message":"Rate Limit Exceeded"}`, nil},
		{502, ``, nil},
		{401, `{"error":"ExpiredToken","message":"Token has expired"}`, ErrReauth},
	} {
		f.createCode, f.createBody = c.code, c.body
		_, err := e.Publish(context.Background(), creds, post, nil)
		var fail *Failure
		if !errors.As(err, &fail) || fail.Kind != c.kind {
			t.Fatalf("%d: %v", c.code, err)
		}
	}
}

func TestBlueskyValidate(t *testing.T) {
	e, _ := Get("bluesky_app_password")
	img := MediaFile{Name: "a.jpg", MimeType: "image/jpeg", Size: 500_000}
	for _, c := range []struct {
		p  PostContent
		ok bool
	}{
		{PostContent{Text: "hi"}, true},
		{PostContent{Text: strings.Repeat("a", 301)}, false},
		{PostContent{Text: "hi", Media: []MediaFile{img, img, img, img}}, true},
		{PostContent{Text: "hi", Media: []MediaFile{img, img, img, img, img}}, false},
		{PostContent{Text: "hi", Media: []MediaFile{{Name: "big.jpg", MimeType: "image/jpeg", Size: 2_000_000}}}, false},
		{PostContent{Text: "hi", Media: []MediaFile{{Name: "v.mp4", MimeType: "video/mp4"}}}, false},
	} {
		if err := e.Validate(c.p, nil); (err == nil) != c.ok {
			t.Errorf("%q %d files: %v", c.p.Text[:2], len(c.p.Media), err)
		}
	}
}

func TestBlueskyRecordKeyIsATID(t *testing.T) {
	k := blueskyRecordKey("x")
	if len(k) != 13 || !strings.ContainsRune("234567abcdefghij", rune(k[0])) || k != blueskyRecordKey("x") {
		t.Fatalf("record key %q", k)
	}
}
