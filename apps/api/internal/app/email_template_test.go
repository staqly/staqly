package app

import (
	"strings"
	"testing"
)

func TestRenderEmailEscapesAndKeepsText(t *testing.T) {
	d := emailDoc{Title: `Your post <script>`, Quote: `a "quote" & <b>`, Rows: []emailRow{{Label: "X · @me", Detail: "Published",
		Link: &emailLink{Text: "Open on X", URL: `https://x.com/a?b=1&c="2"`}}, {Label: "LinkedIn", Detail: "boom <i>", Failed: true}},
		Button: &emailLink{Text: "View post", URL: "https://app.example/posts/p1"}, Unsub: &emailLink{Text: "Unsubscribe", URL: "https://app.example/unsubscribe#t"}}
	h, txt := renderEmail(d, "https://app.example/brand/icon-192.png")
	for _, bad := range []string{"<script>", "<b>", "<i>", `c="2"`} {
		if strings.Contains(h, bad) {
			t.Fatalf("unescaped %q in HTML", bad)
		}
	}
	for _, want := range []string{"&lt;script&gt;", "View post", "https://app.example/brand/icon-192.png", "#dc2626"} {
		if !strings.Contains(h, want) {
			t.Fatalf("HTML lacks %q", want)
		}
	}
	for _, want := range []string{"Your post <script>", "> a \"quote\" & <b>", "- X · @me: Published", "https://x.com/a?b=1&c=\"2\"", "View post: https://app.example/posts/p1", "Unsubscribe: https://app.example/unsubscribe#t"} {
		if !strings.Contains(txt, want) {
			t.Fatalf("text lacks %q:\n%s", want, txt)
		}
	}
}

func TestTextEmailDocFindsCodeAndLink(t *testing.T) {
	d := textEmailDoc("Ana invited you to Studio on staqly", "Ana invited you.\n\nAccept the invitation:\nhttps://app.example/invite?token=abc\n\nIt expires soon.\n")
	if d.Button == nil || d.Button.URL != "https://app.example/invite?token=abc" || d.Button.Text != "Accept the invitation" || len(d.Paragraphs) != 3 {
		t.Fatalf("invitation: %+v", d)
	}
	// The subject leads with the code; the heading drops it (the panel shows it).
	subject, text := emailCodeMessage(purposeSignIn, "012345")
	c := textEmailDoc(subject, text)
	if subject != "012345 is your staqly sign-in code" || c.Code != "012345" || c.Title != "Your staqly sign-in code" || c.Button != nil ||
		len(c.Paragraphs) != 3 || strings.Contains(strings.Join(c.Paragraphs, " "), "012345") {
		t.Fatalf("code: %q %+v", subject, c)
	}
	// One unbroken string in both bodies (copy and the phone's code suggestion), leading zero kept, right after the first line.
	h, txt := renderEmail(c, "")
	first := strings.Index(h, "Here is your code to sign in to staqly:")
	panel := strings.Index(h, `color:#111827">012345</td>`)
	if first < 0 || panel < first || strings.Index(h, "It works <strong") < panel || !strings.Contains(txt, "staqly:\n\n    012345\n\nType it") {
		t.Fatalf("code not one string after the first line:\n%s\n%s", h, txt)
	}
	// **words** are bold in HTML and plain in text.
	if !strings.Contains(h, `600">10 minutes</strong>`) || strings.Contains(txt, "**") || !strings.Contains(txt, "expires in 10 minutes") {
		t.Fatalf("bold:\n%s\n%s", h, txt)
	}
}

func TestUnsubscribeTokenIsSigned(t *testing.T) {
	s := &Service{Cfg: Config{SecretKey: []byte("0123456789abcdef0123456789abcdef")}}
	tok := s.unsubscribeToken("usr_1", "post.published")
	other := &Service{Cfg: Config{SecretKey: []byte("fedcba9876543210fedcba9876543210")}}
	if tok == other.unsubscribeToken("usr_1", "post.published") || s.unsubscribeToken("usr_2", "post.published") == tok {
		t.Fatal("token does not depend on the key and the person")
	}
}

func TestRenderEmailChannelAndError(t *testing.T) {
	d := emailDoc{Title: "News needs reconnecting", Channel: &emailChannel{Name: "News", Detail: "Bluesky · @news", AvatarURL: "javascript:alert(1)"},
		Error: "token <expired>"}
	h, txt := renderEmail(d, "")
	if strings.Contains(h, "javascript:") || !strings.Contains(h, "Bluesky · @news") || !strings.Contains(h, "#fef2f2") ||
		!strings.Contains(h, "token &lt;expired&gt;") {
		t.Fatalf("HTML: %s", h)
	}
	d.Channel.AvatarURL = "https://cdn.example/a.jpg"
	if h, _ = renderEmail(d, ""); !strings.Contains(h, `src="https://cdn.example/a.jpg"`) {
		t.Fatal("avatar missing")
	}
	if !strings.Contains(txt, "News (Bluesky · @news)") || !strings.Contains(txt, "What went wrong: token <expired>") {
		t.Fatalf("text:\n%s", txt)
	}
}
