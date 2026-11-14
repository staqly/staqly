package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
	"github.com/staqly/staqly/apps/api/internal/store"
	"github.com/staqly/staqly/apps/api/internal/store/storetest"
)

const refreshProcessKey = "01234567890123456789012345678901"

type processRotatingProvider struct {
	providers.Base
	endpoint string
}

func (p processRotatingProvider) Refresh(ctx context.Context, _ providers.App, creds providers.Credentials) (providers.Credentials, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint+"/refresh", strings.NewReader(creds["refresh_token"]))
	if err != nil {
		return nil, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, &providers.Failure{Kind: providers.ErrReauth, Msg: "invalid_grant: refresh token already rotated"}
	}
	var fresh providers.Credentials
	err = json.NewDecoder(res.Body).Decode(&fresh)
	return fresh, err
}

func refreshProcessProvider(endpoint string) processRotatingProvider {
	return processRotatingProvider{Base: providers.NewBase("fakeprocessrefresh", "Rotating OAuth", providers.AuthOAuth2, providers.Limits{}), endpoint: endpoint}
}

func TestOAuthRefreshProcessHelper(t *testing.T) {
	if os.Getenv("STAQLY_REFRESH_PROCESS") != "1" {
		t.Skip("subprocess of TestOAuthRefreshAcrossProcesses")
	}
	ctx := t.Context()
	database, err := store.OpenPostgresRaw(ctx, os.Getenv("STAQLY_REFRESH_PROCESS_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	s := New(database, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{SecretKey: []byte(refreshProcessKey)})
	s.Getenv = func(k string) string {
		if k == "PROVIDER_FAKEPROCESSREFRESH_CLIENT_ID" {
			return "client"
		}
		return ""
	}
	p := refreshProcessProvider(os.Getenv("STAQLY_REFRESH_PROCESS_URL"))
	providers.Register(p, providers.StatusAvailable)
	row, creds, err := s.storedCredentials(ctx, os.Getenv("STAQLY_REFRESH_PROCESS_CHANNEL"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.Get(p.endpoint + "/ready")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	e, _ := providers.Get(p.ID())
	if _, err := s.refreshIfDue(ctx, row, e, creds); err != nil {
		t.Fatal(err)
	}
}

// Two OS processes have separate in-memory mutexes. Both can publish different posts through the same
// channel, while a rotating provider accepts each refresh token once (as X and Slack do).
func TestOAuthRefreshAcrossProcesses(t *testing.T) {
	if !storetest.Postgres() {
		t.Skip("two active server processes need PostgreSQL")
	}
	s, principal, _ := publishTest(t)
	s.Cfg.SecretKey = []byte(refreshProcessKey)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	firstRequest, secondRequest := make(chan struct{}), make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	ready := make(chan struct{}, 2)
	var mu sync.Mutex
	calls := 0
	current := "r0"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ready" {
			ready <- struct{}{}
			return
		}
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		calls++
		n := calls
		valid := string(raw) == current
		if valid {
			current = "r1"
		}
		mu.Unlock()
		if n == 1 {
			close(firstRequest)
			<-release
		} else if n == 2 {
			close(secondRequest)
		}
		if !valid {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(providers.Credentials{"access_token": "a1", "refresh_token": "r1", "expires_at": time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)})
	}))
	defer func() {
		releaseOnce.Do(func() { close(release) })
		server.Close()
	}()
	p := refreshProcessProvider(server.URL)
	providers.Register(p, providers.StatusAvailable)
	t.Cleanup(func() { providers.Unregister(p.ID()) })
	e, _ := providers.Get(p.ID())
	channel, err := s.saveChannel(ctx, principal, e, "Shared rotating sign-in", providers.Credentials{"access_token": "a0", "refresh_token": "r0", "expires_at": time.Now().Add(time.Minute).UTC().Format(time.RFC3339)}, providers.Profile{}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	var schema string
	if err := s.DB.QueryRowContext(ctx, "SELECT current_schema()").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	dsn, _ := url.Parse(os.Getenv("STAQLY_TEST_DATABASE_URL"))
	query := dsn.Query()
	query.Set("search_path", schema)
	appName := "oauth_refresh_process_" + randomString(8)
	query.Set("application_name", appName)
	dsn.RawQuery = query.Encode()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	run := func() <-chan error {
		done := make(chan error, 1)
		go func() {
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestOAuthRefreshProcessHelper$", "-test.timeout=15s")
			cmd.Env = append(os.Environ(), "STAQLY_REFRESH_PROCESS=1", "STAQLY_REFRESH_PROCESS_DSN="+dsn.String(), "STAQLY_REFRESH_PROCESS_URL="+server.URL, "STAQLY_REFRESH_PROCESS_CHANNEL="+channel.ID)
			output, err := cmd.CombinedOutput()
			if err != nil {
				err = errors.New(string(output))
			}
			done <- err
		}()
		return done
	}
	first := run()
	select {
	case <-firstRequest:
	case <-ctx.Done():
		t.Fatal("first process never reached token provider")
	}
	<-ready
	second := run()
	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal("second process never read the channel")
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
waiting:
	for {
		select {
		case <-secondRequest: // The old process-local lock allows the duplicate provider request.
			break waiting
		case <-ticker.C:
			var waiting bool
			if err := s.DB.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE application_name = ? AND wait_event_type = 'Lock' AND wait_event = 'advisory')", appName).Scan(&waiting); err != nil {
				t.Fatal(err)
			}
			if waiting { // The second process reached the shared lock before the first may finish.
				break waiting
			}
		case <-ctx.Done():
			t.Fatal("second process neither refreshed nor waited for the shared lock")
		}
	}
	releaseOnce.Do(func() { close(release) })
	for _, done := range []<-chan error{first, second} {
		if err := <-done; err != nil {
			t.Error(err)
		}
	}
	row, creds, err := s.storedCredentials(ctx, channel.ID)
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 || row.Status != "connected" || creds["refresh_token"] != "r1" {
		t.Fatalf("two processes spent the same token: calls=%d channel=%s stored_refresh=%s", calls, row.Status, creds["refresh_token"])
	}
}

func refreshProcessDSN(t *testing.T, s *Service) string {
	t.Helper()
	var schema string
	if err := s.DB.QueryRow("SELECT current_schema()").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	dsn, _ := url.Parse(os.Getenv("STAQLY_TEST_DATABASE_URL"))
	query := dsn.Query()
	query.Set("search_path", schema)
	dsn.RawQuery = query.Encode()
	return dsn.String()
}

func TestOAuthRefreshLockReleaseAfterCancellation(t *testing.T) {
	s, p, _ := publishTest(t)
	other := s
	if storetest.Postgres() {
		pool, err := store.OpenPostgresRaw(t.Context(), refreshProcessDSN(t, s))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { pool.Close() })
		other = New(pool, nil, Config{})
	}
	row := db.Channel{WorkspaceID: p.WorkspaceID, Provider: "cancel-refresh"}
	ctx, cancel := context.WithCancel(t.Context())
	unlock, err := s.lockRefresh(ctx, row)
	if err != nil {
		t.Fatal(err)
	}
	waiting, stopWaiting := context.WithCancel(t.Context())
	stopWaiting()
	if _, err := s.lockRefresh(waiting, row); !errors.Is(err, context.Canceled) {
		t.Fatalf("a canceled waiter must not block behind the owner: %v", err)
	}
	cancel()
	unlock()
	next, stop := context.WithTimeout(t.Context(), time.Second)
	defer stop()
	unlock, err = other.lockRefresh(next, row)
	if err != nil {
		t.Fatalf("canceled owner left a refresh lock behind: %v", err)
	}
	unlock()
}

func TestOAuthRefreshWaitersKeepPoolCapacity(t *testing.T) {
	if !storetest.Postgres() {
		t.Skip("PostgreSQL reserves a session for the cross-process lock")
	}
	s, p, _ := publishTest(t)
	s.DB.SetMaxOpenConns(2)
	s = New(s.DB, nil, Config{})
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	unlock, err := s.lockRefresh(ctx, db.Channel{WorkspaceID: p.WorkspaceID, Provider: "first"})
	if err != nil {
		t.Fatal(err)
	}
	release := sync.OnceFunc(unlock)
	defer release()
	waiter := make(chan func(), 1)
	errCh := make(chan error, 1)
	go func() {
		unlock, err := s.lockRefresh(ctx, db.Channel{WorkspaceID: p.WorkspaceID, Provider: "second"})
		if err != nil {
			errCh <- err
			return
		}
		waiter <- unlock
	}()
	select {
	case unlock := <-waiter:
		unlock()
		t.Fatal("all pool connections were reserved for refresh locks")
	case err := <-errCh:
		t.Fatal(err)
	case <-time.After(50 * time.Millisecond):
	}
	query, stop := context.WithTimeout(ctx, time.Second)
	defer stop()
	var one int
	if err := s.DB.QueryRowContext(query, "SELECT 1").Scan(&one); err != nil {
		t.Fatalf("refresh holders/waiters starved credential reads and writes: %v", err)
	}
	release()
	select {
	case unlock := <-waiter:
		unlock()
	case err := <-errCh:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal("waiter never acquired the released refresh slot")
	}
}

func TestOAuthRefreshLockRecoversConnectionLoss(t *testing.T) {
	if !storetest.Postgres() {
		t.Skip("needs a PostgreSQL backend session")
	}
	s, p, _ := publishTest(t)
	u, _ := url.Parse(refreshProcessDSN(t, s))
	q := u.Query()
	appName := "oauth_refresh_test_" + randomString(8)
	q.Set("application_name", appName)
	u.RawQuery = q.Encode()
	pool, err := store.OpenPostgresRaw(t.Context(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	owner := New(pool, nil, Config{})
	row := db.Channel{WorkspaceID: p.WorkspaceID, Provider: "lost-session"}
	unlock, err := owner.lockRefresh(t.Context(), row)
	if err != nil {
		t.Fatal(err)
	}
	var killed bool
	err = s.DB.QueryRow("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = ? AND query LIKE 'SELECT pg_advisory_lock%'", appName).Scan(&killed)
	unlock() // must release local capacity even when the server can no longer answer the unlock
	if err != nil || !killed {
		t.Fatalf("terminate only the test lock session: killed=%v err=%v", killed, err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	unlock, err = owner.lockRefresh(ctx, row)
	if err != nil {
		t.Fatalf("failed unlock left an unusable session or a local gate in the pool: %v", err)
	}
	unlock()
}
