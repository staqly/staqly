package providers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func removeProServer(t *testing.T) *[]string {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.EscapedPath()+"?"+r.URL.RawQuery)
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("auth: %s", r.Header.Get("Authorization"))
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "gone"), r.URL.Query().Get("id") == "gone":
			http.NotFound(w, r)
		case r.URL.Query().Get("id") == "nope":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"code":403,"message":"forbidden","errors":[{"reason":"forbidden"}]}}`))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(srv.Close)
	la, ya := linkedinAPI, youtubeAPI
	linkedinAPI, youtubeAPI = srv.URL, srv.URL
	t.Cleanup(func() { linkedinAPI, youtubeAPI = la, ya })
	return &got
}

func TestRemoveLinkedInComment(t *testing.T) {
	got := removeProServer(t)
	ctx := context.Background()
	if err := (LinkedIn{}).Remove(ctx, Credentials{"access_token": "tok", "person_id": "p"}, "urn:li:comment:(urn:li:share:1,5)"); err != nil {
		t.Fatal(err)
	}
	if err := (LinkedInPage{}).Remove(ctx, Credentials{"access_token": "tok", "organization_id": "9"}, "urn:li:share:1"); err != nil {
		t.Fatal(err)
	}
	if err := (LinkedIn{}).Remove(ctx, Credentials{"access_token": "tok", "person_id": "p"}, "urn:li:share:gone"); err != nil {
		t.Errorf("gone: %v", err)
	}
	want := []string{
		"DELETE /rest/socialActions/urn%3Ali%3Ashare%3A1/comments/5?actor=urn%3Ali%3Aperson%3Ap",
		"DELETE /rest/posts/urn%3Ali%3Ashare%3A1?",
	}
	if (*got)[0] != want[0] || (*got)[1] != want[1] {
		t.Errorf("requests: %v", *got)
	}
	if err := (LinkedIn{}).Remove(ctx, Credentials{"access_token": "tok"}, "urn:li:share:1"); !errors.Is(err, ErrReauth) {
		t.Errorf("no account: %v", err)
	}
}

func TestRemoveYouTubeComment(t *testing.T) {
	got := removeProServer(t)
	c := Credentials{"access_token": "tok", "scope": "x youtube.force-ssl"}
	if err := (YouTube{}).Remove(context.Background(), c, "comment:vid:c1"); err != nil {
		t.Fatal(err)
	}
	if (*got)[0] != "DELETE /youtube/v3/comments?id=c1" {
		t.Errorf("requests: %v", *got)
	}
}

func TestRemoveYouTubeVideo(t *testing.T) {
	got := removeProServer(t)
	ctx, c := context.Background(), Credentials{"access_token": "tok"}
	if err := (YouTube{}).Remove(ctx, c, "vid"); err != nil {
		t.Fatal(err)
	}
	if err := (YouTube{}).Remove(ctx, c, "gone"); err != nil {
		t.Errorf("gone: %v", err)
	}
	err := (YouTube{}).Remove(ctx, c, "nope")
	if !errors.Is(err, ErrRejected) || strings.Contains(err.Error(), "tok") {
		t.Errorf("refused: %v", err)
	}
	if (*got)[0] != "DELETE /youtube/v3/videos?id=vid" {
		t.Errorf("requests: %v", *got)
	}
	if err := (YouTube{}).Remove(ctx, Credentials{"access_token": "tok", "scope": "youtube.upload"}, "vid"); !errors.Is(err, ErrReauth) {
		t.Errorf("scope: %v", err)
	}
}
