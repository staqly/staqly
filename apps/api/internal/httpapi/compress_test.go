package httpapi_test

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// JSON under /v1 is gzipped when the client asks for it (the dashboard's browsers, the SDKs); clients that
// don't ask get it as before, and NDJSON downloads stay uncompressed streams.
func TestJSONCompression(t *testing.T) {
	_, srv := newLocal(t)
	// A raw transport: Go's default client would ask for gzip itself and hide the encoding.
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}
	get := func(path, accept string) *http.Response {
		req, _ := http.NewRequest("GET", srv.URL+path, nil)
		if accept != "" {
			req.Header.Set("Accept-Encoding", accept)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { res.Body.Close() })
		return res
	}
	for i := range 20 {
		req, _ := http.NewRequest("POST", srv.URL+"/v1/collections", strings.NewReader(`{"name":"c`+string(rune('a'+i))+`"}`))
		req.Header.Set("Content-Type", "application/json")
		res, err := client.Do(req)
		if err != nil || res.StatusCode != 201 {
			t.Fatalf("create collection: %v %v", err, res.Status)
		}
		res.Body.Close()
	}

	res := get("/v1/collections", "gzip, deflate, br")
	if res.Header.Get("Content-Encoding") != "gzip" || !strings.Contains(res.Header.Get("Vary"), "Accept-Encoding") {
		t.Fatalf("collections not gzipped: %v", res.Header)
	}
	zr, err := gzip.NewReader(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(zr).Decode(&page); err != nil || len(page.Data) != 21 { // 20 + the default collection
		t.Fatalf("gzipped body: %v, %d collections", err, len(page.Data))
	}

	res = get("/v1/collections", "")
	if res.Header.Get("Content-Encoding") != "" {
		t.Fatalf("plain client got %q", res.Header.Get("Content-Encoding"))
	}
	if b, _ := io.ReadAll(res.Body); !json.Valid(b) {
		t.Fatalf("plain body is not JSON")
	}

	res = get("/v1/events/export?format=ndjson", "gzip")
	if res.StatusCode != 200 || res.Header.Get("Content-Encoding") != "" {
		t.Fatalf("export: %d, encoding %q", res.StatusCode, res.Header.Get("Content-Encoding"))
	}
}
