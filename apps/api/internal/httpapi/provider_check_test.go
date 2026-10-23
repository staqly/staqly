package httpapi_test

import (
	"testing"

	"github.com/staqly/staqly/apps/api/internal/app"
)

// The server's setup for a platform, independent of accounts: keys, the return address and (Instagram) media
// storage the platform can download from. No keys means no call to the platform.
func TestCheckProvider(t *testing.T) {
	e := newEnv(t)
	manage := e.keyWith(app.ScopeChannelsManage)
	e.svc.Getenv = func(string) string { return "" }

	var c app.ProviderCheck
	e.do(nil, "POST", "/v1/providers/instagram/check", manage, nil, 200, &c)
	got := map[string]app.SetupCheck{}
	for _, ch := range c.Checks {
		got[ch.Key] = ch
	}
	if c.OK || got["keys"].OK || got["platform"].Key != "" {
		t.Fatalf("no keys: %+v", c)
	}
	// The test server's dashboard is http://localhost and media sit on disk: Instagram can use neither.
	if got["return_address"].OK || got["media"].OK || got["media"].Detail == "" {
		t.Fatalf("http and disk: %+v", c)
	}

	e.do(nil, "POST", "/v1/providers/custom/check", manage, nil, 422, nil)
	e.do(nil, "POST", "/v1/providers/nope/check", manage, nil, 404, nil)
	e.do(nil, "POST", "/v1/providers/instagram/check", e.keyWith(app.ScopeChannelsRead), nil, 403, nil)
}
