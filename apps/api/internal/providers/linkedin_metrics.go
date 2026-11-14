package providers

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

// A Page's posts report their numbers through organizationalEntityShareStatistics (the Community Management API's
// rw_organization_admin, which the Page channel already asks for). A member's own posts have no such read without a
// partner-only permission, so the LinkedIn profile channel reports none.
var _ MetricsReader = LinkedInPage{}

func (LinkedInPage) Metrics(ctx context.Context, c Credentials, externalID string) (Metrics, error) {
	// The statistics are asked per kind of post: /rest/posts answers urn:li:share or urn:li:ugcPost.
	param := "shares"
	if strings.HasPrefix(externalID, "urn:li:ugcPost:") {
		param = "ugcPosts"
	}
	req, err := linkedinJSON(ctx, c["access_token"], http.MethodGet, linkedinAPI+"/rest/organizationalEntityShareStatistics"+
		"?q=organizationalEntity&organizationalEntity="+linkedinURN("urn:li:organization:"+c["organization_id"])+
		"&"+param+"=List("+url.QueryEscape(externalID)+")", nil)
	if err != nil {
		return Metrics{}, err
	}
	var out struct {
		Elements []struct {
			Stats struct {
				Impressions int64 `json:"impressionCount"`
				Clicks      int64 `json:"clickCount"`
				Likes       int64 `json:"likeCount"`
				Comments    int64 `json:"commentCount"`
				Shares      int64 `json:"shareCount"`
			} `json:"totalShareStatistics"`
		} `json:"elements"`
	}
	if _, err := linkedinDo(ctx, req, &out); err != nil {
		return Metrics{}, err
	}
	if len(out.Elements) == 0 { // LinkedIn has no numbers for it yet (just published) or the post is gone
		return Metrics{}, nil
	}
	s := out.Elements[0].Stats
	return Metrics{Impressions: &s.Impressions, LinkClicks: &s.Clicks, Likes: &s.Likes, Replies: &s.Comments,
		Reposts: &s.Shares}, nil
}
