package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/providers"
)

// removedDuringRefresh is a platform whose token refresh is still under way when the person removes the channel.
type removedDuringRefresh struct {
	providers.Base
	remove func()
}

func (p removedDuringRefresh) Refresh(context.Context, providers.App, providers.Credentials) (providers.Credentials, error) {
	p.remove()
	return providers.Credentials{"access_token": "fresh", "refresh_token": "r1"}, nil
}

// Removing a channel takes its credentials away for good: a refresh that ends afterwards must not put new ones
// into the removed row.
func TestRefreshAfterChannelRemovalStoresNoCredentials(t *testing.T) {
	s, p, _ := publishTest(t)
	ctx := context.Background()
	s.Getenv = func(k string) string {
		if k == "PROVIDER_FAKEREMOVEDREFRESH_CLIENT_ID" {
			return "client"
		}
		return ""
	}
	var channelID string
	prov := removedDuringRefresh{Base: providers.NewBase("fakeremovedrefresh", "Removed OAuth", providers.AuthOAuth2, providers.Limits{}),
		remove: func() {
			if err := s.DeleteChannel(ctx, p, channelID); err != nil {
				t.Error(err)
			}
		}}
	providers.Register(prov, providers.StatusAvailable)
	e, _ := providers.Get(prov.ID())
	ch, err := s.saveChannel(ctx, p, e, "Account", providers.Credentials{"access_token": "old", "refresh_token": "r0",
		"expires_at": s.Now().Add(-time.Minute).UTC().Format(time.RFC3339)}, providers.Profile{}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	channelID = ch.ID
	row, creds, err := s.storedCredentials(ctx, ch.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The renewed sign-in is dropped: the caller gets no credentials for the removed channel and tries again.
	if fresh, err := s.refreshIfDue(ctx, row, e, creds); err == nil || errors.Is(err, providers.ErrReauth) || fresh != nil {
		t.Fatalf("refresh of a channel removed meanwhile: %v %v, want an error worth a retry", fresh, err)
	}
	var stored string
	if err := s.DB.QueryRowContext(ctx, "SELECT credentials FROM channels WHERE id = ?", ch.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "" {
		t.Fatalf("the removed channel holds credentials again (%d bytes)", len(stored))
	}
}
