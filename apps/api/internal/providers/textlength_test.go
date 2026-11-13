package providers

import (
	"strings"
	"testing"
)

// Every platform but X counts characters (runes, not bytes); X weighs them the way twitter-text v3 does.
func TestTextLength(t *testing.T) {
	plain := Limits{MaxChars: 500}
	for text, want := range map[string]int{
		"":                         0,
		"hello":                    5,
		"zażółć":                   6,
		"日本語":                      3,
		"😀":                        1,
		"👨‍👩‍👧":                    5, // runes, joiners included
		"https://example.com/long": 24,
	} {
		if got := TextLength(plain, text); got != want {
			t.Errorf("plain %q: %d, want %d", text, got, want)
		}
	}

	x := Limits{MaxChars: 280, XWeighted: true}
	for _, c := range []struct {
		name string
		text string
		want int
	}{
		{"empty", "", 0},
		{"latin", "hello", 5},
		{"latin with diacritics", "zażółć", 6},
		{"cyrillic and greek (below U+1100)", "привет αβγ", 10},
		{"last 1-weighted rune before Hangul jamo", "ჿ", 1},
		{"Hangul jamo", "ᄀ", 2},
		{"CJK", "日本語", 6},
		{"general punctuation spaces", "  ", 2},
		{"dashes and quotes", "‐—“‟", 4},
		{"just past the quotes", "†", 2},
		{"primes", "′‷", 2},
		{"just outside the primes", "‱‸", 4},
		{"emoji", "😀", 2},
		{"emoji with a skin tone", "👍🏽", 2},
		{"emoji with variation selector", "❤️", 2},
		{"ZWJ family", "👨‍👩‍👧‍👦", 2},
		{"flag", "🇵🇱", 2},
		{"two flags", "🇵🇱🇺🇦", 4},
		{"a lone regional indicator", "🇵", 2},
		{"three regional indicators: a flag and one left", "🇵🇱🇺", 4},
		{"a flag then text", "🇵🇱a", 3},
		{"subdivision flag (tag sequence)", "🏴\U000e0067\U000e0062\U000e0065\U000e006e\U000e0067\U000e007f", 2},
		{"keycap", "1️⃣", 2},
		{"keycap without the selector", "#⃣", 2},
		{"link: 23 whatever its length", "https://example.com/" + strings.Repeat("p", 60), 23},
		{"short link: still 23", "https://a.co", 23},
		{"bare host link", "see example.com/page ok", 4 + 23 + 3},
		{"two links", "https://a.example/x https://b.example/y", 23 + 1 + 23},
		{"a file name is no link", "README.md", 9},
		{"an email is no link", "a@example.com", 13},
	} {
		if got := TextLength(x, c.text); got != c.want {
			t.Errorf("x %s %q: %d, want %d", c.name, c.text, got, c.want)
		}
	}

	// Exactly at X's limit, and one over.
	at := strings.Repeat("a", 278) + "日"
	if TextLength(x, at) != 280 || TextLength(x, at+"a") != 281 {
		t.Errorf("boundary: %d, %d", TextLength(x, at), TextLength(x, at+"a"))
	}
}
