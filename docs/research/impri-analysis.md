> **Snapshot (2026-10-13)** — research notes from the initial build, comparing staq.ly with [Impri](https://github.com/sekera-radim/impri). Not kept up to date; see [docs/PROGRESS.md](../PROGRESS.md) for the current status.

# Impri reference analysis

Source: `reference/impri` (MIT, (c) 2026 Radim Sekera). All paths below are relative to that repo.
Everything here comes from reading the source. Where something is inferred and not stated in code, it says so.

## 1. Architecture and stack

- **Server**: Node 22 + TypeScript, Fastify 5, `better-sqlite3` (WAL, `foreign_keys=ON`), zod validation, argon2 for key hashing, undici for outbound HTTP, optional ioredis (shared rate limit), Stripe, web-push, nodemailer, Sentry (`server/package.json`, `server/src/index.ts`).
- One process: the HTTP API plus `setInterval` ticks every 60 s for expiry, webhook delivery, channel digests and the watcher scheduler (`server/src/index.ts` `main()`, `server/src/scheduler.ts`). There is no separate worker.
- **MCP**: `@impri/mcp` stdio package (`mcp/src/index.ts`), also mounted in the server as a stateless streamable-HTTP `/mcp` (`server/src/routes/mcp.ts`). Both share `toolDefs.ts` and `dispatch.ts`.
- **UI**: Vue 3 + Vuetify 3 + Pinia + marked + DOMPurify, built with Vite and served by nginx (`ui/package.json`, `ui/Dockerfile`, `ui/nginx.conf`). It is a PWA with a service worker for push (`ui/public/sw.js`).
- **SDKs and CLI**: TS `@impri/sdk` (`sdk/typescript`), Python `impri` (`sdk/python`), and a CLI (`cli/src/commands/*`: approve, get, init, keys, list, presets, push, status, tail, watchers).
- **Integrations**: Claude Agent SDK, CrewAI, LangChain, OpenAI Agents (`integrations/*`).
- **Deploy**: multi-stage Alpine Dockerfile with Litestream S3 replication when `BUCKET_NAME` is set (`docker/server.Dockerfile`, `docker/entrypoint.sh`, `docker/litestream.yml`). Also Fly (`fly.toml`), Caddy plus a systemd backup timer (`deploy/`), and GitLab CI (`.gitlab-ci.yml`).
- **Extras outside our scope**: "watchers" (RSS, Reddit search and URL-diff polling that turn into triage actions; `server/src/scheduler.ts`, `server/src/watcherPresets.ts`) and multi-tenant billing tiers (`server/src/billing.ts`).

## 2. API surface (all under `/v1`, Bearer auth unless marked public)

| Area | Endpoints (file) |
|---|---|
| Actions | `POST /actions`, `GET /actions` (filters `status`, `since`, `kind`, `q`; keyset cursor), `GET /actions/:id`, `POST /actions/:id/decision`, `POST /actions/:id/result`, `POST /actions/bulk-decision` (`routes/actions.ts`) |
| Keys | `POST /keys`, `GET /keys`, `DELETE /keys/:id` (`routes/keys.ts`) |
| Project | `GET`/`PATCH /project`, `POST /project/rotate-webhook-secret`, `GET /project/export`, `DELETE /project/data` (GDPR) (`routes/project.ts`) |
| Rules | `POST`/`GET /rules`, `GET`/`PATCH`/`DELETE /rules/:id` (`routes/rules.ts`) |
| Notification channels | CRUD on `/notification-channels[/:id]`, plus `/:id/test` and `/:id/setup-webhook` (`routes/notification-channels.ts`) |
| Audit | `GET /audit`, `GET /audit/export` (CSV or NDJSON, 5/min) (`routes/audit.ts`) |
| Watchers | CRUD on `/watchers`, `POST /watchers/:id/run`, `GET /watcher-presets`, `POST /watchers/from-preset` |
| Misc | `/usage`, `/billing*`, `/push/*`, `/signup`, `/recover`, `/recovery-code`, `/feedback`, `/admin/*` |
| Chat callbacks (public, signature-verified) | `/integrations/slack/interactions[/:channelId]`, `/integrations/discord/interactions/:channelId` (Ed25519), `/integrations/telegram/webhook/:channelId` (secret_token) |
| Public | `/healthz`, `/readyz` (DB, schema and write canary), `/metrics` (opt-in), `/v1/openapi.json`, `/.well-known/mcp/server-card.json`, `/mcp` |

Conventions:
- IDs are a prefix plus 16 random bytes in base64url: `act_`, `dec_`, `key_`, `proj_`, `wdel_` (`db.ts genId`).
- Timestamps are unix seconds.
- Pagination is an opaque base64url `"<created_at>.<id>"` keyset cursor, returned as `{items, has_more, next_cursor}` (`db.ts encodeCursor`).
- Errors look like `{error, message, issues?}`, and every response carries an `X-Request-Id`.
- Per-key fixed-window rate limits by route: create 60/min, list 300/min, decide 60/min, bulk 10/min (`auth.ts checkRateLimit`, stored in the SQLite `rate_limits` table or in Redis).

## 3. Authentication model

- **API keys**: `im_` + 32 random bytes in base64url. The first 16 chars are stored as `key_prefix` for lookup, and the full key is stored as an argon2 hash. Each request costs one `argon2.verify` (`auth.ts verifyApiKey`).
- **Scopes**: `actions`, `watch`, `admin`. `admin` implies all of them (`schemas.ts`, `auth.ts hasScope`).
- **Bootstrap**: on first run the server creates "Default Project" with an admin key and prints the key to stdout (`index.ts main`, `auth.ts bootstrapAdminKey`). There is also a one-time recovery code `imr_…` (argon2-hashed).
- **Accepted header formats**: `Authorization: Bearer im_…`. A bare `im_…` is also accepted to work around Smithery (`auth.ts extractRawApiKey`).
- **Internal fast lane**: `/mcp` re-enters the REST routes through `app.inject`. Those injected requests carry `x-impri-internal-auth`, an HMAC token with a 5 s TTL signed by a per-process random secret, so argon2 isn't paid twice (`auth.ts mintInternalAuthToken`).
- **UI**: the human pastes an API key, which is kept in `localStorage` (`ui/src/stores/auth.ts`). There are no users, passwords or sessions; the human is just another key holder.
- **Chat approvers**: authorized through per-channel allowlists of platform user IDs (`allowed_approver_slack_user_ids` and the Telegram equivalent). Button payloads carry a truncated HMAC (6 bytes, base64url) over `"sl:"/"tg:"/"dc:" + verdict + actionId` (`notify.ts build*ApprovalSig`).

## 4. Approval workflow and state machine

Statuses: `pending → approved | rejected | expired`, then `approved → executed | execute_failed` (`schemas.ts ActionStatus`).

1. **Create** (`POST /actions`):
   - Checks scope, rate limit and tier quota (402).
   - Validates with zod. Payload is capped at 256 KB, `expires_in` must be 300 s to 30 d (default 72 h), and titles may not contain CR/LF.
   - Dedup: if an `idempotency_key` is given, an existing row is returned with 200. Without a key, a soft dedup on `(kind, preview_hash)` among pending actions also returns 200, with `duplicate_of` set.
   - Rules are evaluated, then one transaction inserts the action, a synthetic `decisions` row for auto-approve or auto-reject (`decided_by = rule:<id>`, `channel = auto`), and the audit rows.
   - Concurrent UNIQUE races on the idempotency key fall back to reading the existing row.
2. **Decide** (`POST /actions/:id/decision`):
   - `{decision: approve|reject, edited?, channel?}`. A non-pending action returns 409.
   - `edited` keys must appear in the action's `editable` whitelist (`preview.body` or `payload.<dot.path>`), otherwise 422.
   - The edit diff is a naive unified diff of `preview.body`. Payload edits are persisted.
   - One transaction writes the decision, flips the status and writes audit plus PII rows. `UNIQUE(decisions.action_id)` makes the first writer win; the loser gets 409.
3. **Result** (`POST /actions/:id/result`): `executed|execute_failed` plus `detail` and a receipt `payload` of at most 16 KB. Only allowed from `approved`.
4. **Expiry tick**: `UPDATE … WHERE status='pending'` inside a transaction, so a concurrent decision wins (`webhooks.ts runExpiryTick`).
5. **Bulk** decisions: up to 50 IDs, each in its own transaction, so a batch can partially succeed. The channel is tagged `bulk-web` or `bulk-api` by sniffing the User-Agent.
6. **Rules engine** (`rules.ts`, `approval_rules` table):
   - Conditions are ANDed: a `kind_pattern` glob, payload conditions (`{path, op ∈ eq|lt|lte|gt|gte|contains|in|not_in, value}`), and target host matches.
   - Outcomes: `auto_approve`, `auto_reject`, `set_expiry`, `escalate`, `require_n_approvers`. The first matching rule by priority wins.
- Agent waiting has two modes: poll `GET /actions/:id` (the MCP `await_decision` polls every 5 s), or receive the `callback_url` webhook.

## 5. Database schema (`server/src/db.ts`)

Tables:
- `projects`: tenant row; holds `webhook_secret`, `tier`, the Stripe fields and `recovery_hash`.
- `api_keys`: `key_hash`, `key_prefix`, `scopes` (JSON), `revoked_at`.
- `actions`:
  - Core: `kind`, `title`, `preview` (JSON `{format: markdown|plain|diff, body}`), `payload`, `target_url`, `callback_url`, `expires_at`, `idempotency_key`, `editable`, `status`, `preview_hash`.
  - Optional: `idempotent`, `undo`, `result_payload`, `color`.
  - Indexes: a partial UNIQUE index on `(project_id, idempotency_key)` and a partial index for pending expiry.
- `decisions`: `UNIQUE(action_id)`; also `verdict`, `decided_by`, `channel`, `final_preview`, `diff`, `comment`.
- `webhook_deliveries`: `status` (pending/retry/delivered/gone/dlq), `attempt`, `next_attempt_at`, `last_status_code`, `last_error`. Has a partial "due" index.
- `audit_log`: autoinc id, `event`, `actor`, `channel`, `data` JSON.
- `pii_log`: request IPs, kept separate so they can be erased under GDPR without rewriting the audit table.
- Other: `push_subscriptions`, `rate_limits`, `watchers`, `watcher_items`, `approval_rules`, `notification_channels` (holds the digest queue), `feedback`.

Migrations are not versioned. `CREATE TABLE IF NOT EXISTS` runs first, then `PRAGMA table_info`-guarded `ALTER`s (`db.ts migrate`).

## 6. Webhook handling

**Callback webhook** (agent's `callback_url`, `webhooks.ts`):
- Body: `{"event":"action.updated","action_id","status","decided_at?","verdict?","final_preview?","diff?"}`. It is always this one event name and is sent on decision, auto-decision and expiry.
- Signature: `hex(HMAC-SHA256(secret, "<unix_ts>.<nonce>.<raw_body>"))`, where the nonce is 8 random bytes in hex.
- Headers:
  - `Content-Type: application/json`
  - `X-Impri-Signature: sha256=<hex>`
  - `X-Impri-Timestamp: <unix secs>`
  - `X-Impri-Nonce: <hex>`
- The secret is per project (`projects.webhook_secret`, 32 random bytes in base64url, rotatable). It falls back to the env `WEBHOOK_SECRET`, whose default is `change-me-in-production` and only triggers a startup warning.
- Verifiers in `mcp/src/webhook.ts`, `sdk/typescript/src/webhook.ts` and `sdk/python/impri/client.py verify_webhook` reject a missing `sha256=` prefix or a timestamp skew over 300 s (default), then compare in constant time. **None of them stores nonces**, so a replay inside the window passes.
- Delivery:
  - `scheduleWebhookDelivery` inserts a `webhook_deliveries` row, and the 60 s tick POSTs every due row sequentially with a 10 s timeout.
  - 2xx marks the row `delivered`. 410 marks it `gone` and nulls the action's `callback_url`.
  - Any other status or error goes to `retry` with backoff `[60, 300, 1500, 7200, 43200]` s (1m, 5m, 25m, 2h, 12h). After 5 retries the row goes to `dlq`.
  - The SSRF guard (`net-guard.ts`: RFC1918, loopback, link-local/metadata, CGNAT, IPv6 ULA and link-local; DNS-pinned undici agent) sends a blocked URL straight to `dlq`. The guard can be disabled with `IMPRI_ALLOW_PRIVATE_TARGETS=1`.
- **Not a true outbox**: the delivery row is inserted after the decision transaction commits, not inside it (`routes/actions.ts`; `scheduleWebhookDelivery` is called after `commitDecision()`). A crash between the two loses the webhook.
- No event ID or delivery-ID header is sent, so receivers have nothing stable to dedupe on.
- The payload is rebuilt from the current row at send time, not snapshotted.
- The last delivery status is exposed on `GET /actions/:id` as `webhook_delivery`.

**Outbound "webhook" notification channel** (`notify.ts sendWebhookChannel`):
- Body: `{event:"action.pending", action_id, title, kind, inbox_url, triggered_at, escalate?}`.
- Signed with the same scheme using the per-channel `hmac_secret`, but only when one is configured; otherwise it is sent unsigned.
- Digesting is controlled by `digest_window_sec`.

## 7. MCP implementation

- **Stdio** (`mcp/src/index.ts`): built on `@modelcontextprotocol/sdk` `Server` with `StdioServerTransport`. It is a thin HTTP client to the REST API (`IMPRI_API_KEY`, `IMPRI_BASE_URL`, default `http://localhost:8484`). It still starts without a key so registries can list the tools.
- **HTTP** (`server/src/routes/mcp.ts`): a hand-rolled stateless JSON-RPC 2.0 handler for protocol `2025-06-18`.
  - `POST /mcp` only. `GET` and `DELETE` return 405 (no SSE, no sessions). Batch requests are rejected.
  - Supported methods: `initialize`, `ping`, `tools/list`, `tools/call`, and notifications (answered with 202).
  - Anonymous callers may use everything except `tools/call` (60/min per IP). Authenticated callers get 120/min per key.
  - `await_decision` is clamped to `MCP_AWAIT_DECISION_MAX_S` (default 20 s).
  - Tool calls go through `app.inject`, so they reuse the same scope, rate-limit, validation and audit logic as REST.
- **Tools** (8; `mcp/src/toolDefs.ts`, with outputSchemas):
  - `impri_push_action`
  - `impri_await_decision`
  - `impri_report_result`
  - `impri_inbox_status`
  - `impri_create_watcher`
  - `impri_list_watchers`
  - `impri_list_watcher_presets`
  - `impri_create_watcher_from_preset`
- The MCP tools never expose deciding, so an agent using MCP can't approve its own actions (though see section 11 for REST). `push_action` doesn't pass `callback_url`, so MCP agents poll.
- Untrusted payloads (`payload.untrusted === true`) are wrapped in `<untrusted-external-content>` markers before being returned to the model (`mcp/src/tools.ts wrapUntrusted`). The UI renders them as plain text (`ui/src/utils/untrusted.ts`).
- Server card for directories: `server/src/mcp/serverCard.ts`. `server.json` and `smithery.yaml` exist for registries.

## 8. SDKs

- **TS `@impri/sdk`** (`sdk/typescript/src/client.ts`):
  - Methods: `createAction` (auto-derives an idempotency key from kind, title and body), `getAction`, `listActions`, `decide`, `bulkDecide`, `reportResult`, `awaitDecision`, `approvalGate`, the watcher CRUD and presets, keys, project, `rotateWebhookSecret`, export and erase, `listAudit`, `exportAudit`.
  - Also exports `verifyWebhook` and typed errors (`errors.ts`).
- **Python `impri`** (`sdk/python/impri/client.py`): the same surface in snake_case, plus `iter_actions` and `iter_watchers` generators and `verify_webhook`.
- **Framework adapters**: a Claude Agent SDK tool, a CrewAI tool and callback, a LangChain tool, and an OpenAI Agents guardrail (`integrations/*`).

## 9. Audit history

- `audit_log` holds append-only rows written in the same transaction as state changes.
  - Action events: `action.created`, `action.rule_applied`, `action.approved`, `action.rejected`, `action.expired`, `action.executed`, `action.execute_failed`.
  - Key and other events: `key.created`, and so on.
- Query and export: filters by event type (exact or prefix), entity and time; keyset paging; CSV with formula-injection escaping (`csvField`) or NDJSON (`routes/audit.ts`).
- **Not tamper-evident**: there is no hash chain or signing. Retention pruning deletes rows when `AUDIT_RETENTION_DAYS` is set (`webhooks.ts pruneAuditLogs`), and `DELETE /project/data` erases data.
- IPs live in `pii_log`, which has its own retention setting.

## 10. Automated tests (approximate counts of `it(`/`test(`/`def test_`)

| Suite | Count |
|---|---|
| `server/test` (Vitest, 31 files covering hardening, concurrency/idempotency, SSRF, Slack/Discord/Telegram signatures, rules, MCP, audit, billing) | ~679 |
| `sdk/python/tests` | ~216 |
| `sdk/typescript/tests` | ~166 |
| `ui/tests` | ~105 |
| `mcp/test` (includes outputSchema validation with ajv) | ~47 |
| integrations | ~130 |
| `cli/tests` | ~25 |

CI (`.gitlab-ci.yml`) runs typecheck, lint, test and build for server, mcp and ui.

## 11. Security assumptions and gaps (verified)

Strengths:
- argon2-hashed keys, prefix lookup and timing-safe comparisons.
- The SSRF guard is DNS-pinned.
- Zod everywhere, with http(s)-only URLs and no CR/LF in titles.
- DOMPurify on markdown.
- Authorization and internal headers are redacted from logs.
- 5xx messages are sanitized before reaching clients.
- Tenant isolation is enforced by binding `project_id` from the key in every query.
- Status changes are guarded with transactions and UNIQUE constraints.

Gaps:
- **Agents can self-approve over REST.** The same `actions` scope covers create and decide (`routes/actions.ts`), and there is no separation between agent and approver keys.
- **`require_n_approvers` isn't enforced.** It is validated and returned by `evaluateRules`, but `routes/actions.ts` never acts on it, and `UNIQUE(decisions.action_id)` makes multi-approver impossible anyway.
- **The `/result` update has no `AND status='approved'` guard** in its SQL and isn't wrapped in a transaction with its audit insert, unlike `/decision`.
- **Webhooks aren't transactional** (see section 6). Receivers get no replay cache, no event ID, and unsigned channel webhooks when no secret is set. The fallback global secret defaults to a public string.
- **Chat-button HMACs are truncated to 48 bits.** They are mitigated by the platform request signatures and the user allowlists.
- **The UI stores a full admin-capable API key in `localStorage`**, so any XSS exposes it.
- **Sequential in-process delivery** means one slow endpoint (10 s timeout) can delay every delivery in that tick.
- **The audit log is mutable** and can be pruned.

## 12. Licensing

- The repo is MIT (`LICENSE`), and the package manifests repeat MIT (`mcp/package.json`, `sdk/typescript/package.json`, `sdk/python/pyproject.toml`, `integrations/*/pyproject.toml`, `integrations/claude-agent-sdk/package.json`). There are no other LICENSE, NOTICE or COPYING files.
- Third-party assets:
  - Marketing-site media in `www/assets` (screenshots, demo mp4/webm/gif, OG images, icons) appears self-produced; the capture and generation scripts are in `www/assets/demo/src` and `www/assets/og/src`. It carries no separate license, so treat it as covered by the author's MIT but don't copy branding.
  - The marketing site uses system font stacks with no bundled webfonts (`www/styles.css`).
  - The UI depends on `@mdi/font` (Material Design Icons, Apache-2.0 upstream; not vendored).
  - Litestream (Apache-2.0) is copied from its image in the Dockerfile.
  - `docs/` holds about 190 markdown pages, mostly SEO and use-case articles; these are the author's prose.

## 13. Ideas we adopt / things we do differently

Adopt:
- The action/decision split, with `UNIQUE(decision.action_id)`-style first-writer-wins plus a status-guarded `UPDATE … WHERE status='pending'`.
- An `editable` whitelist with a stored `final_preview` and diff, and a `result` receipt with an `executed|execute_failed` post-state.
- A partial UNIQUE index on `(scope, idempotency_key)`, with a fallback that returns the existing row on a race.
- An opaque `(created_at, id)` keyset cursor.
- The signing scheme `HMAC-SHA256("<ts>.<nonce>.<body>")` with `sha256=` hex, a 300 s tolerance and the same backoff ladder (1m, 5m, 25m, 2h, 12h then DLQ), plus 410 = gone.
- An SSRF guard with DNS pinning that is opt-out for local targets.
- One dispatch layer shared by MCP stdio and HTTP, outputSchemas on tools, and untrusted-content wrapping.
- A separate PII table and a `/readyz` write canary.

Do differently (Go + SQLite + Next.js, local-first):
- **Real transactional outbox**: insert the `outbox_events` row in the same transaction as the state change, with a stable `event_id`. Send `X-…-Event-Id` and `X-…-Delivery-Id` headers, snapshot the payload at enqueue time, and use a concurrent worker pool with per-endpoint isolation and jittered backoff.
- **Typed events**: `item.created`, `approval.requested`, `approval.decided`, `action.executed`, and so on, instead of a single `action.updated`. Register webhook endpoints as first-class resources with per-endpoint secrets and secret rotation that supports two concurrent secrets. Every outbound webhook is signed.
- **Separate agent and human principals**: agent tokens cannot decide. Humans get a local session (cookie, not localStorage) on loopback.
- **Versioned migrations** (embedded SQL files with a `schema_version`) instead of `table_info` guards.
- **Hash-chained audit**: each row stores the previous row's hash, with no deletes, only redaction events.
- **Generalized model**: approvals attach to items inside collections, and "custom actions" become declared action types with JSON Schema-validated payloads and an explicit executor, so we never ship a dead outcome like `require_n_approvers`.
- **MCP**: use the official Go MCP SDK for stdio, and streamable HTTP on localhost with the same dispatch. Long waits use progress notifications or short `await` calls rather than 5 s polling. Expose list, get and search tools over collections and items.
- **Out of scope**: billing tiers, watchers and multi-tenant SaaS concerns, at least initially.
