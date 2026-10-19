# Codex

Local mode (no key):

```bash
codex mcp add staqly --url http://localhost:3333/mcp
# or STDIO inside the staqly container (Docker)
codex mcp add staqly -- docker exec -i staqly mcp-stdio
```

Or merge [`config.toml`](config.toml) into `~/.codex/config.toml`.
Cloud instance: see [MCP setup](https://docs.staqly.ai/mcp).
