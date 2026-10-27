package mcpserver

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/staqly/staqly/apps/api/internal/app"
)

// Workspace administration: API keys, sessions, workspace settings, plans,
// exports and device links. The services enforce the same rules as REST
// (admin scope; deleting the workspace needs a dashboard session).
//
// Deliberately not exposed: rotating the recovery key (the service only
// allows a dashboard session, so no MCP caller could ever use it) and the
// browser-only flows (sign-in, sign-out, setup, device-link redeem, decide
// links).

func (t *tools) addWorkspaceTools(s *mcp.Server) {
	add(s, t, &mcp.Tool{Name: "list_api_keys", Description: "List API keys (prefix, scopes, last use, expiry, revocation) and the available scopes. Requires admin.",
		Annotations: ro()}, t.listKeys)
	add(s, t, &mcp.Tool{Name: "create_api_key", Description: "Create an API key with the given scopes (cloud only: the Community Edition has no API keys, it uses its access token). The raw key is returned once. Requires admin.",
		Annotations: wr()}, t.createKey)
	add(s, t, &mcp.Tool{Name: "revoke_api_key", Description: "Revoke an API key; it stops working immediately. Revoking a revoked key is a conflict. Requires admin.", Annotations: destructive()}, t.revokeKey)
	add(s, t, &mcp.Tool{Name: "rotate_api_key", Description: "Replace an API key with a new one (same name and scopes); the old key stops working and the new raw key is returned once. Requires admin.",
		Annotations: destructive()}, t.rotateKey)

	add(s, t, &mcp.Tool{Name: "list_sessions", Description: "List signed-in dashboard sessions (browser, created, last seen, expiry). Requires admin.", Annotations: ro()}, t.listSessions)
	add(s, t, &mcp.Tool{Name: "revoke_session", Description: "Sign out a dashboard session. Revoking a revoked session is a conflict. Requires admin.", Annotations: destructive()}, t.revokeSession)

	add(s, t, &mcp.Tool{Name: "get_workspace", Description: "Get the workspace name, ID, plan and time zone.", Annotations: ro()}, t.getWorkspace)
	add(s, t, &mcp.Tool{Name: "rename_workspace", Description: "Rename the workspace and/or set its time zone (IANA name: post repeats and schedule rules without tz use it). Requires admin.", Annotations: wr()}, t.renameWorkspace)
	add(s, t, &mcp.Tool{Name: "delete_workspace", Description: "Permanently delete the workspace and everything in it (cloud only). confirm must equal the workspace name. Like REST, this needs a signed-in dashboard session, so API keys get a forbidden error.",
		Annotations: destructive()}, t.deleteWorkspace)
	add(s, t, &mcp.Tool{Name: "start_workspace_export", Description: "Start a ZIP export of the whole workspace (posts, channels, schedules, media, events). Poll get_workspace_export until status is ready. Requires admin.",
		Annotations: wr()}, t.startExport)
	add(s, t, &mcp.Tool{Name: "get_workspace_export", Description: "Get a workspace export's status (pending, running, ready, failed, expired) and, once ready, its download URL. Requires admin.",
		Annotations: ro()}, t.getExport)

	add(s, t, &mcp.Tool{Name: "list_plans", Description: "List the cloud plans with their limits and prices.", Annotations: ro()}, t.listPlans)
	add(s, t, &mcp.Tool{Name: "get_usage", Description: "Get the workspace's plan, usage this period against its limits, and whether it is suspended.", Annotations: ro()}, t.getUsage)
	add(s, t, &mcp.Tool{Name: "get_billing", Description: "Get billing status: whether Stripe billing is enabled, the plan, the subscription status and the end of the trial or paid period. Requires admin.",
		Annotations: ro()}, t.getBilling)
	add(s, t, &mcp.Tool{Name: "start_billing_checkout", Description: "Start a Stripe Checkout for a plan (starter, plus, pro, business; default pro) billed monthly or yearly (interval month or year; default month) and return its URL for a person to open and pay. A workspace that already pays is switched to that plan instead (prorated) and gets the billing page URL. Cloud with billing enabled. Requires admin.",
		Annotations: wr()}, t.startCheckout)
	add(s, t, &mcp.Tool{Name: "open_billing_portal", Description: "Return a Stripe customer portal URL (payment method, invoices, cancel). Requires admin.",
		Annotations: wr()}, t.billingURL(t.svc.PortalSession))

	add(s, t, &mcp.Tool{Name: "create_device_link", Description: "Create a one-time link (valid 5 minutes) that signs another browser into the dashboard. Only for Community Edition callers (its access token, or MCP over stdio) and dashboard sessions; API keys get a forbidden error.",
		Annotations: wr()}, t.createDeviceLink)
}

// ---- API keys ----

type keyRef struct {
	KeyID string `json:"key_id" jsonschema:"API key ID (key_...)"`
}

func (t *tools) listKeys(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	k, err := t.svc.ListAPIKeys(ctx, t.principal(ctx))
	return nil, map[string]any{"keys": k, "available_scopes": app.AllScopes}, err
}

type createKeyIn struct {
	Name      string   `json:"name" jsonschema:"key name, e.g. the agent that uses it"`
	Scopes    []string `json:"scopes" jsonschema:"scopes to grant, e.g. [\"collections:read\", \"posts:read\", \"posts:write\"]"`
	ExpiresAt string   `json:"expires_at,omitempty" jsonschema:"RFC3339 expiry; omit for a key that does not expire"`
}

func (t *tools) createKey(ctx context.Context, _ *mcp.CallToolRequest, in createKeyIn) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.CreateAPIKey(ctx, t.principal(ctx), app.CreateAPIKeyInput{Name: in.Name, Scopes: in.Scopes, ExpiresAt: in.ExpiresAt}))
}

func (t *tools) revokeKey(ctx context.Context, _ *mcp.CallToolRequest, in keyRef) (*mcp.CallToolResult, any, error) {
	return done(map[string]any{"revoked": true, "id": in.KeyID}, t.svc.RevokeAPIKey(ctx, t.principal(ctx), in.KeyID))
}

func (t *tools) rotateKey(ctx context.Context, _ *mcp.CallToolRequest, in keyRef) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.RotateAPIKey(ctx, t.principal(ctx), in.KeyID))
}

// ---- sessions ----

func (t *tools) listSessions(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	s, err := t.svc.ListSessions(ctx, t.principal(ctx))
	return nil, map[string]any{"sessions": s}, err
}

type sessionRef struct {
	SessionID string `json:"session_id" jsonschema:"session ID (ses_...)"`
}

func (t *tools) revokeSession(ctx context.Context, _ *mcp.CallToolRequest, in sessionRef) (*mcp.CallToolResult, any, error) {
	return done(map[string]any{"revoked": true, "id": in.SessionID}, t.svc.RevokeSession(ctx, t.principal(ctx), in.SessionID))
}

// ---- workspace ----

func (t *tools) getWorkspace(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.GetWorkspace(ctx, t.principal(ctx)))
}

type renameWorkspaceIn struct {
	Name     *string `json:"name,omitempty" jsonschema:"new workspace name"`
	Timezone *string `json:"timezone,omitempty" jsonschema:"IANA time zone name, e.g. Europe/Warsaw or UTC"`
}

func (t *tools) renameWorkspace(ctx context.Context, _ *mcp.CallToolRequest, in renameWorkspaceIn) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.UpdateWorkspace(ctx, t.principal(ctx), app.WorkspaceUpdate{Name: in.Name, Timezone: in.Timezone}))
}

type deleteWorkspaceIn struct {
	Confirm string `json:"confirm" jsonschema:"must be exactly the workspace name"`
}

func (t *tools) deleteWorkspace(ctx context.Context, _ *mcp.CallToolRequest, in deleteWorkspaceIn) (*mcp.CallToolResult, any, error) {
	return done(map[string]any{"deleted": true}, t.svc.DeleteWorkspace(ctx, t.principal(ctx), in.Confirm))
}

// exportOut makes the download URL absolute when the public API URL is known
// (downloading needs the same credentials as the call).
func (t *tools) exportOut(x app.WorkspaceExport, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return nil, nil, err
	}
	if x.DownloadURL != "" {
		x.DownloadURL = strings.TrimRight(t.svc.Cfg.PublicURL, "/") + x.DownloadURL
	}
	return nil, x, nil
}

func (t *tools) startExport(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	return t.exportOut(t.svc.RequestWorkspaceExport(ctx, t.principal(ctx)))
}

type exportRef struct {
	ExportID string `json:"export_id" jsonschema:"export ID (exp_...) from start_workspace_export"`
}

func (t *tools) getExport(ctx context.Context, _ *mcp.CallToolRequest, in exportRef) (*mcp.CallToolResult, any, error) {
	return t.exportOut(t.svc.GetWorkspaceExport(ctx, t.principal(ctx), in.ExportID))
}

// ---- plans ----

func (t *tools) listPlans(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	return nil, map[string]any{"plans": t.svc.LivePlans(ctx)}, nil
}

func (t *tools) getUsage(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.GetUsage(ctx, t.principal(ctx)))
}

type checkoutIn struct {
	Plan     string `json:"plan,omitempty" jsonschema:"starter, plus, pro or business (default pro)"`
	Interval string `json:"interval,omitempty" jsonschema:"month or year (default month); yearly is 20 percent off or more"`
}

func (t *tools) startCheckout(ctx context.Context, _ *mcp.CallToolRequest, in checkoutIn) (*mcp.CallToolResult, any, error) {
	u, err := t.svc.CheckoutSession(ctx, t.principal(ctx), in.Plan, in.Interval)
	if err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"url": u}, nil
}

func (t *tools) getBilling(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.GetBilling(ctx, t.principal(ctx)))
}

func (t *tools) billingURL(f func(context.Context, app.Principal) (string, error)) func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		u, err := f(ctx, t.principal(ctx))
		if err != nil {
			return nil, nil, err
		}
		return nil, map[string]string{"url": u}, nil
	}
}

// ---- device links ----

func (t *tools) createDeviceLink(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	return nil0(t.svc.CreateDeviceLink(ctx, t.principal(ctx), ""))
}
