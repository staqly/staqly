package providers

import (
	"strings"
	"testing"
)

// Every platform of the catalog is listed, and one with an implementation keeps it (the catalog never replaces it
// with a "soon" stand-in). The limits the catalog names are the platforms' published ones.
func TestCatalogLists(t *testing.T) {
	for id, want := range map[string]Limits{
		"x":         {MaxChars: 280, MaxMedia: 4},
		"linkedin":  {MaxChars: 3000, MaxMedia: 9},
		"reddit":    {MaxChars: 40000, MaxMedia: 1, NeedsTitle: true},
		"facebook":  {MaxChars: 63206, MaxMedia: 10},
		"threads":   {MaxChars: 500, MaxMedia: 10},
		"youtube":   {MaxChars: 5000, MaxMedia: 1, NeedsTitle: true, NeedsMedia: true},
		"tiktok":    {MaxChars: 2200, MaxMedia: 1, NeedsMedia: true},
		"pinterest": {MaxChars: 500, MaxMedia: 1, NeedsTitle: true, NeedsMedia: true, NeedsLink: true},
		"mastodon":  {MaxChars: 500, MaxMedia: 4},
		"bluesky":   {MaxChars: 300, MaxMedia: 4},
		"telegram":  {MaxChars: 4096, MaxMedia: 10},
		"medium":    {MaxChars: 0, MaxMedia: 1, NeedsTitle: true},
		"tumblr":    {MaxChars: 4096, MaxMedia: 10},
	} {
		e, ok := Get(id)
		if !ok {
			t.Errorf("%s is not in the catalog", id)
			continue
		}
		l := e.Limits()
		if l.MaxChars != want.MaxChars || l.MaxMedia != want.MaxMedia || l.NeedsTitle != want.NeedsTitle || l.NeedsMedia != want.NeedsMedia || l.NeedsLink != want.NeedsLink {
			t.Errorf("%s limits %+v, want %+v", id, l, want)
		}
	}
	for _, e := range All() {
		_, isSoon := e.Provider.(soon)
		if isSoon != (e.Status == StatusSoon) {
			t.Errorf("%s: a %T registered as %s", e.ID(), e.Provider, e.Status)
		}
	}
	// bluesky.go registers before catalog.go's init runs; x.go after it.
	for _, id := range []string{"bluesky", "x"} {
		if e, _ := Get(id); e.Status != StatusAvailable {
			t.Errorf("%s is %s: the catalog replaced its implementation", id, e.Status)
		}
	}
}

func TestSoonSettingsFields(t *testing.T) {
	keys := func(fields []Field) string {
		var k []string
		for _, f := range fields {
			k = append(k, f.Key)
		}
		return strings.Join(k, ",")
	}
	for _, c := range []struct {
		id     string
		limits Limits
		want   string
	}{
		{"facebook", Limits{}, "post_type"},
		{"threads", Limits{}, "reply_control,tags"},
		{"tiktok", Limits{}, "privacy_level,allow_comments,allow_duet,allow_stitch,ai_generated,branded_content,your_brand"},
		{"youtube", Limits{NeedsTitle: true}, "title,privacy,made_for_kids,tags,category,playlist"},
		{"linkedin", Limits{}, "visibility"},
		{"pinterest", Limits{NeedsTitle: true, NeedsLink: true}, "title,board,link,alt_text"}, // neither added twice
		{"reddit", Limits{NeedsTitle: true}, "title,subreddit,flair,nsfw,spoiler"},
		{"devto", Limits{NeedsTitle: true}, "title,tags,canonical_url"},
		{"medium", Limits{NeedsTitle: true}, "title,tags,canonical_url"},
		{"unknown", Limits{}, ""},
		{"unknown", Limits{NeedsTitle: true}, "title"},
		{"unknown", Limits{NeedsLink: true}, "link"},
		{"unknown", Limits{NeedsTitle: true, NeedsLink: true}, "title,link"},
		{"linkedin", Limits{NeedsTitle: true, NeedsLink: true}, "title,visibility,link"}, // title first, link last
	} {
		fields := soonSettings(c.id, c.limits)
		if got := keys(fields); got != c.want {
			t.Errorf("%s %+v: %s, want %s", c.id, c.limits, got, c.want)
		}
	}

	byKey := func(id string, l Limits, key string) Field {
		for _, f := range soonSettings(id, l) {
			if f.Key == key {
				return f
			}
		}
		t.Fatalf("%s has no %s", id, key)
		return Field{}
	}
	// A required title or link added for the platform, and the platforms' own caps.
	if f := byKey("unknown", Limits{NeedsTitle: true}, "title"); !f.Required || f.MaxLength != 0 {
		t.Errorf("added title %+v", f)
	}
	if f := byKey("unknown", Limits{NeedsLink: true}, "link"); !f.Required || f.Kind != "url" {
		t.Errorf("added link %+v", f)
	}
	for id, max := range map[string]int{"youtube": 100, "pinterest": 100, "reddit": 300, "devto": 250, "medium": 100} {
		if f := byKey(id, Limits{NeedsTitle: true}, "title"); f.MaxLength != max || !f.Required {
			t.Errorf("%s title %+v, want max %d", id, f, max)
		}
	}
	if f := byKey("tiktok", Limits{}, "privacy_level"); !f.Required || len(f.Options) != 4 || f.Default != "" {
		t.Errorf("tiktok privacy %+v", f)
	}
	if f := byKey("threads", Limits{}, "tags"); f.MaxItems != 1 || f.MaxLength != 50 || !f.HashtagRest {
		t.Errorf("threads topic %+v", f)
	}
	if f := byKey("devto", Limits{}, "tags"); f.MaxItems != 4 {
		t.Errorf("devto tags %+v", f)
	}
	if f := byKey("medium", Limits{}, "tags"); f.MaxItems != 5 {
		t.Errorf("medium tags %+v", f)
	}
	if f := byKey("youtube", Limits{}, "category"); f.Default != "22" {
		t.Errorf("youtube category %+v", f)
	}
}
