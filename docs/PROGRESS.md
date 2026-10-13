# Progress and status

Status: **MVP complete, verified end to end** (2026-10-13).

## What's in

| Area | Status |
|---|---|
| Core: collections, items (image/video/text/link/json/file), media, versions, immutable events | ✅ |
| Review: inbox, approve/reject with feedback, edit before approve (`editable` whitelist + diff), bulk, expiry (`expires_in`, `expired`), custom actions | ✅ |
| Agent loop: `GET /items/{id}/wait` long-poll, `/result`, MCP `await_decision` / `report_result` | ✅ |
| Rules engine (auto-approve/reject, set status, expire, escalate to a channel) | ✅ |
| Watchers: RSS/Atom + page changes, 18 presets, baseline, keywords, robots.txt for pages, degrade/pause | ✅ |
| Webhooks: transactional outbox, HMAC, retries, delivery history, SSRF guard | ✅ |
| Notifications: Telegram (long-polling or webhook), Slack, Discord, ntfy, email; chat buttons, one-tap decision links, digests, auto-disable | ✅ |
| MCP: 21 tools, STDIO + Streamable HTTP; verified with Claude Code, Codex, OpenCode | ✅ |
| REST API: OpenAPI 3.2.1 contract enforced by a contract test; idempotency keys; full-text search (FTS5) + keyset pagination | ✅ |
| Local mode (keyless, this machine only) and cloud mode (multi-workspace, scoped keys, sessions + CSRF, recovery keys, plans Free/Indie/Team with 402 limits) | ✅ |
| Dashboard: all screens, phones/tablets, Connect-a-device QR, rules, watchers, notifications, data & privacy, system | ✅ |
| Data: events export (NDJSON/CSV), workspace ZIP export, retention, workspace delete, item purge, backup/restore | ✅ |
| Observability: `/livez`, `/readyz`, `/metrics`, request IDs, rate limits | ✅ |
| CLI `staqly`, TypeScript SDK, Python SDK, docs site (`apps/docs`) | ✅ |
| n8n: example compose + Instagram Avatar demo workflow (verified on a real n8n) | ✅ |
| CI: format, lint, types, tests, builds, generated-code freshness, Docker, e2e | ✅ |
| Release: `make deploy` tags `origin/main` as `prod-<timestamp>`; CI publishes multi-arch images to `ghcr.io/staqly/staqly-{api,web}`; `make up` pulls them | ✅ |
| Binary releases: `make release VERSION=v…` → goreleaser (macOS, Linux, Windows; amd64, arm64); `scripts/install.sh` with checksum check | ✅ |
| Docs site on GitHub Pages (`.github/workflows/docs.yml`, https://staqly.github.io/staqly); Caddy docs image removed | ✅ |
| `staqly serve`: API + MCP + dashboard in one process from the CLI binary, no Docker (`apps/api/server`, `DASHBOARD_ADDR`) | ✅ |
| Dashboard as a static export served by a small Go server (`staqly-web` image without Node.js; `make web` embeds it into `apps/api/web`) | ✅ |
| Observability: structured JSON logs on stdout with `request_id`, `trace_id` (`traceparent`), `cf_ray`, `workspace_id`, `principal`; `LOG_LEVEL` / `LOG_FORMAT` | ✅ |
| Frontend performance: React Compiler on every hot component, memoised rows, id-based structural sharing, event-based polling, `content-visibility`, thumbnails in lists | ✅ |
| Frontend/SDK/infra audit fixes: streaming `/api` proxy (bounded memory), CSP + security headers, safe redirects/links/colours, click-to-load external media, error boundaries, SDK key redaction, hardened Compose, CLI output sanitising, fail-closed n8n demo, owner-only backups | ✅ |
| Performance: image thumbnails (`GET /v1/media/{id}?w=`, cached on disk), in-process write serialisation (no `SQLITE_BUSY` under parallel writers; residual busy = 503 + `Retry-After`), lazy-loaded command palette / dialogs, locally derived item actions, one-page revisits of long lists | ✅ |
| Backend security audit fixes: loopback-only local mode by default (`LOCAL_TRUSTED_PEERS`, `LOCAL_ALLOWED_ORIGINS`), mode-dependent safe defaults, hardened SSRF policy, version-bound decisions, post-approval write rules, bounded search, missing indexes, idempotency/rate-limit hardening | ✅ |

## Verification (executed)

* Go: API + CLI tests with `-race`, including the OpenAPI contract test (every
  route documented, every JSON response in the suite validated).
* Dashboard 203 tests, TS SDK 164, Python SDK 313; lint (oxlint + `curly` /
  padding rules), formatting (oxfmt / ruff / gofmt), typecheck (TypeScript 7,
  mypy strict), builds. Docs site: 0 broken links.
* Security audits (backend; frontend, SDKs and infra): one critical, 4 high and
  the medium/low findings fixed, each with a regression test.
* Dashboard profiling with ~3,000 items: `j` 36,017 → 557 renders, approve
  75,928 → 1,292, idle poll 42 requests → 1, inbox first load 51 MB → 249 KB.
* `scripts/e2e.sh`: 13 steps on Docker Compose — local keyless access and its
  guards, browser sign-in link, REST item + dashboard proxy (12 MB upload),
  approve, webhook retry + signature, history, MCP HTTP + STDIO,
  `await_decision`, search, phone via QR (single-use), persistence across
  restart/recreate, switch to cloud mode, setup-token gate, Free-plan limit.
* Real clients: Claude Code (HTTP + STDIO ✔ Connected), OpenCode (✔ connected,
  tool call), Codex (tool calls; default model was region-blocked, `gpt-5-mini`
  used), n8n 2.42 (generate → approve → published/completed).
* Visual checks of the dashboard and docs at phone/tablet/desktop sizes, light
  and dark, no horizontal overflow.

## Known limitations

* Notification channels were tested against fake servers (and a real ntfy.sh
  test); real Telegram/Slack/Discord/SMTP delivery wasn't exercised.
* Slack: no "reject with reason" modal (interactive messages open the item in
  the dashboard) and no Socket Mode; Telegram polling assumes one server per bot.
* Single server process (outbox and watcher workers have no cross-process
  leases); fine for SQLite's single-writer model.
* Payments aren't integrated: plans are changed by the operator
  (`make set-plan`).
* No per-item `callback_url` (use webhooks or `/wait`).
* The TypeScript code generator (openapi-typescript) still needs the TS 5.9
  compiler API, so `gen` scripts run it via `npx` with TS 5.9; everything else
  uses TypeScript 7.
* oxfmt is pre-1.0.
* Thumbnails: AVIF and images over 50 megapixels are served unscaled (no
  pure-Go AVIF decoder; decompression-bomb guard). `uploads/thumbs` is a
  disposable cache, left out of backups.
* Cloud hosting is not set up yet; the plan is a VM with a volume (e.g.
  DigitalOcean) behind Cloudflare, with Litestream replicating SQLite to R2.

## Next ideas

1. Payments (Stripe) for cloud plans.
2. Web push (PWA) notifications.
3. Slack Socket Mode / reason modal; Telegram `/start <code>` pairing.
4. LangChain / CrewAI / Claude Agent SDK helpers on top of the SDKs.
5. Point-in-time workspace export snapshots; Litestream replication.
6. `deploy/` for the hosted version (Compose + Caddy/cloudflared + Litestream to R2).
