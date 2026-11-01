package providers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func removeServer(t *testing.T, deleted *[]string) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Query().Get("access_token") != "tok" {
			t.Errorf("request: %s %s", r.Method, r.URL)
		}
		*deleted = append(*deleted, r.URL.Path)
		switch r.URL.Path {
		case "/v24.0/gone":
			http.NotFound(w, r)
		case "/v24.0/sub33":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"Unsupported delete request","code":100,"error_subcode":33}}`))
		case "/v24.0/denied":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"message":"(#200) no permission","code":200}}`))
		default:
			_, _ = w.Write([]byte(`{"success":true}`))
		}
	}))
	t.Cleanup(srv.Close)
	fb, th := facebookGraph, threadsGraph
	facebookGraph, threadsGraph = srv.URL, srv.URL
	t.Cleanup(func() { facebookGraph, threadsGraph = fb, th })
}

func TestRemoveMetaGone(t *testing.T) {
	var got []string
	removeServer(t, &got)
	ctx, c := context.Background(), Credentials{"access_token": "tok"}
	for _, id := range []string{"gone", "sub33", "1_2"} {
		if err := (Facebook{}).Remove(ctx, c, id); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
	err := (Facebook{}).Remove(ctx, c, "denied")
	if !errors.Is(err, ErrRejected) || strings.Contains(err.Error(), "tok") {
		t.Errorf("denied: %v", err)
	}
}

func TestRemoveMetaComment(t *testing.T) {
	var got []string
	removeServer(t, &got)
	ctx, c := context.Background(), Credentials{"access_token": "tok"}
	if err := (Facebook{}).Remove(ctx, c, "comment:1_2:9"); err != nil {
		t.Fatal(err)
	}
	if err := (Instagram{}).Remove(ctx, c, "comment:17:9"); err != nil {
		t.Fatal(err)
	}
	if err := (Threads{}).Remove(ctx, c, "th"); err != nil || got[2] != "/v1.0/th" {
		t.Fatalf("threads: %v %v", err, got)
	}
	if got[0] != "/v24.0/9" || got[1] != "/v24.0/9" {
		t.Errorf("paths: %v", got)
	}
	err := (Instagram{}).Remove(ctx, c, "17890")
	var f *Failure
	if !errors.As(err, &f) || f.Kind != ErrManual {
		t.Errorf("instagram media: %v", err)
	}
}

func TestThreadsScopes(t *testing.T) {
	if !strings.Contains(threadsScopes, "threads_delete") {
		t.Error(threadsScopes)
	}
}
