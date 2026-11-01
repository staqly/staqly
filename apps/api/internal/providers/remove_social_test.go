package providers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// removeCodeServer answers every request with code and records "METHOD path body".
func removeCodeServer(t *testing.T, code int) (*httptest.Server, *[]string) {
	t.Helper()
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.Path+" "+string(b))
		w.WriteHeader(code)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestRemoveXGone(t *testing.T) {
	srv, calls := removeCodeServer(t, http.StatusNotFound)
	old := xAPI
	xAPI = srv.URL
	t.Cleanup(func() { xAPI = old })
	if err := (X{}).Remove(context.Background(), Credentials{"access_token": "at"}, "99"); err != nil {
		t.Fatal(err)
	}
	if got := (*calls)[0]; got != "DELETE /2/tweets/99 " {
		t.Fatal(got)
	}
}

func TestRemoveXReauth(t *testing.T) {
	srv, _ := removeCodeServer(t, http.StatusUnauthorized)
	old := xAPI
	xAPI = srv.URL
	t.Cleanup(func() { xAPI = old })
	err := (X{}).Remove(context.Background(), Credentials{"access_token": "secret-tok"}, "99")
	if !errors.Is(err, ErrReauth) || strings.Contains(err.Error(), "secret-tok") {
		t.Fatal(err)
	}
}

func TestRemoveBlueskyReplyID(t *testing.T) {
	f, creds := useFakeBluesky(t)
	var got string
	inner := f.srv.Config.Handler
	f.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "deleteRecord") {
			b, _ := io.ReadAll(r.Body)
			got = string(b)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		inner.ServeHTTP(w, r)
	})
	id := "at://did:plc:jane/app.bsky.feed.post/rk|cid|at://did:plc:jane/app.bsky.feed.post/root|rcid"
	if err := (Bluesky{}).Remove(context.Background(), creds, id); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"repo":"did:plc:jane"`, `"collection":"app.bsky.feed.post"`, `"rkey":"rk"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("%s missing in %s", want, got)
		}
	}
	if err := (Bluesky{}).Remove(context.Background(), creds, "garbage"); !errors.Is(err, ErrRejected) {
		t.Fatal(err)
	}
}

func TestRemoveMastodonReauth(t *testing.T) {
	srv, _ := removeCodeServer(t, http.StatusUnauthorized)
	c := Credentials{"instance": srv.URL, "access_token": "secret-tok"}
	err := (Mastodon{}).Remove(context.Background(), c, "110")
	if !errors.Is(err, ErrReauth) || strings.Contains(err.Error(), "secret-tok") {
		t.Fatal(err)
	}
}

func TestRemoveMastodonGone(t *testing.T) {
	srv, calls := removeCodeServer(t, http.StatusNotFound)
	if err := (Mastodon{}).Remove(context.Background(), Credentials{"instance": srv.URL, "access_token": "t"}, "110"); err != nil {
		t.Fatal(err)
	}
	if (*calls)[0] != "DELETE /api/v1/statuses/110 " {
		t.Fatal((*calls)[0])
	}
}
