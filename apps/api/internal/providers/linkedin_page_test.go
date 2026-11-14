package providers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// useFakeLinkedInPage is fakeLinkedIn plus the member and page-role lookups.
func useFakeLinkedInPage(t *testing.T, acls string) *fakeLinkedIn {
	t.Helper()
	f := useFakeLinkedIn(t)
	f.scope = "r_basicprofile,r_organization_social,rw_organization_admin,w_organization_social"
	f.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v2/me":
			_, _ = w.Write([]byte(`{"id":"782bbtaQ","localizedFirstName":"Jane","localizedLastName":"Doe"}`))
		case r.URL.Path == "/v2/organizationalEntityAcls" && r.Header.Get("Authorization") == "Bearer revoked":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"serviceErrorCode":65601,"code":"REVOKED_ACCESS_TOKEN","status":401}`))
		case r.URL.Path == "/v2/organizationalEntityAcls":
			if r.URL.Query().Get("q") != "roleAssignee" || r.URL.Query().Get("state") != "APPROVED" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(w, acls)
		default:
			f.ServeHTTP(w, r)
		}
	})
	return f
}

const linkedinPageACLs = `{"elements":[
	{"role":"ADMINISTRATOR","organizationalTarget":"urn:li:organization:2414183","organizationalTarget~":{"localizedName":"Jane's Bakery","vanityName":"janes-bakery","logoV2":{"original~":{"elements":[{"identifiers":[{"identifier":"https://media.licdn.com/logo.png"}]}]}}}},
	{"role":"ANALYST","organizationalTarget":"urn:li:organization:5550001","organizationalTarget~":{"localizedName":"Read Only Inc"}},
	{"role":"CONTENT_ADMINISTRATOR","organizationalTarget":"urn:li:organization:777","organizationalTarget~":{"localizedName":"Run Club"}}
]}`

func linkedinPageCreds() Credentials {
	return Credentials{"access_token": "at", "organization_id": "2414183", "expires_at": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)}
}

func TestLinkedInPageConnect(t *testing.T) {
	f := useFakeLinkedInPage(t, linkedinPageACLs)
	lp, _ := Get("linkedin_page")
	if lp.Status != StatusAvailable {
		t.Fatalf("linkedin_page is %q, want available", lp.Status)
	}
	app := App{ClientID: "cid", ClientSecret: "secret"}
	u, _ := lp.AuthURL(app, "https://app.example/cb", "st", nil)
	q, _ := url.Parse(u)
	if !strings.Contains(q.Query().Get("scope"), "w_organization_social") || !strings.Contains(q.Query().Get("scope"), "rw_organization_admin") {
		t.Fatalf("auth url %s", u)
	}
	creds, profile, err := lp.Exchange(context.Background(), app, "https://app.example/cb", "code1", nil)
	if err != nil || creds["access_token"] != "at-authorization_code" || profile.Name != "Jane Doe" {
		t.Fatalf("exchange %v %+v %v", creds, profile, err)
	}

	lister := lp.Provider.(AccountLister)
	accounts, err := lister.Accounts(context.Background(), creds)
	if err != nil || len(accounts) != 2 {
		t.Fatalf("accounts %+v %v", accounts, err)
	}
	a := accounts[0]
	if a.ID != "2414183" || a.Name != "Jane's Bakery" || a.Handle != "janes-bakery" || a.AvatarURL != "https://media.licdn.com/logo.png" ||
		a.ProfileURL != "https://www.linkedin.com/company/janes-bakery/" {
		t.Fatalf("page %+v", a)
	}
	if accounts[1].ID != "777" || accounts[1].Handle != "777" {
		t.Fatalf("content admin page %+v", accounts[1])
	}
	page, pageProfile, err := lister.UseAccount(context.Background(), creds, "2414183")
	if err != nil || page["organization_id"] != "2414183" || page["access_token"] != creds["access_token"] || pageProfile.Handle != "janes-bakery" {
		t.Fatalf("use account %v %+v %v", page, pageProfile, err)
	}
	// An analyst may not post as the page.
	if _, _, err := lister.UseAccount(context.Background(), creds, "5550001"); failureKind(t, err) != ErrRejected {
		t.Fatalf("analyst: %v", err)
	}

	// Without a refresh token the token is checked, and a revoked one asks to reconnect.
	if _, err := lp.Refresh(context.Background(), app, page); err != nil {
		t.Fatal(err)
	}
	page["access_token"] = "revoked"
	if _, err := lp.Refresh(context.Background(), app, page); failureKind(t, err) != ErrReauth {
		t.Fatalf("revoked: %v", err)
	}

	f.scope = "r_basicprofile,w_member_social"
	if _, _, err := lp.Exchange(context.Background(), app, "https://app.example/cb", "code2", nil); failureKind(t, err) != ErrRejected {
		t.Fatalf("no organization scopes: %v", err)
	}
}

func TestLinkedInPageNoPages(t *testing.T) {
	useFakeLinkedInPage(t, `{"elements":[]}`)
	lp, _ := Get("linkedin_page")
	if _, err := lp.Provider.(AccountLister).Accounts(context.Background(), Credentials{"access_token": "at"}); failureKind(t, err) != ErrRejected ||
		!strings.Contains(err.Error(), "administers no pages") {
		t.Fatalf("no pages: %v", err)
	}
}

func TestLinkedInPagePublish(t *testing.T) {
	f := useFakeLinkedInPage(t, linkedinPageACLs)
	lp, _ := Get("linkedin_page")
	img := MediaFile{Name: "a.png", MimeType: "image/png", Size: 3,
		Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("png")), nil }}
	p := PostContent{Text: "We are hiring #bakers", Media: []MediaFile{img}}
	if err := lp.Validate(p, nil); err != nil {
		t.Fatal(err)
	}
	res, err := lp.Publish(context.Background(), linkedinPageCreds(), p, nil)
	if err != nil || res.ExternalID != "urn:li:share:7100" {
		t.Fatalf("publish %+v %v", res, err)
	}
	if f.post["author"] != "urn:li:organization:2414183" || f.post["visibility"] != "PUBLIC" {
		t.Fatalf("post %v", f.post)
	}

	res, err = lp.Provider.(Replier).Reply(context.Background(), linkedinPageCreds(), res.ExternalID, PostContent{Text: "Apply on our site"}, nil)
	if err != nil || f.comment["actor"] != "urn:li:organization:2414183" || f.comment["object"] != "urn:li:share:7100" {
		t.Fatalf("comment %v %v", f.comment, err)
	}

	for _, tc := range []struct {
		code int
		body string
		kind error
	}{
		{401, `{"serviceErrorCode":65601,"code":"REVOKED_ACCESS_TOKEN","status":401}`, ErrReauth},
		{403, `{"message":"Not enough permissions to access: ugcPosts.CREATE","status":403}`, ErrRejected},
		{429, `{"message":"Too many requests","status":429}`, nil},
	} {
		f.postCode, f.postBody = tc.code, tc.body
		_, err := lp.Publish(context.Background(), linkedinPageCreds(), PostContent{Text: "x"}, nil)
		var fl *Failure
		if !errors.As(err, &fl) || fl.Kind != tc.kind {
			t.Errorf("%d: %v", tc.code, err)
		}
	}

	if err := lp.Validate(PostContent{Text: "x", Media: []MediaFile{img, {Name: "v.mp4", MimeType: "video/mp4"}}}, nil); err == nil {
		t.Fatal("pictures and a video passed")
	}
	if _, err := lp.Publish(context.Background(), Credentials{"access_token": "at"}, PostContent{Text: "x"}, nil); failureKind(t, err) != ErrReauth {
		t.Fatalf("no page stored: %v", err)
	}
}
