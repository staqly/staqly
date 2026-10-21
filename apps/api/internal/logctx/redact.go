package logctx

import "strings"

// tokenPaths: path prefixes whose next segment is a bearer token (a preview link, a connect link), with the
// segments after them that are not tokens.
var tokenPaths = []struct{ prefix, except string }{
	{"/v1/previews/", ""},
	{"/v1/connect-links/", "oauth/"},
}

// RedactPath keeps bearer tokens in URL paths out of logs: /v1/previews/<token>/... and
// /v1/connect-links/<token>/... are logged with the token replaced, also behind the dashboard's /api prefix.
func RedactPath(path string) string {
	base := strings.TrimPrefix(path, "/api")
	lead := path[:len(path)-len(base)]
	for _, t := range tokenPaths {
		if !strings.HasPrefix(base, t.prefix) || (t.except != "" && strings.HasPrefix(base, t.prefix+t.except)) {
			continue
		}
		rest := base[len(t.prefix):]
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			return lead + t.prefix + "REDACTED" + rest[i:]
		}
		return lead + t.prefix + "REDACTED"
	}
	return path
}
