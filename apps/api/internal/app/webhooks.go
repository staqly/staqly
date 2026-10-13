package app

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
)

type WebhookConfig struct {
	AllowPrivate  bool          // RFC1918/ULA targets (Docker networks, LAN n8n)
	AllowLoopback bool          // 127.0.0.0/8, ::1 (n8n on the same host, non-Docker)
	RetryBase     time.Duration // first retry delay; doubles per attempt
	RetryMax      time.Duration
	PollInterval  time.Duration
	Concurrency   int
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
		if len(*in.Events) == 0 {
			return invalid("events", "subscribe to at least one event (use \"*\" for all)")
		}
		for _, e := range *in.Events {
			if e == "" || len(e) > 100 || strings.ContainsAny(e, " /") {
				return invalid("events", "invalid event pattern "+e)
			}
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
		return q.CreateWebhook(ctx, db.CreateWebhookParams{ID: w.ID, WorkspaceID: w.WorkspaceID, Url: w.Url,
			Description: w.Description, Secret: w.Secret, Events: w.Events, CollectionIds: w.CollectionIds, Active: w.Active,
			TimeoutMs: w.TimeoutMs, MaxAttempts: w.MaxAttempts, CreatedAt: now, UpdatedAt: now})
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
	return toWebhook(w), err
}

func (s *Service) ListWebhooks(ctx context.Context, p Principal) ([]Webhook, error) {
	if err := p.require(ScopeWebhooksManage); err != nil {
		return nil, err
	}
	rows, err := s.Q.ListWebhooks(ctx, p.WorkspaceID)
	out := []Webhook{}
	for _, r := range rows {
		out = append(out, toWebhook(r))
	}
	return out, err
}

func (s *Service) UpdateWebhook(ctx context.Context, p Principal, id string, in WebhookInput) (Webhook, error) {
	w, err := s.getWebhook(ctx, p, id)
	if err != nil {
		return Webhook{}, err
	}
	if err := s.applyWebhookInput(ctx, p, &w, in); err != nil {
		return Webhook{}, err
	}
	w.UpdatedAt = s.now()
	err = s.Q.UpdateWebhook(ctx, db.UpdateWebhookParams{Url: w.Url, Description: w.Description, Secret: w.Secret,
		Events: w.Events, CollectionIds: w.CollectionIds, Active: w.Active, TimeoutMs: w.TimeoutMs,
		MaxAttempts: w.MaxAttempts, UpdatedAt: w.UpdatedAt, WorkspaceID: p.WorkspaceID, ID: id})
	return toWebhook(w), err
}

func (s *Service) DeleteWebhook(ctx context.Context, p Principal, id string) error {
	if err := p.require(ScopeWebhooksManage); err != nil {
		return err
	}
	n, err := s.Q.DeleteWebhook(ctx, db.DeleteWebhookParams{WorkspaceID: p.WorkspaceID, ID: id})
	if err == nil && n == 0 {
		return notFound("webhook")
	}
	return err
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
			ActorType: p.Kind, ActorID: p.ID, ActorName: p.Name, Data: mustJSON(map[string]any{"webhook_id": w.ID, "message": "Test delivery from staq.ly"}),
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

func (s *Service) ListDeliveries(ctx context.Context, p Principal, webhookID, status string, limit int) ([]Delivery, error) {
	if err := p.require(ScopeWebhooksManage); err != nil {
		return nil, err
	}
	rows, err := s.Q.ListDeliveries(ctx, db.ListDeliveriesParams{WorkspaceID: p.WorkspaceID, WebhookID: optAny(webhookID),
		Status: optAny(status), Lim: int64(clampLimit(limit, 50, 200))})
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
	n, err := s.Q.RequeueDelivery(ctx, db.RequeueDeliveryParams{NextAttemptAt: s.now(), WorkspaceID: p.WorkspaceID, ID: id})
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

func checkIP(ip netip.Addr, cfg WebhookConfig) error {
	switch {
	case ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast(), ip.IsMulticast(), ip.IsUnspecified(),
		ip.IsInterfaceLocalMulticast():
		// Always blocked: includes the 169.254.169.254 cloud metadata endpoint.
		return fmt.Errorf("destination %s is not allowed", ip)
	case ip.IsLoopback() && !cfg.AllowLoopback:
		return fmt.Errorf("loopback destination %s is not allowed (set WEBHOOK_ALLOW_LOOPBACK=true)", ip)
	case ip.IsPrivate() && !cfg.AllowPrivate:
		return fmt.Errorf("private destination %s is not allowed (set WEBHOOK_ALLOW_PRIVATE=true)", ip)
	}
	return nil
}

// RunWorker delivers due webhooks until ctx is cancelled, then waits for
// in-flight requests. Pending rows survive restarts and are picked up again.
func (s *Service) RunWorker(ctx context.Context) {
	client := s.safeClient()
	sem := make(chan struct{}, s.Cfg.Webhook.Concurrency)
	var wg sync.WaitGroup
	defer wg.Wait()
	tick := time.NewTicker(s.Cfg.Webhook.PollInterval)
	defer tick.Stop()
	purge := time.NewTicker(time.Hour)
	defer purge.Stop()
	for {
		due, err := s.Q.DueDeliveries(ctx, db.DueDeliveriesParams{NextAttemptAt: s.now(), Limit: 50})
		if err != nil && ctx.Err() == nil {
			s.Log.Error("webhook poll failed", "err", err)
		}
		for _, d := range due {
			lease := formatTime(s.Now().Add(5 * time.Minute))
			n, err := s.Q.LeaseDelivery(ctx, db.LeaseDeliveryParams{LeaseUntil: lease, Now: nullStr(s.now()), ID: d.ID, ExpectedNext: d.NextAttemptAt})
			if err != nil || n == 0 {
				continue
			}
			d.Attempts++
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			wg.Add(1)
			go func(d db.WebhookDelivery) {
				defer func() { <-sem; wg.Done() }()
				s.deliver(context.WithoutCancel(ctx), client, d)
			}(d)
		}
		select {
		case <-ctx.Done():
			return
		case <-s.notify:
		case <-tick.C:
		case <-purge.C:
			_ = s.Q.PurgeIdempotencyKeys(ctx, formatTime(s.Now().Add(-24*time.Hour)))
		}
		s.expireItems(ctx)
	}
}

func (s *Service) deliver(ctx context.Context, client *http.Client, d db.WebhookDelivery) {
	w, err := s.Q.GetWebhook(ctx, db.GetWebhookParams{WorkspaceID: d.WorkspaceID, ID: d.WebhookID})
	if err != nil {
		return // webhook deleted; its deliveries cascade away
	}
	ev, err := s.Q.GetEvent(ctx, d.EventID)
	if err != nil {
		s.Log.Error("webhook event missing", "delivery_id", d.ID, "err", err)
		return
	}
	body, _ := json.Marshal(map[string]any{
		"id": ev.ID, "type": ev.Type, "created_at": ev.CreatedAt, "workspace_id": ev.WorkspaceID,
		"delivery_id": d.ID, "attempt": d.Attempts, "actor": Actor{Type: ev.ActorType, ID: ev.ActorID, Name: ev.ActorName},
		"data": json.RawMessage(ev.Data),
	})
	ts := s.Now().Unix()
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
	now := s.now()
	upd := db.FinishDeliveryParams{ID: d.ID, NextAttemptAt: now, LastResponse: respBody}
	if code != 0 {
		upd.LastStatusCode.Int64, upd.LastStatusCode.Valid = int64(code), true
	}
	switch {
	case err == nil:
		upd.Status, upd.DeliveredAt = "succeeded", nullStr(now)
	case code == http.StatusGone || d.Attempts >= w.MaxAttempts:
		upd.Status, upd.LastError = "failed", err.Error()
	default:
		upd.Status, upd.LastError = "pending", err.Error()
		upd.NextAttemptAt = formatTime(s.Now().Add(s.backoff(d.Attempts)))
	}
	if err := s.Q.FinishDelivery(ctx, upd); err != nil {
		s.Log.Error("webhook state update failed", "delivery_id", d.ID, "err", err)
	}
	s.Log.Info("webhook delivery", "delivery_id", d.ID, "event", ev.Type, "attempt", d.Attempts, "status", upd.Status, "code", code)
}

// backoff: RetryBase * 2^(attempt-1), capped, with ±20% jitter.
func (s *Service) backoff(attempt int64) time.Duration {
	d := s.Cfg.Webhook.RetryBase << min(attempt-1, 20)
	if d <= 0 || d > s.Cfg.Webhook.RetryMax {
		d = s.Cfg.Webhook.RetryMax
	}
	return time.Duration(float64(d) * (0.8 + 0.4*rand.Float64()))
}
