package providers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// Helpers for the article platforms' tests.

// blogDropConn closes the connection without an answer (a reply that never came).
func blogDropConn(w http.ResponseWriter) {
	hj, _ := w.(http.Hijacker)
	c, _, _ := hj.Hijack()
	_ = c.Close()
}

// blogWantKind checks an error's Kind: ErrReauth, ErrRejected, or nil for a retryable Failure.
func blogWantKind(t *testing.T, err error, kind error, contains string) {
	t.Helper()
	var f *Failure
	if !errors.As(err, &f) {
		t.Fatalf("want a Failure, got %T %v", err, err)
	}
	if f.Kind != kind {
		t.Fatalf("kind = %v, want %v (%s)", f.Kind, kind, f.Msg)
	}
	if contains != "" && !strings.Contains(f.Msg, contains) {
		t.Fatalf("message %q does not say %q", f.Msg, contains)
	}
}

// blogWantInvalid checks a ValidationError on field.
func blogWantInvalid(t *testing.T, err error, field string) {
	t.Helper()
	var v *ValidationError
	if !errors.As(err, &v) || v.Field != field {
		t.Fatalf("want a validation error on %s, got %v", field, err)
	}
}

// blogMemFile is an attachment served from memory.
func blogMemFile(name, mime, body string) MediaFile {
	return MediaFile{ID: "m1", Name: name, MimeType: mime, Size: int64(len(body)),
		Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(body)), nil }}
}

func TestMarkdownHTML(t *testing.T) {
	out, err := markdownHTML("Hello **world**\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n<div class=\"x\">raw</div>\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<strong>world</strong>", "<table>", `<div class="x">raw</div>`} {
		if !strings.Contains(out, want) {
			t.Fatalf("%q lacks %q", out, want)
		}
	}
}

func TestBlogInstance(t *testing.T) {
	u, err := blogInstance("url", " blog.example.com/ ")
	if err != nil || u.String() != "https://blog.example.com" {
		t.Fatalf("got %v %v", u, err)
	}
	if _, err := blogInstance("url", "ftp://example.com"); err == nil {
		t.Fatal("ftp accepted")
	}
}

// Passwords and tokens never cross the internet in clear: http:// only for this machine or a private network.
func TestPlaintextOnlyOnPrivateHosts(t *testing.T) {
	for raw, ok := range map[string]bool{"https://blog.example.com": true, "http://blog.example.com": false,
		"http://localhost:8080": true, "http://192.168.1.5": true, "http://wordpress": true, "http://8.8.8.8": false} {
		if _, err := blogInstance("url", raw); (err == nil) != ok {
			t.Errorf("blogInstance(%q): %v", raw, err)
		}
		if _, err := fediInstance(raw, "mastodon.social", "instance"); (err == nil) != ok {
			t.Errorf("fediInstance(%q): %v", raw, err)
		}
	}
}
