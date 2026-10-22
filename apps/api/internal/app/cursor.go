package app

import (
	"encoding/base64"
	"strings"
)

// keysetCursor: an opaque next_cursor holding the sort key of a page's last row (created_at and id, say).
func keysetCursor(parts ...string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join(parts, "\x00")))
}

// readKeysetCursor: the n parts of a cursor made by keysetCursor; "" reads as the first page.
func readKeysetCursor(c string, n int) ([]string, error) {
	if c == "" {
		return make([]string, n), nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(c)
	parts := strings.Split(string(raw), "\x00")
	if err != nil || len(parts) != n || parts[0] == "" {
		return nil, invalid("cursor", "is invalid")
	}
	return parts, nil
}
