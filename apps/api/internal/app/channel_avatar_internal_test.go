package app

import (
	"strings"
	"testing"
)

// An email shows a channel's own picture through the public route, a lasting platform link as it is, and nothing
// for a signed link that expires (it would be a broken image).
func TestEmailAvatar(t *testing.T) {
	s := &Service{Getenv: func(k string) string {
		if k == "PUBLIC_DASHBOARD_URL" {
			return "https://app.example/"
		}
		return ""
	}}
	own := "data:image/jpeg;base64,/9j/AA=="
	cases := map[string]string{
		own:                         "https://app.example/api/v1/channel-avatars/ws_1/ch_1/" + avatarHash(own),
		"https://cdn.example/a.jpg": "https://cdn.example/a.jpg",
		"https://scontent.cdninstagram.com/a.jpg?_nc_ht=x&oe=6A1B2C3D": "",
		"https://p16.tiktokcdn.com/a.jpeg?x-expires=1700000000":        "",
		"javascript:alert(1)": "",
		"":                    "",
	}
	for avatar, want := range cases {
		if got := s.emailAvatar("ws_1", map[string]any{"id": "ch_1", "avatar_url": avatar}); got != want {
			t.Errorf("%q: got %q, want %q", avatar, got, want)
		}
	}
	doc := func(avatar string) string {
		h, _ := renderEmail(emailDoc{Title: "x", Channel: &emailChannel{Name: "News",
			AvatarURL: s.emailAvatar("ws_1", map[string]any{"id": "ch_1", "avatar_url": avatar})}}, "")
		return h
	}
	if h := doc(own); !strings.Contains(h, `src="https://app.example/api/v1/channel-avatars/ws_1/ch_1/`) {
		t.Fatalf("HTML: %s", h)
	}
	if h := doc("https://x.fbcdn.net/a.jpg?oe=1"); strings.Contains(h, "<img") {
		t.Fatalf("an expiring link must not be shown: %s", h)
	}
}
