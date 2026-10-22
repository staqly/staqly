package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// fakeOAuth is an OAuth platform whose token refresh fails or works as the test says.
type fakeOAuth struct {
	providers.Base
	fail bool
}

func (f *fakeOAuth) Refresh(_ context.Context, _ providers.App, c providers.Credentials) (providers.Credentials, error) {
	if f.fail {
		return nil, errors.New("invalid_grant: refresh token revoked")
	}
	out := providers.Credentials{}
	for k, v := range c {
		out[k] = v
	}
	out["access_token"] = "fresh"
	return out, nil
}

// A channel whose token refresh fails becomes needs_reconnect with the reason and a channel.needs_reconnect
// event (which alerts); one that refreshes keeps working with the new token.
func TestRefreshFailureMarksTheChannelNeedsReconnect(t *testing.T) {
	s, p, _ := publishTest(t)
	ctx := context.Background()
	orig, _ := providers.Get("x")
	fake := &fakeOAuth{Base: providers.NewBase("x", "X", providers.AuthOAuth2, providers.Limits{MaxChars: 280})}
	providers.Register(fake, providers.StatusAvailable)
	t.Cleanup(func() { providers.Register(orig.Provider, orig.Status) })
	s.Getenv = func(k string) string {
		if k == providerEnv("x")+"CLIENT_ID" {
			return "client"
		}
		return ""
	}
	sealed, err := s.seal(providers.Credentials{"access_token": "old", "refresh_token": "r1",
		"expires_at": s.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.W.CreateChannel(ctx, db.CreateChannelParams{ID: "chn_x", WorkspaceID: p.WorkspaceID, Provider: "x", Name: "Brand on X",
		Status: "connected", Credentials: sealed, Settings: "{}", CreatedAt: s.now(), UpdatedAt: s.now()}); err != nil {
		t.Fatal(err)
	}

	if err := s.refreshChannel(ctx, "chn_x"); err != nil {
		t.Fatal(err)
	}
	row, creds, err := s.storedCredentials(ctx, "chn_x")
	if err != nil || row.Status != "connected" || creds["access_token"] != "fresh" {
		t.Fatalf("a working refresh: status %q, token %q, %v", row.Status, creds["access_token"], err)
	}

	fake.fail = true
	if err := s.refreshChannel(ctx, "chn_x"); err != nil {
		t.Fatal(err)
	}
	row, err = s.Q.GetChannelByID(ctx, "chn_x")
	if err != nil || row.Status != "needs_reconnect" || !strings.Contains(row.LastError, "invalid_grant") {
		t.Fatalf("a failed refresh: %+v %v", row, err)
	}
	evs, err := s.ListEvents(ctx, p, EventFilter{ChannelID: "chn_x", Type: "channel.needs_reconnect"})
	if err != nil || len(evs.Data) != 1 {
		t.Fatalf("channel.needs_reconnect events: %+v %v", evs.Data, err)
	}
}

// A schedule run that ran out of attempts records "failed: <why>" and a schedule.failed event (which alerts).
func TestScheduleRunFailureIsRecorded(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := context.Background()
	at := s.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	sc, err := s.CreateSchedule(ctx, p, ScheduleInput{Collection: &post.CollectionID, Name: ptrTo("Daily"), Kind: ptrTo("once"), RunAt: &at})
	if err != nil {
		t.Fatal(err)
	}
	s.jobFailed(ctx, "run_schedule", jobArgs{WorkspaceID: p.WorkspaceID, Payload: jobPayload{ScheduleID: sc.ID}}, errors.New("endpoint answered 500"))
	got, err := s.GetSchedule(ctx, p, sc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.LastResult, "failed: endpoint answered 500") {
		t.Fatalf("last result %q", got.LastResult)
	}
	evs, err := s.ListEvents(ctx, p, EventFilter{ScheduleID: sc.ID, Type: "schedule.failed"})
	if err != nil || len(evs.Data) != 1 {
		t.Fatalf("schedule.failed events: %+v %v", evs.Data, err)
	}
}
