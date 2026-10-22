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

// fakeNoRenew is a platform without refresh tokens (LinkedIn for most apps): a refresh keeps the token while it
// works and fails once it ran out.
type fakeNoRenew struct {
	providers.Base
	now func() time.Time
}

func (f *fakeNoRenew) Refresh(_ context.Context, _ providers.App, c providers.Credentials) (providers.Credentials, error) {
	if exp, err := time.Parse(time.RFC3339, c["expires_at"]); err == nil && exp.After(f.now()) {
		return c, nil
	}
	return nil, &providers.Failure{Kind: providers.ErrReauth, Msg: "signs apps out after 60 days; reconnect the account"}
}

// A token that can't be renewed is looked at again when it runs out: the channel turns needs_reconnect (and
// alerts) then, even if nothing is published, instead of failing its next post.
func TestUnrenewableTokenNeedsReconnectWhenItRunsOut(t *testing.T) {
	s, p, _ := publishTest(t)
	ctx := context.Background()
	base := s.Now()
	orig, _ := providers.Get("x")
	providers.Register(&fakeNoRenew{Base: providers.NewBase("x", "X", providers.AuthOAuth2, providers.Limits{MaxChars: 280}), now: func() time.Time { return s.Now() }}, providers.StatusAvailable)
	t.Cleanup(func() { providers.Register(orig.Provider, orig.Status) })
	s.Getenv = func(k string) string {
		if k == providerEnv("x")+"CLIENT_ID" {
			return "client"
		}
		return ""
	}
	exp := base.Add(24 * time.Hour).UTC().Truncate(time.Second)
	sealed, err := s.seal(providers.Credentials{"access_token": "at", "expires_at": exp.Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.W.CreateChannel(ctx, db.CreateChannelParams{ID: "chn_x", WorkspaceID: p.WorkspaceID, Provider: "x", Name: "Brand",
		Status: "connected", Credentials: sealed, Settings: "{}", CreatedAt: s.now(), UpdatedAt: s.now()}); err != nil {
		t.Fatal(err)
	}

	// A day before: still valid, nothing renewed; the next look is when it runs out.
	if err := s.refreshChannel(ctx, "chn_x"); err != nil {
		t.Fatal(err)
	}
	if row, _ := s.Q.GetChannelByID(ctx, "chn_x"); row.Status != "connected" {
		t.Fatalf("a token still valid: %q", row.Status)
	}
	js := jobsByKey(t, s, "refresh_channel:chn_x")
	if len(js) != 1 || js[0].ScheduledAt.Sub(exp.Add(time.Minute)).Abs() > time.Second {
		t.Fatalf("refresh jobs %+v, want one at %s", js, exp.Add(time.Minute))
	}

	// Then: it ran out, the channel needs reconnecting and says so.
	s.Now = func() time.Time { return exp.Add(time.Minute) }
	if err := s.refreshChannel(ctx, "chn_x"); err != nil {
		t.Fatal(err)
	}
	row, _ := s.Q.GetChannelByID(ctx, "chn_x")
	evs, _ := s.ListEvents(ctx, p, EventFilter{ChannelID: "chn_x", Type: "channel.needs_reconnect"})
	if row.Status != "needs_reconnect" || len(evs.Data) != 1 {
		t.Fatalf("after it ran out: %q, %d events", row.Status, len(evs.Data))
	}
}
