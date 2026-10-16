package app

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// A CLI that leaves a child holding its stdout must not keep the worker past the timeout: Wait gives up
// shortly after the process is killed.
func TestRunTaggerCommandTimeout(t *testing.T) {
	s := &Service{Cfg: Config{Tagger: TaggerConfig{Command: []string{"/bin/sh", "-c", "sleep 30 & sleep 30"}}}}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := s.runTaggerCommand(ctx, "hi"); err == nil {
		t.Fatal("no error after the timeout")
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("waited %s for the command", d)
	}
}

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
