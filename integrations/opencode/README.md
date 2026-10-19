# OpenCode

OpenCode reads MCP servers from `opencode.json` (project) — copy one of these
files into your project root (or merge the `mcp` block into your existing
config). The files use the OpenCode 2 format (`mcp.servers`) with
`codemode: false`, so the staqly tools are called directly rather than through
OpenCode's code mode, and a 200 s tool timeout so a large media upload or a
workspace export does not time out. OpenCode 1
takes `"mcp": { "staqly": { ... } }` without those two keys:

| File | Use |
|---|---|
| [`opencode.json`](opencode.json) | local mode, Streamable HTTP `http://localhost:3333/mcp` (no key) |
| [`opencode.stdio.json`](opencode.stdio.json) | local mode, STDIO via `docker exec -i staqly mcp-stdio` |
| [`opencode.cloud.json`](opencode.cloud.json) | cloud instance; key read from `STAQLY_API_KEY` via `{env:STAQLY_API_KEY}` |

Check the connection:

```bash
opencode mcp list        # → ✓ staqly  connected
```

Verified with OpenCode 2.0.25: each file used as shipped (HTTP, cloud with
`{env:STAQLY_API_KEY}`, STDIO), a real `get_workspace` call via `opencode run`
that OpenCode made as a direct `staqly_get_workspace` tool call.
