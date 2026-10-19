package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCustomEndpoint(t *testing.T) {
	var got struct {
		method, auth, query string
		body                map[string]any
	}
	reply := func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"url":"https://social.test/p/1","external_id":"p1"}`))
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method, got.auth, got.query, got.body = r.Method, r.Header.Get("Authorization"), r.URL.RawQuery, nil
		_ = json.NewDecoder(r.Body).Decode(&got.body)
		reply(w)
	}))
	defer srv.Close()
	c, _ := Get("custom")
	ctx := context.Background()
	post := PostContent{PostID: "post_1", ChannelID: "ch_1", Text: "Hello", Media: []MediaFile{{ID: "med_1", URL: "http://x/v1/media/med_1"}}}

	// No URL: the event-only automation of before.
	if _, err := c.Publish(ctx, Credentials{}, post, nil); !errors.Is(err, ErrAsync) {
		t.Fatalf("no URL should be async, got %v", err)
	}
	creds, profile, err := c.Connect(ctx, map[string]string{"url": srv.URL, "headers": "Authorization: Bearer t0k\n\nX-Team: social"})
	if err != nil || creds["method"] != "POST" || profile.Handle == "" {
		t.Fatalf("connect: %v %v %v", creds, profile, err)
	}
	res, err := c.Publish(ctx, creds, post, Settings{"title": "T"})
	if err != nil || res.URL != "https://social.test/p/1" || res.ExternalID != "p1" {
		t.Fatalf("publish: %+v %v", res, err)
	}
	if got.method != "POST" || got.auth != "Bearer t0k" || got.body["post_id"] != "post_1" || got.body["channel_id"] != "ch_1" || got.body["text"] != "Hello" {
		t.Fatalf("request: %+v", got)
	}
	if m := got.body["media"].([]any)[0].(map[string]any); m["url"] != "http://x/v1/media/med_1" {
		t.Fatalf("media: %v", m)
	}
	if th := got.body["thread"].([]any); len(th) != 0 {
		t.Fatalf("no thread: %v", th)
	}
	threaded := post
	threaded.Thread = []ThreadPart{{Text: "part two", DelaySeconds: 30, Media: []MediaFile{{ID: "med_2", URL: "http://x/v1/media/med_2"}}}}
	if _, err := c.Publish(ctx, creds, threaded, nil); err != nil {
		t.Fatal(err)
	}
	part := got.body["thread"].([]any)[0].(map[string]any)
	if part["content"] != "part two" || part["delay_seconds"] != float64(30) || part["media"].([]any)[0].(map[string]any)["url"] != "http://x/v1/media/med_2" {
		t.Fatalf("thread in the payload: %v", part)
	}

	// GET: the post as query parameters.
	get, _, _ := c.Connect(ctx, map[string]string{"url": srv.URL + "?k=v", "method": "get"})
	if _, err := c.Publish(ctx, get, post, nil); err != nil || got.method != "GET" || !strings.Contains(got.query, "post_id=post_1") || !strings.Contains(got.query, "k=v") {
		t.Fatalf("GET: %+v %v", got, err)
	}

	// 202 or {"status":"publishing"}: the result comes later.
	reply = func(w http.ResponseWriter) { w.WriteHeader(http.StatusAccepted) }
	if _, err := c.Publish(ctx, creds, post, nil); !errors.Is(err, ErrAsync) {
		t.Fatalf("202 should be async, got %v", err)
	}
	// A plain 204 is published, without a link.
	reply = func(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }
	if res, err := c.Publish(ctx, creds, post, nil); err != nil || res.URL != "" {
		t.Fatalf("204: %+v %v", res, err)
	}
	// Anything else fails (the job queue retries it).
	reply = func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream down"))
	}
	if _, err := c.Publish(ctx, creds, post, nil); err == nil || !strings.Contains(err.Error(), "502 upstream down") {
		t.Fatalf("502: %v", err)
	}
}

func TestCustomConnectValidation(t *testing.T) {
	c, _ := Get("custom")
	for _, tc := range []struct {
		fields map[string]string
		field  string
	}{
		{map[string]string{"url": "ftp://x"}, "url"},
		{map[string]string{"url": "https://x.test", "method": "DELETE"}, "method"},
		{map[string]string{"url": "https://x.test", "headers": "no colon here"}, "headers"},
		{map[string]string{"url": "https://x.test", "headers": "Bad Name: v"}, "headers"},
		{map[string]string{"headers": "A: b"}, "url"},
		{map[string]string{"delivery": "endpoint"}, "url"},
	} {
		_, _, err := c.Connect(context.Background(), tc.fields)
		var ve *ValidationError
		if !errors.As(err, &ve) || ve.Field != tc.field {
			t.Errorf("%v: want an error on %s, got %v", tc.fields, tc.field, err)
		}
	}
	// Agents and webhooks: nothing to call, whatever else was typed.
	if creds, _, err := c.Connect(context.Background(), map[string]string{"delivery": "agent", "url": "https://x.test"}); err != nil || len(creds) != 0 {
		t.Fatalf("agent delivery: %v %v", creds, err)
	}
	fields := c.CredentialFields()
	if len(fields) != 4 || fields[0].Key != "delivery" || fields[0].Default != "endpoint" {
		t.Fatalf("delivery first, defaulting to the endpoint: %+v", fields)
	}
	// The URL is required only while the endpoint is chosen; a client that sends nothing keeps the event flow.
	url := fields[1]
	if !url.Required || url.Applies(map[string]string{}) || !url.Applies(map[string]string{"delivery": "endpoint"}) {
		t.Fatalf("url condition: %+v", url)
	}
}

// Your endpoint's 4xx answers are final (the request itself is wrong: retrying sends the same thing), except
// 408 and 429; 5xx answers and network errors are retried.
func TestCustomEndpointClientErrorsAreFinal(t *testing.T) {
	status := http.StatusBadRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte("nope"))
	}))
	c, _ := Get("custom")
	creds := Credentials{"url": srv.URL, "method": "POST"}
	for _, tc := range []struct {
		status int
		final  bool
	}{{400, true}, {401, true}, {404, true}, {422, true}, {408, false}, {429, false}, {500, false}, {503, false}} {
		status = tc.status
		_, err := c.Publish(context.Background(), creds, PostContent{PostID: "p", Text: "x"}, nil)
		if err == nil || errors.Is(err, ErrRejected) != tc.final || !strings.Contains(err.Error(), "answered") {
			t.Errorf("%d: %v (final %v, want %v)", tc.status, err, errors.Is(err, ErrRejected), tc.final)
		}
	}
	srv.Close()
	if _, err := c.Publish(context.Background(), creds, PostContent{PostID: "p", Text: "x"}, nil); err == nil || errors.Is(err, ErrRejected) {
		t.Errorf("network error: %v, want a retried error", err)
	}
}
