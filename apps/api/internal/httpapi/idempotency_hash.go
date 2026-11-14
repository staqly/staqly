package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
)

// canonicalPrefix marks a stored request hash computed over the canonical JSON form. Hashes stored before
// it existed (raw body bytes, no prefix) are still matched by legacyRequestHash until they expire (24 h).
const canonicalPrefix = "v2:"

// requestHash fingerprints a request for Idempotency-Key reuse: method, path, query and body. A JSON body
// is hashed in canonical form (object keys sorted, no insignificant whitespace, strings and escapes
// decoded), so the same request from SDKs that format JSON differently (Python's json.dumps puts spaces
// after separators, JavaScript's JSON.stringify does not) replays instead of failing with
// idempotency_mismatch. Numbers keep their literal text: 1 and 1.0 are different requests, because an
// integer field accepts 1 and rejects 1.0. Other bodies are hashed byte for byte.
func requestHash(r *http.Request, body []byte) string {
	if c, ok := canonicalJSON(r.Header.Get("Content-Type"), body); ok {
		return canonicalPrefix + hashOf(r, c)
	}

	return hashOf(r, body)
}

// legacyRequestHash is the hash stored before canonical JSON hashing (raw body bytes). A key reserved by
// an older server is still recognised for the same bytes. ponytail: dead once no key older than the
// upgrade is left (24 h); remove with the next release.
func legacyRequestHash(r *http.Request, body []byte) string {
	return hashOf(r, body)
}

func hashOf(r *http.Request, body []byte) string {
	sum := sha256.Sum256(append([]byte(r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery+"\n"), body...))

	return hex.EncodeToString(sum[:])
}

// canonicalJSON re-encodes a JSON body (application/json or a +json type) as one value with sorted object
// keys and no insignificant whitespace; numbers are kept exactly as written (json.Number). ok is false for
// other content types and for bodies that are not exactly one JSON value.
func canonicalJSON(contentType string, body []byte) ([]byte, bool) {
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil || mt != "application/json" && !strings.HasSuffix(mt, "+json") {
		return nil, false
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, false // trailing data: not one JSON value
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	// encoding/json writes map keys sorted and json.Number as its literal.
	if err := enc.Encode(v); err != nil {
		return nil, false
	}

	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), true
}
