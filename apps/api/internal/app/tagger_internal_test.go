package app

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestParseSuggestion(t *testing.T) {
	summary, tags := parseSuggestion("Here you go:\n```json\n{\"summary\": \"  Spring sale  post \", \"tags\": [\"Marketing\"]}\n```")
	if summary != "Spring sale post" || !slices.Equal(tags, []string{"marketing"}) {
		t.Fatalf("got %q %q", summary, tags)
	}
	long, _ := parseSuggestion(`{"summary": "` + strings.Repeat("word ", 40) + `"}`)
	if len([]rune(long)) > maxSummary || !strings.HasSuffix(long, "...") {
		t.Fatalf("not clipped: %q", long)
	}
}

// clip never splits a character: agent names (and the item sources taken from them) and model prompts stay
// valid UTF-8.
func TestClipKeepsUTF8(t *testing.T) {
	name := strings.Repeat("ż", 60) // 120 bytes
	got := clip(name, 101)
	if !utf8.ValidString(got) || len(got) != 100 {
		t.Fatalf("clip: %d bytes, valid %v", len(got), utf8.ValidString(got))
	}
	if src := agentName("api", name); !utf8.ValidString(src) {
		t.Fatalf("agent name not valid UTF-8: %q", src)
	}
	if clip("abc", 5) != "abc" || clip("abcdef", 3) != "abc" {
		t.Fatal("ascii")
	}
}

func TestParseTags(t *testing.T) {
	for reply, want := range map[string][]string{
		`["Social Media", "marketing"]`:                             {"social media", "marketing"},
		"Sure!\n```json\n[\"#news\", \"tech\", \"ai\", \"x\"]\n```": {"news", "tech", "ai"},
		`["a,b", "", "ok"]`:                                         {"ok"},
		`no tags here`:                                              nil,
	} {
		if got := parseTags(reply); !slices.Equal(got, want) {
			t.Errorf("parseTags(%q) = %q, want %q", reply, got, want)
		}
	}
}
