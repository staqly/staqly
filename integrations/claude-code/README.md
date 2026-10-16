# Claude Code

Local mode (no key):

```bash
claude mcp add --transport http staqly http://localhost:3333/mcp
# or STDIO inside the staqly container (Docker)
claude mcp add staqly -- docker exec -i staqly mcp-stdio
```

Or copy [`mcp.json`](mcp.json) into your project as `.mcp.json`.
Cloud instance: see [docs/mcp.md](../../docs/mcp.md#cloud-mode).
