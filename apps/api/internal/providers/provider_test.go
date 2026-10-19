package providers

import (
	"strings"
	"testing"
)

func TestCatalogAndLimits(t *testing.T) {
	all := All()
	if len(all) < 35 {
		t.Fatalf("catalog has %d providers, want the 34 platforms plus custom", len(all))
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
