package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestResponsesMetaPaging(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "" && r.URL.Query().Get("filter") != "stream" {
			t.Errorf("filter: %s", r.URL.RawQuery)
		}
		page := r.URL.Query().Get("page")
		var items []string
		for i := 0; i < 100; i++ {
			items = append(items, fmt.Sprintf(`{"id":"%s-%d","from":{"id":"1","name":"Ann"},"message":"hi","created_time":"2026-01-01T00:00:00+0000","like_count":3,"parent":{"id":"p"}}`, page, i))
		}
		n := 0
		fmt.Sscan(page, &n)
		_, _ = fmt.Fprintf(w, `{"data":[%s],"paging":{"next":"%s/v24.0/1_2/comments?page=%d&access_token=tok"}}`, strings.Join(items, ","), srv.URL, n+1)
	}))
	defer srv.Close()
	g := facebookGraph
	facebookGraph = srv.URL
	defer func() { facebookGraph = g }()
	got, err := Facebook{}.Responses(context.Background(), Credentials{"access_token": "tok"}, "1_2", time.Time{})
	if err != nil || len(got) != 500 {
		t.Fatalf("%d %v", len(got), err)
	}
	if got[0].Author != "Ann" || got[0].ParentID != "p" || !got[0].Own || *got[0].Likes != 3 {
		t.Fatalf("%+v", got[0])
	}
}

func TestResponsesMetaSince(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"c1","username":"bob","text":"When?","timestamp":"2026-02-01T00:00:00+0000","like_count":0,
"replies":{"data":[{"id":"c2","username":"me","from":{"id":"u1"},"text":"Soon","timestamp":"2026-02-02T00:00:00+0000"}]}},
{"id":"old","username":"x","text":"old","timestamp":"2025-01-01T00:00:00+0000"}]}`))
	}))
	defer srv.Close()
	g := facebookGraph
	facebookGraph = srv.URL
	defer func() { facebookGraph = g }()
	got, err := Instagram{}.Responses(context.Background(), Credentials{"access_token": "tok", "user_id": "u1"}, "17890", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil || len(got) != 2 || got[0].ID != "c1" || got[0].AuthorHandle != "@bob" || got[1].ParentID != "c1" || !got[1].Own {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestThreadsScopesReadReplies(t *testing.T) {
	if !strings.Contains(threadsScopes, "threads_read_replies") {
		t.Fatal(threadsScopes)
	}
}

func TestFacebookReactionsByType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"reactions":{"summary":{"total_count":9}},"comments":{"summary":{"total_count":1}},"r_like":{"summary":{"total_count":5}},"r_love":{"summary":{"total_count":4}}}`))
	}))
	defer srv.Close()
	g := facebookGraph
	facebookGraph = srv.URL
	defer func() { facebookGraph = g }()
	m, err := Facebook{}.Metrics(context.Background(), Credentials{"access_token": "tok"}, "vid")
	if err != nil || len(m.Reactions) != 6 || m.Reactions[0].Emoji != "like" || m.Reactions[0].Count != 5 || m.Reactions[1].Count != 4 || m.Reactions[5].Emoji != "angry" {
		t.Fatalf("%+v %v", m, err)
	}
}

func TestResponsesThreadsConversation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1.0/9/conversation" || !strings.Contains(r.URL.Query().Get("fields"), "is_reply_owned_by_me") {
			t.Errorf("request %s", r.URL)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"a","text":"hi","username":"ann","timestamp":"2026-02-01T00:00:00+0000","replied_to":{"id":"9"}},
{"id":"b","text":"thx","username":"me","timestamp":"2026-02-02T00:00:00+0000","is_reply_owned_by_me":true,"replied_to":{"id":"a"}}]}`))
	}))
	defer srv.Close()
	g := threadsGraph
	threadsGraph = srv.URL
	defer func() { threadsGraph = g }()
	got, err := Threads{}.Responses(context.Background(), Credentials{"access_token": "tok"}, "9", time.Time{})
	if err != nil || len(got) != 2 || got[0].ParentID != "" || got[0].Own || got[1].ParentID != "a" || !got[1].Own || got[1].AuthorHandle != "@me" {
		t.Fatalf("%+v %v", got, err)
	}
}
