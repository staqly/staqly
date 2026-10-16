# MCP setup

staqly exposes 71 MCP tools over two transports, covering every REST
operation except the browser-only flows (sign-in/out, setup, device-link
redeem, decision links), recovery key rotation (dashboard session only), the
agent list (`/v1/agents`) and a new summary (`POST /v1/items/{id}/summarize`). Both use the same domain
services as REST. In **local mode** (default) no key is needed. In **cloud
mode** ([cloud.md](cloud.md)) MCP requires an API key and enforces its scopes
(a read-only key cannot write; `approve_item`/`reject_item` need
`items:review`; custom actions need `actions:execute`). The recovery key is
never accepted or exposed by MCP.

| Tool | Scope (cloud mode) |
|---|---|
| `list_collections`, `get_collection` | collections:read |
| `create_collection` | collections:write |
| `list_items`, `get_item`, `search_items`, `list_pending_approvals`, `get_item_history`, `list_available_actions` | items:read |
| `create_item` (incl. `media_base64` upload), `update_item`, `unarchive_item` | items:write |
| `approve_item`, `reject_item` | items:review |
| `execute_custom_action` | actions:execute |
| `execute_bulk_action` | items:review (approve/reject) or actions:execute |
| `update_collection`, `delete_collection` | collections:write |
| `get_item_versions`, `get_media`, `list_events`, `export_events`, `await_decision`, `get_flow`, `list_tags`, `list_requested_work` | items:read |
| `delete_item`, `upload_media`, `delete_media`, `report_result` | items:write |
| `list_watchers`, `create_watcher`, `get_watcher`, `update_watcher`, `delete_watcher`, `run_watcher` | watchers:manage |
| `list_webhooks`, `get_webhook`, `create_webhook`, `update_webhook`, `delete_webhook`, `rotate_webhook_secret`, `test_webhook`, `list_webhook_deliveries`, `retry_webhook_delivery` | webhooks:manage |
| `list_notification_channels`, `get_notification_channel`, `create_notification_channel`, `update_notification_channel`, `delete_notification_channel`, `test_notification_channel`, `list_notification_deliveries` | channels:manage |
| `list_api_keys`, `create_api_key`, `revoke_api_key`, `rotate_api_key`, `list_sessions`, `revoke_session`, `rename_workspace`, `purge_item`, `start_workspace_export`, `get_workspace_export`, `choose_free_plan`, `get_billing`, `start_billing_checkout`, `open_billing_portal` | admin |
| `get_workspace`, `list_plans`, `get_usage` | none |
| `delete_workspace` | dashboard session only (API keys get 403) |
| `create_device_link` | local mode or dashboard session (API keys get 403) |

A suspended workspace can call read-only tools plus `revoke_session`,
`revoke_api_key`, `create_device_link`, `start_workspace_export`,
`choose_free_plan`, `start_billing_checkout`, `open_billing_portal`, `delete_workspace` and
`purge_item` (the REST allowlist); other tools answer with `plan_expired`.
In local mode `delete_workspace` answers with a conflict (one workspace, delete
the data volume instead) and `choose_free_plan` with `not_available`.
`default_view` on `create_collection`/`update_collection` is kept for API
compatibility only; the dashboard always shows a collection as a table.
`test_webhook` and `test_notification_channel` are rate-limited per caller.

`await_decision` waits up to `timeout_seconds` (default 45, max 300) and
returns `decided=false` when the time is up; the agent calls it again to keep
waiting. Keep the value under your client's tool-call timeout (often 60 s);
raise that timeout in the client before passing a longer wait.

## Local mode

### Streamable HTTP (recommended)

```bash
claude mcp add --transport http staqly http://localhost:3333/mcp
claude mcp list        # → staqly: http://localhost:3333/mcp (HTTP) - ✔ Connected

codex mcp add staqly --url http://localhost:3333/mcp
```

Project-scoped `.mcp.json` for Claude Code / `~/.codex/config.toml` for Codex:

```json
{ "mcpServers": { "staqly": { "type": "http", "url": "http://localhost:3333/mcp" } } }
```

```toml
[mcp_servers.staqly]
url = "http://localhost:3333/mcp"
```

### STDIO

The `mcp-stdio` binary ships in the `ghcr.io/staqly/staqly` image. Run it
inside the running container so it uses the same database (use `-i`, never
`-t`):

```bash
claude mcp add staqly -- docker exec -i staqly mcp-stdio
codex mcp add staqly -- docker exec -i staqly mcp-stdio
```

### OpenCode

`opencode.json` in your project (see [integrations/opencode](../integrations/opencode)):

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "staqly": { "type": "remote", "url": "http://localhost:3333/mcp", "enabled": true }
  }
}
```

STDIO: `"staqly": { "type": "local", "command": ["docker", "exec", "-i", "staqly", "mcp-stdio"], "enabled": true }`.
Check with `opencode mcp list`. Cloud: add
`"headers": { "Authorization": "Bearer {env:STAQLY_API_KEY}" }`.

Without Docker: `cd apps/api && go build -o staqly-mcp ./cmd/mcp-stdio` and
run it with `DATA_DIR` pointing at the server's data directory.

## Cloud mode

Streamable HTTP with a bearer key; stateless, so every request is
authenticated and there is no session to hijack. Create a key per agent under
**API keys** in the sidebar (e.g. *Reviewer agent*:
`collections:read items:read items:review`).

```bash
export STAQLY_API_KEY={API_KEY}
claude mcp add --transport http staqly https://staqly.example.com/mcp \
  --header "Authorization: Bearer $STAQLY_API_KEY"
codex mcp add staqly --url https://staqly.example.com/mcp --bearer-token-env-var STAQLY_API_KEY
```

```json
{
  "mcpServers": {
    "staqly": {
      "type": "http",
      "url": "https://staqly.example.com/mcp",
      "headers": { "Authorization": "Bearer ${STAQLY_API_KEY}" }
    }
  }
}
```

```toml
[mcp_servers.staqly]
url = "https://staqly.example.com/mcp"
bearer_token_env_var = "STAQLY_API_KEY"
```

STDIO against a cloud-mode container on the same host:
`docker exec -i -e STAQLY_API_KEY staqly mcp-stdio` with the key in the
client's environment (`claude mcp add staqly -e STAQLY_API_KEY={API_KEY} -- ...`).
The STDIO process re-checks the key at most once a minute, so a revoked or
expired key stops working within a minute. Without a key in cloud mode it
exits with `cloud mode: STAQLY_API_KEY is not set`.

`staqly mcp config <claude|codex|opencode|cursor|windsurf>` prints all of the
above for the current CLI profile (`--stdio`, `--container <name>`,
`--snippet`).

## What was verified

On this machine (Claude Code CLI, Codex CLI 0.162.1, n8n 2.42.7):

* `claude mcp list` reported ✔ Connected for the HTTP and STDIO configurations,
  both keyless (local mode) and with a bearer key; `claude -p "create a
  collection called 'Claude Code Test' and show me all pending images"`
  created the collection and listed pending images via MCP.
* `codex exec` called `staqly/create_item` (with a bearer key) and
  `staqly/list_pending_approvals` (keyless) successfully. The default Codex
  model was unavailable for the account's region, so `-m gpt-5-mini` was used —
  unrelated to staqly.
* Automated tests cover scope enforcement over HTTP MCP and a full client
  session (all tools listed; create → pending → approve → search
  "approved today" → publish → complete) over the in-memory transport.

## Example prompts

* “Create a collection called Instagram Avatar.”
* “Show me all pending images.”
* “Add this generated image to my Instagram Avatar collection.” (the agent
  passes `media_base64` or an external `url`)
* “Find the images I approved yesterday.” (→ `search_items` with
  `status=approved`, `type=image`, `decided_after`, `decided_before`)
* “Reject {ITEM_ID} with feedback: the lighting is too harsh.”
* “Regenerate {ITEM_ID} with feedback: make her smile.” (→ `execute_custom_action`)
