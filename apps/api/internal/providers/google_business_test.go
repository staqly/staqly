package providers

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"
)

func gbpCreds() Credentials {
	return Credentials{"access_token": "at", "refresh_token": "rt-1", "location": "accounts/111/locations/9001",
		"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}
}

func TestGoogleBusinessConnect(t *testing.T) {
	f := useFakeGoogle(t)
	gb, _ := Get("google_business")
	if gb.Status != StatusAvailable || gb.Name() != "Google Business Profile" {
		t.Fatalf("google_business is %q %q", gb.Name(), gb.Status)
	}
	if !gb.Provider.(Needer).Needs().PublicMedia {
		t.Fatal("Google downloads the photo: it needs public media")
	}
	app := App{ClientID: "cid", ClientSecret: "secret"}
	u, _ := gb.AuthURL(app, "http://localhost:8080/channels/callback/google_business", "st", nil)
	q, _ := url.Parse(u)
	if !strings.Contains(q.Query().Get("scope"), "business.manage") || q.Query().Get("access_type") != "offline" {
		t.Fatalf("auth url %s", u)
	}
	creds, profile, err := gb.Exchange(context.Background(), app, "http://localhost:8080/cb", "code1", nil)
	if err != nil || creds["refresh_token"] != "rt-1" || profile.Name != "Jane Doe" {
		t.Fatalf("exchange %v %+v %v", creds, profile, err)
	}

	// Two business accounts: the second refuses its locations, the first still shows.
	lister := gb.Provider.(AccountLister)
	accounts, err := lister.Accounts(context.Background(), creds)
	if err != nil || len(accounts) != 1 {
		t.Fatalf("accounts %+v %v", accounts, err)
	}
	a := accounts[0]
	if a.ID != "accounts/111/locations/9001" || a.Name != "Jane's Bakery, Springfield" || a.Handle != "Jane's Bakery (9001)" ||
		a.ProfileURL != "https://maps.google.com/?cid=1" {
		t.Fatalf("account %+v", a)
	}
	loc, locProfile, err := lister.UseAccount(context.Background(), creds, a.ID)
	if err != nil || loc["location"] != a.ID || loc["refresh_token"] != "rt-1" || locProfile.Handle != a.Handle {
		t.Fatalf("use account %v %+v %v", loc, locProfile, err)
	}
	if _, _, err := lister.UseAccount(context.Background(), creds, "9001"); failureKind(t, err) != ErrRejected {
		t.Fatalf("not a location: %v", err)
	}
	fresh, err := gb.Refresh(context.Background(), app, loc)
	if err != nil || fresh["access_token"] != "at-refresh_token" || fresh["location"] != a.ID {
		t.Fatalf("refresh %v %v", fresh, err)
	}

	f.scope = "openid email profile"
	if _, _, err := gb.Exchange(context.Background(), app, "http://localhost:8080/cb", "code2", nil); failureKind(t, err) != ErrRejected {
		t.Fatalf("business.manage unticked: %v", err)
	}
}

func TestGoogleBusinessPublish(t *testing.T) {
	f := useFakeGoogle(t)
	gb, _ := Get("google_business")
	photo := MediaFile{Name: "bread.jpg", MimeType: "image/jpeg", SignedURL: "https://bucket.example.com/bread.jpg?sig=1"}
	s := Settings{"call_to_action": "ORDER", "call_to_action_link": "https://example.com/order"}
	p := PostContent{Text: "Fresh sourdough today", Media: []MediaFile{photo}}
	if err := gb.Validate(p, s); err != nil {
		t.Fatal(err)
	}
	res, err := gb.Publish(context.Background(), gbpCreds(), p, s)
	if err != nil || res.ExternalID != "accounts/111/locations/9001/localPosts/555" || !strings.Contains(res.URL, "lpsid=555") {
		t.Fatalf("publish %+v %v", res, err)
	}
	if f.post["summary"] != "Fresh sourdough today" || f.post["topicType"] != "STANDARD" ||
		fmt.Sprint(f.post["media"]) != "[map[mediaFormat:PHOTO sourceUrl:https://bucket.example.com/bread.jpg?sig=1]]" ||
		fmt.Sprint(f.post["callToAction"]) != "map[actionType:ORDER url:https://example.com/order]" {
		t.Fatalf("post %v", f.post)
	}

	// Text alone, a call button without a link.
	if _, err := gb.Publish(context.Background(), gbpCreds(), PostContent{Text: "Open on Sunday"}, Settings{"call_to_action": "CALL"}); err != nil {
		t.Fatal(err)
	}
	if f.post["media"] != nil || fmt.Sprint(f.post["callToAction"]) != "map[actionType:CALL]" {
		t.Fatalf("text post %v", f.post)
	}

	// A photo on the server's own disk can't be fetched by Google.
	if _, err := gb.Publish(context.Background(), gbpCreds(), PostContent{Text: "x", Media: []MediaFile{{Name: "a.jpg", MimeType: "image/jpeg"}}}, nil); failureKind(t, err) != ErrRejected {
		t.Fatalf("no public link: %v", err)
	}

	for _, tc := range []struct {
		code int
		body string
		kind error
	}{
		{200, `{"name":"accounts/111/locations/9001/localPosts/556","state":"REJECTED"}`, ErrRejected},
		{400, `{"error":{"code":400,"message":"Request contains an invalid argument.","status":"INVALID_ARGUMENT"}}`, ErrRejected},
		{401, `{"error":{"code":401,"message":"Request had invalid authentication credentials.","status":"UNAUTHENTICATED"}}`, ErrReauth},
		{429, `{"error":{"code":429,"message":"Quota exceeded","status":"RESOURCE_EXHAUSTED"}}`, nil},
		{500, `{"error":{"code":500,"message":"Internal error","status":"INTERNAL"}}`, nil},
	} {
		f.postCode, f.postBody = tc.code, tc.body
		if _, err := gb.Publish(context.Background(), gbpCreds(), PostContent{Text: "x"}, nil); failureKind(t, err) != tc.kind {
			t.Errorf("%d %s: %v", tc.code, tc.body, err)
		}
	}
	f.postCode = 0

	f.dropPost = true
	_, err = gb.Publish(context.Background(), gbpCreds(), PostContent{Text: "x"}, nil)
	if failureKind(t, err) != ErrRejected || !strings.Contains(err.Error(), "may have published") {
		t.Fatalf("lost answer: %v", err)
	}
}

func TestGoogleBusinessValidate(t *testing.T) {
	gb, _ := Get("google_business")
	for _, tc := range []struct {
		name  string
		p     PostContent
		s     Settings
		field string
	}{
		{"no text", PostContent{Media: []MediaFile{{Name: "a.jpg", MimeType: "image/jpeg"}}}, nil, "content"},
		{"too long", PostContent{Text: strings.Repeat("a", 1501)}, nil, "content"},
		{"video", PostContent{Text: "x", Media: []MediaFile{{Name: "a.mp4", MimeType: "video/mp4"}}}, nil, "media"},
		{"two photos", PostContent{Text: "x", Media: []MediaFile{{MimeType: "image/png"}, {MimeType: "image/png"}}}, nil, "media"},
		{"button without link", PostContent{Text: "x"}, Settings{"call_to_action": "BOOK"}, "call_to_action_link"},
		{"unknown button", PostContent{Text: "x"}, Settings{"call_to_action": "DONATE", "call_to_action_link": "https://example.com"}, "call_to_action"},
	} {
		err := gb.Validate(tc.p, tc.s)
		var v *ValidationError
		if !errors.As(err, &v) || v.Field != tc.field {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
}
