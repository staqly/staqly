package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// X's numbers come from the tweet's public_metrics with the account's token; a revoked token asks to reconnect.
func TestXMetrics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer at" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"title":"Unauthorized"}`))
			return
		}
		if r.URL.Path != "/2/tweets/123" || r.URL.Query().Get("tweet.fields") != "public_metrics" {
			t.Errorf("request: %s", r.URL)
		}
		_, _ = w.Write([]byte(`{"data":{"id":"123","public_metrics":{"impression_count":900,"like_count":12,"retweet_count":3,"reply_count":2,"quote_count":1,"bookmark_count":4}}}`))
	}))
	defer srv.Close()
	old := xAPI
	xAPI = srv.URL
	defer func() { xAPI = old }()

	m, err := X{}.Metrics(context.Background(), Credentials{"access_token": "at"}, "123")
	if err != nil || *m.Impressions != 900 || *m.Likes != 12 || *m.Reposts != 3 || *m.Replies != 2 || *m.Quotes != 1 ||
		*m.Bookmarks != 4 || m.LinkClicks != nil {
		t.Fatalf("metrics: %+v %v", m, err)
	}
	_, err = X{}.Metrics(context.Background(), Credentials{"access_token": "old"}, "123")
	if f, ok := err.(*Failure); !ok || f.Kind != ErrReauth {
		t.Fatalf("revoked token: %v", err)
	}
}
