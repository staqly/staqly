package providers

import (
	"errors"
	"strings"
	"testing"
)

func TestCatalogAndLimits(t *testing.T) {
	all := All()
	if len(all) < 32 {
		t.Fatalf("catalog has %d providers, want the 31 platforms plus custom", len(all))
	}
	if all[0].ID() != "custom" || all[0].Status != StatusAvailable {
		t.Fatalf("custom should lead the catalog as available, got %s %s", all[0].ID(), all[0].Status)
	}
	x, _ := Get("x")
	err := CheckLimits(x.Limits(), PostContent{Text: strings.Repeat("a", 281)}, nil)
	if err == nil || !strings.Contains(err.Error(), "280") {
		t.Fatalf("X limit not enforced: %v", err)
	}
	yt, _ := Get("youtube")
	if err := CheckLimits(yt.Limits(), PostContent{Text: "hi", Media: []MediaFile{{ID: "m"}}}, nil); err == nil || !strings.Contains(err.Error(), "title") {
		t.Fatalf("YouTube needs a title: %v", err)
	}
	if err := CheckLimits(Limits{}, PostContent{}, nil); err == nil {
		t.Fatal("an empty post must not validate")
	}
	if err := CheckLimits(Limits{MaxMedia: 1}, PostContent{Text: "ok", Media: []MediaFile{{ID: "a"}, {ID: "b"}}}, nil); err == nil {
		t.Fatal("too many files must not validate")
	}
}

func TestCheckFields(t *testing.T) {
	fields := []Field{
		{Key: "kind", Label: "Kind", Kind: "select", Default: "a", Options: []Option{{Value: "a"}, {Value: "b"}}},
		{Key: "who", Label: "Who", Kind: "select", Required: true, Options: []Option{{Value: "x"}}},
		{Key: "flag", Label: "Flag", Kind: "boolean"},
		{Key: "n", Label: "N", Kind: "number"},
		{Key: "site", Label: "Site", Kind: "url"},
		{Key: "tags", Label: "Tags", Kind: "list", MaxItems: 2},
		{Key: "note", Label: "Note", Kind: "textarea", MaxLength: 3},
		{Key: "extra", Label: "Extra", Kind: "text", Required: true, When: &Condition{Key: "kind", Value: "b"}},
	}
	ok := Settings{"who": "x", "flag": "true", "n": "1.5", "site": "https://example.com", "tags": "a, b,", "note": "abc"}
	if err := CheckFields(fields, ok); err != nil {
		t.Fatalf("valid settings: %v", err)
	}
	for _, tc := range []struct {
		change Settings
		field  string
	}{
		{Settings{"who": ""}, "who"},
		{Settings{"who": "y"}, "who"},
		{Settings{"kind": "c"}, "kind"},
		{Settings{"flag": "yes"}, "flag"},
		{Settings{"n": "one"}, "n"},
		{Settings{"site": "example.com"}, "site"},
		{Settings{"tags": "a,b,c"}, "tags"},
		{Settings{"note": "abcd"}, "note"},
		{Settings{"kind": "b"}, "extra"}, // shown only for kind b, then required
	} {
		s := Settings{}
		for k, v := range ok {
			s[k] = v
		}
		for k, v := range tc.change {
			s[k] = v
		}
		var ve *ValidationError
		if err := CheckFields(fields, s); !errors.As(err, &ve) || ve.Field != tc.field {
			t.Errorf("%v: want %s, got %v", tc.change, tc.field, err)
		}
	}
	if got := ListValues(" a, ,b ,"); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("ListValues: %v", got)
	}
}

// Every soon platform that needs a title or a link has the field, and its fields validate.
func TestSoonSettings(t *testing.T) {
	for _, e := range All() {
		l := e.Limits()
		keys := map[string]bool{}
		for _, f := range e.SettingsFields() {
			keys[f.Key] = true
		}
		if l.NeedsTitle && !keys["title"] {
			t.Errorf("%s needs a title field", e.ID())
		}
		if l.NeedsLink && !keys["link"] {
			t.Errorf("%s needs a link field", e.ID())
		}
	}
	tt, _ := Get("tiktok")
	var ve *ValidationError
	if err := tt.Validate(PostContent{Text: "a", Media: []MediaFile{{MimeType: "video/mp4"}}}, Settings{}); !errors.As(err, &ve) || ve.Field != "privacy_level" {
		t.Fatalf("tiktok privacy is required: %v", err)
	}
}

func TestWithPostFillsTitleAndTags(t *testing.T) {
	fields := Base{}.WithSettings(Field{Key: "title"}, Field{Key: "tags", MaxItems: 2}, Field{Key: "privacy"}).SettingsFields()
	if !fields[0].FromPost || !fields[1].FromPost || fields[2].FromPost || !NativeTags(fields) || NativeTags(fields[2:]) {
		t.Fatalf("fields: %+v", fields)
	}
	s := WithPost(fields, Limits{}, "Launch", []string{"a", "b", "c"}, Settings{"title": "own", "privacy": "unlisted"})
	if s["title"] != "Launch" || s["tags"] != "a, b" || s["privacy"] != "unlisted" {
		t.Fatalf("settings: %v", s)
	}
	// A post from before shared titles and tags keeps the channel's own.
	if s := WithPost(fields, Limits{}, "", nil, Settings{"title": "own", "tags": "x"}); s["title"] != "own" || s["tags"] != "x" {
		t.Fatalf("old post: %v", s)
	}
	// A platform that needs a title but has no field for it still gets the post's.
	if s := WithPost(nil, Limits{NeedsTitle: true}, "Launch", nil, nil); s["title"] != "Launch" {
		t.Fatalf("needs title: %v", s)
	}
}
