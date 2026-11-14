package providers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// proRespServer answers every request with body (status code), recording "METHOD URI".
func proRespServer(t *testing.T, code int, body string) (*httptest.Server, *[]string) {
	t.Helper()
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestResponsesYouTube(t *testing.T) {
	srv, calls := proRespServer(t, 200, `{"items":[{"snippet":{"topLevelComment":{"id":"c1","snippet":{"authorDisplayName":"Ann","textOriginal":"Great <b>video</b>","likeCount":3,"publishedAt":"2026-10-30T10:00:00Z"}}},
	"replies":{"comments":[{"id":"c1.r1","snippet":{"authorDisplayName":"Me","authorChannelId":{"value":"UC1"},"textOriginal":"Thanks","parentId":"c1","publishedAt":"2026-10-30T11:00:00Z"}}]}}]}`)
	old := youtubeAPI
	youtubeAPI = srv.URL
	t.Cleanup(func() { youtubeAPI = old })
	got, err := (YouTube{}).Responses(context.Background(), Credentials{"access_token": "t", "channel_id": "UC1"}, "vid", time.Time{})
	if err != nil || len(got) != 2 {
		t.Fatal(got, err)
	}
	if got[0].ID != "c1" || got[0].Text != "Great <b>video</b>" || got[0].Likes == nil || *got[0].Likes != 3 || got[0].Own {
		t.Fatalf("%+v", got[0])
	}
	if got[1].ParentID != "c1" || !got[1].Own {
		t.Fatalf("%+v", got[1])
	}
	if (*calls)[0] != "GET /youtube/v3/commentThreads?maxResults=100&order=time&part=snippet%2Creplies&textFormat=plainText&videoId=vid" {
		t.Fatal((*calls)[0])
	}
	since := time.Date(2026, 10, 30, 10, 30, 0, 0, time.UTC)
	got, _ = (YouTube{}).Responses(context.Background(), Credentials{"access_token": "t"}, "vid", since)
	if len(got) != 1 || got[0].ID != "c1.r1" {
		t.Fatal(got)
	}
}

func TestResponsesLinkedInNoAccess(t *testing.T) {
	srv, _ := proRespServer(t, 403, `{"message":"Not enough permissions to access: partnerApiSocialActions.FINDER-comments.NO_VERSION"}`)
	old := linkedinAPI
	linkedinAPI = srv.URL
	t.Cleanup(func() { linkedinAPI = old })
	_, err := (LinkedIn{}).Responses(context.Background(), Credentials{"access_token": "t"}, "urn:li:share:1", time.Time{})
	var f *Failure
	if !errors.As(err, &f) || !errors.Is(f.Kind, ErrRejected) || !strings.Contains(f.Msg, "Community Management API") {
		t.Fatal(err)
	}
}

func TestResponsesLinkedInPage(t *testing.T) {
	srv, calls := proRespServer(t, 200, `{"elements":[{"$URN":"urn:li:comment:(urn:li:share:1,5)","actor":"urn:li:person:p","message":{"text":"hi"},"created":{"time":1793000000000},"likesSummary":{"totalLikes":2}},
	{"$URN":"urn:li:comment:(urn:li:share:1,6)","actor":"urn:li:organization:9","parentComment":"urn:li:comment:(urn:li:share:1,5)","message":{"text":"re"},"created":{"time":1793000001000}}]}`)
	old := linkedinAPI
	linkedinAPI = srv.URL
	t.Cleanup(func() { linkedinAPI = old })
	got, err := (LinkedInPage{}).Responses(context.Background(), Credentials{"access_token": "t", "organization_id": "9"}, "urn:li:share:1", time.Time{})
	if err != nil || len(got) != 2 || got[0].Own || !got[1].Own || got[1].ParentID != got[0].ID || *got[0].Likes != 2 {
		t.Fatal(got, err)
	}
	if (*calls)[0] != "GET /rest/socialActions/urn%3Ali%3Ashare%3A1/comments?count=100&start=0" {
		t.Fatal((*calls)[0])
	}
}

func TestResponsesRedditTree(t *testing.T) {
	srv, calls := proRespServer(t, 200, `[{"data":{"children":[{"kind":"t3","data":{"id":"x"}}]}},
	{"data":{"children":[
	 {"kind":"t1","data":{"id":"a","parent_id":"t3_x","author":"bob","body":"top","score":5,"created_utc":1793000000,"permalink":"/r/s/comments/x/t/a/",
	  "replies":{"data":{"children":[
	    {"kind":"t1","data":{"id":"b","parent_id":"t1_a","author":"Jane","body":"nested","score":1,"created_utc":1793000100,"permalink":"/r/s/comments/x/t/b/","replies":""}},
	    {"kind":"more","data":{"children":["zz"]}}]}}}},
	 {"kind":"more","data":{"children":["yy"]}}]}}]`)
	old := redditOAuth
	redditOAuth = srv.URL
	t.Cleanup(func() { redditOAuth = old })
	c := Credentials{"access_token": "t", "username": "jane"}
	got, err := (Reddit{}).Responses(context.Background(), c, "t3_x", time.Time{})
	if err != nil || len(got) != 2 {
		t.Fatal(got, err)
	}
	if got[0].ID != "a" || got[0].ParentID != "" || got[1].ID != "b" || got[1].ParentID != "a" || !got[1].Own || got[0].Own {
		t.Fatalf("%+v", got)
	}
	if (*calls)[0] != "GET /comments/x?raw_json=1&limit=500&sort=new" {
		t.Fatal((*calls)[0])
	}
	if got, err := (Reddit{}).Responses(context.Background(), c, "", time.Time{}); got != nil || err != nil || len(*calls) != 1 {
		t.Fatal(got, err)
	}
}

func TestResponsesSlackThread(t *testing.T) {
	srv, calls := proRespServer(t, 200, `{"ok":true,"messages":[{"ts":"1793000000.000100","user":"U0","text":"the post"},
	{"ts":"1793000010.000200","user":"U1","text":"nice","reactions":[{"name":"+1","count":2}]},{"ts":"1793000020.000300","bot_id":"B1","text":"mine"}]}`)
	old := slackAPI
	slackAPI = srv.URL
	t.Cleanup(func() { slackAPI = old })
	got, err := (Slack{}).Responses(context.Background(), Credentials{"access_token": "t"}, "C1:1793000000.000100/1793000020.000300,C2:1793000000.000100", time.Time{})
	if err != nil || len(*calls) != 2 || (*calls)[0] != "GET /conversations.replies?channel=C1&limit=200&ts=1793000000.000100" {
		t.Fatal(got, err, *calls)
	}
	if len(got) != 4 || got[0].ID != "C1:1793000010.000200" || got[0].Author != "U1" || *got[0].Likes != 2 || got[0].Own || !got[1].Own {
		t.Fatalf("%+v", got)
	}
}
