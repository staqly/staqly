package app

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/logctx"
)

type WebhookConfig struct {
	AllowPrivate  bool          // RFC1918/ULA targets (Docker networks, LAN n8n)
	AllowLoopback bool          // 127.0.0.0/8, ::1 (n8n on the same host, non-Docker)
	RetryBase     time.Duration // first retry delay; doubles per attempt
	RetryMax      time.Duration
	PollInterval  time.Duration
	Concurrency   int
	// PerWorkspace caps in-flight deliveries per workspace (fairness: one
	// slow receiver can't take every worker slot). Default 2.
	PerWorkspace int

	envPrefix string // env var prefix named in policy errors (default WEBHOOK)
}

func (c *WebhookConfig) defaults() {
	if c.RetryBase == 0 {
		c.RetryBase = 15 * time.Second
	}
	if c.RetryMax == 0 {
		c.RetryMax = time.Hour
	}
	if c.PollInterval == 0 {
		c.PollInterval = 2 * time.Second
	}
	if c.Concurrency == 0 {
		c.Concurrency = 4
	}
	if c.PerWorkspace == 0 {
		c.PerWorkspace = 2
	}
}

type Webhook struct {
	ID            string   `json:"id"`
	URL           string   `json:"url"`
	Description   string   `json:"description"`
	Secret        string   `json:"secret"`
	Events        []string `json:"events"`
	CollectionIDs []string `json:"collection_ids"`
	Active        bool     `json:"active"`
	TimeoutMS     int64    `json:"timeout_ms"`
	MaxAttempts   int64    `json:"max_attempts"`
	CreatedAt     string   `json:"created_at"`
	UpdatedAt     string   `json:"updated_at"`
}

// maskWebhookSecret hides a webhook signing secret: it is only shown in full in the
// create and rotate responses (copy it then), like an API key.
func maskWebhookSecret(v string) string {
	if len(v) <= 10 {
		return "whsec_****"
	}
	return "whsec_****" + v[len(v)-4:]
}

// toMaskedWebhook is toWebhook with the signing secret masked.
func toMaskedWebhook(w db.Webhook) Webhook {
	out := toWebhook(w)
	out.Secret = maskWebhookSecret(out.Secret)
	return out
}

func toWebhook(w db.Webhook) Webhook {
	out := Webhook{ID: w.ID, URL: w.Url, Description: w.Description, Secret: w.Secret, Active: w.Active == 1,
		TimeoutMS: w.TimeoutMs, MaxAttempts: w.MaxAttempts, CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt}
	_ = json.Unmarshal([]byte(w.Events), &out.Events)
	_ = json.Unmarshal([]byte(w.CollectionIds), &out.CollectionIDs)
	return out
}

type Delivery struct {
	ID             string  `json:"id"`
	WebhookID      string  `json:"webhook_id"`
	EventID        string  `json:"event_id"`
	EventType      string  `json:"event_type"`
	Status         string  `json:"status"`
	Attempts       int64   `json:"attempts"`
	NextAttemptAt  *string `json:"next_attempt_at"`
	LastStatusCode *int64  `json:"last_status_code"`
	LastError      string  `json:"last_error"`
	LastResponse   string  `json:"last_response"`
	LastAttemptAt  *string `json:"last_attempt_at"`
	DeliveredAt    *string `json:"delivered_at"`
	CreatedAt      string  `json:"created_at"`
}

func toDelivery(d db.WebhookDelivery) Delivery {
	out := Delivery{ID: d.ID, WebhookID: d.WebhookID, EventID: d.EventID, EventType: d.EventType, Status: d.Status,
		Attempts: d.Attempts, LastError: d.LastError, LastResponse: d.LastResponse,
		LastAttemptAt: ptr(d.LastAttemptAt), DeliveredAt: ptr(d.DeliveredAt), CreatedAt: d.CreatedAt}
	if d.Status == "pending" {
		out.NextAttemptAt = &d.NextAttemptAt
	}
	if d.LastStatusCode.Valid {
		out.LastStatusCode = &d.LastStatusCode.Int64
	}
	return out
}

type WebhookInput struct {
	URL           *string   `json:"url"`
	Description   *string   `json:"description"`
	Events        *[]string `json:"events"`
	CollectionIDs *[]string `json:"collection_ids"`
	Active        *bool     `json:"active"`
	TimeoutMS     *int64    `json:"timeout_ms"`
	MaxAttempts   *int64    `json:"max_attempts"`
	RotateSecret  bool      `json:"rotate_secret"`
}

func validateWebhookURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return invalid("url", "must be an absolute http(s) URL")
	}
	if u.User != nil {
		return invalid("url", "must not contain credentials")
	}
	return nil
}

func (s *Service) applyWebhookInput(ctx context.Context, p Principal, w *db.Webhook, in WebhookInput) error {
	if in.URL != nil {
		if err := validateWebhookURL(*in.URL); err != nil {
			return err
		}
		w.Url = strings.TrimSpace(*in.URL)
	}
	if in.Description != nil {
		d, err := trimmed(*in.Description, "description", 500, false)
		if err != nil {
			return err
		}
		w.Description = d
	}
	if in.Events != nil {
		if err := s.validateEventPatterns(ctx, p.WorkspaceID, *in.Events); err != nil {
			return err
		}
		w.Events = mustJSON(*in.Events)
	}
	if in.CollectionIDs != nil {
		for _, id := range *in.CollectionIDs {
			if _, err := s.Q.GetCollection(ctx, db.GetCollectionParams{WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
				return invalid("collection_ids", "unknown collection "+id)
			}
		}
		w.CollectionIds = mustJSON(*in.CollectionIDs)
	}
	if in.Active != nil {
		w.Active = 0
		if *in.Active {
			w.Active = 1
		}
	}
	if in.TimeoutMS != nil {
		if *in.TimeoutMS < 1000 || *in.TimeoutMS > 60000 {
			return invalid("timeout_ms", "must be between 1000 and 60000")
		}
		w.TimeoutMs = *in.TimeoutMS
	}
	if in.MaxAttempts != nil {
		if *in.MaxAttempts < 1 || *in.MaxAttempts > 20 {
			return invalid("max_attempts", "must be between 1 and 20")
		}
		w.MaxAttempts = *in.MaxAttempts
	}
	if in.RotateSecret {
		w.Secret = "whsec_" + randomString(24)
	}
	return nil
}

func (s *Service) CreateWebhook(ctx context.Context, p Principal, in WebhookInput) (Webhook, error) {
	if err := p.require(ScopeWebhooksManage); err != nil {
		return Webhook{}, err
	}
	if in.URL == nil {
		return Webhook{}, invalid("url", "is required")
	}
	now := s.now()
	w := db.Webhook{ID: newID("wh"), WorkspaceID: p.WorkspaceID, Events: `["*"]`, CollectionIds: "[]", Active: 1,
		TimeoutMs: 10000, MaxAttempts: 8, CreatedAt: now, UpdatedAt: now}
	in.RotateSecret = true
	if err := s.applyWebhookInput(ctx, p, &w, in); err != nil {
		return Webhook{}, err
	}
	err := s.tx(ctx, func(q *db.Queries) error {
		plan, err := s.planFor(ctx, q, p.WorkspaceID)
		if err != nil {
			return err
		}
		n, err := q.CountWebhooks(ctx, p.WorkspaceID)
		if err != nil {
			return err
		}
		if err := checkCount(plan, LimitWebhooks, plan.Limits.Webhooks, n); err != nil {
			return err
		}
		if err := q.CreateWebhook(ctx, db.CreateWebhookParams{ID: w.ID, WorkspaceID: w.WorkspaceID, Url: w.Url,
			Description: w.Description, Secret: w.Secret, Events: w.Events, CollectionIds: w.CollectionIds, Active: w.Active,
			TimeoutMs: w.TimeoutMs, MaxAttempts: w.MaxAttempts, CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		_, err = s.emit(ctx, q, p, "webhook.created", "", "", map[string]any{"webhook": toMaskedWebhook(w)})
		return err
	})
	return toWebhook(w), err
}

func (s *Service) getWebhook(ctx context.Context, p Principal, id string) (db.Webhook, error) {
	if err := p.require(ScopeWebhooksManage); err != nil {
		return db.Webhook{}, err
	}
	w, err := s.Q.GetWebhook(ctx, db.GetWebhookParams{WorkspaceID: p.WorkspaceID, ID: id})
	if isNoRows(err) {
		return w, notFound("webhook")
	}
	return w, err
}

func (s *Service) GetWebhook(ctx context.Context, p Principal, id string) (Webhook, error) {
	w, err := s.getWebhook(ctx, p, id)
	return toMaskedWebhook(w), err
}

func (s *Service) ListWebhooks(ctx context.Context, p Principal) ([]Webhook, error) {
	if err := p.require(ScopeWebhooksManage); err != nil {
		return nil, err
	}
	rows, err := s.Q.ListWebhooks(ctx, p.WorkspaceID)
	out := []Webhook{}
	for _, r := range rows {
		out = append(out, toMaskedWebhook(r))
	}
	return out, err
}

func (s *Service) UpdateWebhook(ctx context.Context, p Principal, id string, in WebhookInput) (Webhook, error) {
	w, err := s.getWebhook(ctx, p, id)
	if err != nil {
		return Webhook{}, err
	}
	if in.Active != nil && *in.Active == (w.Active == 1) && onlyChange(in, "Active") {
		if *in.Active {
			return Webhook{}, alreadyErr("webhook", "active")
		}
		return Webhook{}, alreadyErr("webhook", "inactive")
	}
	if err := s.applyWebhookInput(ctx, p, &w, in); err != nil {
		return Webhook{}, err
	}
	w.UpdatedAt = s.now()
	err = s.tx(ctx, func(q *db.Queries) error {
		if err := q.UpdateWebhook(ctx, db.UpdateWebhookParams{Url: w.Url, Description: w.Description, Secret: w.Secret,
			Events: w.Events, CollectionIds: w.CollectionIds, Active: w.Active, TimeoutMs: w.TimeoutMs,
			MaxAttempts: w.MaxAttempts, UpdatedAt: w.UpdatedAt, WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
			return err
		}
		_, err := s.emit(ctx, q, p, "webhook.updated", "", "", map[string]any{"webhook": toMaskedWebhook(w),
			"secret_rotated": in.RotateSecret})
		return err
	})
	if !in.RotateSecret {
		return toMaskedWebhook(w), err
	}
	return toWebhook(w), err
}

func (s *Service) DeleteWebhook(ctx context.Context, p Principal, id string) error {
	if err := p.require(ScopeWebhooksManage); err != nil {
		return err
	}
	return s.tx(ctx, func(q *db.Queries) error {
		n, err := q.DeleteWebhook(ctx, db.DeleteWebhookParams{WorkspaceID: p.WorkspaceID, ID: id})
		if err == nil && n == 0 {
			return notFound("webhook")
		}
		if err != nil {
			return err
		}
		_, err = s.emit(ctx, q, p, "webhook.deleted", "", "", map[string]any{"webhook_id": id})
		return err
	})
}

// TestWebhook queues a webhook.test event for this webhook only.
func (s *Service) TestWebhook(ctx context.Context, p Principal, id string) (Delivery, error) {
	w, err := s.getWebhook(ctx, p, id)
	if err != nil {
		return Delivery{}, err
	}
	var d db.WebhookDelivery
	err = s.tx(ctx, func(q *db.Queries) error {
		ev, err := q.InsertEvent(ctx, db.InsertEventParams{ID: newID("evt"), WorkspaceID: p.WorkspaceID, Type: "webhook.test",
			ActorType: p.Kind, ActorID: p.ID, ActorName: p.Name, Data: mustJSON(map[string]any{"webhook_id": w.ID, "message": "Test delivery from staqly"}),
			CreatedAt: s.now()})
		if err != nil {
			return err
		}
		d = db.WebhookDelivery{ID: newID("dlv"), WebhookID: w.ID, EventID: ev.ID, EventType: ev.Type, Status: "pending",
			NextAttemptAt: ev.CreatedAt, CreatedAt: ev.CreatedAt}
		return q.InsertDelivery(ctx, db.InsertDeliveryParams{ID: d.ID, WorkspaceID: p.WorkspaceID, WebhookID: w.ID,
			EventID: ev.ID, EventType: ev.Type, NextAttemptAt: ev.CreatedAt, CreatedAt: ev.CreatedAt})
	})
	return toDelivery(d), err
}

// ListDeliveries lists webhook deliveries, newest first; itemID narrows them to events about one item.
func (s *Service) ListDeliveries(ctx context.Context, p Principal, webhookID, status, itemID string, limit int) ([]Delivery, error) {
	if err := p.require(ScopeWebhooksManage); err != nil {
		return nil, err
	}
	rows, err := s.Q.ListDeliveries(ctx, db.ListDeliveriesParams{WorkspaceID: p.WorkspaceID, WebhookID: optAny(webhookID),
		Status: optAny(status), ItemID: optAny(itemID), Lim: int64(clampLimit(limit, 50, 200))})
	out := []Delivery{}
	for _, r := range rows {
		out = append(out, toDelivery(r))
	}
	return out, err
}

// RetryDelivery re-queues a finished (succeeded or failed) delivery.
func (s *Service) RetryDelivery(ctx context.Context, p Principal, id string) (Delivery, error) {
	if err := p.require(ScopeWebhooksManage); err != nil {
		return Delivery{}, err
	}
	n, err := s.W.RequeueDelivery(ctx, db.RequeueDeliveryParams{NextAttemptAt: s.now(), WorkspaceID: p.WorkspaceID, ID: id})
	if err != nil {
		return Delivery{}, err
	}
	d, gerr := s.Q.GetDelivery(ctx, db.GetDeliveryParams{WorkspaceID: p.WorkspaceID, ID: id})
	if gerr != nil {
		return Delivery{}, notFound("delivery")
	}
	if n == 0 {
		return Delivery{}, conflict("delivery is already pending")
	}
	s.wake()
	return toDelivery(d), nil
}

// ---- delivery worker ----

// Sign returns the X-Staqly-Signature value: hex HMAC-SHA256 over "<ts>.<body>".
func Sign(secret string, ts int64, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(m, "%d.", ts)
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

// safeClient refuses to connect to addresses outside the configured policy.
// The check runs on the resolved IP at dial time, so DNS rebinding can't
// sneak past a hostname check. Redirects are not followed.
func (s *Service) safeClient() *http.Client {
	cfg := s.Cfg.Webhook
	dialer := &net.Dialer{Timeout: 10 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip, err := netip.ParseAddr(host)
		if err != nil {
			return err
		}
		return checkIP(ip.Unmap(), cfg)
	}}
	tr := &http.Transport{DialContext: dialer.DialContext, Proxy: nil, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 60 * time.Second}
	return &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// Addresses that are never a legitimate outbound destination, whatever the
// policy: "this network", IETF protocol assignments, benchmarking, reserved
// space, the Azure wire server, Teredo and documentation ranges.
var deniedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"), // includes 255.255.255.255
	netip.MustParsePrefix("168.63.129.16/32"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2001:db8::/32"),
}

// Shared address space (carrier-grade NAT, also used by Tailscale): treated as
// private, so *_ALLOW_PRIVATE decides.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// embeddedIPv4 returns the IPv4 address carried inside IPv6 translation and
// tunnelling formats (NAT64 64:ff9b::/96 and 64:ff9b:1::/48, IPv4-compatible
// ::/96, 6to4 2002::/16), so they are checked like the IPv4 they reach.
func embeddedIPv4(ip netip.Addr) (netip.Addr, bool) {
	if !ip.Is6() || ip.Is4In6() {
		return netip.Addr{}, false
	}
	b := ip.As16()
	v4 := func(o int) netip.Addr { return netip.AddrFrom4([4]byte{b[o], b[o+1], b[o+2], b[o+3]}) }
	switch {
	case netip.MustParsePrefix("64:ff9b::/96").Contains(ip), netip.MustParsePrefix("64:ff9b:1::/48").Contains(ip):
		return v4(12), true
	case netip.MustParsePrefix("2002::/16").Contains(ip):
		return v4(2), true
	case netip.MustParsePrefix("::/96").Contains(ip) && !ip.IsUnspecified() && !ip.IsLoopback():
		return v4(12), true
	}
	return netip.Addr{}, false
}

// policyError is a destination refused by checkIP (safe to show to users).
type policyError struct{ msg string }

func (e *policyError) Error() string { return e.msg }

func denied(format string, a ...any) error { return &policyError{msg: fmt.Sprintf(format, a...)} }

// checkIP applies the outbound destination policy to a resolved address.
func checkIP(ip netip.Addr, cfg WebhookConfig) error {
	ip = ip.Unmap()
	if v4, ok := embeddedIPv4(ip); ok {
		if err := checkIP(v4, cfg); err != nil {
			return denied("destination %s embeds %s: %s", ip, v4, err)
		}
	}
	env := cfg.envPrefix
	if env == "" {
		env = "WEBHOOK"
	}
	for _, p := range deniedPrefixes {
		if p.Contains(ip) {
			return denied("destination %s is not allowed", ip)
		}
	}
	switch {
	case ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast(), ip.IsMulticast(), ip.IsUnspecified(),
		ip.IsInterfaceLocalMulticast():
		// Always blocked: includes the 169.254.169.254 cloud metadata endpoint.
		return denied("destination %s is not allowed", ip)
	case ip.IsLoopback() && !cfg.AllowLoopback:
		return denied("loopback destination %s is not allowed (set %s_ALLOW_LOOPBACK=true)", ip, env)
	case (ip.IsPrivate() || cgnat.Contains(ip)) && !cfg.AllowPrivate:
		return denied("private destination %s is not allowed (set %s_ALLOW_PRIVATE=true)", ip, env)
	}
	return nil
}

// RunWorker delivers due webhooks until ctx is cancelled, then waits for
// in-flight requests. Pending rows survive restarts and are picked up again.
// Fairness: at most Concurrency requests in flight overall and PerWorkspace
// per workspace; a saturated workspace is skipped (its rows stay due) rather
// than blocking the loop for everyone else.
func (s *Service) RunWorker(ctx context.Context) {
	client := s.safeClient()
	cfg := s.Cfg.Webhook
	var (
		mu       sync.Mutex
		inflight = map[string]int{}
		total    int
		wg       sync.WaitGroup
	)
	defer wg.Wait()
	tick := time.NewTicker(cfg.PollInterval)
	defer tick.Stop()
	purge := time.NewTicker(time.Hour)
	defer purge.Stop()
	for {
		due, err := s.Q.DueDeliveries(ctx, s.now(), cfg.PerWorkspace, 50)
		if err != nil && ctx.Err() == nil {
			s.logger(ctx).Error("webhook poll failed", "err", err)
		}
		for _, d := range due {
			mu.Lock()
			busy := total >= cfg.Concurrency || inflight[d.WorkspaceID] >= cfg.PerWorkspace
			if !busy {
				total++
				inflight[d.WorkspaceID]++
			}
			mu.Unlock()
			if busy {
				continue
			}
			release := func() {
				mu.Lock()
				total--
				if inflight[d.WorkspaceID]--; inflight[d.WorkspaceID] <= 0 {
					delete(inflight, d.WorkspaceID)
				}
				mu.Unlock()
			}
			lease := formatTime(s.Now().Add(5 * time.Minute))
			n, err := s.W.LeaseDelivery(ctx, db.LeaseDeliveryParams{LeaseUntil: lease, Now: nullStr(s.now()), ID: d.ID, ExpectedNext: d.NextAttemptAt})
			if err != nil || n == 0 {
				release()
				continue
			}
			d.Attempts++
			wg.Add(1)
			go func(d db.WebhookDelivery) {
				defer func() { release(); wg.Done(); s.wake() }() // a freed slot may unblock a skipped workspace
				s.deliver(context.WithoutCancel(ctx), client, d)
			}(d)
		}
		select {
		case <-ctx.Done():
			return
		case <-s.notify:
		case <-tick.C:
		case <-purge.C:
			_ = s.W.PurgeIdempotencyKeys(ctx, formatTime(s.Now().Add(-24*time.Hour)))
		}
		s.expireItems(s.workerContext(ctx, "expiry"))
		s.failStaleProcessing(s.workerContext(ctx, "processing-timeout"))
	}
}

// sanitizeDeliveryError keeps delivery errors short and, in cloud mode, free
// of network details (resolved addresses, ports, TLS internals) that would
// turn the delivery log into a port scanner.
func (s *Service) sanitizeDeliveryError(err error) string {
	msg := err.Error()
	if s.Cfg.Mode != ModeCloud {
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return msg
	}
	var ne net.Error
	var pe *policyError
	switch {
	case errors.As(err, &pe):
		return pe.Error()
	case strings.HasPrefix(msg, "receiver responded"):
		return msg
	case errors.As(err, &ne) && ne.Timeout(), errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	}
	return "connection failed"
}

func (s *Service) deliver(ctx context.Context, client *http.Client, d db.WebhookDelivery) {
	ctx = logctx.With(ctx, "workspace_id", d.WorkspaceID, "webhook_id", d.WebhookID, "delivery_id", d.ID)
	w, err := s.Q.GetWebhook(ctx, db.GetWebhookParams{WorkspaceID: d.WorkspaceID, ID: d.WebhookID})
	if err != nil {
		return // webhook deleted; its deliveries cascade away
	}
	if w.Active != 1 {
		// Disabled after the delivery was queued: finish it instead of
		// leaving it pending forever (re-enable and retry it to resend).
		if err := s.W.FinishDelivery(ctx, db.FinishDeliveryParams{ID: d.ID, Status: "failed", NextAttemptAt: s.now(),
			LastError: "webhook disabled"}); err != nil {
			s.logger(ctx).Error("webhook state update failed", "err", err)
		}
		return
	}
	ev, err := s.Q.GetEvent(ctx, d.EventID)
	if err != nil {
		s.logger(ctx).Error("webhook event missing", "err", err)
		return
	}
	body, _ := json.Marshal(map[string]any{
		"id": ev.ID, "type": ev.Type, "created_at": ev.CreatedAt, "workspace_id": ev.WorkspaceID,
		"delivery_id": d.ID, "attempt": d.Attempts, "actor": Actor{Type: ev.ActorType, ID: ev.ActorID, Name: ev.ActorName},
		"data": json.RawMessage(ev.Data),
	})
	ts := s.Now().Unix()
	start := time.Now()
	rctx, cancel := context.WithTimeout(ctx, time.Duration(w.TimeoutMs)*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodPost, w.Url, bytes.NewReader(body))
	var code int
	var respBody string
	if err == nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "Staqly-Webhooks/1.0")
		req.Header.Set("X-Staqly-Event", ev.Type)
		req.Header.Set("X-Staqly-Event-Id", ev.ID)
		req.Header.Set("X-Staqly-Delivery", d.ID)
		req.Header.Set("X-Staqly-Timestamp", strconv.FormatInt(ts, 10))
		req.Header.Set("X-Staqly-Signature", Sign(w.Secret, ts, body))
		var resp *http.Response
		resp, err = client.Do(req)
		if err == nil {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
			resp.Body.Close()
			code, respBody = resp.StatusCode, string(b)
			if code < 200 || code > 299 {
				err = fmt.Errorf("receiver responded %d", code)
			}
		}
	}
	if s.Cfg.Mode == ModeCloud {
		// Never store response bodies in cloud mode: with a permissive
		// destination policy they would let a tenant read internal services.
		respBody = ""
	}
	now := s.now()
	upd := db.FinishDeliveryParams{ID: d.ID, NextAttemptAt: now, LastResponse: respBody}
	if code != 0 {
		upd.LastStatusCode.Int64, upd.LastStatusCode.Valid = int64(code), true
	}
	switch {
	case err == nil:
		upd.Status, upd.DeliveredAt = "succeeded", nullStr(now)
	case code == http.StatusGone || d.Attempts >= w.MaxAttempts:
		upd.Status, upd.LastError = "failed", s.sanitizeDeliveryError(err)
	default:
		upd.Status, upd.LastError = "pending", s.sanitizeDeliveryError(err)
		upd.NextAttemptAt = formatTime(s.Now().Add(s.backoff(d.Attempts)))
	}
	metricWebhookDuration.Observe(time.Since(start).Seconds())
	result := upd.Status
	if result == "pending" {
		result = "retry"
	}
	metricWebhookAttempts.Inc(result)
	if err := s.W.FinishDelivery(ctx, upd); err != nil {
		s.logger(ctx).Error("webhook state update failed", "err", err)
	}
	s.logger(ctx).Info("webhook delivery", "event", ev.Type, "attempt", d.Attempts, "status", upd.Status, "code", code)
}

// backoff: RetryBase * 2^(attempt-1), capped, with ±20% jitter.
func (s *Service) backoff(attempt int64) time.Duration {
	d := s.Cfg.Webhook.RetryBase << min(attempt-1, 20)
	if d <= 0 || d > s.Cfg.Webhook.RetryMax {
		d = s.Cfg.Webhook.RetryMax
	}
	return time.Duration(float64(d) * (0.8 + 0.4*rand.Float64()))
}
