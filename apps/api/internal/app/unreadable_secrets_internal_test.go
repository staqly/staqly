package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/staqly/staqly/apps/api/internal/providers"
)

// A channel whose sealed credentials this server's key can't open (a restore without secret.key, a changed
// STAQLY_SECRET_KEY): the publish fails at once with a reason the person can act on, without asking the platform
// or retrying, the channel turns needs_reconnect, and its check reports the same instead of a 500.
func TestUnreadableCredentialsFailFastAndMarkTheChannel(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := context.Background()
	f, chID := scriptedChannel(t, s, p, "fakesealed")
	if _, err := s.UpdatePost(ctx, p, post.ID, PostPatch{Targets: &[]PostTargetInput{{ChannelID: chID}}}); err != nil {
		t.Fatal(err)
	}
	other := append([]byte{}, s.Cfg.SecretKey...)
	other[0] ^= 0xff
	s.Cfg.SecretKey = other

	if _, err := s.PublishPost(ctx, p, post.ID); err != nil {
		t.Fatal(err)
	}
	s.RunDueJobsOnce(ctx)
	got, _ := s.GetPost(ctx, p, post.ID)
	tg := targetOf(t, got, chID)
	row, _ := s.Q.GetChannelByID(ctx, chID)
	if got.Status != "failed" || tg.Status != "failed" || tg.Attempts != 1 || !strings.Contains(tg.Error, "reconnect the channel") || f.count() != 0 {
		t.Fatalf("post %s, target %+v, calls %d", got.Status, tg, f.count())
	}
	if row.Status != "needs_reconnect" || !strings.Contains(row.LastError, "cannot be read") {
		t.Fatalf("channel %s %q", row.Status, row.LastError)
	}

	res, err := s.TestChannel(ctx, p, chID)
	if err != nil || res["ok"] != false || !strings.Contains(res["error"].(string), "cannot be read") {
		t.Fatalf("check: %v %v", res, err)
	}
	if _, err := s.channelCredentials(ctx, row); !errors.Is(err, providers.ErrReauth) {
		t.Fatalf("channelCredentials: %v, want ErrReauth", err)
	}
}
