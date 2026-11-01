package providers

import (
	"context"
	"errors"
	"testing"
)

func TestLinkedInPageMetrics(t *testing.T) {
	srv, calls := proRespServer(t, 200, `{"elements":[{"totalShareStatistics":{"impressionCount":120,"uniqueImpressionsCount":90,
	"clickCount":7,"likeCount":5,"commentCount":2,"shareCount":1,"engagement":0.12}}]}`)
	old := linkedinAPI
	linkedinAPI = srv.URL
	t.Cleanup(func() { linkedinAPI = old })
	c := Credentials{"access_token": "t", "organization_id": "9"}
	m, err := (LinkedInPage{}).Metrics(context.Background(), c, "urn:li:share:1")
	if err != nil || *m.Impressions != 120 || *m.LinkClicks != 7 || *m.Likes != 5 || *m.Replies != 2 || *m.Reposts != 1 {
		t.Fatal(m, err)
	}
	if _, err := (LinkedInPage{}).Metrics(context.Background(), c, "urn:li:ugcPost:2"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"GET /rest/organizationalEntityShareStatistics?q=organizationalEntity&organizationalEntity=urn%3Ali%3Aorganization%3A9&shares=List(urn%3Ali%3Ashare%3A1)",
		"GET /rest/organizationalEntityShareStatistics?q=organizationalEntity&organizationalEntity=urn%3Ali%3Aorganization%3A9&ugcPosts=List(urn%3Ali%3AugcPost%3A2)",
	}
	for i, w := range want {
		if (*calls)[i] != w {
			t.Fatalf("call %d: %s", i, (*calls)[i])
		}
	}
}

func TestLinkedInPageMetricsNoneYet(t *testing.T) {
	srv, _ := proRespServer(t, 200, `{"elements":[]}`)
	old := linkedinAPI
	linkedinAPI = srv.URL
	t.Cleanup(func() { linkedinAPI = old })
	m, err := (LinkedInPage{}).Metrics(context.Background(), Credentials{"access_token": "t", "organization_id": "9"}, "urn:li:share:1")
	if err != nil || m.Impressions != nil || m.Likes != nil {
		t.Fatal(m, err)
	}
}

func TestLinkedInPageMetricsSignInGone(t *testing.T) {
	srv, _ := proRespServer(t, 401, `{"status":401,"message":"Invalid access token"}`)
	old := linkedinAPI
	linkedinAPI = srv.URL
	t.Cleanup(func() { linkedinAPI = old })
	_, err := (LinkedInPage{}).Metrics(context.Background(), Credentials{"access_token": "t", "organization_id": "9"}, "urn:li:share:1")
	if !errors.Is(err, ErrReauth) {
		t.Fatal(err)
	}
}
