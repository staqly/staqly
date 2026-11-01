package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestResponsesXSearch(t *testing.T) {
	forbidden := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/2/tweets/search/recent" || r.URL.Query().Get("query") != "conversation_id:100" || r.URL.Query().Get("expansions") != "author_id" {
			t.Errorf("request %s", r.URL)
		}
		if forbidden {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"title":"Client Forbidden","reason":"client-not-enrolled","type":"https://api.twitter.com/2/problems/client-forbidden"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"100","text":"root","author_id":"me"},
{"id":"101","text":"Nice","author_id":"7","created_at":"2026-02-01T00:00:00Z","public_metrics":{"like_count":2},"referenced_tweets":[{"type":"replied_to","id":"100"}]},
{"id":"102","text":"thx","author_id":"me","created_at":"2026-02-02T00:00:00Z","public_metrics":{"like_count":0},"referenced_tweets":[{"type":"replied_to","id":"101"}]}],
"includes":{"users":[{"id":"7","name":"Ann","username":"ann"}]}}`))
	}))
	defer srv.Close()
	old := xAPI
	xAPI = srv.URL
	defer func() { xAPI = old }()
	c := Credentials{"access_token": "tok", "user_id": "me"}
	got, err := X{}.Responses(context.Background(), c, "100", time.Time{})
	if err != nil || len(got) != 2 || got[0].Author != "Ann" || got[0].AuthorHandle != "@ann" || got[0].ParentID != "" || *got[0].Likes != 2 ||
		got[1].ParentID != "101" || !got[1].Own {
		t.Fatalf("%+v %v", got, err)
	}
	forbidden = true
	_, err = X{}.Responses(context.Background(), c, "100", time.Time{})
	if f, ok := err.(*Failure); !ok || f.Kind != ErrRejected || f.Msg != "X: reading replies needs a paid X API plan" {
		t.Fatalf("%v", err)
	}
}

func TestResponsesBlueskyThread(t *testing.T) {
	f, creds := useFakeBluesky(t)
	f.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "createSession"):
			_, _ = w.Write([]byte(`{"accessJwt":"x","did":"did:plc:jane","handle":"jane.example.com"}`))
		case strings.HasSuffix(r.URL.Path, "getPostThread"):
			if r.URL.Query().Get("uri") != "at://did:plc:jane/app.bsky.feed.post/root" {
				t.Errorf("uri %s", r.URL.Query().Get("uri"))
			}
			_, _ = w.Write([]byte(`{"thread":{"post":{"uri":"at://did:plc:jane/app.bsky.feed.post/root"},"replies":[
{"post":{"uri":"at://did:ben/app.bsky.feed.post/a","author":{"did":"did:ben","handle":"ben.example.com","displayName":"Ben"},"record":{"text":"hello","createdAt":"2026-02-01T00:00:00Z"},"likeCount":4},
 "replies":[{"post":{"uri":"at://did:plc:jane/app.bsky.feed.post/b","author":{"did":"did:plc:jane","handle":"jane.example.com"},"record":{"text":"hi","createdAt":"2026-02-02T00:00:00Z"}}}]},
{"$type":"app.bsky.feed.defs#blockedPost"}]}}`))
		default:
			http.NotFound(w, r)
		}
	})
	got, err := Bluesky{}.Responses(context.Background(), creds, "at://did:plc:jane/app.bsky.feed.post/root|cid", time.Time{})
	if err != nil || len(got) != 2 || got[0].ParentID != "" || got[0].AuthorHandle != "@ben.example.com" || *got[0].Likes != 4 ||
		got[1].ParentID != "at://did:ben/app.bsky.feed.post/a" || !got[1].Own || got[1].URL != blueskyApp+"/profile/jane.example.com/post/b" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestResponsesMastodonContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/accounts/verify_credentials":
			_, _ = w.Write([]byte(`{"id":"1","username":"me"}`))
		case "/api/v1/statuses/5/context":
			_, _ = w.Write([]byte(`{"descendants":[
{"id":"6","in_reply_to_id":"5","content":"<p>Hi <a href=\"x\">@me</a><br/>5 &amp; 6</p><script>x</script>","url":"https://m/6","created_at":"2026-02-01T00:00:00Z","favourites_count":3,"account":{"id":"2","acct":"ann@m","display_name":"Ann"}},
{"id":"7","in_reply_to_id":"6","content":"<p>ok</p>","created_at":"2026-02-02T00:00:00Z","account":{"id":"1","acct":"me"}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	got, err := Mastodon{}.Responses(context.Background(), Credentials{"instance": srv.URL, "access_token": "tok"}, "5", time.Time{})
	if err != nil || len(got) != 2 || got[0].Text != "Hi @me\n5 & 6\nx" || got[0].ParentID != "" || got[0].AuthorHandle != "@ann@m" ||
		got[1].ParentID != "6" || !got[1].Own || *got[0].Likes != 3 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestResponsesMastodonStoredAccountID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "verify_credentials") {
			t.Errorf("unexpected %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"descendants":[{"id":"7","in_reply_to_id":"5","content":"ok","created_at":"2026-02-02T00:00:00Z","account":{"id":"1","acct":"me"}}]}`))
	}))
	defer srv.Close()
	got, err := Mastodon{}.Responses(context.Background(), Credentials{"instance": srv.URL, "access_token": "tok", "account_id": "1"}, "5", time.Time{})
	if err != nil || len(got) != 1 || !got[0].Own {
		t.Fatalf("%+v %v", got, err)
	}
}
