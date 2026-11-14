package mcpserver

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/staqly/staqly/apps/api/internal/app"
	"github.com/staqly/staqly/apps/api/internal/httpapi"
)

// Webhooks and alerts: the REST management surface for integrations, as thin
// wrappers over the same services.

// testLimiter budgets test_webhook / test_alert per principal
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

	add(s, t, &mcp.Tool{Name: "list_alerts", Description: "List alerts: where staqly tells people what happened (Telegram, Slack, Discord, ntfy, email). Secrets are masked. Requires alerts:manage.",
		Annotations: ro()}, t.listAlerts)
	add(s, t, &mcp.Tool{Name: "get_alert", Description: "Get one alert. Secrets are masked. Requires alerts:manage.", Annotations: ro()}, t.getAlert)
	add(s, t, &mcp.Tool{Name: "create_alert", Description: "Create an alert that tells people when posts go out or fail and when a channel needs reconnecting. Requires alerts:manage.",
		Annotations: wr()}, t.createAlert)
	add(s, t, &mcp.Tool{Name: "update_alert", Description: "Update an alert's name, config, events, collection filter, digest window or enabled flag. Setting only enabled to its current value is a conflict. Requires alerts:manage.",
		Annotations: wr()}, t.updateAlert)
	add(s, t, &mcp.Tool{Name: "delete_alert", Description: "Delete an alert. Requires alerts:manage.", Annotations: destructive()}, t.deleteAlert)
	add(s, t, &mcp.Tool{Name: "test_alert", Description: "Send a test message through an alert right away. Rate-limited. Requires alerts:manage.",
		Annotations: wr()}, t.testAlert)
	add(s, t, &mcp.Tool{Name: "list_alert_deliveries", Description: "List recent messages sent (or failed) through an alert, newest first. Requires alerts:manage.",
		Annotations: ro()}, t.alertDeliveries)
}

// ---- webhooks ----

type webhookRef struct {
	WebhookID string `json:"webhook_id" jsonschema:"webhook ID (wh_...)"`
}

type webhookFields struct {
	Description   *string   `json:"description,omitempty" jsonschema:"what the receiver does, shown in the dashboard"`
	Events        *[]string `json:"events,omitempty" jsonschema:"event types to send, e.g. [\"post.published\"]; [\"*\"] (default) sends all"`
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
	PostID    string `json:"post_id,omitempty" jsonschema:"only deliveries of events about this post (post_...)"`
	Limit     int    `json:"limit,omitempty" jsonschema:"max deliveries (default 50, max 200)"`
	Cursor    string `json:"cursor,omitempty" jsonschema:"next_cursor of the previous page, for older deliveries"`
}

func (t *tools) listDeliveries(ctx context.Context, _ *mcp.CallToolRequest, in listDeliveriesIn) (*mcp.CallToolResult, any, error) {
	d, err := t.svc.ListDeliveries(ctx, t.principal(ctx), in.WebhookID, in.Status, in.PostID, in.Limit, in.Cursor)
	return nil, map[string]any{"deliveries": d.Data, "next_cursor": d.NextCursor}, err
}

type deliveryRef struct {
	DeliveryID string `json:"delivery_id" jsonschema:"delivery ID (dlv_...)"`
}

func (t *tools) retryDelivery(ctx context.Context, _ *mcp.CallToolRequest, in deliveryRef) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.RetryDelivery(ctx, t.principal(ctx), in.DeliveryID))
}

// ---- alerts ----

type alertRef struct {
	AlertID string `json:"alert_id" jsonschema:"alert ID (nch_...)"`
}

type alertFields struct {
	Enabled             *bool          `json:"enabled,omitempty" jsonschema:"false pauses the alert"`
	Config              map[string]any `json:"config,omitempty" jsonschema:"type-specific settings. slack/discord: webhook_url (or bot_token + channel). telegram: bot_token, chat_id. ntfy: topic, server_url, access_token, priority. email: host, port, username, password, from, to (a list of addresses), tls (starttls, tls or none)"`
	Events              *[]string      `json:"events,omitempty" jsonschema:"event types to tell about, e.g. [\"post.published\", \"post.failed\"]"`
	CollectionIDs       *[]string      `json:"collection_ids,omitempty" jsonschema:"only events from these collections (col_...); empty means all"`
	DigestWindowSeconds *int64         `json:"digest_window_seconds,omitempty" jsonschema:"group messages within this many seconds into one; 0 sends each"`
}

func (f alertFields) input(name *string, typ string) app.AlertInput {
	ci := app.AlertInput{Name: name, Type: typ, Enabled: f.Enabled, Events: f.Events, CollectionIDs: f.CollectionIDs,
		DigestWindowSeconds: f.DigestWindowSeconds}
	if f.Config != nil {
		ci.Config = map[string]json.RawMessage{}
		for k, v := range f.Config {
			ci.Config[k], _ = json.Marshal(v)
		}
	}
	return ci
}

func (t *tools) listAlerts(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	c, err := t.svc.ListAlerts(ctx, t.principal(ctx))
	return nil, map[string]any{"alerts": c}, err
}

func (t *tools) getAlert(ctx context.Context, _ *mcp.CallToolRequest, in alertRef) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.GetAlert(ctx, t.principal(ctx), in.AlertID))
}

type createAlertIn struct {
	Name string `json:"name" jsonschema:"alert name"`
	Type string `json:"type" jsonschema:"telegram, slack, discord, ntfy or email"`
	alertFields
}

func (t *tools) createAlert(ctx context.Context, _ *mcp.CallToolRequest, in createAlertIn) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.CreateAlert(ctx, t.principal(ctx), in.input(&in.Name, in.Type)))
}

type updateAlertIn struct {
	AlertID string  `json:"alert_id" jsonschema:"alert ID (nch_...)"`
	Name    *string `json:"name,omitempty" jsonschema:"new name"`
	alertFields
}

func (t *tools) updateAlert(ctx context.Context, _ *mcp.CallToolRequest, in updateAlertIn) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.UpdateAlert(ctx, t.principal(ctx), in.AlertID, in.input(in.Name, "")))
}

func (t *tools) deleteAlert(ctx context.Context, _ *mcp.CallToolRequest, in alertRef) (*mcp.CallToolResult, any, error) {
	return done(map[string]any{"deleted": true, "id": in.AlertID}, t.svc.DeleteAlert(ctx, t.principal(ctx), in.AlertID))
}

func (t *tools) testAlert(ctx context.Context, _ *mcp.CallToolRequest, in alertRef) (*mcp.CallToolResult, any, error) {
	if err := t.allowTest(ctx); err != nil {
		return nil, nil, err
	}
	return nil0(t.svc.TestAlert(ctx, t.principal(ctx), in.AlertID))
}

type alertDeliveriesIn struct {
	AlertID string `json:"alert_id" jsonschema:"alert ID (nch_...)"`
	Limit   int    `json:"limit,omitempty" jsonschema:"max deliveries (default 50, max 200)"`
}

func (t *tools) alertDeliveries(ctx context.Context, _ *mcp.CallToolRequest, in alertDeliveriesIn) (*mcp.CallToolResult, any, error) {
	d, err := t.svc.ListAlertDeliveries(ctx, t.principal(ctx), in.AlertID, in.Limit)
	return nil, map[string]any{"deliveries": d}, err
}
