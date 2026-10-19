# Codex

Staqly Community Edition: Streamable HTTP with the access token (printed when staqly starts;
`staqly serve token`, or `docker exec staqly server token` with Docker):

```bash
export STAQLY_API_KEY=$(staqly serve token)
codex mcp add staqly --url http://localhost:3333/mcp --bearer-token-env-var STAQLY_API_KEY
# or STDIO inside the staqly container (Docker), no token needed
codex mcp add staqly -- docker exec -i staqly mcp-stdio
```

Or merge [`config.toml`](config.toml) into `~/.codex/config.toml`.
Cloud instance: see [MCP setup](https://docs.staqly.ai/mcp).
