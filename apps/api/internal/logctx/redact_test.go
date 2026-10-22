package logctx

import "testing"

func TestRedactPath(t *testing.T) {
	for in, want := range map[string]string{
		"/v1/previews/tok123/comments":       "/v1/previews/REDACTED/comments",
		"/v1/previews/tok123":                "/v1/previews/REDACTED",
		"/api/v1/previews/tok123/media/m":    "/api/v1/previews/REDACTED/media/m",
		"/v1/connect-links/tok123/channels":  "/v1/connect-links/REDACTED/channels",
		"/api/v1/connect-links/tok123":       "/api/v1/connect-links/REDACTED",
		"/v1/connect-links/oauth/callback":   "/v1/connect-links/oauth/callback",
		"/v1/posts/pst_1":                    "/v1/posts/pst_1",
		"/api/v1/posts/pst_1/previews/prv_1": "/api/v1/posts/pst_1/previews/prv_1",
	} {
		if got := RedactPath(in); got != want {
			t.Errorf("RedactPath(%q) = %q, want %q", in, got, want)
		}
	}
}
