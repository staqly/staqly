package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// ListMonk sends a post as an email campaign from a self-hosted listmonk (v4 or newer: API users) over its REST API,
// signed in with an API user and its token (HTTP Basic). A post's text is the email in Markdown, its title setting
// the subject; the campaign goes to the lists the post names and starts at once, or stays a draft.
//
// Campaigns are named after the post ("<subject> [staqly <post id>]"), so an attempt after one whose answer was lost
// finds the campaign it made instead of making a second one.
type ListMonk struct{ Base }

func init() {
	Register(ListMonk{NewBase("listmonk", "ListMonk", AuthToken, Limits{MaxChars: 0, NeedsTitle: true}).WithSettings(
		Field{Key: "title", Label: "Subject", Kind: "text", Required: true, MaxLength: 500},
		Field{Key: "lists", Label: "Lists", Kind: "list", Required: true, Placeholder: "Newsletter",
			Help: "Names or ids of the listmonk lists the campaign goes to."},
		Field{Key: "send", Label: "When", Kind: "select", Default: "now", Options: []Option{
			{Value: "now", Label: "Send the campaign"},
			{Value: "draft", Label: "Leave it as a draft in listmonk"},
		}},
		Field{Key: "template_id", Label: "Template id", Kind: "number",
			Help: "A listmonk campaign template; empty for the default one."},
	)}, StatusAvailable)
}

func (ListMonk) CredentialFields() []Field {
	return []Field{
		{Key: "url", Label: "listmonk address", Kind: "url", Required: true, Placeholder: "https://lists.example.com"},
		{Key: "username", Label: "API user", Kind: "text", Required: true,
			Help: "listmonk → Admin → Users: a user of type API with a role that manages campaigns and reads lists."},
		{Key: "token", Label: "API token", Kind: "secret", Required: true, Help: "Shown once when the API user is created."},
	}
}

func (l ListMonk) Validate(p PostContent, s Settings) error {
	if err := CheckLimits(l.Limits(), p, s); err != nil {
		return err
	}
	if err := blogTitled(s); err != nil {
		return err
	}
	if strings.TrimSpace(p.Text) == "" {
		return invalid("content", "write the email")
	}
	if v := strings.TrimSpace(s["template_id"]); v != "" {
		if n, err := strconv.Atoi(v); err != nil || n < 0 {
			return invalid("template_id", "the template id is a whole number")
		}
	}
	return CheckFields(l.SettingsFields(), s)
}

type listmonkSite struct{ base, user, token string }

func (s listmonkSite) call(ctx context.Context, method, path string, body, out any) error {
	req, err := blogJSON(ctx, method, s.base+path, body)
	if err != nil {
		return err
	}
	return listmonkDo(ctx, s, req, out)
}

func listmonkDo(ctx context.Context, s listmonkSite, req *http.Request, out any) error {
	req.SetBasicAuth(s.user, s.token)
	return blogDo(ctx, "listmonk", req, out, listmonkError)
}

type listmonkList struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// lists reads the lists the API user may use. listmonk answers {"data":[...]} (minimal) or {"data":{"results":[...]}}.
func (s listmonkSite) lists(ctx context.Context) ([]listmonkList, error) {
	var out struct {
		Data json.RawMessage `json:"data"`
	}
	if err := s.call(ctx, http.MethodGet, "/api/lists?minimal=true&per_page=all", nil, &out); err != nil {
		return nil, err
	}
	var lists []listmonkList
	if err := json.Unmarshal(out.Data, &lists); err == nil {
		return lists, nil
	}
	var paged struct {
		Results []listmonkList `json:"results"`
	}
	if err := json.Unmarshal(out.Data, &paged); err != nil {
		return nil, &Failure{Kind: ErrRejected, Msg: "listmonk answered the lists in a shape staqly doesn't know (" + blogSnippet(out.Data) + ")"}
	}
	return paged.Results, nil
}

// Connect checks the API user by reading its lists.
func (ListMonk) Connect(ctx context.Context, f map[string]string) (Credentials, Profile, error) {
	u, err := blogInstance("url", f["url"])
	if err != nil {
		return nil, Profile{}, err
	}
	user, token := strings.TrimSpace(f["username"]), strings.TrimSpace(f["token"])
	if user == "" {
		return nil, Profile{}, invalid("username", "enter the API user's name")
	}
	if token == "" {
		return nil, Profile{}, invalid("token", "paste the API token")
	}
	site := listmonkSite{base: u.String(), user: user, token: token}
	if _, err := site.lists(ctx); err != nil {
		return nil, Profile{}, blogRetry(err)
	}
	host := strings.TrimPrefix(strings.TrimPrefix(site.base, "https://"), "http://")
	return Credentials{"url": site.base, "username": user, "token": token},
		Profile{Handle: user + "@" + host, Name: "listmonk at " + host, ProfileURL: site.base + "/admin"}, nil
}

type listmonkCampaign struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

func (ListMonk) Publish(ctx context.Context, c Credentials, p PostContent, s Settings) (Result, error) {
	site := listmonkSite{base: strings.TrimRight(c["url"], "/"), user: c["username"], token: c["token"]}
	if site.base == "" || site.user == "" || site.token == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "listmonk: no API user stored; reconnect the channel"}
	}
	result := func(id int) Result {
		return Result{ExternalID: strconv.Itoa(id), URL: site.base + "/admin/campaigns/" + strconv.Itoa(id)}
	}
	subject := strings.TrimSpace(s["title"])
	name := subject
	if p.PostID != "" {
		name += " [staqly " + p.PostID + "]"
	}
	send := s["send"] != "draft"

	// An earlier attempt may have made (and started) the campaign before its answer was lost.
	var camp listmonkCampaign
	if p.PostID != "" {
		found, err := site.find(ctx, p.PostID, name)
		if err != nil {
			return Result{}, blogRetry(err)
		}
		camp = found
	}
	switch camp.Status {
	case "running", "finished", "scheduled":
		return result(camp.ID), nil
	case "paused", "cancelled":
		return Result{}, &Failure{Kind: ErrRejected, Msg: "listmonk: this post's campaign was " + camp.Status + " in listmonk; resume it there"}
	}

	if camp.ID == 0 {
		ids, err := site.listIDs(ctx, ListValues(s["lists"]))
		if err != nil {
			return Result{}, blogRetry(err)
		}
		body := map[string]any{
			"name":         name,
			"subject":      subject,
			"lists":        ids,
			"type":         "regular",
			"content_type": "markdown",
			"body":         p.Text,
		}
		if v := strings.TrimSpace(s["template_id"]); v != "" && v != "0" {
			n, _ := strconv.Atoi(v)
			body["template_id"] = n
		}
		var out struct {
			Data listmonkCampaign `json:"data"`
		}
		if err := site.call(ctx, http.MethodPost, "/api/campaigns", body, &out); err != nil {
			if p.PostID != "" {
				return Result{}, blogRetry(err) // the next attempt finds a campaign this one made
			}
			return Result{}, listmonkLost(err, "may have made the campaign")
		}
		if out.Data.ID == 0 {
			return Result{}, &Failure{Kind: ErrRejected, Msg: "listmonk made the campaign but did not say its id; check the campaigns in listmonk"}
		}
		camp = out.Data
	}
	if !send {
		return result(camp.ID), nil
	}
	err := site.call(ctx, http.MethodPut, "/api/campaigns/"+strconv.Itoa(camp.ID)+"/status", map[string]string{"status": "running"}, nil)
	if err != nil {
		if p.PostID != "" {
			return Result{}, blogRetry(err) // the next attempt sees whether it started
		}
		return Result{}, listmonkLost(err, "may have started sending the campaign")
	}
	return result(camp.ID), nil
}

// listmonkLost: without a post id to find the campaign again, an answer that never came can't be retried safely.
func listmonkLost(err error, what string) error {
	var f *Failure
	if errors.As(err, &f) || errors.Is(err, context.Canceled) {
		return err
	}
	return &Failure{Kind: ErrRejected, Msg: "listmonk " + what + ", but its answer was lost (" + err.Error() + "); check the campaigns in listmonk before posting it again"}
}

// find looks for this post's campaign by its name.
func (s listmonkSite) find(ctx context.Context, postID, name string) (listmonkCampaign, error) {
	var out struct {
		Data struct {
			Results []listmonkCampaign `json:"results"`
		} `json:"data"`
	}
	q := url.Values{"query": {postID}, "per_page": {"50"}, "no_body": {"true"}}
	if err := s.call(ctx, http.MethodGet, "/api/campaigns?"+q.Encode(), nil, &out); err != nil {
		return listmonkCampaign{}, err
	}
	for _, c := range out.Data.Results {
		if c.Name == name {
			return c, nil
		}
	}
	return listmonkCampaign{}, nil
}

// listIDs turns list names or ids into ids the API user may use.
func (s listmonkSite) listIDs(ctx context.Context, want []string) ([]int, error) {
	lists, err := s.lists(ctx)
	if err != nil {
		return nil, err
	}
	var ids []int
	for _, w := range want {
		id := 0
		for _, l := range lists {
			if strings.EqualFold(l.Name, w) || strconv.Itoa(l.ID) == w {
				id = l.ID
				break
			}
		}
		if id == 0 {
			var names []string
			for _, l := range lists {
				names = append(names, l.Name)
			}
			if len(names) == 0 {
				return nil, &Failure{Kind: ErrRejected, Msg: "listmonk has no list " + w + ", and the API user may use none; give its role access to the lists"}
			}
			return nil, &Failure{Kind: ErrRejected, Msg: "listmonk has no list " + w + " for this API user; use one of: " + strings.Join(names, ", ")}
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, &Failure{Kind: ErrRejected, Msg: "listmonk: name at least one list"}
	}
	return ids, nil
}

// listmonkError maps listmonk's replies ({"message"}) to what staqly does next.
func listmonkError(status int, raw []byte) error {
	var e struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &e)
	detail := e.Message
	if detail == "" {
		detail = blogSnippet(raw)
	}
	lower := strings.ToLower(detail)
	switch {
	case status == http.StatusForbidden && strings.Contains(lower, "permission"):
		return &Failure{Kind: ErrRejected, Msg: "listmonk refused it (" + detail + "); give the API user's role the campaigns and lists permissions"}
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return &Failure{Kind: ErrReauth, Msg: "listmonk did not accept the API user and token (" + detail + "); reconnect with a valid token"}
	case status == http.StatusTooManyRequests:
		return &Failure{Msg: "listmonk rate limit reached; staqly tries again in a few minutes"}
	case status >= 500:
		return &Failure{Msg: fmt.Sprintf("listmonk is unavailable (%d: %s); staqly tries again in a few minutes", status, detail)}
	case status == http.StatusNotFound:
		return &Failure{Kind: ErrRejected, Msg: "listmonk's API did not answer at this address (" + detail + "); check the address"}
	}
	return &Failure{Kind: ErrRejected, Msg: "listmonk: " + detail}
}
