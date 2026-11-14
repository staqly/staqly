package app

import (
	"os/user"
	"strings"
	"sync"
)

// osUserName is the account the server runs as, looked up once.
var osUserName = sync.OnceValue(func() string {
	u, err := user.Current()
	if err != nil {
		return ""
	}
	name := strings.TrimSpace(u.Username)
	if i := strings.LastIndexByte(name, '\\'); i >= 0 { // Windows: DOMAIN\user
		name = name[i+1:]
	}
	if name == "root" || name == "staqly" || name == "nobody" {
		return ""
	}
	return name
})

// localUserName names the person behind a local install in the activity log:
// LOCAL_USER_NAME (compose passes the host's $USER), else the OS account the
// server runs as (`staqly serve` runs as you), else "Local". Service accounts
// (root, the image's staqly user) are not people, so they fall back too.
func (s *Service) localUserName() string {
	if v := strings.TrimSpace(s.Env("LOCAL_USER_NAME", "")); v != "" {
		return v
	}
	if name := osUserName(); name != "" {
		return name
	}
	return "Local"
}
