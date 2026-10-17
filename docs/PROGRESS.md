# Progress and status

Status: **MVP complete, verified end to end** (2026-10-13; dashboard and docs updates through 2026-10-16).

## What's in

| Area | Status |
|---|---|
| Core: collections, items (image/video/text/link/json/file), media, versions, immutable events | ✅ |
| Items: decision and run as separate fields and badges (filters for both), tags (`?tag=`, `/v1/tags`), content locked after a decision, actions with their own run names, live updates without a reload, MCP `list_requested_work` and `list_tags` | ✅ |
| Agents: every MCP client (by clientInfo) and API client (by User-Agent) seen, per credential, with last seen, items added and requests (`GET /v1/agents`, `DELETE /v1/agents/{id}`; shown in Integrations); agents not seen for `RETENTION_AGENTS_DAYS` (default 30) are removed | ✅ |
| Cloud in production: `make cloud` = `compose.yml` + `compose.cloud.yml`, the pre-built image behind Caddy (automatic HTTPS for `STAQLY_DOMAIN`, only 80/443 published) | ✅ |
| `staqly service install` / `status` / `logs` / `uninstall`: `staqly serve` in the background at login (macOS LaunchAgent) or boot (Linux systemd --user + linger), restarted if it stops; no Docker needed locally | ✅ |
| Auto-tags and summaries: every new item gets a one-line `summary` (max 120 chars, under the title in the inbox) and untagged ones 1-3 tags, from a durable queue (`summary_next_at`, `TAGGER_CONCURRENCY` at a time, 5 tries with backoff). Local: the user's own AI CLI (`claude -p --model haiku` with no tools, else `codex exec` in the read-only sandbox, or `TAGGER_COMMAND`); cloud/Docker: an OpenAI-compatible `TAGGER_URL`. Regenerate with context: `POST /v1/items/{id}/summarize`, `item.summarized` | ✅ |
| Inbox steps: Review · Approved, Rejected · Processing · Completed, Failed · Expired, Archived as tabs with counts; one Filter button (collection, source, tags, type); replaces the separate Flow page. Rows show the summary as the only subtitle and a Status column | ✅ |
| Default collection per workspace (listed first, can be renamed, cannot be archived or deleted); processing items fail after the workspace's processing timeout (Settings, 1 minute to 24 hours; default `PROCESSING_TIMEOUT`, 1h), and the item shows how long it has waited of that limit | ✅ |
| Review: inbox, approve/reject with feedback, edit before approve (`editable` whitelist + diff), bulk, expiry (`expires_in`, `expired`), custom actions | ✅ |
| Plans and reviews (2026-10-16): data export (workspace ZIP, activity log export) is Pro only in cloud (`data_export` plan limit, 402 on Free; the Backups tab and export cards hidden there); reject needs feedback on every path (REST, MCP, CLI; chat buttons and email links without a reason record "Rejected by ... without a reason."); bulk actions over 100 items go in batches; SDKs: `items.summarize`, workspace exports list, agents list/forget | ✅ |
| Review safety (2026-10-16): approve waits 10 s with Undo in its toast; bulk approve and bulk reject ask first; collection actions (Publish, Regenerate...) are confirmed in a dialog; Shift+P runs a row's main action outside Review; runs end as Completed / Failed / Canceled (Canceled = stopped by hand in the dashboard, stored as failed); unarchiving an item archived while pending puts it back in Review; reviewer feedback in the dashboard is capped at 750 characters | ✅ |
| Agent loop: `GET /items/{id}/wait` long-poll, `/result`, MCP `await_decision` / `report_result` | ✅ |
| Rules removed (2026-10-14): no approval rules (auto-approve, auto-reject, set status, expire, escalate), no `/v1/rules`, MCP rule tools, `rules:manage` scope, `rules` plan limit, `rules.json` in the export, `staqly rules`, dashboard Rules page or `item.escalated`; every decision is made by a person. Item expiry via `expires_in` stays (migration `00012_drop_rules`) | ✅ |
| Watchers: RSS/Atom + page changes, baseline, keywords, robots.txt, degrade/pause | ✅ |
| Watcher presets removed (2026-10-15): the catalog of third-party sources (Hacker News, Reddit, Pinterest, YouTube, GitHub, npm, PyPI, Stack Overflow, arXiv, Google News, Product Hunt...) is gone, since those mostly undocumented feeds can't be kept working (Reddit's robots.txt disallows everything). No `GET /v1/watcher-presets`, `POST /v1/watchers/from-preset`, `preset_id` on watchers, or MCP `list_watcher_presets` / `create_watcher_from_preset` (MCP now 71 tools). A watcher is a generic HTTP poll of a URL the user gives (`POST /v1/watchers`): Feed (`rss`) or Page change (`url_diff`, JSON endpoints too), detected from the URL when `kind` is left out (the dashboard no longer asks); robots.txt now applies to feeds as well as pages; the dashboard's Add a watcher sheet is one form. Push sources post to `POST /v1/items` (or MCP `create_item`); no WebSocket watcher (migration `00013_drop_watcher_presets` drops `watchers.preset_id`) | ✅ |
| Watchers add to several collections (2026-10-15): `collections` (IDs or names, 1-20) on create/update in REST, MCP, CLI (`--collection` repeatable) and both SDKs, `collection_ids` on watchers (`collection_id` is the first); each new entry becomes an item in every one. Migration `00015_watcher_collections`; deleting a watcher's first collection moves it to the next one instead of deleting the watcher. Known ceiling: the seen mark commits with the first collection's item, so a failure creating the others (quota) skips the entry there. Dashboard: collections multi-select, keyword filters under Advanced with an explanation | ✅ |
| Webhooks: transactional outbox, HMAC, retries, delivery history, SSRF guard | ✅ |
| Notifications: Telegram (long-polling or webhook), Slack, Discord, ntfy, email; chat buttons, one-tap decision links, digests, auto-disable | ✅ |
| MCP: 71 tools (every REST operation except browser-only flows and recovery key rotation, including billing: `get_billing`, `start_billing_checkout`, `open_billing_portal`), STDIO + Streamable HTTP; verified with Claude Code, Codex, OpenCode; `staqly mcp config` + docs for Cursor and Windsurf (not yet verified in the apps). `await_decision` waits 45 s by default (max 300), under the usual 60 s client tool timeout; call it again to keep waiting | ✅ |
| Agents live in Integrations (2026-10-15): the separate Agents page is gone (`/agents` redirects); Integrations lists the agents, and its Recent activity shows every agent, or only the selected one. Events record the agent that caused them (`actor.agent`, migration `00014_event_agent`), and `GET /v1/events?agent=<name>` (repeatable) filters by it. Settings, Collections and Activity headers got icons like the other pages | ✅ |
| REST API: OpenAPI 3.2.1 contract enforced by a contract test; idempotency keys; full-text search (FTS5) + keyset pagination | ✅ |
| Local mode (keyless, this machine only) and cloud mode (multi-workspace, scoped keys, sessions + CSRF, recovery keys, plans Free/Pro with 402 limits) | ✅ |
| Dashboard: all screens, phones/tablets, Connect-a-device QR, watchers, notifications, data & privacy, system | ✅ |
| Dashboard (2026-10-14): collections are always a table (gallery/list views removed; `default_view` stays in the API for compatibility only); collection colours; webhook signing secret shown once after create/rotate; "Send a test approval" in an empty inbox; Reject next to Approve; required form fields marked with `*` and invalid fields in red; one validation message per row in the actions editor | ✅ |
| Activity log: every event shows its actor (dashboard session, API key with id, watcher, expiry, Stripe, operator), its entity (item, collection, webhook, watcher, channel, key, session... with a link and copyable id), exact time and a Data toggle with the raw JSON; filters by type prefix, actor ID, entity (item) ID and a From/To range (`GET /v1/events?actor_id=&type_prefix=&from=&to=`); Export uses every current filter | ✅ |
| Audit trail of admin changes: `webhook.*`, `watcher.*` (incl. degraded / paused / recovered), `notification_channel.*`, `session.revoked`, `api_key.rotated`, `workspace.updated`, `workspace.plan_changed` (operator, Stripe, self-service switch to Free); `webhook.*` events are audit-only, never delivered to webhooks | ✅ |
| No repeated states: setting a state that is already set (archive an archived collection or item, pause a paused watcher, enable an enabled webhook / channel, revoke a revoked key or session, report a result twice, run an action whose `set_status` is the current status, choose the current plan) answers 409 "... is already ..." with no version, event or webhook; bulk actions report it per item; the dashboard only offers the opposite transition and locks toggles while a change is in flight | ✅ |
| Data: events export (NDJSON/CSV), workspace ZIP export, retention, workspace delete, item purge, backup/restore | ✅ |
| Settings → Backups (2026-10-16): locally the backup commands for `staqly serve` and Docker; in cloud on paid plans the workspace's ZIP exports with Download (`GET /v1/workspace/exports`), hidden on Free. Data & privacy on a local install: a Delete all data card with the commands for the binary and the container (one workspace, so no delete button). `GET /v1/setup` reports `billing_enabled`; new cloud workspaces are told about the 7-day Pro trial | ✅ |
| Item details (2026-10-16): video as a play tile that opens a dialog, images zoom on click, JSON content and metadata shown as JSON with a full-screen view and Copy; the edit form also covers media items (title, description, metadata, collection, source; the file itself stays). Length limits are counted in characters (Unicode code points) by the API, matching the dashboard's `maxLength` | ✅ |
| Observability: `/livez`, `/readyz`, `/metrics` (HTTP, decisions + time to decide, webhooks, watchers, notifications), request IDs, rate limits, optional Sentry error reporting (`SENTRY_DSN`) | ✅ |
| CLI `staqly`, TypeScript SDK, Python SDK, docs site (`apps/docs`) | ✅ |
| n8n: example compose + Instagram Avatar demo workflow (verified on a real n8n) | ✅ |
| Monorepo: Turborepo + pnpm workspaces (Go and Python packages wrapped by `package.json`); `apps/api`, `apps/cli`, `apps/dashboard` (dashboard), `apps/web` (landing page), `apps/docs`, `sdks/typescript`, `sdks/python`, `e2e`; `pnpm check` (`make check`) = format, lint, types, tests of every package; CI runs the same turbo tasks per package, the e2e suite (`e2e/`, Vitest + TS SDK, `compose.e2e.yml`) against the built images, and checks generated code | ✅ |
| Release: `make release VERSION=v0.2.0` tags `origin/main`; CI publishes the binaries (goreleaser: macOS, Linux, Windows; amd64, arm64; installed by `scripts/install.sh` with a checksum check) and the multi-arch image `ghcr.io/staqly/staqly:<version>` + `:latest` from the same tag (only when CI on that commit passed; no `prod-*` tags) | ✅ |
| Running staqly: the binary (`staqly serve`, `staqly service install`) or the single image with plain `docker run` (one container `staqly`, API on `127.0.0.1:3333`, dashboard `:8080`, volume `staqly-data`, hardened flags); docs, README and the dashboard's sign-in, backup and delete hints use only image commands (`docker exec staqly server connect-link`, `server backup` + `docker cp`, `docker logs`). Compose (`make dev`) is for developing staqly; cloud servers use `make cloud` | ✅ |
| Dev stack: `make dev` = `compose.yml` + `compose.dev.yml` (images from the checkout, debug text logs, private targets allowed, mailpit on :8025, opt-in Stripe CLI with `COMPOSE_PROFILES=stripe` and `make stripe-secret`); `make dev-down` | ✅ |
| API playground in the docs: requests go to the reader's cloud instance with their API key (cloud-only CORS, `CORS_ALLOWED_ORIGINS`) | ✅ |
| Plans: Free (50 approvals/month; unlimited items and collections) and Pro ($30, unlimited, 100 GB, 100 MB files); 7-day Pro trial for new cloud workspaces; unpaid → suspended (read-only + export) across REST, MCP, decision links and watchers; self-service switch to Free (migration `00002_pro_plan_and_trial`) | ✅ |
| Billing with Stripe (cloud): Checkout for Pro, customer portal, signed webhook `POST /v1/billing/webhook` applied once per event, plan follows the subscription (paid period + 3 days grace), subscription canceled on workspace delete; `STRIPE_SECRET_KEY` / `STRIPE_WEBHOOK_SECRET` / `STRIPE_PRICE_PRO`; CLI `staqly billing`, MCP billing tools (migration `00003_billing`) | ✅ |
| Operator: `server set-plan <ws> <plan> --until DATE` comps a plan until a date; `server stats` prints workspaces by plan, trials, suspensions, signups and this month's usage as JSON | ✅ |
| Plan & usage also shows items created this period by status (each opens its Inbox tab) and the webhook delivery backlog (queued, retrying, failed); `GET /v1/workspace/usage` returns `items_by_status` and `webhook_deliveries` | ✅ |
| Docs site on GitHub Pages (`.github/workflows/docs.yml`, https://docs.staqly.ai); Caddy docs image removed | ✅ |
| `make site`: the landing page (:8070) and docs (:8090) from the checkout, linked to each other and the dev dashboard | ✅ |
| `staqly serve`: API + MCP + dashboard in one process from the CLI binary, no Docker (`apps/api/server`, `DASHBOARD_ADDR`) | ✅ |
| Dashboard as a static export served by a small Go server (embedded in the server, no Node.js at runtime; `make dashboard` builds it into `apps/api/web`). One image `ghcr.io/staqly/staqly` runs API, MCP and dashboard in one process | ✅ |
| Observability: structured JSON logs on stdout with `request_id`, `trace_id` (`traceparent`), `cf_ray`, `workspace_id`, `principal`; `LOG_LEVEL` / `LOG_FORMAT` | ✅ |
| Frontend performance: React Compiler on every hot component, memoised rows, id-based structural sharing, event-based polling, `content-visibility`, thumbnails in lists | ✅ |
| Frontend/SDK/infra audit fixes: streaming `/api` proxy (bounded memory), CSP + security headers, safe redirects/links/colours, click-to-load external media, error boundaries, SDK key redaction, hardened Compose, CLI output sanitising, fail-closed n8n demo, owner-only backups | ✅ |
| Performance: image thumbnails (`GET /v1/media/{id}?w=`, cached on disk), in-process write serialisation (no `SQLITE_BUSY` under parallel writers; residual busy = 503 + `Retry-After`), lazy-loaded command palette / dialogs, locally derived item actions, one-page revisits of long lists | ✅ |
| Backend security audit fixes: loopback-only local mode by default (`LOCAL_TRUSTED_PEERS`, `LOCAL_ALLOWED_ORIGINS`), mode-dependent safe defaults, hardened SSRF policy, version-bound decisions, post-approval write rules, bounded search, missing indexes, idempotency/rate-limit hardening | ✅ |
| Second backend audit: the dashboard relay names each browser in `X-Forwarded-For` (per-browser sign-up/recovery/connect limits behind Caddy), model replies bounded and model address hidden from cloud tenants, `summarize` rate-limited, unarchive back to review survives edits made while archived, PATCH with tags + a stale `expected_version` applies nothing, summary queue writes scoped by workspace, `staqly service` units owner-only | ✅ |
| CLI covers the REST surface: notification channels, media upload/download, item delete/purge/bulk/versions, events export, workspace get/rename/usage/export, plans + switch to Free, sessions, connect links, key rotation, watchers get/pause/resume, webhooks get + update (not in the CLI: editing a watcher, `PATCH /v1/watchers/{id}`; by design: workspace delete and recovery-key rotation, which the API only allows from a dashboard session) | ✅ |

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
* `e2e/` (Vitest + TS SDK on `compose.e2e.yml`, 43 tests in 8 files): local
  keyless access and its guards, browser sign-in link, REST item + dashboard proxy (12 MB upload),
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
* Billing with Stripe is tested against a fake Stripe API in the Go tests;
  live payments haven't been exercised. Without Stripe settings, plans are
  changed by the operator (`server set-plan`, e.g. `docker exec staqly server set-plan ws_... pro`).
* No per-item `callback_url` (use webhooks or `/wait`).
* The TypeScript code generator (openapi-typescript) still needs the TS 5.9
  compiler API, so `gen` scripts run it via `npx` with TS 5.9; everything else
  uses TypeScript 7.
* oxfmt is pre-1.0.
* Thumbnails: AVIF and images over 50 megapixels are served unscaled (no
  pure-Go AVIF decoder; decompression-bomb guard). `uploads/thumbs` is a
  disposable cache, left out of backups.
* The hosted service isn't running yet. `make cloud` deploys one server (the
  image behind Caddy); Litestream replication of SQLite to R2 is still a plan.

## Next ideas

1. Web push (PWA) notifications.
2. Slack Socket Mode / reason modal; Telegram `/start <code>` pairing.
3. LangChain / CrewAI / Claude Agent SDK helpers on top of the SDKs.
4. Point-in-time workspace export snapshots.
5. Litestream replication to R2 for the hosted version.
