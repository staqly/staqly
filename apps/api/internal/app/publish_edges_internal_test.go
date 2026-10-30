package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// fakeScripted publishes as its script says: each call takes the next error (nil = published); past the script
// every call publishes.
type fakeScripted struct {
	providers.Base
	mu    sync.Mutex
	calls int
	errs  []error
}

func (f *fakeScripted) Publish(context.Context, providers.Credentials, providers.PostContent, providers.Settings) (providers.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		if err != nil {
			return providers.Result{}, err
		}
	}
	n := strconv.Itoa(f.calls)
	return providers.Result{ExternalID: f.ID() + "-" + n, URL: "https://example.com/" + f.ID() + "/" + n}, nil
}

func (f *fakeScripted) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// scriptedChannel connects a channel on a fake platform of its own (id) that answers with errs.
func scriptedChannel(t *testing.T, s *Service, p Principal, id string, errs ...error) (*fakeScripted, string) {
	t.Helper()
	f := &fakeScripted{Base: providers.NewBase(id, id, providers.AuthNone, providers.Limits{MaxChars: 280}), errs: errs}
	providers.Register(f, providers.StatusAvailable)
	t.Cleanup(func() { providers.Unregister(id) })
	e, _ := providers.Get(id)
	ch, err := s.saveChannel(context.Background(), p, e, id, providers.Credentials{}, providers.Profile{}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	return f, ch.ID
}

func targetOf(t *testing.T, post Post, channelID string) PostTarget {
	t.Helper()
	for _, tg := range post.Targets {
		if tg.ChannelID == channelID {
			return tg
		}
	}
	t.Fatalf("no target for %s in %+v", channelID, post.Targets)
	return PostTarget{}
}

// One channel takes the post, the other refuses it: the post fails, and retrying the refused target publishes
// there only. The channel that has it is never asked again (no double post), and can't be retried.
func TestPartialFailureRetriesOnlyTheFailedChannel(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := context.Background()
	ok, okID := scriptedChannel(t, s, p, "fakeok")
	no, noID := scriptedChannel(t, s, p, "fakeno", &providers.Failure{Kind: providers.ErrRejected, Msg: "too spicy"})
	if _, err := s.UpdatePost(ctx, p, post.ID, PostPatch{Targets: &[]PostTargetInput{{ChannelID: okID}, {ChannelID: noID}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PublishPost(ctx, p, post.ID); err != nil {
		t.Fatal(err)
	}
	s.RunDueJobsOnce(ctx)
	got, _ := s.GetPost(ctx, p, post.ID)
	if got.Status != "failed" || targetOf(t, got, okID).Status != "published" || targetOf(t, got, noID).Status != "failed" ||
		!strings.Contains(targetOf(t, got, noID).Error, "too spicy") {
		t.Fatalf("after the first run: %s %+v", got.Status, got.Targets)
	}

	var conflictErr *Error
	if _, err := s.RetryTarget(ctx, p, post.ID, okID); !errors.As(err, &conflictErr) || conflictErr.Status != 409 {
		t.Fatalf("retrying a published target: %v, want 409", err)
	}
	if _, err := s.RetryTarget(ctx, p, post.ID, noID); err != nil {
		t.Fatal(err)
	}
	s.RunDueJobsOnce(ctx)
	got, _ = s.GetPost(ctx, p, post.ID)
	if got.Status != "published" || targetOf(t, got, noID).Status != "published" || ok.count() != 1 || no.count() != 2 {
		t.Fatalf("after the retry: %s %+v (calls: ok %d, no %d)", got.Status, got.Targets, ok.count(), no.count())
	}
}

// A platform that keeps failing for a moment is tried again with backoff (the error shows meanwhile); once every
// attempt failed the target fails with the last error, and the platform was asked once per attempt.
func TestTransientPublishErrorsRetryThenFail(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := context.Background()
	boom := errors.New("platform said 503")
	f, chID := scriptedChannel(t, s, p, "fakeflap", boom, boom, boom, boom)
	if _, err := s.UpdatePost(ctx, p, post.ID, PostPatch{Targets: &[]PostTargetInput{{ChannelID: chID}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PublishPost(ctx, p, post.ID); err != nil {
		t.Fatal(err)
	}
	s.RunDueJobsOnce(ctx)
	got, _ := s.GetPost(ctx, p, post.ID)
	if tg := targetOf(t, got, chID); got.Status != "publishing" || tg.Status != "publishing" || tg.Attempts != 1 || !strings.Contains(tg.Error, "503") {
		t.Fatalf("after one failure: %s %+v", got.Status, tg)
	}
	start := time.Now()
	for _, later := range []time.Duration{2 * time.Minute, 10 * time.Minute, 30 * time.Minute, time.Hour} {
		s.Now = func() time.Time { return start.Add(later) }
		s.RunDueJobsOnce(ctx)
	}
	got, _ = s.GetPost(ctx, p, post.ID)
	if tg := targetOf(t, got, chID); got.Status != "failed" || tg.Status != "failed" || !strings.Contains(tg.Error, "503") || f.count() != 3 {
		t.Fatalf("after every attempt: %s %+v (calls %d)", got.Status, tg, f.count())
	}
}

// A platform refusing the sign-in fails the target at once (no retries) and marks the channel; posts on it
// after that fail without asking the platform.
func TestReauthOnPublishMarksTheChannel(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := context.Background()
	f, chID := scriptedChannel(t, s, p, "fakegone", &providers.Failure{Kind: providers.ErrReauth, Msg: "token revoked"})
	if _, err := s.UpdatePost(ctx, p, post.ID, PostPatch{Targets: &[]PostTargetInput{{ChannelID: chID}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PublishPost(ctx, p, post.ID); err != nil {
		t.Fatal(err)
	}
	s.RunDueJobsOnce(ctx)
	got, _ := s.GetPost(ctx, p, post.ID)
	row, _ := s.Q.GetChannelByID(ctx, chID)
	if got.Status != "failed" || row.Status != "needs_reconnect" || !strings.Contains(row.LastError, "token revoked") || f.count() != 1 {
		t.Fatalf("post %s, channel %s %q, calls %d", got.Status, row.Status, row.LastError, f.count())
	}
	if _, err := s.RetryTarget(ctx, p, post.ID, chID); err != nil {
		t.Fatal(err)
	}
	s.RunDueJobsOnce(ctx)
	got, _ = s.GetPost(ctx, p, post.ID)
	if tg := targetOf(t, got, chID); tg.Status != "failed" || !strings.Contains(tg.Error, "reconnect") || f.count() != 1 {
		t.Fatalf("retry on a channel that needs reconnecting: %+v, calls %d", tg, f.count())
	}
}

// Was a bug: every post failing on a revoked sign-in marked the channel again, one channel.needs_reconnect event
// (and alert) each, also when they fail at the same moment, and a channel the person had turned off was switched to needs_reconnect.
func TestChannelNeedsReconnectOnce(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := context.Background()
	chID := post.Targets[0].ChannelID
	row, _ := s.Q.GetChannelByID(ctx, chID)
	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() {
			if err := s.channelNeedsReconnect(ctx, row, errors.New("token revoked")); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	evs, err := s.ListEvents(ctx, p, EventFilter{ChannelID: chID, Type: "channel.needs_reconnect"})
	if err != nil || len(evs.Data) != 1 {
		t.Fatalf("channel.needs_reconnect events: %d %v", len(evs.Data), err)
	}

	other := customChannelFor(t, s, p, "Paused")
	if err := s.W.SetChannelHealth(ctx, db.SetChannelHealthParams{Status: "disabled", UpdatedAt: s.now(), ID: other}); err != nil {
		t.Fatal(err)
	}
	row, _ = s.Q.GetChannelByID(ctx, other)
	if err := s.channelNeedsReconnect(ctx, row, errors.New("token revoked")); err != nil {
		t.Fatal(err)
	}
	if row, _ = s.Q.GetChannelByID(ctx, other); row.Status != "disabled" {
		t.Fatalf("a channel turned off became %s", row.Status)
	}
}

// Your automation's result after the processing timeout settled the target is refused (409), and so is a second
// result; the timeout fails the post. A result for a target that is not on your automation is refused too.
func TestCustomResultAfterTimeoutIsRefused(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := context.Background()
	s.Cfg.ProcessingTimeout = time.Minute
	custom := post.Targets[0].ChannelID
	_, fakeID := scriptedChannel(t, s, p, "fakesync")
	if _, err := s.UpdatePost(ctx, p, post.ID, PostPatch{Targets: &[]PostTargetInput{{ChannelID: custom}, {ChannelID: fakeID}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PublishPost(ctx, p, post.ID); err != nil {
		t.Fatal(err)
	}
	s.RunDueJobsOnce(ctx)
	var appErr *Error
	if _, err := s.ReportTargetResult(ctx, p, post.ID, fakeID, ResultInput{Status: "published"}); !errors.As(err, &appErr) || appErr.Status != 409 {
		t.Fatalf("a result for a platform target: %v, want 409", err)
	}
	if _, err := s.ReportTargetResult(ctx, p, post.ID, custom, ResultInput{Status: "done"}); !errors.As(err, &appErr) || appErr.Status != 422 {
		t.Fatalf("an unknown status: %v, want 422", err)
	}
	if _, err := s.ReportTargetResult(ctx, p, post.ID, custom, ResultInput{Status: "published", URL: "javascript:alert(1)"}); !errors.As(err, &appErr) || appErr.Status != 422 {
		t.Fatalf("a URL that is not http(s): %v, want 422", err)
	}
	if _, err := s.ReportTargetResult(ctx, p, post.ID, "ch_nope", ResultInput{Status: "published"}); !errors.As(err, &appErr) || appErr.Status != 404 {
		t.Fatalf("a target the post does not have: %v, want 404", err)
	}

	start := time.Now()
	s.Now = func() time.Time { return start.Add(2 * time.Minute) }
	s.RunDueJobsOnce(ctx)
	got, _ := s.GetPost(ctx, p, post.ID)
	if tg := targetOf(t, got, custom); got.Status != "failed" || tg.Status != "failed" || !strings.Contains(tg.Error, "in time") {
		t.Fatalf("after the timeout: %s %+v", got.Status, tg)
	}
	for range 2 {
		if _, err := s.ReportTargetResult(ctx, p, post.ID, custom, ResultInput{Status: "published", URL: "https://example.com/late"}); !errors.As(err, &appErr) || appErr.Status != 409 {
			t.Fatalf("a late result: %v, want 409", err)
		}
	}
	got, _ = s.GetPost(ctx, p, post.ID)
	if tg := targetOf(t, got, custom); tg.Status != "failed" || tg.ExternalURL != "" {
		t.Fatalf("a late result changed the target: %+v", tg)
	}
}

// Another workspace sees none of this one's posts, targets and channels by id: results, retries, targets on its
// channels and channel changes all answer as if they did not exist.
func TestTargetsAndChannelsAcrossWorkspaces(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := context.Background()
	custom := post.Targets[0].ChannelID
	if _, err := s.PublishPost(ctx, p, post.ID); err != nil {
		t.Fatal(err)
	}
	s.RunDueJobsOnce(ctx)
	if err := s.W.CreateWorkspace(ctx, db.CreateWorkspaceParams{ID: "ws_other", Name: "Other", RecoveryVerifier: "x",
		RecoveryRotatedAt: "t", CreatedAt: "t", UpdatedAt: "t"}); err != nil {
		t.Fatal(err)
	}
	other := Principal{WorkspaceID: "ws_other", Kind: PrincipalAPIKey, ID: "key_other", Name: "other", Scopes: AllScopes}
	col, err := s.CreateCollection(ctx, other, CollectionInput{Name: ptrTo("Theirs")})
	if err != nil {
		t.Fatal(err)
	}

	var appErr *Error
	notFound := func(what string, err error) {
		t.Helper()
		if !errors.As(err, &appErr) || appErr.Status != 404 && appErr.Status != 422 {
			t.Errorf("%s from another workspace: %v, want 404 or 422", what, err)
		}
	}
	_, err = s.ReportTargetResult(ctx, other, post.ID, custom, ResultInput{Status: "published"})
	notFound("a result", err)
	_, err = s.RetryTarget(ctx, other, post.ID, custom)
	notFound("a retry", err)
	_, err = s.CreatePost(ctx, other, PostInput{Title: "Post", Collection: col.ID, Content: "x", Targets: []PostTargetInput{{ChannelID: custom}}})
	notFound("a post on its channel", err)
	_, err = s.UpdateChannel(ctx, other, custom, ChannelPatch{Name: ptrTo("Taken")})
	notFound("a channel rename", err)
	notFound("a channel delete", s.DeleteChannel(ctx, other, custom))

	got, _ := s.GetPost(ctx, p, post.ID)
	ch, _ := s.Q.GetChannelByID(ctx, custom)
	if targetOf(t, got, custom).Status != "publishing" || ch.Name != "Worker" {
		t.Fatalf("another workspace changed this one: target %+v, channel %q", targetOf(t, got, custom), ch.Name)
	}
}

// The OAuth state is signed and short-lived: a state that was changed, signed with another key, expired, or made
// for another workspace or platform is refused, and the team's and a connect link's sign-ins can't finish each other.
func TestOAuthStateRefusals(t *testing.T) {
	s, p, _ := publishTest(t)
	ctx := context.Background()
	if len(s.Cfg.SecretKey) == 0 {
		s.Cfg.SecretKey = []byte("0123456789abcdef0123456789abcdef")
	}
	now := time.Now()
	s.Now = func() time.Time { return now }
	sign := func(st oauthState) string {
		raw, err := s.signState(st)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	good := oauthState{Provider: "custom", Workspace: p.WorkspaceID, Nonce: "n1", Expires: now.Add(10 * time.Minute).Unix()}
	if st, err := s.verifyState(sign(good), "custom", p.WorkspaceID); err != nil || st.Nonce != "n1" {
		t.Fatalf("a good state: %+v %v", st, err)
	}
	raw := sign(good)
	body, sig, _ := strings.Cut(raw, ".")
	forged := &Service{Cfg: Config{SecretKey: []byte("another key, another server.....")}}
	otherKey, _ := forged.signState(good)
	flipped := []byte(sig)
	flipped[0] ^= 1
	for name, state := range map[string]string{
		"empty":             "",
		"no signature":      body,
		"signature not hex": body + ".zz",
		"changed signature": body + "." + string(flipped),
		"changed body":      strings.Split(sign(oauthState{Provider: "custom", Workspace: "ws_other", Nonce: "n1", Expires: good.Expires}), ".")[0] + "." + sig,
		"another key":       otherKey,
		"other workspace":   sign(oauthState{Provider: "custom", Workspace: "ws_other", Nonce: "n1", Expires: good.Expires}),
		"other platform":    sign(oauthState{Provider: "x", Workspace: p.WorkspaceID, Nonce: "n1", Expires: good.Expires}),
	} {
		var appErr *Error
		if _, err := s.verifyState(state, "custom", p.WorkspaceID); !errors.As(err, &appErr) || appErr.Status != 422 {
			t.Errorf("%s: %v, want 422", name, err)
		}
	}
	expired := good
	expired.Expires = now.Add(-time.Second).Unix()
	var appErr *Error
	if _, err := s.verifyState(sign(expired), "custom", p.WorkspaceID); !errors.As(err, &appErr) || appErr.Status != 410 {
		t.Fatalf("an expired state: %v, want 410", err)
	}

	link := good
	link.Link = "cl_1"
	if _, err := s.CompleteOAuth(ctx, p, "custom", "code", sign(link), "https://app.example/cb", nil); !errors.As(err, &appErr) || appErr.Status != 422 {
		t.Fatalf("a connect link's state in the team's sign-in: %v, want 422", err)
	}
	if _, _, err := s.CompleteOAuthViaLink(ctx, "custom", "code", sign(good), "https://app.example/cb", nil); !errors.As(err, &appErr) || appErr.Status != 422 {
		t.Fatalf("the team's state through a connect link: %v, want 422", err)
	}
	if _, _, err := s.CompleteOAuthViaLink(ctx, "x", "code", sign(link), "https://app.example/cb", nil); !errors.As(err, &appErr) || appErr.Status != 422 {
		t.Fatalf("a link state for another platform: %v, want 422", err)
	}
	if s.IsLinkState(sign(good)) || !s.IsLinkState(sign(link)) || s.IsLinkState("garbage") {
		t.Fatal("IsLinkState")
	}
}

// Was a bug: an error cut inside a character (or a platform answer that is not UTF-8) was stored as it was, and
// PostgreSQL refuses text that is not valid UTF-8: the target stayed publishing.
func TestStoredErrorsAreValidUTF8(t *testing.T) {
	for _, s := range []string{"bad \xff byte", strings.Repeat("ż", 600), "fine"} {
		if got := truncateRunes(s, 500); !utf8.ValidString(got) || utf8.RuneCountInString(got) > 503 {
			t.Errorf("%q: %q", s[:min(len(s), 20)], got[:min(len(got), 20)])
		}
	}
}
