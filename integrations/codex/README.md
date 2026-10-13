# Codex

Local mode (no key):

```bash
codex mcp add staqly --url http://localhost:3333/mcp
# or STDIO inside the API container
codex mcp add staqly -- docker exec -i staqly-api mcp-stdio
```

Or merge [`config.toml`](config.toml) into `~/.codex/config.toml`.
Cloud instance: see [docs/mcp.md](../../docs/mcp.md#cloud-mode).
