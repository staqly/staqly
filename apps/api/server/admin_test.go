package server

import (
	"io"
	"os"
	"strings"
	"testing"
)

// capture runs Main with stdout and stderr captured.
func capture(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	code = Main(args, "test")
	os.Stdout, os.Stderr = oldOut, oldErr
	outW.Close()
	errW.Close()
	o, _ := io.ReadAll(outR)
	e, _ := io.ReadAll(errR)
	return code, string(o), string(e)
}

// On the Community Edition the operator commands show the real plan (unlocked, not the retired free), and
// `server recovery-key` refuses: there is no recovery key, the access token is the credential.
func TestLocalOperatorCommands(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	t.Setenv("STAQLY_MODE", "local")
	if code, _, errOut := capture(t, "token"); code != 0 {
		t.Fatalf("token: %d %s", code, errOut)
	}
	code, out, _ := capture(t, "workspaces")
	if code != 0 || !strings.Contains(out, "\tunlocked\t") || strings.Contains(out, "free") {
		t.Fatalf("workspaces: %d %q", code, out)
	}
	code, out, _ = capture(t, "stats")
	if code != 0 || !strings.Contains(out, `"unlocked": 1`) || strings.Contains(out, `"free"`) {
		t.Fatalf("stats: %d %s", code, out)
	}
	code, out, errOut := capture(t, "recovery-key")
	if code != 1 || out != "" || !strings.Contains(errOut, "server token --rotate") {
		t.Fatalf("recovery-key: %d %q %q", code, out, errOut)
	}
}
