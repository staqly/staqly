package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/logctx"
)

// Dispatch: the emit hook writes one notification_outbox row per matching
// channel inside the event's transaction; RunNotifyWorker batches pending rows
// per channel (digest window) and sends them with retries and backoff.

func init() {
	RegisterEmitHook(notifyEmitHook)
	RegisterWorker(BackgroundWorker{Name: "notifications", Run: func(ctx context.Context, s *Service) { s.RunNotifyWorker(ctx) }})
	RegisterWorker(BackgroundWorker{Name: "telegram-poller", Run: func(ctx context.Context, s *Service) { s.RunTelegramPoller(ctx) }})
}

func notifyEmitHook(ctx context.Context, s *Service, q *db.Queries, ev db.Event) error {
	if strings.HasPrefix(ev.Type, "notification.") {
		return nil
	}
	chans, err := q.ListEnabledNotifyChannels(ctx, ev.WorkspaceID)
	if err != nil || len(chans) == 0 {
		return err
	}
	pendingCreate := -1 // lazily computed: is the created item pending?
	for _, c := range chans {
		if !channelMatches(c.Events, c.CollectionIds, ev.Type, ev.CollectionID.String) {
			continue
		}
		if ev.Type == "item.created" {
			if pendingCreate == -1 {
				pendingCreate = 0
				if eventItemStatus(ev.Data) == "pending" {
					pendingCreate = 1
				}
			}
			if pendingCreate == 0 {
				continue
			}
		}
		created, _ := time.Parse(timeFormat, ev.CreatedAt)
		next := formatTime(created.Add(time.Duration(c.DigestWindowSeconds) * time.Second))
		if err := q.InsertNotifyOutbox(ctx, db.InsertNotifyOutboxParams{ID: newID("ntf"), WorkspaceID: ev.WorkspaceID,
			ChannelID: c.ID, EventID: ev.ID, EventType: ev.Type, ItemID: ev.ItemID, NextAttemptAt: next,
			CreatedAt: ev.CreatedAt}); err != nil {
			return err
		}
	}
	return nil
}

func eventItemStatus(data string) string {
	var d struct {
		Item struct {
			Status string `json:"status"`
		} `json:"item"`
	}
	_ = json.Unmarshal([]byte(data), &d)
	return d.Item.Status
}

func channelMatches(eventsJSON, colsJSON, typ, collectionID string) bool {
	var cols []string
	_ = json.Unmarshal([]byte(colsJSON), &cols)
	if len(cols) > 0 {
		found := false
		for _, c := range cols {
			found = found || c == collectionID
		}
		if !found {
			return false
		}
	}
	var pats []string
	_ = json.Unmarshal([]byte(eventsJSON), &pats)
	for _, p := range pats {
		if eventMatches(p, typ) {
			return true
		}
	}
	return false
}

// ---- worker ----

func (s *Service) envDuration(key string, def time.Duration) time.Duration {
	if d, err := time.ParseDuration(s.Env(key, "")); err == nil && d > 0 {
		return d
	}
	return def
}

func (s *Service) envInt(key string, def int) int {
	if n, err := strconv.Atoi(s.Env(key, "")); err == nil && n > 0 {
		return n
	}
	return def
}

// RunNotifyWorker sends due notifications until ctx is cancelled.
func (s *Service) RunNotifyWorker(ctx context.Context) {
	tick := time.NewTicker(s.envDuration("NOTIFY_POLL_INTERVAL", 2*time.Second))
	defer tick.Stop()
	purge := time.NewTicker(6 * time.Hour)
	defer purge.Stop()
	for {
		if err := s.ProcessNotifyOutbox(ctx); err != nil && ctx.Err() == nil {
			s.logger(ctx).Error("notification worker", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-purge.C:
			_ = s.W.PurgeDecisionLinks(ctx, formatTime(s.Now().Add(-24*time.Hour)))
		}
	}
}

var notifyMu sync.Map // *Service -> *sync.Mutex (one processing pass at a time)

// ProcessNotifyOutbox runs one pass: every channel with a due row gets one
// message containing all of its pending rows.
// ponytail: single process; a second server instance would need row leases like webhook deliveries.
func (s *Service) ProcessNotifyOutbox(ctx context.Context) error {
	m, _ := notifyMu.LoadOrStore(s, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	ids, err := s.Q.DueNotifyChannels(ctx, db.DueNotifyChannelsParams{NextAttemptAt: s.now(), Limit: 50})
	if err != nil {
		return err
	}
	// A small worker pool: one slow or hanging channel (SMTP, a dead
	// webhook) must not delay every other channel's notifications.
	sem := make(chan struct{}, s.envInt("NOTIFY_CONCURRENCY", 4))
	var wg sync.WaitGroup
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(id string) {
			defer func() { <-sem; wg.Done() }()
			s.processChannel(ctx, id)
		}(id)
	}
	wg.Wait()
	return nil
}

func (s *Service) processChannel(ctx context.Context, channelID string) {
	r, err := s.Q.GetNotifyChannelByID(ctx, channelID)
	if err != nil {
		return
	}
	ctx = logctx.With(ctx, "workspace_id", r.WorkspaceID, "channel_id", channelID)
	rows, err := s.Q.PendingNotifyOutbox(ctx, db.PendingNotifyOutboxParams{ChannelID: channelID, Limit: 50})
	if err != nil || len(rows) == 0 {
		return
	}
	ch, err := s.loadChannel(r)
	if err != nil {
		s.failRows(ctx, nil, rows, err)
		return
	}
	msg, skipped := s.buildMessage(ctx, ch, rows)
	for _, o := range skipped {
		s.finishRow(ctx, o, "skipped", o.Attempts, "")
	}
	if len(msg.Entries) == 0 {
		return
	}
	if err := s.sendNotification(ctx, ch, msg); err != nil {
		s.failRows(ctx, ch, msg.rows, err)
		return
	}
	for _, o := range msg.rows {
		s.finishRow(ctx, o, "sent", o.Attempts+1, "")
	}
	_ = s.W.RecordNotifySuccess(ctx, db.RecordNotifySuccessParams{LastSentAt: nullStr(s.now()), ID: channelID})
}

func (s *Service) finishRow(ctx context.Context, o db.NotificationOutbox, status string, attempts int64, lastErr string) {
	p := db.FinishNotifyOutboxParams{Status: status, Attempts: attempts, NextAttemptAt: o.NextAttemptAt, LastError: lastErr, ID: o.ID}
	if status == "sent" {
		p.SentAt = nullStr(s.now())
	}
	if status == "pending" {
		p.NextAttemptAt = formatTime(s.Now().Add(s.notifyBackoff(attempts)))
	}
	if err := s.W.FinishNotifyOutbox(ctx, p); err != nil {
		s.logger(ctx).Error("notification state update failed", "outbox_id", o.ID, "err", err)
	}
}

func (s *Service) failRows(ctx context.Context, ch *notifyChannel, rows []db.NotificationOutbox, sendErr error) {
	msg := truncateRunes(sendErr.Error(), 500)
	maxAttempts := int64(s.envInt("NOTIFY_MAX_ATTEMPTS", 5))
	for _, o := range rows {
		st := "pending"
		if o.Attempts+1 >= maxAttempts {
			st = "failed"
		}
		s.finishRow(ctx, o, st, o.Attempts+1, msg)
	}
	if ch == nil {
		return
	}
	var n int64
	err := s.tx(ctx, func(q *db.Queries) (err error) { // a write returning a row: not via the ungated W.QueryRow
		n, err = q.RecordNotifyFailure(ctx, db.RecordNotifyFailureParams{LastError: msg, ID: ch.row.ID})
		return err
	})
	if err == nil && n >= int64(s.envInt("NOTIFY_MAX_FAILS", 10)) {
		reason := fmt.Sprintf("disabled after %d consecutive failures: %s", n, msg)
		_ = s.W.DisableNotifyChannel(ctx, db.DisableNotifyChannelParams{DisabledReason: reason, Now: s.now(), ID: ch.row.ID})
		s.logger(ctx).Warn("notification channel disabled", "failures", n)
	}
	s.logger(ctx).Info("notification failed", "type", ch.row.Type, "rows", len(rows))
}

func (s *Service) notifyBackoff(attempt int64) time.Duration {
	base := s.envDuration("NOTIFY_RETRY_BASE", 30*time.Second)
	d := base << min(max(attempt-1, 0), 16)
	if lim := 30 * time.Minute; d <= 0 || d > lim {
		d = lim
	}
	return d
}

// ---- message model ----

type notifyEntry struct {
	ItemID     string
	EventType  string
	Title      string
	ItemType   string
	Collection string
	Preview    string
	Status     string
	Actor      string
	Note       string
	WebURL     string
	MediaID    string
	MediaMime  string
	Version    int64  // item version shown; decisions are bound to it
	Actionable bool   // pending item: show approve/reject
	ApproveURL string // one-tap links (set when the channel needs them)
	RejectURL  string
}

type notifyMessage struct {
	Headline string // test messages
	Entries  []notifyEntry
	rows     []db.NotificationOutbox
}

func (m *notifyMessage) actionable() []int {
	var idx []int
	for i, e := range m.Entries {
		if e.Actionable {
			idx = append(idx, i)
		}
	}
	return idx
}

// title is a one-line summary for subjects and notification titles.
func (m *notifyMessage) title() string {
	if m.Headline != "" {
		return "staqly test notification"
	}
	if len(m.Entries) == 1 {
		e := m.Entries[0]
		return eventLabel(e) + ": " + e.Title
	}
	return fmt.Sprintf("%d updates in staqly", len(m.Entries))
}

func eventLabel(e notifyEntry) string {
	switch e.EventType {
	case "item.created":
		return "New item to review"
	case "item.approved":
		return "Approved"
	case "item.rejected":
		return "Rejected"
	}
	l := strings.ReplaceAll(strings.TrimPrefix(e.EventType, "item."), "_", " ")
	if l == "" {
		return e.EventType
	}
	return strings.ToUpper(l[:1]) + l[1:]
}

func (s *Service) webURL() string {
	return strings.TrimRight(s.Env("PUBLIC_DASHBOARD_URL", notifyFirst(s.Cfg.PublicDashboardURL, "http://localhost:"+notifyFirst(s.Cfg.DashboardPort, "8080"))), "/")
}

// publicAPIBase is the API URL reachable by chat platforms and phones
// (decision links, Telegram webhook). Default: through the dashboard proxy
// (PUBLIC_DASHBOARD_URL + "/api"), because the API itself listens on 127.0.0.1 and
// phones reach only the dashboard; otherwise PUBLIC_API_URL.
func (s *Service) publicAPIBase() string {
	def := notifyFirst(s.Cfg.PublicURL, "http://localhost:3333")
	if s.Cfg.PublicDashboardURL != "" {
		def = strings.TrimRight(s.Cfg.PublicDashboardURL, "/") + "/api"
	}
	return strings.TrimRight(s.Env("NOTIFY_PUBLIC_API_URL", def), "/")
}

func notifyFirst(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n])) + "..."
}

// buildMessage turns pending outbox rows into one message. Rows about items
// that are no longer pending (decided in the same transaction, or
// meanwhile) are skipped; several rows about one item collapse into one entry.
func (s *Service) buildMessage(ctx context.Context, ch *notifyChannel, rows []db.NotificationOutbox) (*notifyMessage, []db.NotificationOutbox) {
	msg := &notifyMessage{}
	var skipped []db.NotificationOutbox
	byItem := map[string]int{}
	for _, o := range rows {
		ev, err := s.Q.GetEvent(ctx, o.EventID)
		if err != nil {
			skipped = append(skipped, o)
			continue
		}
		e := notifyEntry{EventType: ev.Type, Actor: ev.ActorName}
		var data struct {
			Item       *Item `json:"item"`
			Collection struct {
				Name string `json:"name"`
			} `json:"collection"`
			Note    string `json:"note"`
			Watcher *struct {
				Name string `json:"name"`
			} `json:"watcher"`
			Error    string `json:"error"`
			Feedback string `json:"feedback"`
		}
		_ = json.Unmarshal([]byte(ev.Data), &data)
		e.Collection, e.Note = data.Collection.Name, data.Note
		if e.Note == "" && data.Feedback != "" && (ev.Type == "item.rejected" || ev.Type == "item.approved") {
			e.Note = "Feedback: " + data.Feedback
		}
		if ev.ItemID.Valid {
			cur, err := s.Q.GetItem(ctx, db.GetItemParams{WorkspaceID: ch.row.WorkspaceID, ID: ev.ItemID.String})
			if err != nil {
				skipped = append(skipped, o)
				continue
			}
			if ev.Type == "item.created" && cur.Status != "pending" {
				skipped = append(skipped, o)
				continue
			}
			it := s.toItem(ctx, cur)
			e.ItemID, e.Title, e.ItemType, e.Status, e.Version = it.ID, it.Title, it.Type, it.Status, it.Version
			e.Preview = itemPreview(it)
			e.WebURL = s.webURL() + "/items/" + it.ID
			e.Actionable = cur.Status == "pending"
			if it.Media != nil {
				e.MediaID, e.MediaMime = it.Media.ID, it.Media.MimeType
			}
			if _, ok := byItem[it.ID]; ok {
				msg.rows = append(msg.rows, o)
				continue
			}
			byItem[it.ID] = len(msg.Entries)
		} else {
			e.Title = ev.Type
			if data.Watcher != nil {
				e.Title = "Watcher " + data.Watcher.Name
			}
			e.Preview = data.Error
		}
		msg.Entries = append(msg.Entries, e)
		msg.rows = append(msg.rows, o)
	}
	return msg, skipped
}

func itemPreview(it Item) string {
	var p string
	switch {
	case it.Content != "":
		p = it.Content
	case it.URL != "":
		p = it.URL
	case it.Media != nil:
		p = it.Media.OriginalName
	}
	p = strings.Join(strings.Fields(p), " ")
	return truncateRunes(p, 280)
}

// sendNotification renders and delivers msg on ch. Decision links are
// created only for channels that show them.
func (s *Service) sendNotification(ctx context.Context, ch *notifyChannel, msg *notifyMessage) error {
	if needsLinks(ch) {
		for i := range msg.Entries {
			e := &msg.Entries[i]
			if !e.Actionable || i >= 10 {
				continue
			}
			var err error
			if e.ApproveURL, e.RejectURL, err = s.createDecisionLinks(ctx, ch, e.ItemID, e.Version); err != nil {
				return err
			}
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	var err error
	switch ch.row.Type {
	case ChannelTelegram:
		err = s.sendTelegram(ctx, ch, msg)
	case ChannelSlack:
		err = s.sendSlack(ctx, ch, msg)
	case ChannelDiscord:
		err = s.sendDiscord(ctx, ch, msg)
	case ChannelNtfy:
		err = s.sendNtfy(ctx, ch, msg)
	case ChannelEmail:
		err = s.sendEmail(ctx, ch, msg)
	default:
		err = errors.New("unknown channel type")
	}
	result := "ok"
	if err != nil {
		result = "error"
	}
	metricNotifications.Inc(ch.row.Type, result)
	return err
}

func needsLinks(ch *notifyChannel) bool {
	switch ch.row.Type {
	case ChannelTelegram:
		return false
	case ChannelDiscord:
		return !ch.discordApp()
	case ChannelSlack:
		// Interactive Slack messages (signed buttons + an approver list) must
		// not also carry unauthenticated decide links.
		return !ch.slackInteractive()
	}
	return true // ntfy, email
}

// ---- outbound HTTP ----

var notifyClients sync.Map // *Service -> *http.Client

// notifyHTTP is SSRF-guarded: public internet only unless
// NOTIFY_ALLOW_PRIVATE / NOTIFY_ALLOW_LOOPBACK. Metadata/link-local always blocked.
func (s *Service) notifyHTTP() *http.Client {
	if c, ok := notifyClients.Load(s); ok {
		return c.(*http.Client)
	}
	cfg := WebhookConfig{AllowPrivate: s.Env("NOTIFY_ALLOW_PRIVATE", "") == "true", AllowLoopback: s.Env("NOTIFY_ALLOW_LOOPBACK", "") == "true"}
	dialer := &net.Dialer{Timeout: 10 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip, err := netip.ParseAddr(host)
		if err != nil {
			return err
		}
		return checkNotifyIP(ip.Unmap(), cfg)
	}}
	c := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{DialContext: dialer.DialContext, Proxy: nil, MaxIdleConnsPerHost: 2,
			IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 30 * time.Second},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	actual, _ := notifyClients.LoadOrStore(s, c)
	return actual.(*http.Client)
}

func checkNotifyIP(ip netip.Addr, cfg WebhookConfig) error {
	cfg.envPrefix = "NOTIFY"
	return checkIP(ip, cfg)
}

// httpError is a sanitized error: it never contains the request URL (which
// may embed a bot token) and keeps only a short response excerpt.
type httpError struct {
	Status int
	Body   string
}

func (e *httpError) Error() string {
	if e.Status == 0 {
		return e.Body
	}
	return fmt.Sprintf("HTTP %d: %s", e.Status, e.Body)
}

// doRequest sends req; non-2xx responses become *httpError. out (optional)
// receives the decoded JSON body.
func (s *Service) doRequest(req *http.Request, out any) error {
	resp, err := s.notifyHTTP().Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return &httpError{Body: "request to " + req.URL.Host + " failed: " + err.Error()}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &httpError{Status: resp.StatusCode, Body: truncateRunes(strings.TrimSpace(string(body)), 300)}
	}
	if out != nil && len(body) > 0 {
		if err := json.Unmarshal(body, out); err != nil {
			return &httpError{Status: resp.StatusCode, Body: "invalid JSON response"}
		}
	}
	return nil
}

func (s *Service) postJSON(ctx context.Context, rawURL string, hdr map[string]string, body any, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(b))
	if err != nil {
		return &httpError{Body: "invalid URL"}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "staqly-notify/1.0")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	return s.doRequest(req, out)
}

// readImage returns the bytes of an image item's media for direct upload
// (our media URLs need auth, so chat apps can't fetch them).
func (s *Service) readImage(ctx context.Context, workspaceID string, e notifyEntry, maxBytes int64) ([]byte, string, string, bool) {
	if e.MediaID == "" || !strings.HasPrefix(e.MediaMime, "image/") || e.MediaMime == "image/avif" {
		return nil, "", "", false
	}
	m, err := s.Q.GetMedia(ctx, db.GetMediaParams{WorkspaceID: workspaceID, ID: e.MediaID})
	if err != nil || m.SizeBytes > maxBytes {
		return nil, "", "", false
	}
	b, err := readFileLimit(s.mediaPath(m.StorageName), maxBytes)
	if err != nil {
		return nil, "", "", false
	}
	return b, m.OriginalName, m.MimeType, true
}

func readFileLimit(p string, max int64) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err == nil && int64(len(b)) > max {
		err = errors.New("too large")
	}
	return b, err
}
