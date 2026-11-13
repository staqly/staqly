package app

import (
	"testing"

	"github.com/staqly/staqly/apps/api/internal/providers"
)

// Unpublish deletes on platforms whose API can; Instagram login deletes comments only, so the App greys it out there.
func TestProviderInfoDeletesPosts(t *testing.T) {
	for id, want := range map[string]bool{"facebook": true, "threads": true, "instagram": true, "instagram_standalone": false} {
		e, ok := providers.Get(id)
		if !ok {
			t.Fatalf("no provider %s", id)
		}
		if got := providerInfo(e, true).DeletesPosts; got != want {
			t.Errorf("%s: deletes_posts %v, want %v", id, got, want)
		}
	}
}
