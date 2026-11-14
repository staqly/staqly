# Claude Code

Staqly Community Edition: Streamable HTTP with the access token (printed when staqly starts;
`staqly serve token`, or `docker exec staqly server token` with Docker):

```bash
export STAQLY_API_KEY=$(staqly serve token)
claude mcp add --transport http staqly http://localhost:3333/mcp --header "Authorization: Bearer $STAQLY_API_KEY"
# or STDIO inside the staqly container (Docker), no token needed
claude mcp add staqly -- docker exec -i staqly mcp-stdio
```

Or copy [`mcp.json`](mcp.json) into your project as `.mcp.json` (it reads the
token from `STAQLY_API_KEY`). Cloud instance: see [MCP setup](https://docs.staqly.ai/mcp).
