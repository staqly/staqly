package mcpserver

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/httpapi"
)

// Webhooks, watchers and notification channels: the REST management
// surface for integrations, as thin wrappers over the same services.

// testLimiter budgets test_webhook / test_notification_channel per principal
// (each makes an outbound request), like the REST test endpoints. Package
// level because the HTTP transport builds a server per request.
var testLimiter = httpapi.NewTestLimiter()

func (t *tools) allowTest(ctx context.Context) error {
	ok, wait := testLimiter.Allow(t.principal(ctx))
	if ok {
		return nil
	}
	secs := max(1, int(wait.Seconds()+0.999))
	return &app.Error{Code: "rate_limited", Status: 429, Message: "too many test requests; retry after " + strconv.Itoa(secs) + "s"}
}

func (t *tools) addIntegrationTools(s *mcp.Server) {
	add(s, t, &mcp.Tool{Name: "list_webhooks", Description: "List webhooks with their URL, events, filters and state. Requires webhooks:manage.", Annotations: ro()}, t.listWebhooks)
	add(s, t, &mcp.Tool{Name: "get_webhook", Description: "Get one webhook. Requires webhooks:manage.", Annotations: ro()}, t.getWebhook)
	add(s, t, &mcp.Tool{Name: "create_webhook", Description: "Create a webhook that receives signed event POSTs. The response contains the signing secret once. Requires webhooks:manage.",
		Annotations: wr()}, t.createWebhook)
	add(s, t, &mcp.Tool{Name: "update_webhook", Description: "Update a webhook's URL, description, events, collection filter, active flag, timeout or retry count. Setting only active to its current value is a conflict. Requires webhooks:manage.",
		Annotations: wr()}, t.updateWebhook)
	add(s, t, &mcp.Tool{Name: "delete_webhook", Description: "Delete a webhook and stop its deliveries. Requires webhooks:manage.", Annotations: destructive()}, t.deleteWebhook)
	add(s, t, &mcp.Tool{Name: "rotate_webhook_secret", Description: "Replace a webhook's signing secret; the new secret is returned once and the old one stops working. Requires webhooks:manage.",
		Annotations: destructive()}, t.rotateWebhookSecret)
	add(s, t, &mcp.Tool{Name: "test_webhook", Description: "Queue a webhook.test delivery to this webhook only. Rate-limited. Requires webhooks:manage.", Annotations: wr()}, t.testWebhook)
	add(s, t, &mcp.Tool{Name: "list_webhook_deliveries", Description: "List webhook delivery attempts (status, response code, error), newest first. Requires webhooks:manage.",
		Annotations: ro()}, t.listDeliveries)
	add(s, t, &mcp.Tool{Name: "retry_webhook_delivery", Description: "Re-queue a finished (succeeded or failed) webhook delivery. Requires webhooks:manage.", Annotations: wr()}, t.retryDelivery)

	add(s, t, &mcp.Tool{Name: "create_watcher", Description: "Watch a custom RSS/Atom feed (kind rss) or a web page for changes (kind url_diff); new entries become pending items. Requires watchers:manage.",
		Annotations: wr()}, t.createWatcher)
	add(s, t, &mcp.Tool{Name: "get_watcher", Description: "Get one watcher with its status, last run and errors. Requires watchers:manage.", Annotations: ro()}, t.getWatcher)
	add(s, t, &mcp.Tool{Name: "update_watcher", Description: "Update a watcher's name, URL, collections, keywords or interval, or pause/resume it (pausing a paused or resuming an active watcher is a conflict). Requires watchers:manage.",
		Annotations: wr()}, t.updateWatcher)
	add(s, t, &mcp.Tool{Name: "delete_watcher", Description: "Delete a watcher (items it created stay). Requires watchers:manage.", Annotations: destructive()}, t.deleteWatcher)
	add(s, t, &mcp.Tool{Name: "run_watcher", Description: "Check a watcher's source now instead of waiting for the next interval. Requires watchers:manage.", Annotations: wr()}, t.runWatcher)

	add(s, t, &mcp.Tool{Name: "list_notification_channels", Description: "List notification channels (Telegram, Slack, Discord, ntfy, email). Secrets are masked. Requires channels:manage.",
		Annotations: ro()}, t.listChannels)
	add(s, t, &mcp.Tool{Name: "get_notification_channel", Description: "Get one notification channel. Secrets are masked. Requires channels:manage.", Annotations: ro()}, t.getChannel)
	add(s, t, &mcp.Tool{Name: "create_notification_channel", Description: "Create a notification channel that tells reviewers about new pending items. Requires channels:manage.",
		Annotations: wr()}, t.createChannel)
	add(s, t, &mcp.Tool{Name: "update_notification_channel", Description: "Update a notification channel's name, config, events, collection filter, digest window or enabled flag. Setting only enabled to its current value is a conflict. Requires channels:manage.",
		Annotations: wr()}, t.updateChannel)
	add(s, t, &mcp.Tool{Name: "delete_notification_channel", Description: "Delete a notification channel. Requires channels:manage.", Annotations: destructive()}, t.deleteChannel)
	add(s, t, &mcp.Tool{Name: "test_notification_channel", Description: "Send a test message through a notification channel right away. Rate-limited. Requires channels:manage.",
		Annotations: wr()}, t.testChannel)
	add(s, t, &mcp.Tool{Name: "list_notification_deliveries", Description: "List recent notifications sent (or failed) through a channel, newest first. Requires channels:manage.",
		Annotations: ro()}, t.channelDeliveries)
}

// ---- webhooks ----

type webhookRef struct {
	WebhookID string `json:"webhook_id" jsonschema:"webhook ID (wh_...)"`
}

type webhookFields struct {
	Description   *string   `json:"description,omitempty" jsonschema:"what the receiver does, shown in the dashboard"`
	Events        *[]string `json:"events,omitempty" jsonschema:"event types to send, e.g. [\"item.approved\"]; [\"*\"] (default) sends all"`
	CollectionIDs *[]string `json:"collection_ids,omitempty" jsonschema:"only events from these collections (col_...); empty sends all"`
	Active        *bool     `json:"active,omitempty" jsonschema:"false pauses deliveries"`
	TimeoutMS     *int64    `json:"timeout_ms,omitempty" jsonschema:"per-attempt timeout in milliseconds"`
	MaxAttempts   *int64    `json:"max_attempts,omitempty" jsonschema:"delivery attempts before giving up"`
}

func (f webhookFields) input(url *string) app.WebhookInput {
	return app.WebhookInput{URL: url, Description: f.Description, Events: f.Events, CollectionIDs: f.CollectionIDs,
		Active: f.Active, TimeoutMS: f.TimeoutMS, MaxAttempts: f.MaxAttempts}
}

func (t *tools) listWebhooks(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	w, err := t.svc.ListWebhooks(ctx, t.principal(ctx))
	return nil, map[string]any{"webhooks": w}, err
}

func (t *tools) getWebhook(ctx context.Context, _ *mcp.CallToolRequest, in webhookRef) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.GetWebhook(ctx, t.principal(ctx), in.WebhookID))
}

type createWebhookIn struct {
	URL string `json:"url" jsonschema:"https URL that receives the POSTs"`
	webhookFields
}

func (t *tools) createWebhook(ctx context.Context, _ *mcp.CallToolRequest, in createWebhookIn) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.CreateWebhook(ctx, t.principal(ctx), in.input(&in.URL)))
}

type updateWebhookIn struct {
	WebhookID string  `json:"webhook_id" jsonschema:"webhook ID (wh_...)"`
	URL       *string `json:"url,omitempty" jsonschema:"new receiver URL"`
	webhookFields
}

func (t *tools) updateWebhook(ctx context.Context, _ *mcp.CallToolRequest, in updateWebhookIn) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.UpdateWebhook(ctx, t.principal(ctx), in.WebhookID, in.input(in.URL)))
}

func (t *tools) deleteWebhook(ctx context.Context, _ *mcp.CallToolRequest, in webhookRef) (*mcp.CallToolResult, any, error) {
	return done(map[string]any{"deleted": true, "id": in.WebhookID}, t.svc.DeleteWebhook(ctx, t.principal(ctx), in.WebhookID))
}

func (t *tools) rotateWebhookSecret(ctx context.Context, _ *mcp.CallToolRequest, in webhookRef) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.UpdateWebhook(ctx, t.principal(ctx), in.WebhookID, app.WebhookInput{RotateSecret: true}))
}

func (t *tools) testWebhook(ctx context.Context, _ *mcp.CallToolRequest, in webhookRef) (*mcp.CallToolResult, any, error) {
	if err := t.allowTest(ctx); err != nil {
		return nil, nil, err
	}
	return nil0(t.svc.TestWebhook(ctx, t.principal(ctx), in.WebhookID))
}

type listDeliveriesIn struct {
	WebhookID string `json:"webhook_id,omitempty" jsonschema:"only deliveries of this webhook (wh_...)"`
	Status    string `json:"status,omitempty" jsonschema:"pending, succeeded or failed"`
	ItemID    string `json:"item_id,omitempty" jsonschema:"only deliveries of events about this item (itm_...)"`
	Limit     int    `json:"limit,omitempty" jsonschema:"max deliveries (default 50, max 200)"`
}

func (t *tools) listDeliveries(ctx context.Context, _ *mcp.CallToolRequest, in listDeliveriesIn) (*mcp.CallToolResult, any, error) {
	d, err := t.svc.ListDeliveries(ctx, t.principal(ctx), in.WebhookID, in.Status, in.ItemID, in.Limit)
	return nil, map[string]any{"deliveries": d}, err
}

type deliveryRef struct {
	DeliveryID string `json:"delivery_id" jsonschema:"delivery ID (dlv_...)"`
}

func (t *tools) retryDelivery(ctx context.Context, _ *mcp.CallToolRequest, in deliveryRef) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.RetryDelivery(ctx, t.principal(ctx), in.DeliveryID))
}

// ---- watchers ----

type watcherRef struct {
	WatcherID string `json:"watcher_id" jsonschema:"watcher ID (wat_...)"`
}

type watcherFields struct {
	Collections     *[]string `json:"collections,omitempty" jsonschema:"collection IDs or exact names; each new entry is added to every one of them (instead of collection)"`
	Keywords        *[]string `json:"keywords,omitempty" jsonschema:"only entries whose title or summary contains one of these words (case-insensitive)"`
	ExcludeKeywords *[]string `json:"exclude_keywords,omitempty" jsonschema:"skip entries whose title or summary contains any of these words"`
	Every           *string   `json:"every,omitempty" jsonschema:"check interval, e.g. 30m or 6h"`
}

type createWatcherIn struct {
	Name       string `json:"name" jsonschema:"watcher name"`
	Kind       string `json:"kind,omitempty" jsonschema:"rss (RSS/Atom feed) or url_diff (page or JSON changes); leave out to detect it from the URL"`
	URL        string `json:"url" jsonschema:"feed or page URL"`
	Collection string `json:"collection,omitempty" jsonschema:"collection ID or exact name that receives new entries (or pass collections)"`
	watcherFields
}

func (t *tools) createWatcher(ctx context.Context, _ *mcp.CallToolRequest, in createWatcherIn) (*mcp.CallToolResult, any, error) {
	var kind *string
	if in.Kind != "" {
		kind = &in.Kind
	}
	var col *string
	if in.Collection != "" {
		col = &in.Collection
	}
	return nil0(t.svc.CreateWatcher(ctx, t.principal(ctx), app.WatcherInput{Name: &in.Name, Kind: kind, URL: &in.URL,
		Collection: col, Collections: in.Collections, Keywords: in.Keywords, ExcludeKeywords: in.ExcludeKeywords, Every: in.Every}))
}

func (t *tools) getWatcher(ctx context.Context, _ *mcp.CallToolRequest, in watcherRef) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.GetWatcher(ctx, t.principal(ctx), in.WatcherID))
}

type updateWatcherIn struct {
	WatcherID  string  `json:"watcher_id" jsonschema:"watcher ID (wat_...)"`
	Name       *string `json:"name,omitempty" jsonschema:"new name"`
	URL        *string `json:"url,omitempty" jsonschema:"new feed or page URL"`
	Collection *string `json:"collection,omitempty" jsonschema:"collection ID or exact name that receives new entries"`
	Status     *string `json:"status,omitempty" jsonschema:"active (resume) or paused"`
	watcherFields
}

func (t *tools) updateWatcher(ctx context.Context, _ *mcp.CallToolRequest, in updateWatcherIn) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.UpdateWatcher(ctx, t.principal(ctx), in.WatcherID, app.WatcherInput{Name: in.Name, URL: in.URL,
		Collection: in.Collection, Collections: in.Collections, Status: in.Status, Keywords: in.Keywords, ExcludeKeywords: in.ExcludeKeywords, Every: in.Every}))
}

func (t *tools) deleteWatcher(ctx context.Context, _ *mcp.CallToolRequest, in watcherRef) (*mcp.CallToolResult, any, error) {
	return done(map[string]any{"deleted": true, "id": in.WatcherID}, t.svc.DeleteWatcher(ctx, t.principal(ctx), in.WatcherID))
}

func (t *tools) runWatcher(ctx context.Context, _ *mcp.CallToolRequest, in watcherRef) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.RunWatcherNow(ctx, t.principal(ctx), in.WatcherID))
}

// ---- notification channels ----

type channelRef struct {
	ChannelID string `json:"channel_id" jsonschema:"notification channel ID (nch_...)"`
}

type channelFields struct {
	Enabled             *bool          `json:"enabled,omitempty" jsonschema:"false pauses the channel"`
	Config              map[string]any `json:"config,omitempty" jsonschema:"type-specific settings, e.g. {\"webhook_url\": \"https://hooks.slack.com/services/T000/B000/XXXX\"} for slack"`
	Events              *[]string      `json:"events,omitempty" jsonschema:"event types to notify about, e.g. [\"item.created\"]"`
	CollectionIDs       *[]string      `json:"collection_ids,omitempty" jsonschema:"only events from these collections (col_...); empty notifies for all"`
	DigestWindowSeconds *int64         `json:"digest_window_seconds,omitempty" jsonschema:"group notifications within this many seconds into one message; 0 sends each"`
}

func (f channelFields) input(name *string, typ string) app.ChannelInput {
	ci := app.ChannelInput{Name: name, Type: typ, Enabled: f.Enabled, Events: f.Events, CollectionIDs: f.CollectionIDs,
		DigestWindowSeconds: f.DigestWindowSeconds}
	if f.Config != nil {
		ci.Config = map[string]json.RawMessage{}
		for k, v := range f.Config {
			ci.Config[k], _ = json.Marshal(v)
		}
	}
	return ci
}

func (t *tools) listChannels(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	c, err := t.svc.ListNotificationChannels(ctx, t.principal(ctx))
	return nil, map[string]any{"channels": c}, err
}

func (t *tools) getChannel(ctx context.Context, _ *mcp.CallToolRequest, in channelRef) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.GetNotificationChannel(ctx, t.principal(ctx), in.ChannelID))
}

type createChannelIn struct {
	Name string `json:"name" jsonschema:"channel name"`
	Type string `json:"type" jsonschema:"telegram, slack, discord, ntfy or email"`
	channelFields
}

func (t *tools) createChannel(ctx context.Context, _ *mcp.CallToolRequest, in createChannelIn) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.CreateNotificationChannel(ctx, t.principal(ctx), in.input(&in.Name, in.Type)))
}

type updateChannelIn struct {
	ChannelID string  `json:"channel_id" jsonschema:"notification channel ID (nch_...)"`
	Name      *string `json:"name,omitempty" jsonschema:"new name"`
	channelFields
}

func (t *tools) updateChannel(ctx context.Context, _ *mcp.CallToolRequest, in updateChannelIn) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.UpdateNotificationChannel(ctx, t.principal(ctx), in.ChannelID, in.input(in.Name, "")))
}

func (t *tools) deleteChannel(ctx context.Context, _ *mcp.CallToolRequest, in channelRef) (*mcp.CallToolResult, any, error) {
	return done(map[string]any{"deleted": true, "id": in.ChannelID}, t.svc.DeleteNotificationChannel(ctx, t.principal(ctx), in.ChannelID))
}

func (t *tools) testChannel(ctx context.Context, _ *mcp.CallToolRequest, in channelRef) (*mcp.CallToolResult, any, error) {
	if err := t.allowTest(ctx); err != nil {
		return nil, nil, err
	}
	return nil0(t.svc.TestNotificationChannel(ctx, t.principal(ctx), in.ChannelID))
}

type channelDeliveriesIn struct {
	ChannelID string `json:"channel_id" jsonschema:"notification channel ID (nch_...)"`
	Limit     int    `json:"limit,omitempty" jsonschema:"max deliveries (default 50, max 200)"`
}

func (t *tools) channelDeliveries(ctx context.Context, _ *mcp.CallToolRequest, in channelDeliveriesIn) (*mcp.CallToolResult, any, error) {
	d, err := t.svc.ListNotificationDeliveries(ctx, t.principal(ctx), in.ChannelID, in.Limit)
	return nil, map[string]any{"deliveries": d}, err
}
