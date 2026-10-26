package mcpserver

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/staqly/staqly/apps/api/internal/app"
)

// The dashboard's assistant (app/assistant.go) uses these tools: the MCP server connected in memory, running as
// the person signed in, so its calls take the same path and scope checks as an agent's.

// assistantHidden: tools the assistant does not get. Their results carry secrets (API keys, webhook secrets,
// sign-in links) that would go to the AI service, they take secrets typed into the chat (connect_channel), they
// can't be undone at the scale of a workspace, or they make no sense in a chat (raw uploads, automation
// callbacks, exports).
var assistantHidden = map[string]bool{
	"create_api_key": true, "rotate_api_key": true, "create_device_link": true, "create_webhook": true,
	"rotate_webhook_secret": true, "connect_channel": true, "delete_workspace": true, "purge_post": true,
	"list_sessions": true, "start_billing_checkout": true, "open_billing_portal": true, "upload_media": true,
	"report_publish_result": true, "report_post_metrics": true, "export_events": true, "start_workspace_export": true,
	"get_workspace_export": true,
}

// AssistantTools connects an in-memory MCP session for p; close ends it.
func AssistantTools(svc *app.Service, version string) func(ctx context.Context, p app.Principal) (app.AssistantTools, func(), error) {
	app.SetAssistantInstructions(serverInstructions())
	return func(ctx context.Context, p app.Principal) (app.AssistantTools, func(), error) {
		// ponytail: a server per request (~100 tools reflected, a few ms); share one with a context principal
		// if chats get busy.
		srv := New(svc, p, version)
		ct, st := mcp.NewInMemoryTransports()
		ss, err := srv.Connect(ctx, st, nil)
		if err != nil {
			return app.AssistantTools{}, nil, err
		}
		cs, err := mcp.NewClient(&mcp.Implementation{Name: "staqly assistant", Version: version}, nil).Connect(ctx, ct, nil)
		if err != nil {
			ss.Close()
			return app.AssistantTools{}, nil, err
		}
		closeAll := func() { cs.Close(); ss.Close() }
		list, err := cs.ListTools(ctx, nil)
		if err != nil {
			closeAll()
			return app.AssistantTools{}, nil, err
		}
		out := app.AssistantTools{Call: func(ctx context.Context, name, args string) (string, error) {
			if assistantHidden[name] {
				return "", &app.Error{Code: "not_available", Status: 409, Message: name + " is not available in the assistant"}
			}
			var in map[string]any
			if err := json.Unmarshal([]byte(args), &in); err != nil {
				return "", &app.Error{Code: "validation_failed", Status: 422, Message: "the arguments are not a JSON object"}
			}
			res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: in})
			if err != nil {
				return "", err
			}
			var b strings.Builder
			if res.StructuredContent != nil {
				raw, _ := json.Marshal(res.StructuredContent)
				b.Write(raw)
			} else {
				for _, c := range res.Content {
					if t, ok := c.(*mcp.TextContent); ok {
						b.WriteString(t.Text)
					}
				}
			}
			if res.IsError {
				return "", &app.Error{Code: "tool_failed", Status: 422, Message: b.String()}
			}
			return b.String(), nil
		}}
		for _, t := range list.Tools {
			if assistantHidden[t.Name] {
				continue
			}
			schema, _ := json.Marshal(t.InputSchema)
			out.List = append(out.List, app.AssistantTool{Name: t.Name, Description: t.Description, Schema: schema,
				ReadOnly: t.Annotations != nil && t.Annotations.ReadOnlyHint})
		}
		return out, closeAll, nil
	}
}
