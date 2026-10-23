package providers

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// Google Business Profile (formerly Google My Business): Google's OAuth 2.0 (google.go) with the business.manage
// scope, one location per staqly channel (an AccountLister over the Account Management API's accounts and the
// Business Information API's locations), posts through the v4 localPosts endpoint. Google fetches a post's photo
// itself from a link, so a photo needs a public address (MediaFile.SignedURL: S3-compatible storage). Google must
// approve the Cloud project for the Business Profile APIs first: until then every call is refused with a quota of 0.
type GoogleBusiness struct{ Base }

// Endpoints, variables so tests point them at httptest.
var (
	gbpAccountsAPI = "https://mybusinessaccountmanagement.googleapis.com"
	gbpInfoAPI     = "https://mybusinessbusinessinformation.googleapis.com"
	gbpPostsAPI    = "https://mybusiness.googleapis.com"
)

const (
	gbpScope  = "https://www.googleapis.com/auth/business.manage"
	gbpName   = "Google Business Profile"
	gbpFields = "name,title,storefrontAddress,metadata"
)

// gbpActions are the buttons a post may carry (LocalPost.callToAction.actionType).
var gbpActions = []Option{
	{Value: "none", Label: "No button"},
	{Value: "LEARN_MORE", Label: "Learn more"},
	{Value: "BOOK", Label: "Book"},
	{Value: "ORDER", Label: "Order online"},
	{Value: "SHOP", Label: "Buy"},
	{Value: "SIGN_UP", Label: "Sign up"},
	{Value: "CALL", Label: "Call now"},
}

func init() {
	Register(GoogleBusiness{NewBase("google_business", gbpName, AuthOAuth2,
		Limits{MaxChars: 1500, MaxMedia: 1, MediaKinds: []string{"image"}}).WithSettings(
		Field{Key: "call_to_action", Label: "Button", Kind: "select", Default: "none", Options: gbpActions,
			Help: "A button under the post. Call now dials the phone number of the business profile."},
		Field{Key: "call_to_action_link", Label: "Button link", Kind: "url", Placeholder: "https://example.com/book",
			Help: "Where the button leads (every button but Call now needs one)."},
	)}, StatusAvailable)
}

var (
	_ AccountLister = GoogleBusiness{}
	_ AppChecker    = GoogleBusiness{}
	_ Needer        = GoogleBusiness{}
)

// Google Business Profile downloads a post's photo from a link; http return addresses work for localhost only,
// which the Cloud console checks itself.
func (GoogleBusiness) Needs() Needs { return Needs{PublicMedia: true} }

func (g GoogleBusiness) Validate(p PostContent, s Settings) error {
	if err := CheckLimits(g.Limits(), p, s); err != nil {
		return err
	}
	if strings.TrimSpace(p.Text) == "" {
		return invalid("content", "Google Business Profile posts need text")
	}
	for _, m := range p.Media {
		if m.MimeType != "image/jpeg" && m.MimeType != "image/png" {
			return invalid("media", "Google Business Profile takes one JPEG or PNG picture")
		}
	}
	action := s["call_to_action"]
	if action != "" && action != "none" && action != "CALL" && strings.TrimSpace(s["call_to_action_link"]) == "" {
		return invalid("call_to_action_link", "this button needs a link")
	}
	return CheckFields(g.SettingsFields(), s)
}

func (GoogleBusiness) AuthURL(app App, redirect, state string, extra map[string]string) (string, error) {
	return googleAuthURL(app, redirect, state, googleIdentity+" "+gbpScope, extra), nil
}

func (GoogleBusiness) Exchange(ctx context.Context, app App, redirect, code string, extra map[string]string) (Credentials, Profile, error) {
	return googleExchange(ctx, gbpName, app, redirect, code, extra, gbpScope,
		"Google did not allow managing the business: connect again and leave every permission Google asks for ticked")
}

// Refresh renews the hour-long access token with the refresh token.
func (GoogleBusiness) Refresh(ctx context.Context, app App, c Credentials) (Credentials, error) {
	return googleRefresh(ctx, gbpName, app, c)
}

func (GoogleBusiness) CheckApp(ctx context.Context, app App, redirect string) (bool, error) {
	return googleCheckApp(ctx, gbpName, app, redirect)
}

type gbpLocation struct {
	Name              string `json:"name"` // locations/{id}
	Title             string `json:"title"`
	StorefrontAddress struct {
		Locality     string   `json:"locality"`
		AddressLines []string `json:"addressLines"`
	} `json:"storefrontAddress"`
	Metadata struct {
		MapsURI string `json:"mapsUri"`
	} `json:"metadata"`
}

// account is the location as a picker entry; its id is the v4 resource name the posts API takes
// (accounts/{account}/locations/{location}).
func (l gbpLocation) account(accountName string) Account {
	id := strings.TrimPrefix(l.Name, "locations/")
	name := l.Title
	if name == "" {
		name = "Location " + id
	}
	if l.StorefrontAddress.Locality != "" {
		name += ", " + l.StorefrontAddress.Locality
	}
	return Account{ID: accountName + "/locations/" + id, Name: name, Handle: l.Title + " (" + id + ")", ProfileURL: l.Metadata.MapsURI}
}

// Accounts lists every location of every business account the sign-in manages.
func (GoogleBusiness) Accounts(ctx context.Context, c Credentials) ([]Account, error) {
	token := c["access_token"]
	var accounts []string
	page := ""
	for {
		var out struct {
			Accounts []struct {
				Name string `json:"name"`
			} `json:"accounts"`
			NextPageToken string `json:"nextPageToken"`
		}
		q := url.Values{"pageSize": {"20"}}
		if page != "" {
			q.Set("pageToken", page)
		}
		if err := googleGet(ctx, gbpName, token, gbpAccountsAPI+"/v1/accounts?"+q.Encode(), &out); err != nil {
			return nil, gbpApproval(err)
		}
		for _, a := range out.Accounts {
			accounts = append(accounts, a.Name)
		}
		if page = out.NextPageToken; page == "" {
			break
		}
	}
	var list []Account
	var lastErr error
	for _, a := range accounts {
		page := ""
		for {
			var out struct {
				Locations     []gbpLocation `json:"locations"`
				NextPageToken string        `json:"nextPageToken"`
			}
			q := url.Values{"readMask": {gbpFields}, "pageSize": {"100"}}
			if page != "" {
				q.Set("pageToken", page)
			}
			if err := googleGet(ctx, gbpName, token, gbpInfoAPI+"/v1/"+a+"/locations?"+q.Encode(), &out); err != nil {
				// One business account the person may not read (a group they were removed from) hides the others' locations.
				lastErr = err
				break
			}
			for _, l := range out.Locations {
				list = append(list, l.account(a))
			}
			if page = out.NextPageToken; page == "" {
				break
			}
		}
	}
	if len(list) == 0 && lastErr != nil {
		return nil, gbpApproval(lastErr)
	}
	if len(list) == 0 {
		return nil, &Failure{Kind: ErrRejected, Msg: "This Google account manages no business locations: add the business on business.google.com (or ask its owner to make you a manager), then connect again"}
	}
	return list, nil
}

// gbpApproval explains the rate limit a Cloud project gets before Google approves it for the Business Profile APIs
// (a quota of 0 requests a minute).
func gbpApproval(err error) error {
	var f *Failure
	if errors.As(err, &f) && f.Kind == nil && strings.Contains(f.Msg, "rate limit") {
		// For whoever runs the server: request access at https://developers.google.com/my-business/content/prereqs
		// and enable the Business Profile APIs in the OAuth client's Cloud project.
		return &Failure{Msg: "Google Business Profile refused the request: Google approves each app for its Business Profile APIs before it may use them, so ask whoever runs this server whether it has; otherwise try again in a few minutes"}
	}
	return err
}

// UseAccount keeps the sign-in and names the location it publishes to.
func (GoogleBusiness) UseAccount(ctx context.Context, c Credentials, accountID string) (Credentials, Profile, error) {
	parts := strings.Split(accountID, "/")
	if len(parts) != 4 || parts[0] != "accounts" || parts[2] != "locations" || parts[1] == "" || parts[3] == "" {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "Google Business Profile: not a location (" + accountID + ")"}
	}
	var l gbpLocation
	if err := googleGet(ctx, gbpName, c["access_token"], gbpInfoAPI+"/v1/locations/"+url.PathEscape(parts[3])+"?readMask="+gbpFields, &l); err != nil {
		return nil, Profile{}, gbpApproval(err)
	}
	if l.Name == "" {
		l.Name = "locations/" + parts[3]
	}
	a := l.account(parts[0] + "/" + parts[1])
	return googleCopy(c, "location", accountID), Profile{Handle: a.Handle, Name: a.Name, ProfileURL: a.ProfileURL}, nil
}

func (GoogleBusiness) Publish(ctx context.Context, c Credentials, p PostContent, s Settings) (Result, error) {
	token, location := c["access_token"], c["location"]
	if token == "" || location == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "Google Business Profile: no location stored; reconnect the account"}
	}
	body := map[string]any{"summary": p.Text, "topicType": "STANDARD"}
	for _, m := range p.Media {
		if m.SignedURL == "" {
			// For whoever runs the server: Google downloads the photo itself, so files must be in S3-compatible
			// storage (STORAGE_DRIVER=s3) at an address it can reach.
			return Result{}, &Failure{Kind: ErrRejected, Msg: "Google Business Profile downloads a post's photo from a link, and this server keeps it where Google can't reach; ask whoever runs it to store media in the cloud, or post without the photo"}
		}
		body["media"] = []map[string]string{{"mediaFormat": "PHOTO", "sourceUrl": m.SignedURL}}
	}
	if action := s["call_to_action"]; action != "" && action != "none" {
		cta := map[string]string{"actionType": action}
		if action != "CALL" {
			cta["url"] = strings.TrimSpace(s["call_to_action_link"])
		}
		body["callToAction"] = cta
	}
	req, err := googleRequest(ctx, token, http.MethodPost, gbpPostsAPI+"/v4/"+location+"/localPosts", body)
	if err != nil {
		return Result{}, err
	}
	var out struct {
		Name      string `json:"name"`
		State     string `json:"state"`
		SearchURL string `json:"searchUrl"`
	}
	// Google has no idempotency key: publishing again after an answer that never came could post twice.
	if _, err := googleDo(ctx, gbpName, req, &out); err != nil {
		var f *Failure
		if !errors.As(err, &f) {
			return Result{}, &Failure{Kind: ErrRejected, Msg: "Google Business Profile may have published the post, but its answer was lost (" + err.Error() + "); check the business profile before posting it again"}
		}
		return Result{}, err
	}
	if out.State == "REJECTED" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Google Business Profile rejected the post under its content policies (phone numbers in the text are refused, for example)"}
	}
	if out.Name == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Google Business Profile published the post but did not say its id; check the business profile"}
	}
	link := out.SearchURL
	if link == "" {
		link = "https://business.google.com/"
	}
	return Result{ExternalID: out.Name, URL: link}, nil
}
