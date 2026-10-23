package providers

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// LinkedInPage (a company or showcase page) over the same versioned REST API as LinkedIn: OAuth 2.0 with the
// organization scopes of LinkedIn's Community Management API, which LinkedIn reviews before an app may use it. The
// sign-in is a member's; the pages they administer are listed (organizationalEntityAcls) and each becomes a channel
// that posts, uploads and comments as urn:li:organization:<id>. Tokens last 60 days; refresh tokens come with the
// approved product (365 days), and without one the channel is reconnected when its token runs out.
type LinkedInPage struct{ Base }

const linkedinPageScopes = "r_basicprofile w_organization_social r_organization_social rw_organization_admin"

// linkedinPageRoles are the page roles that may post as the page.
var linkedinPageRoles = map[string]bool{"ADMINISTRATOR": true, "CONTENT_ADMINISTRATOR": true}

func init() {
	Register(LinkedInPage{NewBase("linkedin_page", "LinkedIn Page", AuthOAuth2,
		Limits{MaxChars: 3000, MaxMedia: 9, MediaKinds: []string{"image", "gif", "video"}})}, StatusAvailable)
}

var (
	_ AccountLister = LinkedInPage{}
	_ Replier       = LinkedInPage{}
	_ AppChecker    = LinkedInPage{}
)

func (l LinkedInPage) Validate(p PostContent, s Settings) error {
	if err := CheckLimits(l.Limits(), p, s); err != nil {
		return err
	}
	for _, m := range p.Media {
		if strings.HasPrefix(m.MimeType, "video/") && len(p.Media) > 1 {
			return invalid("media", "LinkedIn takes up to 9 pictures or one video")
		}
	}
	return CheckFields(l.SettingsFields(), s)
}

func (LinkedInPage) AuthURL(app App, redirect, state string, _ map[string]string) (string, error) {
	q := url.Values{
		"response_type": {"code"},
		"client_id":     {app.ClientID},
		"redirect_uri":  {redirect},
		"scope":         {linkedinPageScopes},
		"state":         {state},
	}
	return linkedinOAuth + "/authorization?" + q.Encode(), nil
}

// The keys are checked the same way as LinkedIn's (an app token request).
func (LinkedInPage) CheckApp(ctx context.Context, app App, redirect string) (bool, error) {
	return LinkedIn{}.CheckApp(ctx, app, redirect)
}

func (LinkedInPage) Exchange(ctx context.Context, app App, redirect, code string, _ map[string]string) (Credentials, Profile, error) {
	t, err := LinkedIn{}.token(ctx, app, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect}})
	if err != nil {
		return nil, Profile{}, err
	}
	if t.Scope != "" && (!strings.Contains(t.Scope, "w_organization_social") || !strings.Contains(t.Scope, "rw_organization_admin")) {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "LinkedIn did not allow posting as a page: the app needs LinkedIn's Community Management API; connect again once it has it"}
	}
	creds := t.into(Credentials{}, time.Now())
	// Who signed in, for the picker; the pages are what matter, so a profile LinkedIn won't show is no failure.
	var me struct {
		ID        string `json:"id"`
		FirstName string `json:"localizedFirstName"`
		LastName  string `json:"localizedLastName"`
	}
	req, err := linkedinJSON(ctx, t.AccessToken, http.MethodGet, linkedinAPI+"/v2/me", nil)
	if err != nil {
		return nil, Profile{}, err
	}
	if _, err := linkedinDo(ctx, req, &me); err != nil {
		if errors.Is(err, ErrReauth) {
			return nil, Profile{}, err
		}
	}
	creds["person_id"] = me.ID
	return creds, Profile{Name: strings.TrimSpace(me.FirstName + " " + me.LastName)}, nil
}

// Refresh renews the token when LinkedIn gave a refresh token; otherwise it checks that LinkedIn still takes it.
func (LinkedInPage) Refresh(ctx context.Context, app App, c Credentials) (Credentials, error) {
	return LinkedIn{}.refresh(ctx, app, c, linkedinAPI+"/v2/organizationalEntityAcls?q=roleAssignee&state=APPROVED&count=1")
}

// Accounts lists the pages the member administers (or creates content for), with their names and logos.
func (LinkedInPage) Accounts(ctx context.Context, c Credentials) ([]Account, error) {
	token := c["access_token"]
	var list []Account
	seen := map[string]bool{}
	const count = 100
	for start := 0; start < 1000; start += count {
		var out struct {
			Elements []struct {
				Role   string `json:"role"`
				Target string `json:"organizationalTarget"`
				Org    struct {
					Name   string `json:"localizedName"`
					Vanity string `json:"vanityName"`
					Logo   struct {
						Original struct {
							Elements []struct {
								Identifiers []struct {
									Identifier string `json:"identifier"`
								} `json:"identifiers"`
							} `json:"elements"`
						} `json:"original~"`
					} `json:"logoV2"`
				} `json:"organizationalTarget~"`
			} `json:"elements"`
		}
		q := "q=roleAssignee&state=APPROVED&start=" + strconv.Itoa(start) + "&count=" + strconv.Itoa(count) +
			"&projection=(elements*(role,organizationalTarget~(localizedName,vanityName,logoV2(original~:playableStreams))))"
		req, err := linkedinJSON(ctx, token, http.MethodGet, linkedinAPI+"/v2/organizationalEntityAcls?"+q, nil)
		if err != nil {
			return nil, err
		}
		if _, err := linkedinDo(ctx, req, &out); err != nil {
			return nil, err
		}
		for _, e := range out.Elements {
			id, ok := strings.CutPrefix(e.Target, "urn:li:organization:")
			if !ok || !linkedinPageRoles[e.Role] || seen[id] {
				continue // a showcase or school page answers with another URN kind; roles like analyst can't post
			}
			seen[id] = true
			a := Account{ID: id, Name: e.Org.Name, Handle: e.Org.Vanity, ProfileURL: "https://www.linkedin.com/company/" + id + "/"}
			if a.Name == "" {
				a.Name = "Page " + id
			}
			if a.Handle == "" {
				a.Handle = id
			} else {
				a.ProfileURL = "https://www.linkedin.com/company/" + a.Handle + "/"
			}
			if els := e.Org.Logo.Original.Elements; len(els) > 0 && len(els[0].Identifiers) > 0 {
				a.AvatarURL = els[0].Identifiers[0].Identifier
			}
			list = append(list, a)
		}
		if len(out.Elements) < count {
			break
		}
	}
	if len(list) == 0 {
		return nil, &Failure{Kind: ErrRejected, Msg: "This LinkedIn member administers no pages: ask a page's admin to make you an admin or content admin (page admin tools -> Manage admins), then connect again"}
	}
	return list, nil
}

// UseAccount keeps the member's sign-in and names the page it posts as.
func (l LinkedInPage) UseAccount(ctx context.Context, c Credentials, accountID string) (Credentials, Profile, error) {
	list, err := l.Accounts(ctx, c)
	if err != nil {
		return nil, Profile{}, err
	}
	for _, a := range list {
		if a.ID == accountID {
			out := Credentials{}
			for k, v := range c {
				out[k] = v
			}
			out["organization_id"] = a.ID
			return out, Profile{Handle: a.Handle, Name: a.Name, AvatarURL: a.AvatarURL, ProfileURL: a.ProfileURL}, nil
		}
	}
	return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "This LinkedIn member can't post as that page; connect again with one of its admins"}
}

func (LinkedInPage) Publish(ctx context.Context, c Credentials, p PostContent, _ Settings) (Result, error) {
	token, org := c["access_token"], c["organization_id"]
	if token == "" || org == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "LinkedIn: no page stored; reconnect the account"}
	}
	return linkedinPublish(ctx, token, "urn:li:organization:"+org, "PUBLIC", p)
}

// Reply comments under the post as the page (see LinkedIn.Reply).
func (LinkedInPage) Reply(ctx context.Context, c Credentials, parentExternalID string, p PostContent, _ Settings) (Result, error) {
	token, org := c["access_token"], c["organization_id"]
	if token == "" || org == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "LinkedIn: no page stored; reconnect the account"}
	}
	return linkedinComment(ctx, token, "urn:li:organization:"+org, parentExternalID, p)
}
