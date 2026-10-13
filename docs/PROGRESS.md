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
| Observability: `/livez`, `/readyz`, `/metrics`, request IDs, rate limits, optional Sentry | ✅ |
| CLI `staqly`, TypeScript SDK, Python SDK, docs site (`apps/docs`) | ✅ |
| n8n: example compose + Instagram Avatar demo workflow (verified on a real n8n) | ✅ |
| CI: format, lint, types, tests, builds, generated-code freshness, Docker, e2e | ✅ |

## Verification (executed)

* Go: API + CLI tests with `-race`, including the OpenAPI contract test (every
  route documented, every JSON response in the suite validated).
* Dashboard 106 tests, TS SDK 160, Python SDK 297; lint (oxlint), formatting
  (oxfmt / ruff / gofmt), typecheck (TypeScript 7, mypy strict), builds.
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
* Slack: no "reject with reason" modal (opens the decide page) and no Socket
  Mode; Telegram polling assumes one server per bot.
* Single server process (outbox and watcher workers have no cross-process
  leases); fine for SQLite's single-writer model.
* Payments aren't integrated: plans are changed by the operator
  (`make set-plan`).
* No per-item `callback_url` (use webhooks or `/wait`).
* The TypeScript code generator (openapi-typescript) still needs the TS 5.9
  compiler API, so `gen` scripts run it via `npx` with TS 5.9; everything else
  uses TypeScript 7.
* oxfmt is pre-1.0.

## Next ideas

1. Payments (Stripe) for cloud plans.
2. Web push (PWA) notifications.
3. Slack Socket Mode / reason modal; Telegram `/start <code>` pairing.
4. LangChain / CrewAI / Claude Agent SDK helpers on top of the SDKs.
5. Point-in-time workspace export snapshots; Litestream replication.

Research notes from the initial build: [docs/research](research/).
