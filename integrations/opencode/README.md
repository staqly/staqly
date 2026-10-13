# OpenCode

OpenCode reads MCP servers from `opencode.json` (project) — copy one of these
files into your project root (or merge the `mcp` block into your existing
config):

| File | Use |
|---|---|
| [`opencode.json`](opencode.json) | local mode, Streamable HTTP `http://localhost:3333/mcp` (no key) |
| [`opencode.stdio.json`](opencode.stdio.json) | local mode, STDIO via `docker exec -i staqly-api mcp-stdio` |
| [`opencode.cloud.json`](opencode.cloud.json) | cloud instance; key read from `STAQLY_API_KEY` via `{env:STAQLY_API_KEY}` |

Check the connection:

```bash
opencode mcp list        # → ✓ staqly  connected
```

Verified with OpenCode 2.0.25 (HTTP and STDIO; a real `list_collections`
call via `opencode run`).
