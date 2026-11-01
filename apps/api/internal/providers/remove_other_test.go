package providers

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestRemoveRedditNoID(t *testing.T) {
	err := (Reddit{}).Remove(context.Background(), Credentials{"access_token": "t"}, "")
	var f *Failure
	if !errors.As(err, &f) || f.Kind != ErrManual {
		t.Fatal(err)
	}
}

func TestRemoveRedditPost(t *testing.T) {
	srv, calls := removeCodeServer(t, http.StatusOK)
	old := redditOAuth
	redditOAuth = srv.URL
	t.Cleanup(func() { redditOAuth = old })
	if err := (Reddit{}).Remove(context.Background(), Credentials{"access_token": "t"}, "t3_ab"); err != nil {
		t.Fatal(err)
	}
	if (*calls)[0] != "POST /api/del id=t3_ab" {
		t.Fatal((*calls)[0])
	}
}

func TestRemoveTumblr(t *testing.T) {
	srv, calls := removeCodeServer(t, http.StatusOK)
	old := tumblrAPI
	tumblrAPI = srv.URL
	t.Cleanup(func() { tumblrAPI = old })
	if err := (Tumblr{}).Remove(context.Background(), Credentials{"access_token": "t", "blog": "b1"}, "77"); err != nil {
		t.Fatal(err)
	}
	if (*calls)[0] != "POST /v2/blog/b1/post/delete id=77" {
		t.Fatal((*calls)[0])
	}
}

func TestRemovePinterestGone(t *testing.T) {
	srv, calls := removeCodeServer(t, http.StatusNotFound)
	old := pinterestAPI
	pinterestAPI = srv.URL
	t.Cleanup(func() { pinterestAPI = old })
	if err := (Pinterest{}).Remove(context.Background(), Credentials{"access_token": "t"}, "5"); err != nil {
		t.Fatal(err)
	}
	if (*calls)[0] != "DELETE /v5/pins/5 " {
		t.Fatal((*calls)[0])
	}
}
