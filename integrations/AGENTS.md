# integrations — n8n, Claude Code, Codex, OpenCode

* `n8n/instagram-avatar-demo.json` — importable demo workflow "publish through
  n8n": the Config node (`apiToken`) and **Verify & parse** (`CONFIG.apiToken`)
  hold the access token (local) or an API key; a mock generator writes a post with a PNG
  and publishes it right away on a channel of provider `custom` ("your own
  automation"); staqly sends `post.publish_requested` to the workflow's
  `staqly-publish` trigger, **Verify & parse** checks the signature (fails
  closed until the secret is pasted), **Mock Publisher** pretends to post and
  **Report result** closes the target with
  `POST /v1/posts/{id}/targets/{channel}/result`; `post.published` /
  `post.failed` land in **Record outcome**. It is plain n8n JSON: edit it in
  n8n and re-export, or edit the JSON directly; keep node types/versions that
  work on n8n 1.x and 2.x (httpRequest 4.2, webhook 2, code 2, switch 3.2,
  set 3.4). After changes, run the real-n8n suite
  (`pnpm --filter @staqly/e2e test:n8n`, dev stack running; see
  e2e/README.md): generate with and without the token, publish → report →
  published with the link, outcome recorded, forged webhook rejected. Work
  happens on `post.publish_requested` (the target is already publishing);
  outcomes go through `/result`, never by editing the post.
* `n8n/compose.yml` — **example only** of running n8n next to staqly (joins
  the `staqly_default` network). n8n is not part of the main stack. The image
  tag is pinned to the version the suite ran against (`N8N_VERSION`,
  `N8N_PORT` override).
* `n8n/setup-demo.sh` — creates the collection, connects the channel on your
  own automation and registers the webhook (`post.publish_requested`,
  `post.published`, `post.failed`); prints the signing secret and a demo
  token. Needs `STAQLY_API_KEY` (the local access token, or a cloud API key;
  `STAQLY_API_URL` for cloud), `STAQLY_DEMO_COLLECTION` / `STAQLY_DEMO_CHANNEL` for other names.
* `claude-code/`, `codex/`, `opencode/` — MCP config examples. Only document CLI commands
  that were verified against the installed `claude` / `codex` CLIs.
