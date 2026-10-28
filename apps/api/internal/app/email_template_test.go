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
	c := textEmailDoc("Your Staqly sign-in code: 123456", "Your sign-in code is 123456.\n\nIt works once.\n")
	if c.Code != "123456" || c.Button != nil {
		t.Fatalf("code: %+v", c)
	}
	// One unbroken string in both bodies (copy and the phone's code suggestion), leading zero kept.
	_, text := emailCodeMessage(purposeSignIn, "012345")
	h, txt := renderEmail(textEmailDoc("Your Staqly sign-in code: 012345", text), "")
	if !strings.Contains(h, `letter-spacing:0.2em;color:#111827">012345</span>`) || !strings.Contains(txt, "    012345\n") {
		t.Fatalf("code not one string:\n%s\n%s", h, txt)
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
