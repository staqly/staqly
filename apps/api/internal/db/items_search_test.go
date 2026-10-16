package db

import (
	"strings"
	"testing"
)

// Audit H3: search input is bounded before it reaches FTS5.
func TestFTSQueryBounds(t *testing.T) {
	cases := map[string]string{
		"":                   "",
		"cat cat CAT":        `"cat"*`, // deduplicated
		"a b":                `"a" AND "b"`,
		"x cat":              `"x" AND "cat"*`, // one-letter words are exact
		"ż":                  `"ż"`,
		`"; DROP TAB items;`: `"drop"* AND "tab"* AND "items"*`,
	}
	for in, want := range cases {
		if got := FTSQuery(in); got != want {
			t.Errorf("FTSQuery(%q) = %s, want %s", in, got, want)
		}
	}
	many := strings.Repeat("word ", 5) + "w01 w02 w03 w04 w05 w06 w07 w08 w09 w10 w11 w12 w13 w14 w15 w16 w17 w18 w19 w20"
	if n := strings.Count(FTSQuery(many), " AND ") + 1; n != ftsMaxTerms {
		t.Fatalf("want %d terms, got %d: %s", ftsMaxTerms, n, FTSQuery(many))
	}
	long := strings.Repeat("a", 200) + " " + strings.Repeat("b", 100) + " " + strings.Repeat("c", 50)
	q := FTSQuery(long)
	if strings.Contains(q, "bbb") || !strings.Contains(q, "aaa") {
		t.Fatalf("byte cap not applied: %d bytes", len(q))
	}
	// One word over the cap is clipped (at a rune boundary), not dropped: an empty query would match every item.
	if q := FTSQuery(strings.Repeat("ż", 200)); q != `"`+strings.Repeat("ż", 128)+`"*` {
		t.Fatalf("long word: %s", q)
	}
}
