package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCanonicalJSON(t *testing.T) {
	for _, tc := range []struct{ ct, body, want string }{
		{"application/json", `{ "b": [1, 2.50, 1e3], "a": {"y": null, "x": true} }`, `{"a":{"x":true,"y":null},"b":[1,2.50,1e3]}`},
		{"application/json; charset=utf-8", `"caf\u00e9 \u003c&>"`, `"café <&>"`},
		{"application/merge-patch+json", `{"z":1,"a":2}`, `{"a":2,"z":1}`},
		{"application/json", `12345678901234567890123`, `12345678901234567890123`}, // beyond float64: kept exact
		{"application/json", `{"a":1} {"b":2}`, ""},                                // two values: raw bytes
		{"application/json", `{"a":`, ""},                                          // invalid
		{"application/json", ``, ""},
		{"text/plain", `{"a":1}`, ""},
		{"", `{"a":1}`, ""},
	} {
		got, ok := canonicalJSON(tc.ct, []byte(tc.body))
		if ok != (tc.want != "") || string(got) != tc.want {
			t.Errorf("canonicalJSON(%q, %q) = %q, %v; want %q", tc.ct, tc.body, got, ok, tc.want)
		}
	}
}

func TestRequestHashVersions(t *testing.T) {
	r := httptest.NewRequest("POST", "/v1/x?a=1", nil)
	r.Header.Set("Content-Type", "application/json")
	if h := requestHash(r, []byte(`{"a": 1}`)); !strings.HasPrefix(h, canonicalPrefix) || h != requestHash(r, []byte(`{"a":1}`)) {
		t.Fatalf("JSON hash %q", h)
	}
	r2 := httptest.NewRequest("POST", "/v1/x?a=2", nil)
	r2.Header.Set("Content-Type", "application/json")
	if requestHash(r, []byte(`{"a":1}`)) == requestHash(r2, []byte(`{"a":1}`)) {
		t.Fatal("query must be part of the hash")
	}
	r.Header.Set("Content-Type", "text/plain")
	if h := requestHash(r, []byte(`{"a": 1}`)); strings.HasPrefix(h, canonicalPrefix) || h != legacyRequestHash(r, []byte(`{"a": 1}`)) {
		t.Fatalf("non-JSON hash %q must stay the raw-bytes hash", h)
	}
}
