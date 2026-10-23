package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeListMonk answers like listmonk's REST API.
type fakeListMonk struct {
	mu         sync.Mutex
	paged      bool // answer the lists as {"results":[...]}
	campaigns  []map[string]any
	created    map[string]any // the last campaign body
	statuses   []string       // status changes sent
	createCode int
	createBody string
	dropCreate bool // make the campaign, then close the connection
	dropStatus bool // start it, then close the connection
	srv        *httptest.Server
}

func (f *fakeListMonk) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if u, p, ok := r.BasicAuth(); !ok || u != "staqly" || p != "tok" {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"invalid API credentials"}`))
		return
	}
	switch {
	case r.URL.Path == "/api/lists":
		lists := `[{"id":1,"name":"Newsletter"},{"id":2,"name":"Customers"}]`
		if f.paged {
			_, _ = w.Write([]byte(`{"data":{"results":` + lists + `,"total":2}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":` + lists + `}`))
	case r.URL.Path == "/api/campaigns" && r.Method == http.MethodGet:
		q := r.URL.Query().Get("query")
		var out []map[string]any
		for _, c := range f.campaigns {
			if strings.Contains(c["name"].(string), q) {
				out = append(out, c)
			}
		}
		b, _ := json.Marshal(map[string]any{"data": map[string]any{"results": out}})
		_, _ = w.Write(b)
	case r.URL.Path == "/api/campaigns" && r.Method == http.MethodPost:
		f.created = nil
		_ = json.NewDecoder(r.Body).Decode(&f.created)
		if f.createCode != 0 {
			w.WriteHeader(f.createCode)
			_, _ = w.Write([]byte(f.createBody))
			return
		}
		c := map[string]any{"id": 10 + len(f.campaigns), "name": f.created["name"], "status": "draft"}
		f.campaigns = append(f.campaigns, c)
		if f.dropCreate {
			f.dropCreate = false
			blogDropConn(w)
			return
		}
		b, _ := json.Marshal(map[string]any{"data": c})
		_, _ = w.Write(b)
	case strings.HasSuffix(r.URL.Path, "/status") && r.Method == http.MethodPut:
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.statuses = append(f.statuses, r.URL.Path+" "+body["status"])
		for _, c := range f.campaigns {
			if "/api/campaigns/"+strconv.Itoa(c["id"].(int))+"/status" == r.URL.Path {
				c["status"] = body["status"]
			}
		}
		if f.dropStatus {
			f.dropStatus = false
			blogDropConn(w)
			return
		}
		_, _ = w.Write([]byte(`{"data":{}}`))
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Unknown endpoint"}`))
	}
}

func newFakeListMonk(t *testing.T) *fakeListMonk {
	f := &fakeListMonk{}
	f.srv = httptest.NewServer(f)
	t.Cleanup(f.srv.Close)
	return f
}

func TestListMonkConnect(t *testing.T) {
	f := newFakeListMonk(t)
	l, _ := Get("listmonk")
	if l.Status != StatusAvailable {
		t.Fatal("listmonk is not available")
	}
	fields := map[string]string{"url": f.srv.URL + "/", "username": "staqly", "token": "tok"}
	c, p, err := l.Connect(context.Background(), fields)
	if err != nil {
		t.Fatal(err)
	}
	host := strings.TrimPrefix(f.srv.URL, "http://")
	if c["url"] != f.srv.URL || p.Handle != "staqly@"+host || p.ProfileURL != f.srv.URL+"/admin" {
		t.Fatalf("creds %v profile %+v", c, p)
	}
	f.paged = true
	if _, _, err := l.Connect(context.Background(), map[string]string(c)); err != nil {
		t.Fatal(err)
	}
	fields["token"] = "wrong"
	_, _, err = l.Connect(context.Background(), fields)
	blogWantKind(t, err, ErrReauth, "API user")
}

func TestListMonkPublish(t *testing.T) {
	f := newFakeListMonk(t)
	l, _ := Get("listmonk")
	creds := Credentials{"url": f.srv.URL, "username": "staqly", "token": "tok"}
	s := Settings{"title": "October news", "lists": "newsletter, 2", "template_id": "3"}
	post := PostContent{PostID: "pst_1", Text: "Hello **readers**"}
	if err := l.Validate(post, s); err != nil {
		t.Fatal(err)
	}
	res, err := l.Publish(context.Background(), creds, post, s)
	if err != nil {
		t.Fatal(err)
	}
	if res.ExternalID != "10" || res.URL != f.srv.URL+"/admin/campaigns/10" {
		t.Fatalf("result %+v", res)
	}
	c := f.created
	lists, _ := c["lists"].([]any)
	if c["name"] != "October news [staqly pst_1]" || c["subject"] != "October news" || c["content_type"] != "markdown" ||
		c["body"] != "Hello **readers**" || c["template_id"] != float64(3) || len(lists) != 2 || lists[0] != float64(1) || lists[1] != float64(2) {
		t.Fatalf("campaign %v", c)
	}
	if strings.Join(f.statuses, ",") != "/api/campaigns/10/status running" {
		t.Fatalf("statuses %v", f.statuses)
	}

	// The same post again (a retry after its answer was lost): the running campaign is the result.
	res, err = l.Publish(context.Background(), creds, post, s)
	if err != nil || res.ExternalID != "10" || len(f.campaigns) != 1 || len(f.statuses) != 1 {
		t.Fatalf("again: %+v %v campaigns %d statuses %v", res, err, len(f.campaigns), f.statuses)
	}

	// A draft stays a draft.
	res, err = l.Publish(context.Background(), creds, PostContent{PostID: "pst_2", Text: "x"}, Settings{"title": "Draft", "lists": "Customers", "send": "draft"})
	if err != nil || res.ExternalID != "11" || len(f.statuses) != 1 || f.created["template_id"] != nil {
		t.Fatalf("draft: %+v %v statuses %v", res, err, f.statuses)
	}
}

func TestListMonkLostAnswers(t *testing.T) {
	f := newFakeListMonk(t)
	l, _ := Get("listmonk")
	creds := Credentials{"url": f.srv.URL, "username": "staqly", "token": "tok"}
	s := Settings{"title": "News", "lists": "Newsletter"}

	// The campaign was made but the answer lost: retried, and the next attempt starts the one made.
	f.dropCreate = true
	_, err := l.Publish(context.Background(), creds, PostContent{PostID: "pst_1", Text: "x"}, s)
	blogWantKind(t, err, nil, "could not be reached")
	res, err := l.Publish(context.Background(), creds, PostContent{PostID: "pst_1", Text: "x"}, s)
	if err != nil || res.ExternalID != "10" || len(f.campaigns) != 1 || len(f.statuses) != 1 {
		t.Fatalf("after a lost create: %+v %v campaigns %d", res, err, len(f.campaigns))
	}

	// Started but the answer lost: the next attempt sees it running.
	f.dropStatus = true
	_, err = l.Publish(context.Background(), creds, PostContent{PostID: "pst_2", Text: "x"}, s)
	blogWantKind(t, err, nil, "")
	res, err = l.Publish(context.Background(), creds, PostContent{PostID: "pst_2", Text: "x"}, s)
	if err != nil || res.ExternalID != "11" || len(f.statuses) != 2 {
		t.Fatalf("after a lost start: %+v %v statuses %v", res, err, f.statuses)
	}

	// Without a post id to find the campaign again, a lost answer is not retried.
	f.dropCreate = true
	_, err = l.Publish(context.Background(), creds, PostContent{Text: "x"}, s)
	blogWantKind(t, err, ErrRejected, "may have made the campaign")
}

func TestListMonkFailures(t *testing.T) {
	f := newFakeListMonk(t)
	l, _ := Get("listmonk")
	creds := Credentials{"url": f.srv.URL, "username": "staqly", "token": "tok"}
	post, s := PostContent{PostID: "pst_1", Text: "x"}, Settings{"title": "News", "lists": "Newsletter"}

	_, err := l.Publish(context.Background(), creds, post, Settings{"title": "News", "lists": "Missing"})
	blogWantKind(t, err, ErrRejected, "Newsletter, Customers")

	f.createCode, f.createBody = http.StatusBadRequest, `{"message":"Invalid template ID."}`
	_, err = l.Publish(context.Background(), creds, post, s)
	blogWantKind(t, err, ErrRejected, "Invalid template")

	f.createCode, f.createBody = http.StatusForbidden, `{"message":"permission denied: campaigns:manage"}`
	_, err = l.Publish(context.Background(), creds, post, s)
	blogWantKind(t, err, ErrRejected, "permissions")

	f.createCode, f.createBody = http.StatusInternalServerError, `{"message":"Error creating campaign"}`
	_, err = l.Publish(context.Background(), creds, post, s)
	blogWantKind(t, err, nil, "unavailable")

	f.createCode, f.createBody = http.StatusTooManyRequests, ``
	_, err = l.Publish(context.Background(), creds, post, s)
	blogWantKind(t, err, nil, "rate limit")

	creds["token"] = "revoked"
	_, err = l.Publish(context.Background(), creds, post, s)
	blogWantKind(t, err, ErrReauth, "")
}

func TestListMonkValidate(t *testing.T) {
	l, _ := Get("listmonk")
	blogWantInvalid(t, l.Validate(PostContent{Text: "x"}, Settings{"lists": "1"}), "title")
	blogWantInvalid(t, l.Validate(PostContent{Text: "x"}, Settings{"title": "T"}), "lists")
	blogWantInvalid(t, l.Validate(PostContent{Text: "x", Media: []MediaFile{{MimeType: "image/png"}}}, Settings{"title": "T", "lists": "1"}), "media")
	blogWantInvalid(t, l.Validate(PostContent{Text: " "}, Settings{"title": "T", "lists": "1"}), "content")
	blogWantInvalid(t, l.Validate(PostContent{Text: "x"}, Settings{"title": "T", "lists": "1", "template_id": "1.5"}), "template_id")
	if err := l.Validate(PostContent{Text: "x"}, Settings{"title": "T", "lists": "1"}); err != nil {
		t.Fatal(err)
	}
}
