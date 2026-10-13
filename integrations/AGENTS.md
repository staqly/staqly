# integrations — n8n, Claude Code, Codex, examples

* `n8n/instagram-avatar-demo.json` — importable demo workflow (local mode, no
  credentials). It is plain n8n JSON: edit it in n8n and re-export, or edit
  the JSON directly; keep node types/versions that work on n8n 1.x and 2.x
  (httpRequest 4.2, webhook 2, code 2, switch 3.2, set 3.4, if 2).
  After changes, test it against a real n8n: approve → completed with
  `instagram_post_id`, reject → `metadata.feedback_log`, regenerate → new
  item + original archived, forged webhook rejected when a secret is set.
* `n8n/compose.yml` — **example only** of running n8n next to staq.ly (joins
  the `staqly_default` network). n8n is not part of the main stack.
* `n8n/setup-demo.sh` — creates the collection and the webhook; works keyless
  locally, `STAQLY_API_URL` + `STAQLY_API_KEY` for cloud.
* `claude-code/`, `codex/`, `opencode/` — MCP config examples. Only document CLI commands
  that were verified against the installed `claude` / `codex` CLIs.
* `examples/assets/` — small PNGs used by seed and e2e scripts.
