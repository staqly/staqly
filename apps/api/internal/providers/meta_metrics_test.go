package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Facebook, Instagram and Threads read a post's numbers from the Graph API; an Instagram channel without the
// insights permission still gets likes and comments.
func TestMetaMetrics(t *testing.T) {
	insights := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v24.0/1_2":
			_, _ = w.Write([]byte(`{"reactions":{"data":[],"summary":{"total_count":7}},"comments":{"data":[],"summary":{"total_count":2}},"shares":{"count":3}}`))
		case "/v24.0/vid":
			if r.URL.Query().Get("fields") != "reactions.summary(total_count).limit(0),comments.summary(total_count).limit(0)" {
				t.Errorf("video fields: %s", r.URL.Query().Get("fields"))
			}
			_, _ = w.Write([]byte(`{"reactions":{"summary":{"total_count":1}},"comments":{"summary":{"total_count":0}}}`))
		case "/v24.0/ig":
			_, _ = w.Write([]byte(`{"like_count":10,"comments_count":4,"media_product_type":"FEED"}`))
		case "/v24.0/ig/insights":
			if !insights {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":{"message":"(#10) Application does not have permission for this action","code":10}}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"name":"views","values":[{"value":120}]},{"name":"shares","values":[{"value":5}]},{"name":"saved","values":[{"value":6}]}]}`))
		case "/v1.0/th/insights":
			_, _ = w.Write([]byte(`{"data":[{"name":"views","values":[{"value":50}]},{"name":"likes","values":[{"value":9}]},{"name":"replies","values":[{"value":1}]},{"name":"reposts","values":[{"value":2}]},{"name":"quotes","values":[{"value":0}]},{"name":"shares","total_value":{"value":4}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	fb, th := facebookGraph, threadsGraph
	facebookGraph, threadsGraph = srv.URL, srv.URL
	defer func() { facebookGraph, threadsGraph = fb, th }()
	ctx, c := context.Background(), Credentials{"access_token": "tok"}
	v := func(p *int64) int64 {
		if p == nil {
			return -1
		}
		return *p
	}

	m, err := Facebook{}.Metrics(ctx, c, "1_2")
	if err != nil || v(m.Likes) != 7 || v(m.Replies) != 2 || v(m.Shares) != 3 {
		t.Fatalf("facebook post: %+v %v", m, err)
	}
	if m, err = (Facebook{}).Metrics(ctx, c, "vid"); err != nil || v(m.Likes) != 1 || m.Shares != nil {
		t.Fatalf("facebook video: %+v %v", m, err)
	}
	if m, err = (Instagram{}).Metrics(ctx, c, "ig"); err != nil || v(m.Likes) != 10 || v(m.Replies) != 4 ||
		v(m.Impressions) != 120 || v(m.Shares) != 5 || v(m.Bookmarks) != 6 {
		t.Fatalf("instagram: %+v %v", m, err)
	}
	insights = false
	if m, err = (Instagram{}).Metrics(ctx, c, "ig"); err != nil || v(m.Likes) != 10 || m.Impressions != nil {
		t.Fatalf("instagram without insights: %+v %v", m, err)
	}
	if m, err = (Threads{}).Metrics(ctx, c, "th"); err != nil || v(m.Impressions) != 50 || v(m.Likes) != 9 ||
		v(m.Reposts) != 2 || v(m.Quotes) != 0 || v(m.Shares) != 4 {
		t.Fatalf("threads: %+v %v", m, err)
	}
}
