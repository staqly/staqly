package app

import "testing"

// Offsets count UTF-16 code units, as browsers do: an emoji is two, é one. Wrong offsets fall back to the
// quote's first occurrence; a quote that's gone is not found.
func TestAnchorIn(t *testing.T) {
	text := "🚀 Launch: v1.4 is out, v1.4 rocks"
	for _, tc := range []struct {
		quote        string
		start, end   int64
		wantS, wantE int64
		wantFound    bool
	}{
		{"Launch", 3, 9, 3, 9, true},
		{"v1.4", 24, 28, 24, 28, true},  // the second one, where the browser said
		{"v1.4", 99, 103, 11, 15, true}, // out of range: the first one
		{"v1.4", -1, -1, 11, 15, true},  // no offsets
		{"v1.4", 3, 7, 11, 15, true},    // offsets on other words
		{"v2.0", 11, 15, -1, -1, false}, // gone
		{"", 0, 0, -1, -1, false},
	} {
		s, e, ok := anchorIn(text, tc.quote, tc.start, tc.end)
		if s != tc.wantS || e != tc.wantE || ok != tc.wantFound {
			t.Errorf("anchorIn(%q, %d, %d) = %d, %d, %v; want %d, %d, %v", tc.quote, tc.start, tc.end, s, e, ok, tc.wantS, tc.wantE, tc.wantFound)
		}
	}
}
