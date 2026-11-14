package server

import (
	"strings"
	"testing"
)

// The admin listener is opt-in through ADMIN_ADDR in both modes; the CLI manages the admins table.
func TestAdminCommands(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	t.Setenv("STAQLY_MODE", "local")
	if code, _, e := capture(t, "admin", "add", "ops@example.com", "--name", "Ops"); code != 0 {
		t.Fatalf("add: %d %s", code, e)
	}
	if code, _, e := capture(t, "admin", "add", "ops@example.com"); code != 1 || !strings.Contains(e, "already an admin") {
		t.Fatalf("dup: %d %s", code, e)
	}
	code, out, _ := capture(t, "admins")
	if code != 0 || !strings.Contains(out, "ops@example.com\tOps\t") {
		t.Fatalf("list: %d %q", code, out)
	}
	if code, _, e := capture(t, "admin", "remove", "ops@example.com"); code != 1 || !strings.Contains(e, "last admin") {
		t.Fatalf("last: %d %s", code, e)
	}
	if code, _, _ := capture(t, "admin", "bogus"); code != 2 {
		t.Fatal(code)
	}
}
