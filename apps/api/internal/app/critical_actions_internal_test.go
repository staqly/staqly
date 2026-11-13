package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/blob"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// Was a bug: a thread reply the platform accepted, whose result the database refused, was sent again by the
// job's retry (the receipt that protects a post's target was not kept for replies).
func TestReplyIsNotSentAgainAfterAFailedResultWrite(t *testing.T) {
	for _, outage := range []string{"one attempt", "every attempt"} {
		t.Run(outage, func(t *testing.T) {
			s, p, post := publishTest(t)
			ctx := context.Background()
			old := settleRetry
			settleRetry = nil
			t.Cleanup(func() { settleRetry = old })
			var got []string
			f := &fakeReplier{Base: providers.NewBase("fakereplier", "Fake Replier", providers.AuthNone, providers.Limits{MaxChars: 280, MaxMedia: 4}), replies: &got}
			providers.Register(f, providers.StatusAvailable)
			t.Cleanup(func() { providers.Unregister(f.ID()) })
			e, _ := providers.Get(f.ID())
			ch, err := s.saveChannel(ctx, p, e, "Replies", providers.Credentials{}, providers.Profile{}, nil, "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.UpdatePost(ctx, p, post.ID, PostPatch{Targets: &[]PostTargetInput{{ChannelID: ch.ID}},
				Thread: &[]ThreadPart{{Content: "part one", DelaySeconds: 3600}}}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.PublishPost(ctx, p, post.ID); err != nil {
				t.Fatal(err)
			}
			s.RunDueJobsOnce(ctx) // the post is out; its reply waits an hour
			if pub, _ := s.GetPost(ctx, p, post.ID); pub.Status != "published" || len(got) != 0 {
				t.Fatalf("setup: post %s, replies sent %q", pub.Status, got)
			}

			lift := refuseWrites(t, s, `UPDATE OF replies ON post_targets`)
			s.Now = func() time.Time { return time.Now().Add(2 * time.Hour) }
			s.RunDueJobsOnce(ctx) // the platform takes the reply; recording it fails
			if outage == "one attempt" {
				lift()
			}
			for hours := 3; hours <= 5; hours++ { // the job's retries (1, 5, 15 min)
				s.Now = func() time.Time { return time.Now().Add(time.Duration(hours) * time.Hour) }
				s.RunDueJobsOnce(ctx)
			}
			if len(got) != 1 {
				t.Fatalf("the platform got the same reply %d times: %q", len(got), got)
			}
			if outage == "every attempt" {
				lift() // out of attempts: the minute sweep is not needed, jobFailed already ran (and was refused)
				return
			}
			pub, _ := s.GetPost(ctx, p, post.ID)
			if r := pub.Targets[0].Replies; len(r) != 1 || r[0].Status != "published" || r[0].ExternalID != "rpart one" {
				t.Fatalf("the reply was recorded as %+v, want published with the platform's id", r)
			}
		})
	}
}

type fakeSlowRefresh struct {
	providers.Base
	started, release chan struct{}
}

func (f *fakeSlowRefresh) Refresh(_ context.Context, _ providers.App, c providers.Credentials) (providers.Credentials, error) {
	close(f.started)
	<-f.release
	return providers.Credentials{"access_token": "renewed", "user_id": c["user_id"],
		"expires_at": time.Now().Add(60 * 24 * time.Hour).UTC().Format(time.RFC3339)}, nil
}

// Was a bug: a token refresh in flight when Meta's data deletion request wiped the channel wrote the renewed
// sign-in back afterwards. The refresh writes only over the credentials it read.
func TestRefreshDoesNotBringBackCredentialsAfterMetaDataDeletion(t *testing.T) {
	s, p, _ := publishTest(t)
	ctx := context.Background()
	real, had := providers.Get("threads")
	f := &fakeSlowRefresh{Base: providers.NewBase("threads", "Threads", providers.AuthOAuth2, providers.Limits{MaxChars: 500}),
		started: make(chan struct{}), release: make(chan struct{})}
	providers.Register(f, providers.StatusAvailable)
	t.Cleanup(func() {
		providers.Unregister("threads")
		if had {
			providers.Register(real.Provider, real.Status)
		}
	})
	env := map[string]string{"PROVIDER_THREADS_CLIENT_ID": "client", "PROVIDER_THREADS_CLIENT_SECRET": "th-secret", "PUBLIC_DASHBOARD_URL": "https://app.example"}
	s.Getenv = func(k string) string { return env[k] }
	e, _ := providers.Get("threads")
	ch, err := s.saveChannel(ctx, p, e, "Threads", providers.Credentials{"access_token": "a0", "user_id": "17841",
		"expires_at": time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)}, providers.Profile{}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.refreshChannel(ctx, ch.ID) }()
	select {
	case <-f.started:
	case err := <-done:
		t.Fatalf("setup: the refresh ended without asking the platform: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("setup: the refresh never reached the platform")
	}
	if _, _, err := s.MetaDataDeletion(ctx, signedRequest("th-secret", `{"user_id":"17841"}`)); err != nil {
		t.Fatal(err)
	}
	stored := func() (creds string) {
		if err := s.DB.QueryRow(`SELECT credentials FROM channels WHERE id = ?`, ch.ID).Scan(&creds); err != nil {
			t.Fatal(err)
		}
		return creds
	}
	if stored() != "" {
		t.Fatal("setup: the data deletion did not wipe the sign-in")
	}
	close(f.release)
	if err := <-done; err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if creds := stored(); creds != "" {
		var back providers.Credentials
		_ = s.unseal(creds, &back)
		t.Fatalf("after Meta was told the data is deleted, the refresh stored the sign-in again (access_token %q, user_id %q)",
			back["access_token"], back["user_id"])
	}
}

// Was a bug: a channel removed while its send was at the platform took the target with it; the platform then
// accepted the post, and staqly had no record of what went out (the post ended failed, with no target).
func TestRemovingAChannelDuringASendKeepsWhatWentOut(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := context.Background()
	f, ch := scriptedChannelOn(t, s, p, "scriptdc1")
	f.started, f.hold = make(chan struct{}, 1), make(chan struct{})
	targets := []PostTargetInput{{ChannelID: ch}}
	if _, err := s.UpdatePost(ctx, p, post.ID, PostPatch{Targets: &targets}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PublishPost(ctx, p, post.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { s.RunDueJobsOnce(ctx); close(done) }()
	<-f.started // the platform call is on its way
	if err := s.DeleteChannel(ctx, p, ch); err != nil {
		t.Fatal(err)
	}
	close(f.hold)
	<-done
	if _, sent := f.counts(); sent != 1 {
		t.Fatalf("setup: the platform accepted the post %d times, want 1", sent)
	}
	got, _ := s.GetPost(ctx, p, post.ID)
	if len(got.Targets) != 1 || got.Targets[0].Status != "published" || got.Targets[0].ExternalURL != "https://example.com/ext" || got.Status != "published" {
		t.Fatalf("the post is live at https://example.com/ext; staqly says post %s, targets %+v", got.Status, got.Targets)
	}
	if n := len(eventsOfType(t, s, p, "post.target_published")); n != 1 {
		t.Fatalf("%d post.target_published events, want 1", n)
	}
}

// Was a bug: a key with posts:publish but without schedules:manage turned a one-time schedule off by cancelling
// its queued run. Cancelling a schedule's run needs schedules:manage too.
func TestCancellingAScheduleRunNeedsSchedulesManage(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := context.Background()
	at := s.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	sc, err := s.CreateSchedule(ctx, p, ScheduleInput{Collection: &post.CollectionID, Name: ptrTo("Once"), Kind: ptrTo("once"), RunAt: &at})
	if err != nil {
		t.Fatal(err)
	}
	jobs := jobsByKey(t, s, "run_schedule:"+sc.ID)
	if len(jobs) != 1 || !sc.Enabled {
		t.Fatalf("setup: %d run_schedule jobs, schedule enabled %v", len(jobs), sc.Enabled)
	}
	key := Principal{WorkspaceID: p.WorkspaceID, Kind: PrincipalAPIKey, ID: "key_publisher", Name: "Publisher key",
		Scopes: []string{ScopePostsRead, ScopePostsPublish}}
	if _, err := s.UpdateSchedule(ctx, key, sc.ID, ScheduleInput{Enabled: ptrTo(false)}); err == nil {
		t.Fatal("setup: the key may manage schedules")
	}
	_, cancelErr := s.CancelJob(ctx, key, jobPublicID(jobs[0].ID))
	got, err := s.GetSchedule(ctx, p, sc.ID)
	if err != nil {
		t.Fatal(err)
	}
	var e *Error
	if !errors.As(cancelErr, &e) || e.Status != 403 || !got.Enabled {
		t.Fatalf("a key without schedules:manage cancelled the schedule's run (error: %v, want 403); the schedule is now enabled=%v, last result %q",
			cancelErr, got.Enabled, got.LastResult)
	}
	key.Scopes = append(key.Scopes, ScopeSchedulesManage)
	if _, err := s.CancelJob(ctx, key, jobPublicID(jobs[0].ID)); err != nil {
		t.Fatalf("cancel with schedules:manage: %v", err)
	}
	if got, _ = s.GetSchedule(ctx, p, sc.ID); got.Enabled {
		t.Fatal("the cancelled one-time schedule is still on")
	}
}

// Was a bug: a custom endpoint failed every try of attempt A and the person retried the failed job in the queue,
// which brought the same job back: a late result naming A settled the retry. The retry is a new job, as after
// Retry on the post (TestLateResultOfATimedOutAttemptDoesNotSettleTheRetry).
func TestLateResultOfAFailedJobDoesNotSettleItsQueueRetry(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := context.Background()
	s.Cfg.Webhook.AllowLoopback = true
	var mu sync.Mutex
	var asked []string
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			AttemptID string `json:"attempt_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		asked = append(asked, body.AttemptID)
		mu.Unlock()
		w.WriteHeader(http.StatusBadGateway) // the automation took the post, the answer was lost
	}))
	t.Cleanup(endpoint.Close)
	ch, err := s.ConnectChannel(ctx, p, ChannelInput{Provider: "custom", Name: "Endpoint", Fields: map[string]string{"delivery": "endpoint", "url": endpoint.URL}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdatePost(ctx, p, post.ID, PostPatch{Targets: &[]PostTargetInput{{ChannelID: ch.ID}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PublishPost(ctx, p, post.ID); err != nil {
		t.Fatal(err)
	}
	for hours := 0; hours < 4; hours++ { // every try of the job
		s.Now = func() time.Time { return time.Now().Add(time.Duration(hours) * time.Hour) }
		s.RunDueJobsOnce(ctx)
	}
	jobs := jobsByKey(t, s, "publish_target:"+post.ID+":"+ch.ID)
	got, _ := s.GetPost(ctx, p, post.ID)
	mu.Lock()
	attemptA := ""
	if len(asked) > 0 {
		attemptA = asked[0]
	}
	mu.Unlock()
	if len(jobs) != 1 || jobStatus(jobs[0].State) != "failed" || got.Targets[0].Status != "failed" || attemptA == "" {
		t.Fatalf("setup: jobs %d, target %+v, endpoint asked with attempts %v", len(jobs), got.Targets[0], asked)
	}
	retried, err := s.RetryJob(ctx, p, jobPublicID(jobs[0].ID))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ = s.GetPost(ctx, p, post.ID); got.Targets[0].Status != "publishing" || retried.ID == attemptA || retried.Status != "queued" {
		t.Fatalf("the retry is job %s (%s), attempt A was %s; the target is %s", retried.ID, retried.Status, attemptA, got.Targets[0].Status)
	}
	var e *Error
	_, err = s.ReportTargetResult(ctx, p, post.ID, ch.ID, ResultInput{Status: "failed", Error: "attempt A gave up", AttemptID: attemptA})
	got, _ = s.GetPost(ctx, p, post.ID)
	if !errors.As(err, &e) || e.Status != 409 || got.Targets[0].Status != "publishing" {
		t.Fatalf("A's late report (attempt %s) after the queue retry: error %v, want 409; the retried target is now %s (%q)",
			attemptA, err, got.Targets[0].Status, got.Targets[0].Error)
	}
}

// Was a bug: cancelling a queued publish_target job cancelled the job in one transaction and failed its target
// in the next; when that second write was refused the target stayed publishing with no job, and cancel, retry
// of the job and retry of the target were all refused. Both happen together now, so the cancel can be repeated.
func TestCancelThatCannotSettleTheTargetLeavesTheJobQueued(t *testing.T) {
	s, p, post := publishTest(t)
	ctx := context.Background()
	ch := post.Targets[0].ChannelID
	if _, err := s.PublishPost(ctx, p, post.ID); err != nil {
		t.Fatal(err)
	}
	jobs := jobsByKey(t, s, "publish_target:"+post.ID+":"+ch)
	if len(jobs) != 1 {
		t.Fatalf("setup: %d publish_target jobs", len(jobs))
	}
	id := jobPublicID(jobs[0].ID)
	lift := refuseWrites(t, s, `UPDATE ON post_targets WHEN NEW.status = 'failed'`)
	_, cancelErr := s.CancelJob(ctx, p, id)
	lift()
	if cancelErr == nil {
		t.Fatal("setup: the cancel succeeded although the target could not be settled")
	}
	job, _ := s.getJob(ctx, p, id)
	got, _ := s.GetPost(ctx, p, post.ID)
	if job.Status != "queued" || got.Targets[0].Status != "publishing" {
		t.Fatalf("after the refused cancel the job is %s and the target %s, want queued and publishing (nothing changed)", job.Status, got.Targets[0].Status)
	}
	if _, err := s.CancelJob(ctx, p, id); err != nil {
		t.Fatalf("cancel again: %v", err)
	}
	job, _ = s.getJob(ctx, p, id)
	if got, _ = s.GetPost(ctx, p, post.ID); job.Status != "cancelled" || got.Targets[0].Status != "failed" || got.Status != "failed" {
		t.Fatalf("after the cancel the job is %s, the target %s and the post %s", job.Status, got.Targets[0].Status, got.Status)
	}
	if _, err := s.RetryTarget(ctx, p, post.ID, ch); err != nil {
		t.Fatalf("retry of the cancelled target: %v", err)
	}
}

// faultyBlob is the media store with a scripted Open for media files: the first `stall` opens wait for
// release and then hand out a file whose read fails; with openErr set every open fails with it.
type faultyBlob struct {
	blob.Store
	stall   atomic.Int32
	waiting chan struct{}
	release chan struct{}
	openErr error
}

type brokenFile struct{ io.ReadSeekCloser }

func (brokenFile) Read([]byte) (int, error) { return 0, errors.New("read: connection reset by peer") }

func (b *faultyBlob) Open(ctx context.Context, key string) (io.ReadSeekCloser, error) {
	if !strings.Contains(key, "/media/") {
		return b.Store.Open(ctx, key)
	}
	if b.openErr != nil {
		return nil, b.openErr
	}
	f, err := b.Store.Open(ctx, key)
	if err != nil || b.stall.Add(-1) < 0 {
		return f, err
	}
	b.waiting <- struct{}{}
	<-b.release
	return brokenFile{f}, nil
}

func exportTest(t *testing.T) (*Service, Principal, *faultyBlob) {
	t.Helper()
	s, p, _ := publishTest(t)
	p.Scopes = AllScopes
	dir := t.TempDir()
	s.Getenv = func(k string) string { // the ZIP goes to DATA_DIR/exports
		if k == "DATA_DIR" {
			return dir
		}
		return ""
	}
	if _, err := s.UploadMedia(t.Context(), p, "photo.png", feedbackPNG(t), false); err != nil {
		t.Fatal(err)
	}
	b := &faultyBlob{Store: s.Cfg.Blob, waiting: make(chan struct{}, 1), release: make(chan struct{})}
	s.Cfg.Blob = b
	return s, p, b
}

// Was a bug: builder A stalled past its lease, builder B took the export over and finished it; A then woke up,
// failed and removed the ZIP under the export's name, which was B's by then.
func TestStaleExportBuilderLeavesTheWinnersArchive(t *testing.T) {
	s, p, b := exportTest(t)
	ctx := t.Context()
	s.pg = true // several server processes: nothing is requeued just because this one started
	var ahead atomic.Int64
	start := time.Now()
	s.Now = func() time.Time { return start.Add(time.Duration(ahead.Load())) }
	x, err := s.RequestWorkspaceExport(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if !x.IncludesMedia {
		t.Fatal("setup: a data-only export never opens a media file")
	}
	b.stall.Store(1)
	aDone := make(chan struct{})
	go func() { s.ProcessExports(ctx); close(aDone) }() // builder A
	<-b.waiting                                         // A is stalled in the middle of its build
	ahead.Store(int64(exportLease + time.Minute))
	s.ProcessExports(ctx) // builder B: the lease ran out, it builds the export again
	if st := exportStatus(t, s, x.ID); st != "ready" {
		t.Fatalf("setup: builder B left the export %s", st)
	}
	if _, err := os.Stat(s.exportPath(x.ID)); err != nil {
		t.Fatalf("setup: builder B's archive: %v", err)
	}
	close(b.release) // A wakes up and its build fails
	<-aDone
	st := exportStatus(t, s, x.ID)
	if _, err := os.Stat(s.exportPath(x.ID)); err != nil || st != "ready" {
		_, _, openErr := s.OpenWorkspaceExport(ctx, p, x.ID)
		t.Fatalf("after the stale builder ended the export is %s and its archive: %v (download: %v)", st, err, openErr)
	}
}

// Was a bug: a full export whose media files could not be opened because the store was failing (not because
// they were gone) turned ready without them. It fails now; a file that is really missing is still left out.
func TestExportFailsWhenTheStoreCannotOpenAMediaFile(t *testing.T) {
	s, p, b := exportTest(t)
	ctx := t.Context()
	b.openErr = blob.ErrNotExist
	gone, err := s.RequestWorkspaceExport(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	s.ProcessExports(ctx)
	if st := exportStatus(t, s, gone.ID); st != "ready" {
		t.Fatalf("an export with a media file that is gone is %s, want ready without it", st)
	}
	s.Now = func() time.Time { return time.Now().Add(48 * time.Hour) } // the first export expired
	b.openErr = errors.New("s3: 503 SlowDown")
	x, err := s.RequestWorkspaceExport(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	s.ProcessExports(ctx)
	got, err := s.GetWorkspaceExport(ctx, p, x.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IncludesMedia {
		t.Fatal("setup: a data-only export")
	}
	if got.Status != "failed" {
		t.Fatalf("the export is %s (includes_media %v, error %q) although the store failed to open its one media file", got.Status, got.IncludesMedia, got.Error)
	}
}
