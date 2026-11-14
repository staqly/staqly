package providers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Removing links keeps file names (README.md is no link on X), the text of a markdown link, and takes links whose
// path has an @ (a Mastodon profile) like any other.
func TestLinksEdgeCases(t *testing.T) {
	for in, want := range map[string]string{
		"Update your README.md and setup.py today":   "Update your README.md and setup.py today",
		"Run deploy.sh then see example.co":          "Run deploy.sh then see",
		"see mastodon.social/@staqly now":            "see now",
		"Read [the docs](https://example.com/d) now": "Read the docs now",
		"mail me at jane@example.com":                "mail me at jane@example.com",
	} {
		if got, _ := ApplyLinks(LinksRemove, in); got != want {
			t.Errorf("remove %q: %q, want %q", in, got, want)
		}
	}
	if got, _ := ApplyLinks(LinksDefang, "Read [the docs](https://example.com/d)"); got != "Read the docs (example[.]com/d)" {
		t.Errorf("defang a markdown link: %q", got)
	}
	if !HasLiveLink("follow mastodon.social/@staqly") {
		t.Error("a link with @ in its path is a live link (billed as one on X)")
	}
}

// X counts CJK and emoji as 2, an emoji sequence as 2 in all and a link as 23.
func TestXWeightedLength(t *testing.T) {
	x, _ := Get("x")
	for _, tc := range []struct {
		text string
		ok   bool
	}{
		{strings.Repeat("日", 141), false},                                                    // 282
		{strings.Repeat("日", 140), true},                                                     // 280
		{strings.Repeat("😀", 141), false},                                                    // 282
		{strings.Repeat("👨‍👩‍👧", 60), true},                                                  // 120 (300 runes)
		{strings.Repeat("🇵🇱", 140), true},                                                    // flags: 280
		{strings.Repeat("a", 250) + " https://example.com/" + strings.Repeat("p", 60), true}, // 250 + 1 + 23
	} {
		if err := x.Validate(PostContent{Text: tc.text}, Settings{}); (err == nil) != tc.ok {
			t.Errorf("%d runes: %v, want ok=%v", len([]rune(tc.text)), err, tc.ok)
		}
	}
}

// A # inside a link is the link's, not a LinkedIn hashtag.
func TestLinkedInHashInLink(t *testing.T) {
	if got := linkedinText("Install: https://example.com/docs#install #howto"); got != `Install: https://example.com/docs\#install {hashtag|\#|howto}` {
		t.Fatalf("%q", got)
	}
}

// X answers 200 with only an errors list for a post that is gone: no zeros cached as its numbers.
func TestXMetricsOfAGonePost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"errors":[{"value":"123","title":"Not Found Error","resource_type":"tweet"}]}`))
	}))
	defer srv.Close()
	old := xAPI
	xAPI = srv.URL
	defer func() { xAPI = old }()
	if _, err := (X{}).Metrics(context.Background(), Credentials{"access_token": "at"}, "123"); !errors.Is(err, ErrGone) {
		t.Fatalf("a gone post: %v", err)
	}
}

// A post goes out with the files a platform takes: of its kinds, in order, at most MaxMedia.
func TestFitMedia(t *testing.T) {
	pic, gif, vid, pdf := MediaFile{Name: "a", MimeType: "image/png"}, MediaFile{Name: "g", MimeType: "image/gif"},
		MediaFile{Name: "v", MimeType: "video/mp4"}, MediaFile{Name: "d", MimeType: "application/pdf"}
	names := func(fs []MediaFile) (out string) {
		for _, f := range fs {
			out += f.Name
		}
		return out
	}
	for _, c := range []struct {
		l    Limits
		in   []MediaFile
		want string
	}{
		{Limits{MaxMedia: 1, MediaKinds: []string{"image"}}, []MediaFile{vid, pic, gif}, "a"}, // Pinterest
		{Limits{MaxMedia: 1, MediaKinds: []string{"video"}}, []MediaFile{pic, vid}, "v"},      // YouTube
		{Limits{MaxMedia: 4, MediaKinds: []string{"image", "video"}}, []MediaFile{gif, pdf, vid}, "gv"},
		{Limits{MaxMedia: 10, MediaKinds: []string{"image", "video", "gif", "file"}}, []MediaFile{gif, pdf}, "gd"},
		{Limits{MaxChars: 500}, []MediaFile{pic}, ""}, // takes no media
	} {
		if got := names(FitMedia(c.l, c.in)); got != c.want {
			t.Errorf("%+v %s: %q, want %q", c.l, names(c.in), got, c.want)
		}
	}
}

// A Pinterest app on Trial access says so, instead of blaming the board.
func TestPinterestTrialAccess(t *testing.T) {
	err := pinterestError(403, []byte(`{"code":29,"message":"Your application does not have access to read pinner data. Your application must be approved for pinner data access."}`))
	if !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "Trial access") {
		t.Fatalf("%v", err)
	}
}

// The board setting lists the account's boards, by id with their names.
func TestPinterestBoardOptions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"items":[{"id":"11","name":"Buddha"},{"id":"12","name":"Opuszczone miejsca"}],"bookmark":""}`))
	}))
	defer srv.Close()
	old := pinterestAPI
	pinterestAPI = srv.URL
	defer func() { pinterestAPI = old }()
	opts, err := (Pinterest{}).Options(context.Background(), Credentials{"access_token": "t"}, "board")
	if err != nil || len(opts) != 2 || opts[1].Value != "12" || opts[1].Label != "Opuszczone miejsca" {
		t.Fatalf("%+v %v", opts, err)
	}
}
