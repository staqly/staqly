# MCP setup

staq.ly exposes 14 MCP tools over two transports. Both use the same domain
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
| `create_item` (incl. `media_base64` upload), `update_item` | items:write |
| `approve_item`, `reject_item` | items:review |
| `execute_custom_action` | actions:execute |

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

The `mcp-stdio` binary ships in the API image. Run it inside the running
container so it uses the same database (use `-i`, never `-t`):

```bash
claude mcp add staqly -- docker exec -i staqly-api mcp-stdio
codex mcp add staqly -- docker exec -i staqly-api mcp-stdio
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

STDIO: `"staqly": { "type": "local", "command": ["docker", "exec", "-i", "staqly-api", "mcp-stdio"], "enabled": true }`.
Check with `opencode mcp list`. Cloud: add
`"headers": { "Authorization": "Bearer {env:STAQLY_API_KEY}" }`.

Without Docker: `cd apps/api && go build -o staqly-mcp ./cmd/mcp-stdio` and
run it with `DATA_DIR` pointing at the server's data directory.

## Cloud mode

Streamable HTTP with a bearer key; stateless, so every request is
authenticated and there is no session to hijack. Create a key per agent under
**Integrations → API keys** (e.g. *Reviewer agent*:
`collections:read items:read items:review`).

```bash
export STAQLY_API_KEY={API_KEY}
claude mcp add --transport http staqly https://staq.example.com/mcp \
  --header "Authorization: Bearer $STAQLY_API_KEY"
codex mcp add staqly --url https://staq.example.com/mcp --bearer-token-env-var STAQLY_API_KEY
```

```json
{
  "mcpServers": {
    "staqly": {
      "type": "http",
      "url": "https://staq.example.com/mcp",
      "headers": { "Authorization": "Bearer ${STAQLY_API_KEY}" }
    }
  }
}
```

```toml
[mcp_servers.staqly]
url = "https://staq.example.com/mcp"
bearer_token_env_var = "STAQLY_API_KEY"
```

STDIO against a cloud-mode container on the same host:
`docker exec -i -e STAQLY_API_KEY staqly-api mcp-stdio` with the key in the
client's environment (`claude mcp add staqly -e STAQLY_API_KEY={API_KEY} -- …`).

## What was verified

On this machine (Claude Code CLI, Codex CLI 0.162.1, n8n 2.42.7):

* `claude mcp list` reported ✔ Connected for the HTTP and STDIO configurations,
  both keyless (local mode) and with a bearer key; `claude -p "create a
  collection called 'Claude Code Test' and show me all pending images"`
  created the collection and listed pending images via MCP.
* `codex exec` called `staqly/create_item` (with a bearer key) and
  `staqly/list_pending_approvals` (keyless) successfully. The default Codex
  model was unavailable for the account's region, so `-m gpt-5-mini` was used —
  unrelated to staq.ly.
* Automated tests cover scope enforcement over HTTP MCP and a full client
  session (all 14 tools listed; create → pending → approve → search
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
