> **Snapshot (2026-10-13)** — research notes from the initial build, comparing staq.ly with [Impri](https://github.com/sekera-radim/impri). Not kept up to date; see [docs/PROGRESS.md](../PROGRESS.md) for the current status.

# Impri → staq.ly feature-parity gap analysis

Date: 2026-10-13.

Sources:
- **Impri docs**: https://impri.dev/docs. Every page linked from the index was cross-checked against the clone's `docs/*.md`, which match the live site; clone HEAD is `5ba0b75`, 2026-10-10.
- **Impri source**: `reference/impri/{server/src,mcp,sdk,cli,ui,integrations,docker,deploy,status}`.
- **staq.ly**: `apps/api`, `apps/web`, `packages/sdk-typescript`, `integrations/`, `docs/`.

See also [impri-analysis.md](impri-analysis.md), which covers architecture and security.

Legend:

| Mark | Meaning |
|---|---|
| ✅ | We have it (equal or better) |
| 🟡 | Partial |
| ❌ | Missing |
| ➖ | Not applicable to staq.ly (reason given) |
| 🚧 | In progress right now (`internal/app/rules.go`, `watchers.go`, `feeds.go`, migration `00004_rules_watchers.sql`; the REST routes are being wired in `httpapi/server.go`) |

Sizes: S = up to ~1 day, M = 2–4 days, L = a week or more.

> **Model mapping.** An Impri `action` (kind, title, preview, payload, editable, target_url, callback_url, expires_in) maps to a staq.ly **item** inside a **collection**:
> - title → `title`
> - preview → `content` + media
> - payload → `metadata`
> - kind → collection + `type` + `source`
>
> Impri `approved → executed | execute_failed` maps to our `approved → processing → completed | failed`. Impri `scopes` (`actions`, `watch`, `admin`) map to our 10 fine-grained scopes.

---

## 1. Feature table

### 1.1 Approval model (actions ↔ items)

| Impri feature | How it works in Impri | staq.ly status | What we need to build | Size |
|---|---|---|---|---|
| Create approval request | `POST /v1/actions`: `kind`, `title` (no CR/LF), `preview{format: markdown\|plain\|diff, body ≤256 KB}`, `payload`, `target_url`, `callback_url`, `expires_in`, `idempotency_key`, `editable[]`, `idempotent`, `undo`. Returns `inbox_url`. | ✅ `POST /v1/collections/{id\|name}/items` (JSON or multipart with media). Items carry title, content, metadata, media, source, external_ref and status `pending`. Richer than Impri: media, types, versions. | Optional: `content_format` (markdown/plain/diff) so diffs render as diffs. Optional `undo` / `idempotent` hints in metadata, shown in the UI. Return `inbox_url` / `web_url` in the item response. | S |
| Per-request expiry | `expires_in` is 300 s–30 d, default **72 h on every action**. The expiry tick sets status `expired` (a distinct status), fires the callback and writes `action.expired` audit. | 🟡 🚧 Expiry exists only through an `expire` rule (`items.expires_at`). The worker sets the item to `rejected` with feedback "Expired before review" and emits `item.expired`. There is no per-item `expires_in` on create and no collection default. | Add `expires_in` / `expires_at` to `CreateItemInput`, plus a collection-level `default_expiry_seconds`. Decide whether `expired` becomes a real status: today it collapses into `rejected`, so filters cannot tell them apart. Our recommendation is to add `expired` to `Statuses`. That needs a migration plus `allowed_statuses` updates. | S–M |
| Edit-before-approve with a whitelist | Per action: `editable: ["preview.body", "payload.x.y"]`. `edited` keys outside the whitelist get 422. Payload edits are persisted. | 🟡 `ActionInput.edits` on approve can change title/content/url/media/metadata/metadata_patch. There is **no whitelist**, so the reviewer can edit any field. | Add an optional `editable` (array of `title`, `content`, `metadata.<path>`) on the item, with a collection default. Enforce it in `RunAction` and return 422 `field_not_editable`. The UI then shows only the editable fields. | S |
| Diff of the edit | Impri stores `decisions.final_preview` and a naive unified `diff`, and includes `final_preview` and `diff` in the callback payload. The UI shows a `- original / + edited` block while editing. | 🟡 We snapshot every version (`item_versions`), and the event has `edited: true`. **No diff** is stored, sent in the webhook or rendered. | Compute a unified diff of `content` (and a JSON-patch of `metadata`) when `edits` are present. Store it in the event `data.diff` so webhook receivers get it. Add a diff view to the item history and the edit form. | S–M |
| Result reporting | `POST /v1/actions/:id/result {status: executed\|execute_failed, detail, payload ≤16 KB}`, only from `approved`. Writes `action.executed` / `action.execute_failed` audit. | ✅ `PATCH /v1/items/{id}` with `status: completed\|failed` and `metadata_patch` (receipt), with a version guard. | Optional sugar: `POST /v1/items/{id}/result {status: completed\|failed, detail, receipt}` that writes `metadata.result` and emits `item.completed` / `item.failed`. This gives agents and the MCP one obvious call. | S |
| Await decision (long-poll) | No server long-poll. SDK `awaitDecision` / `approvalGate` and MCP `impri_await_decision` poll `GET /actions/:id` every 5 s. Over HTTP MCP the wait is clamped to `MCP_AWAIT_DECISION_MAX_S` = 20 s; over stdio the default is 300 s. | ❌ No wait primitive in REST, MCP or SDK. Agents must poll `get_item` themselves. | `GET /v1/items/{id}/await?timeout=25&until=decided` (server-side long-poll, ≤30 s, returns as soon as status ≠ pending). Implement it with an in-process notifier keyed by item ID that `mutateItem` signals after commit. Add the MCP tool `await_decision` (loops on short waits, sends progress notifications) and SDK `awaitDecision()` / `approvalGate()`. | M |
| Per-request callback URL | `callback_url` per action. Sends signed `action.updated` on decision, auto-decision and expiry. Retries 1m/5m/25m/2h/12h, then DLQ; 410 = gone. | 🟡 Webhooks are workspace endpoints with event and collection filters, which is better for n8n fan-out. There is **no per-item callback**, so the n8n "Wait → resume URL" pattern needs a global webhook. | Optional `callback_url` on the item. On a decision or terminal status, enqueue an outbox delivery to that URL, signed with the workspace's default secret (or the first webhook's secret). Reuse the existing outbox and worker and the SSRF guard. | M |
| Delivery status on the request | `GET /actions/:id` includes `webhook_delivery` (last status). | 🟡 There is a delivery history page and `GET /v1/webhook-deliveries`, but nothing on the item. | Add `GET /v1/webhook-deliveries?item_id=` and a "Deliveries" tab on the item. | S |
| Idempotency | Body `idempotency_key` with a partial UNIQUE index returns the existing row with 200. Impri also does a soft dedup on `(kind, preview_hash)` among pending actions and sets `duplicate_of`. | ✅ `Idempotency-Key` header with a 24 h replay window covers every unsafe request, plus `external_ref`. | Optional soft dedup: `dedupe: true` returns the existing pending item with the same collection and content hash. | S |
| Statuses | `pending, approved, rejected, expired, executed, execute_failed` | ✅ `draft, pending, approved, rejected, processing, completed, failed, archived`. Richer. | Add `expired` (see above). | – |
| Bulk decisions | `POST /v1/actions/bulk-decision`: up to 50 IDs, `verdict`, `comment`. Each ID runs in its own transaction, and the response is 200 with per-item results. Items with `editable` are excluded. The channel is tagged `bulk-web` or `bulk-api`. | ✅ `POST /v1/items/bulk-actions` (approve, reject or any custom action, per-item results). | Tag `channel: bulk` in event data. | S |
| Decision channel attribution | `decisions.channel`: `api`, `web`, `bulk-*`, `slack`, `telegram`, `discord` or `auto`. `decided_by` is a key ID, a chat user (`sl:U…`) or `rule:<id>`. | 🟡 The event `actor` is a principal (session, key, local or system rule). There is no channel field because there are no chat channels yet. | Add `data.channel` to decision events (`web`, `api`, `mcp`, `bulk`, `telegram`, `slack`, `discord`, `rule`, `expiry`). Ship it together with WP-C. | S |
| Self-approval protection | None. The same `actions` scope creates and decides. | ✅ Better: `items:write` and `items:review` are split. | – | – |
| Multi-approver (`require_n_approvers`) | Validated in the rule schema (`n` is 2–100) and returned by `evaluateRules`, but **never enforced**. `routes/actions.ts` ignores it, and `UNIQUE(decisions.action_id)` makes it impossible. Impri does not actually have this feature. | ❌ (parity is effectively ✅, because Impri's version is dead code) | Only if wanted as a differentiator: <ul><li>a rule action `require_approvals {n}`</li><li>an `item_approvals(item_id, principal_id, verdict)` table</li><li>approve records a vote, and the item flips to `approved` once N distinct principals have approved (one reject rejects)</li><li>UI shows "1 of 2 approvals"</li></ul> It needs distinct human identities, which cloud mode only has as keys and sessions. | M |
| Untrusted-content marking | Watcher items get `payload.untrusted = true`. The MCP wraps them in `<untrusted-external-content>`, and the UI renders them as plain text. | 🟡 🚧 Watcher items set `metadata.untrusted = true` (`watchers.go:443`). The MCP does not wrap them. The UI is not at risk, because it renders content as plain text (no markdown renderer in `apps/web`). | Wrap the content and title of `untrusted` items in the MCP `get_item`, `list_items` and `search_items` outputs, and add an "external" badge in the UI. | S |

### 1.2 Rules engine 🚧

| Impri feature | How it works in Impri | staq.ly status | What we need to build | Size |
|---|---|---|---|---|
| Rule CRUD | `POST/GET/PATCH/DELETE /v1/rules[/:id]` (admin scope). At most 50 per project (409). `priority` 1–9999, first match wins, evaluated once at create time. 5 s LRU cache. | 🚧 `rules` table and `app/rules.go`. Routes `/v1/rules` are being wired. `rules:manage` scope; plan limit `Rules`. Priority 0–100000, first match wins, evaluated inside the create transaction. `times_applied` counter (Impri has none). | Finish: OpenAPI entries, a web **Rules** page (list, reorder by priority, enable toggle, editor with live "test against item" preview), SDK methods and an MCP read tool. | M |
| Conditions | `kind_pattern` glob; `payload_conditions[]` (`eq lt lte gt gte contains in not_in`, dot path); `target_url_hosts[]`. All ANDed. | 🚧 `collection_ids`, `types`, `source_pattern` glob, `text_contains`, metadata `conditions[]` with `eq ne lt lte gt gte contains in not_in exists`. A superset, apart from the URL host. | Add `url_hosts[]` matching the item `url` host (case-insensitive), so "auto-approve staging links" is possible. | S |
| Outcomes | `auto_approve`, `auto_reject`, `set_expiry {expires_in}`, `escalate {channel}` (notify a specific channel), `require_n_approvers` (not enforced). Writes `action.rule_applied` audit. Auto-decisions do **not** notify channels. | 🚧 `auto_approve`, `auto_reject`, `set_status`, `expire {after_seconds}`, `escalate {note}` (emits `item.escalated` → webhooks only). | `escalate.channel_id` targeting a notification channel (needs WP-B). Skip notifications for auto-decided items. | S (after WP-B) |
| Rule audit events | `rule.created`, `rule.updated`, `rule.deleted`, `action.rule_applied` | ✅ 🚧 Emits `rule.created/updated/deleted` and `item.rule_applied`. | – | – |

### 1.3 Watchers 🚧

| Impri feature | How it works in Impri | staq.ly status | What we need to build | Size |
|---|---|---|---|---|
| Watcher kinds | `rss`, `reddit_search`, `url_diff`. Each new entry becomes a `watcher.triage` pending action with `untrusted: true`. | 🚧 `rss`, `url_diff`. Entries become pending items in a chosen **collection**, which is better targeting. Reddit search goes through an RSS preset. | – (`reddit_search` isn't needed; RSS search covers it) | – |
| CRUD and run now | `POST/GET/PATCH/DELETE /v1/watchers`, `POST /v1/watchers/:id/run` (20/min). `watch` scope. | 🚧 `CreateWatcher`, `List`, `Get`, `Update`, `Delete`, `RunWatcherNow`; `watchers:manage` scope; routes being wired. | Finish: OpenAPI, a web **Watchers** page (list with status chip, last run, items created, error; run-now; pause/resume; edit) and a preset picker dialog. | M |
| Presets | 20 presets in `server/src/watcherPresets.ts`. `GET /v1/watcher-presets` and `POST /v1/watchers/from-preset {preset_id, params, name?, schedule?}`. | 🚧 21 presets (`watchers.go`). We add `pinterest-board` but **lack `hn-show-ask`**. The routes `/watcher-presets` and `/watchers/from-preset` are being wired. | Add `hn-show-ask` (`https://hnrss.org/show` and `/ask`). | S |
| Scoring rules | `keywords: [{pattern (regex, ReDoS-checked), points 1–100}]`, `keywords_none[]` (exclude) and `min_score`. Items are sorted by score, and `Score: N` appears in the preview. | 🟡 🚧 Plain include and exclude keyword lists (boolean match). No points, no `min_score`, no ranking. | Optional: `keywords` as `{pattern, points}` plus `min_score`, store `metadata.score` and sort the burst by score. This keeps the simple list as sugar (points = 1). | S–M |
| Schedule window, jitter, timezone | `schedule {every ≥60s, jitter, window "HH:MM-HH:MM"}`, evaluated in the **project timezone** (`PATCH /v1/project {timezone}`), DST-correct. Runs outside the window are deferred and counted as `window_skipped`. | 🟡 🚧 `every` plus automatic jitter (≤10 %, max 60 s). No window, no timezone. | Add `window` (`HH:MM-HH:MM`) and `jitter` on the watcher, and `timezone` on the workspace (`PATCH /v1/workspace`). | S–M |
| Plan interval limits | Minimum interval: Free 15 min, Indie 5 min, Team 1 min (402). Watcher count: 3 / 20 / ∞. | 🚧 `MinWatcherIntervalSeconds` and `Watchers` in `PlanLimits`. | – | – |
| robots.txt | `Impri-Watcher/1.0` user agent; a disallow degrades the watcher with `robots_disallowed`. | ✅ 🚧 `feeds.go` robots cache (1 h) and the same error. | – | – |
| Degraded and paused | 3 consecutive failures → `degraded`; degraded for 24 h → auto-`paused`; a successful run reactivates. Only `active` and `paused` can be set over the API. | ✅ 🚧 Same idea, with a threshold of 5 failures and pause after 24 h. | Emit `watcher.degraded` and `watcher.paused` events (webhooks and notifications) so the user learns about it. | S |
| Baseline, dedup, burst | The first run is a baseline only. Dedup lives in `watcher_items` (90 d, cap 10 k per watcher). If a run finds more than 25 hits, only the top 10 are published, plus an overflow note. Minimum 1 s per host; 5 MB / 15 s fetch caps. | ✅ 🚧 Baseline, `watcher_seen` (90 d prune), `maxNewItemsPerRun = 20`. | Optional "N more skipped" summary item. | S |
| Watcher audit | `watcher.created`, `watcher.updated`, `watcher.deleted`, `watcher.hit` | 🟡 🚧 `watcher.created` only so far. | Emit `watcher.updated`, `watcher.deleted` and `watcher.run` (with counts). | S |
| `DISABLE_WATCHER_SCHEDULER` | Environment flag | ❌ | Add a `WATCHERS_ENABLED=false` config flag. | S |

### 1.4 Notification channels (the biggest functional gap)

Impri channels (`/v1/notification-channels`, admin scope; table `notification_channels`):
- Six types: `slack`, `discord`, `telegram`, `ntfy`, `email`, `webhook`.
- They fire when an action becomes pending, including on `set_expiry` and `escalate`. Auto-decided actions never notify.
- Per-channel `digest_window_sec` (10–3600, default 60): the first message goes immediately; later ones are queued and flushed every ~30 s as one digest.
- A channel is auto-disabled after 5 consecutive failures; any config update resets it.
- `POST /:id/test` (5/min) bypasses the digest.
- Secrets are masked as `****last4` and stored in plaintext.
- Outbound requests go through the SSRF guard.
- There are also instance-wide `NTFY_URL/NTFY_TOPIC` and `NOTIFY_EMAIL` fallbacks.
- **There are no quiet hours.**

| Impri feature | How it works in Impri | staq.ly status | What we need to build | Size |
|---|---|---|---|---|
| Channel framework | CRUD plus `/test`, digest queue, failure counter and auto-disable, masked secrets, audit `channel.created/updated/deleted/tested` | ❌ | <ul><li>Migration: `notification_channels(id, workspace_id, name, type, enabled, config JSON, digest_window_sec, collection_ids JSON, min_interval, fail_count, last_error, last_fired_at)` and `notification_queue(channel_id, item_id, enqueued_at, UNIQUE(channel_id,item_id))`.</li><li>Enqueue inside the same transaction as `item.created` when the item is pending (and on `item.escalated`), via a hook in `emit`.</li><li>A worker loop (like the webhook worker) that flushes per channel: one message, or a digest for two or more.</li><li>`/v1/notification-channels` CRUD plus `/test`; scope `notifications:manage` (new) or `webhooks:manage`.</li><li>Web page **Settings → Notifications**.</li></ul> | L |
| Generic webhook channel | `{event: "action.pending", action_id, title, kind, inbox_url, triggered_at, escalate}`, HMAC-signed only when `hmac_secret` is set | ✅ Our webhooks are better: typed events, always signed, event and delivery IDs, outbox. Subscribing to `item.created` gives the same result. | – | – |
| Telegram | `bot_token` and `chat_id`; Bot API `sendMessage` | ❌ | Telegram notifier: `sendMessage`, or `sendPhoto` / `sendVideo` for image and video items by **multipart-uploading the media from our storage**. Media is private, so we never send a URL. Includes a caption with title and a link to `PUBLIC_WEB_URL/items/{id}`. | M |
| Slack | Incoming-webhook `url`, or bot token + `channel_id` (approval mode) | ❌ | Slack notifier: incoming webhook with Block Kit (title, content excerpt, "Open" button). Images need `files.uploadV2` with a bot token, because media isn't public. | M |
| Discord | Webhook `url` posted with `?wait=true`, or bot (approval mode) | ❌ | Discord notifier: webhook with embed; image as a multipart attachment. | S–M |
| ntfy | `url` and `topic` (`^[A-Za-z0-9_/-]{1,64}$`); headers `Title`, `Tags`, `Click` → inbox | ❌ | ntfy notifier, which is trivial. Add an `Actions:` header with "Approve" and "Reject" `http` actions pointing at a signed one-tap URL (see WP-C), which Impri lacks. Support an optional `Authorization` token for self-hosted ntfy. | S |
| Email | SMTP (`SMTP_HOST/PORT/SECURE/USER/PASS/FROM`); plain text, subject `[Impri] {title}` | ❌ | SMTP notifier (`net/smtp` + STARTTLS), plain-text and HTML parts, with a signed one-click approve/reject link (optional, behind a confirmation page). Configure SMTP in env; per-channel `address`. | M |
| Web push | VAPID (`VAPID_PUBLIC_KEY/PRIVATE_KEY/SUBJECT`); `GET /v1/push/vapid-public-key`, `POST` and `DELETE /v1/push/subscribe` (`actions` scope); `push_subscriptions` table; the UI is a PWA with `sw.js`; a push is sent per pending action | ❌ No service worker or manifest in `apps/web`. | <ul><li>`push_subscriptions(workspace_id, session_id, endpoint UNIQUE, p256dh, auth)` and the VAPID env, with keys auto-generated into `/data` when unset.</li><li>Endpoints `/v1/push/*`.</li><li>Next.js `manifest.webmanifest` and `public/sw.js` with notification-click deep links to `/items/{id}`.</li><li>A Settings toggle.</li><li>Use `github.com/SherClockHolmes/webpush-go`.</li></ul> Caveat: browsers need a secure context, which `localhost` satisfies; a LAN `http://192.168…` does not, so docs should say "HTTPS or localhost only". | M |
| Digest and dedup | `digest_window_sec`; one queue entry per action per channel | ❌ | Part of the framework above. | (in L) |
| Quiet hours | **Not in Impri** | ❌ (no parity gap) | Optional differentiator: `quiet_hours {start, end}` per channel in the workspace timezone; queue during quiet hours and send one digest after. | S |
| Escalation routing | Rule `escalate {channel}` sends to that channel, with `escalate: true` in the payload | 🟡 🚧 `item.escalated` event → webhooks | Rule param `channel_id` (after the framework). | S |
| Channel verification and setup | `POST /:id/test`. Telegram: `POST /:id/setup-webhook` calls `setWebhook` with `secret_token`, which needs a public `BASE_URL`. Discord: the Interactions Endpoint URL must answer Discord's signed PING (Ed25519). Slack: Interactivity URL plus signing secret. | 🟡 Webhooks have `POST /v1/webhooks/{id}/test`. | `/test` per channel. A Telegram "connect" helper: the user sends `/start <code>` to the bot, and we read `getUpdates` to auto-fill `chat_id` and the approver user ID, which beats copying IDs by hand. | S–M |

### 1.5 Interactive approval from chat

| Impri feature | How it works in Impri | staq.ly status | What we need to build | Size |
|---|---|---|---|---|
| Telegram buttons | `approval_mode: true` sends an inline keyboard ✅/❌ plus "View in inbox". The callback arrives at `POST /v1/integrations/telegram/webhook/:channelId`, verified by `X-Telegram-Bot-Api-Secret-Token`, an HMAC in `callback_data` (6-byte truncated) and the `allowed_approver_user_ids` allowlist. It decides in the same transaction as REST, then edits the message to show the outcome. Digests fall back to a link. | ❌ | <ul><li>`POST /v1/integrations/telegram/{channel_id}` (public, secret-token verified).</li><li>`callback_data = "a\|r:" + item_id + ":" + HMAC` (use ≥12 bytes; Telegram allows 64 bytes).</li><li>Allowlist; actor `Telegram: @user`.</li><li>Go through `RunAction`, so rules, version guard and webhooks all apply.</li><li>Edit the message after the decision.</li><li>Reject feedback: a reply `ForceReply` prompt "Reason?", stored as feedback. Impri has none, and our reject usually requires feedback.</li><li>Custom actions (regenerate, publish) as extra buttons.</li></ul> Local mode: Telegram needs a public HTTPS URL, so document long-polling `getUpdates` mode for local installs. It needs no inbound port, which is a real advantage for local-first. | M–L |
| Slack buttons | Block Kit buttons; `POST /v1/integrations/slack/interactions[/:channelId]`; Slack signing-secret v0 HMAC plus button HMAC plus `allowed_approver_slack_user_ids`. The message is updated via `response_url`. The cloud has an "Add to Slack" OAuth shared app (`/v1/integrations/slack/install-url`, `/oauth/callback`, `app-info`); with it the allowlist starts empty (fail-closed). | ❌ | BYO-app mode first: bot token + signing secret + channel. Interactions endpoint; a modal for the reject reason. OAuth shared app only for cloud (L, needs a published Slack app). Local mode needs a public URL (Socket Mode is an alternative: no inbound port; recommended for local). | M (BYO) / L (OAuth) |
| Discord buttons | Bot with components; `POST /v1/integrations/discord/interactions/:channelId`, Ed25519 (`public_key`), button HMAC, `allowed_approver_discord_user_ids` (max 50). Answers the PING. | ❌ | Same pattern. The Ed25519 verify is in Go's stdlib `crypto/ed25519`. Use a modal for the reject reason. | M |
| One-tap approve from ntfy or email | Not in Impri | ❌ | A signed, single-use, expiring decision link `GET /v1/decide/{token}` that shows a confirmation page, then `POST`. Use it for ntfy `Actions` and email. This is the cheapest "approve from phone" path for local installs on the LAN. | S–M |

### 1.6 Audit log, GDPR, retention

| Impri feature | How it works in Impri | staq.ly status | What we need to build | Size |
|---|---|---|---|---|
| Audit trail | `audit_log`: append-only by convention, **not tamper-evident**. Covers actions, keys, watchers, rules, channels, project and GDPR events. `GET /v1/audit` filters: `type` (exact or `prefix.`), `actor`, `entity_id` (also inside `data`), `since`, `until`; keyset cursor; limit 200. Admin scope. | ✅ / 🟡 `events` table, **immutable by DB trigger** (better). Covers item, collection, api_key, rule, watcher, session, device, recovery and workspace events. `GET /v1/events` filters `item_id`, `collection_id`, `type` (exact), `actor_type`; seq cursor. **Activity** page in the web app. Missing: `since` / `until`, type prefix, actor ID filter, and `webhook.created/updated/deleted` events. | Add `since`, `until`, `type` prefix (`item.`), `actor_id`. Emit `webhook.created/updated/deleted/secret_rotated`. | S |
| Audit export | `GET /v1/audit/export?format=json\|csv` (NDJSON or RFC 4180 CSV with formula-injection escaping), streamed, 5/min | ❌ | `GET /v1/events/export?format=ndjson\|csv&…filters`, streamed from a rows cursor, CSV with a `'` prefix for `= + - @`. Add an "Export" button on the Activity page. | S |
| Hash chain | **Not in Impri** | ❌ (no parity gap) | Optional: `events.prev_hash` / `hash` (SHA-256 over seq, type, data, prev_hash) and `GET /v1/events/verify`. It was planned in impri-analysis §13. | M |
| Retention | `AUDIT_RETENTION_DAYS` and `PII_RETENTION_DAYS` (default unlimited); daily prune; export honours the window | ❌ Events, deliveries and versions grow forever (already noted in PROGRESS "next improvements"). | Add `EVENTS_RETENTION_DAYS`, `DELIVERIES_RETENTION_DAYS` and `VERSIONS_KEEP` (per item), pruned by a daily job. The immutability trigger only blocks UPDATE, so DELETE-prune works. Optionally make retention per plan. | S |
| PII separation | Request IPs only in `pii_log` (erasable); the audit row's `ip` stays NULL | ✅ We don't store request IPs in events (sessions keep the user agent only). | – | – |
| GDPR export | `GET /v1/project/export`: one JSON with project, actions, decisions, watchers and audit (no keys, PII or push); writes `gdpr.export` audit | ❌ (only operator-level `make backup`) | `GET /v1/workspace/export`, which streams a **ZIP**: `workspace.json`, `collections.json`, `items.ndjson` (with versions), `events.ndjson`, `webhooks.json` (secrets redacted), `rules.json`, `watchers.json` and `media/…`. Emit `workspace.exported`. Add a Settings button. This also covers PROGRESS "next improvements #7". | M |
| GDPR erase | `DELETE /v1/project/data`: hard-deletes actions, decisions, deliveries, watchers, audit and PII; keeps the project and keys; writes one `gdpr.erase` tombstone | ❌ | Two endpoints: <ul><li>`DELETE /v1/workspace/data` (confirm by name): items, media files, versions, events, deliveries, watchers and rules, keeping keys, sessions and collections, and leaving a `workspace.erased` tombstone.</li><li>Cloud: `DELETE /v1/workspace`, which deletes everything including keys and sessions and logs out.</li></ul> | M |
| Privacy policy and ToS pages | `docs/privacy.md`, `docs/terms.md` (hosted cloud) | ❌ | Cloud only: static pages in the web app or the marketing site. | S |

### 1.7 API keys, scopes, recovery, accounts

| Impri feature | How it works in Impri | staq.ly status | What we need to build | Size |
|---|---|---|---|---|
| API keys | `im_` + 32 bytes, argon2-hashed, 16-char prefix. `POST/GET/DELETE /v1/keys` (admin). Shown once. Revoked keys are listed. No rotation (create, then revoke). | ✅ Better: `stk_` 256-bit SHA-256 hashed keys, `expires_at`, `last_used_at`, **rotate endpoint**, revoke, UI page, plan limit. | – | – |
| Scopes | `actions` (create, list, decide, bulk, result, push subscribe), `watch` (watchers and presets), `admin` (keys, project, rules, channels, audit, billing, GDPR; implies all) | ✅ Finer-grained: `collections:read/write`, `items:read/write`, `items:review`, `actions:execute`, `webhooks:manage`, `rules:manage`, `watchers:manage`, `admin`. | New scopes to add with the matching WPs: `notifications:manage` (channels), `events:read` or audit export under `admin`, `workspace:export` under `admin`. Keep `rules:manage` out of the default key (as today). | S |
| Bootstrap key | Printed to stdout on the first start | ✅ / ➖ Local mode is keyless; cloud setup returns a key and a recovery key. | – | – |
| Recovery | `imr_` recovery code (argon2). `POST /v1/recover {project_id, recovery_code}` mints a new admin key and rotates the code; rate-limited 5/min per IP and 5/min per project; same 401 for every failure. `POST /v1/recovery-code` rotates it. A `reset-key` CLI is mentioned. | ✅ Recovery key: `POST /v1/sessions/recover` returns a session; `POST /v1/recovery-key/rotate`; `server recovery-key` / `make recovery-key` for operators. **But `/v1/sessions/recover` has no rate limit** (only signup is limited, at 5/h). | Rate-limit `/v1/sessions/recover` and `/v1/device-links/redeem` (5/min per IP). Check that a successful recover rotates the key, or document why not. | S |
| Signup | `POST /v1/signup`, 3/min per IP; anonymous | ✅ `POST /v1/workspaces`, 5/h per IP, or a `SETUP_TOKEN`. | – | – |
| Connect a device (QR) | `ConnectDeviceDialog.vue` | ✅ QR device links with sessions and device revocation. | – | – |
| Product feedback | `POST /v1/feedback`, `GET /v1/admin/feedback` | ➖ Operator-facing SaaS growth feature; not needed locally. Optional for cloud. | – | S |

### 1.8 Billing, tiers, usage, operator

| Impri feature | How it works in Impri | staq.ly status | What we need to build | Size |
|---|---|---|---|---|
| Tiers and limits | Free / Indie / Team: approvals (decisions) per month 100 / 2 000 / ∞, watchers 3 / 20 / ∞, minimum interval 15 / 5 / 1 min; 402 with `limit` and `tier`. Self-host (no Stripe key) is unlimited. | ✅ `plans.go`: items per month, webhooks, keys, storage, upload size, rules, watchers, minimum watcher interval; 402 `plan_limit_exceeded` with details. Local mode is unlocked. | – | – |
| Usage | `GET /v1/usage` and `GET /v1/billing` usage; UI UsageScreen | ✅ `GET /v1/workspace/usage`, `/v1/plans`, Plan & usage page. | – | – |
| Stripe billing | `GET /v1/billing`, `POST /v1/billing/checkout {plan, period: monthly\|yearly}`, `POST /v1/billing/portal`, `POST /v1/billing/webhook` (Stripe signature; `checkout.session.completed`, `customer.subscription.*`). Projects store the `stripe_customer_id` and subscription status. Env `STRIPE_*`, `APP_URL`. | ❌ Plans are set manually (`make set-plan`). | Cloud only: <ul><li>migration adding `workspaces.stripe_customer_id`, `subscription_status`, `current_period_end`</li><li>`/v1/billing`, `/v1/billing/checkout`, `/v1/billing/portal`, `/v1/billing/webhook` (`stripe-go`)</li><li>Price IDs in env; downgrade on `subscription.deleted`</li><li>"Upgrade" buttons on the Plan page</li><li>billing disabled when `STRIPE_SECRET_KEY` is unset</li></ul> | L |
| Operator stats | `OPERATOR_PROJECT_ID`. `GET /v1/admin/stats` (signups, by tier, activity, activation funnel) and `/stats/daily`; 404 for non-operators | 🟡 Shell only: `make workspaces`, `server workspaces`, `set-plan`, `recovery-key`, `connect-link`. | Cloud only: `OPERATOR_WORKSPACE_ID`, `GET /v1/admin/stats` (workspaces total and 24h/7d/30d, by plan, items created 7d, active watchers, a funnel of key created → first item → first human decision), and an operator page. | M |
| Comp a tier | `POST /v1/admin/comp-tier {project_id, tier, expires_in}` | 🟡 `make set-plan` (shell) | `POST /v1/admin/workspaces/{id}/plan` for operators. | S |

### 1.9 Observability, health, ops

| Impri feature | How it works in Impri | staq.ly status | What we need to build | Size |
|---|---|---|---|---|
| Liveness | `GET /healthz` | ✅ `GET /health` (includes a DB ping and the version) | – | – |
| Readiness | `GET /readyz`: checks `db_reachable`, `schema_applied`, `db_writable` (insert and delete canary) and Redis (advisory); 503 on failure | ❌ | `GET /ready`: `SELECT 1`, goose version equals the latest, a write canary in a `BEGIN IMMEDIATE … ROLLBACK`, uploads dir writable, worker heartbeat age < 2 min. | S |
| Prometheus metrics | Opt-in `METRICS_ENABLED`, `METRICS_TOKEN` bearer or a separate `METRICS_HOST:PORT`. About 25 metrics: http requests and latency by route pattern, rate-limited, actions by status, pending, decisions by verdict and channel, decision latency histogram, expired, watcher runs / hits / burst / degraded, webhook deliveries / duration / DLQ size, notifications ok/error, digest flushes, channel auto-disabled, `db_size_bytes`, `build_info`, uptime | ❌ | `internal/metrics` using `prometheus/client_golang` (or a hand-rolled text exposition), `METRICS_ENABLED`, `METRICS_TOKEN`, `METRICS_ADDR`. Metrics: `staqly_http_requests_total{route,method,status_class}`, duration histogram, `items{status}` gauge, `decisions_total{verdict,channel}`, `decision_latency_seconds`, `webhook_deliveries_total{result}`, `webhook_pending`, `webhook_failed`, `watcher_runs_total{kind,result}`, `notifications_total{type,result}`, `db_size_bytes`, `uploads_bytes`, `build_info`. Route labels come from the chi route pattern. | M |
| Error reporting | Sentry server (`SENTRY_DSN`, `_ENVIRONMENT`, `_RELEASE`) and UI (`VITE_SENTRY_DSN`); a true no-op when unset; 4xx not reported; PII scrubbed | ❌ | `sentry-go` behind `SENTRY_DSN` (recover middleware plus worker panics) and `@sentry/nextjs` behind `NEXT_PUBLIC_SENTRY_DSN`. Scrub `Authorization`, cookies and `X-CSRF-Token`. | S–M |
| Structured logs and request ID | pino JSON; `x-request-id` header; lifecycle events (`action.created`, `watcher.run`, …) | ✅ / 🟡 slog plus chi `RequestID` in logs. The ID **isn't returned as a response header**. | Set the `X-Request-Id` response header and include `request_id` in error bodies. | S |
| Rate limiting | Per key and route, fixed window: create 60/min, list 300, decide 60, bulk 10, audit export 5, channels write 30 / test 5, watchers create 30 / run 20, MCP 120 per key and 60 per IP anonymous, signup 3, recover 5. SQLite table or Redis. Metric `rate_limited_total`. | ❌ Only signup (5/h per IP). | `httpapi/ratelimit.go`: a per-principal and per-route-class limiter (token bucket, in memory, which is fine for a single process). Defaults: write 120/min, read 600/min, review 120/min, bulk 20/min, export 5/min, MCP 240/min, recover 5/min per IP. `429` with `Retry-After` and `X-RateLimit-*`. Disabled for the `local` principal. | S–M |
| Backups | `deploy/backup.sh` (SQLite `.backup`), a systemd timer, `fly-backup.sh`; **Litestream** continuous S3 replication when `BUCKET_NAME` is set (with auto-restore on boot) | 🟡 `make backup` / `make restore` (`VACUUM INTO` + uploads tar); `server backup <file>`. No schedule, no continuous replication, and **uploads are files**, so Litestream alone isn't enough. | <ul><li>Optional Litestream in the API image (`LITESTREAM_REPLICA_URL` → `litestream replicate -exec server`, plus `restore -if-replica-exists`).</li><li>Uploads to S3 via `rclone sync` cron, or document S3-compatible storage.</li><li>A `scripts/backup-cron` example (systemd timer).</li></ul> | M |
| Production deploy kit | `deploy/Caddyfile`, `docker-compose.prod.yml`, `fly.toml`, `.env.example` with Sentry, Stripe and VAPID | 🟡 `docs/cloud.md` describes a reverse proxy; `compose.yml`, `compose.lan.yml`. | `deploy/Caddyfile` (`/v1`, `/mcp`, `/openapi.yaml` → API; the rest → web), `compose.prod.yml`, and optionally `fly.toml`. | S |
| Status page | A separate Cloudflare Worker (`status/worker.js`): pings API, app and web every 15 min, 60-day history in KV, HTML plus `/api/status` | ❌ | Cloud only. Use an external uptime service, or port `status/` (any HTTP monitor will do). | S |
| Redis | Optional, only for a shared rate limit across instances | ➖ Single Go process by design (SQLite writer). | – | – |

### 1.10 MCP

| Impri tool | staq.ly equivalent | Status |
|---|---|---|
| `impri_push_action` (kind, title, preview, payload, editable, expires_in, idempotency_key) | `create_item` (collection, media upload, metadata) | ✅ Add `expires_in`, `editable` and `idempotency_key` args when the API gets them. |
| `impri_await_decision` (poll 5 s, `timeout_s` up to 300 stdio / 20 HTTP) | – | ❌ Add `await_decision {item_id, timeout_s ≤ 25}`, backed by the long-poll endpoint and emitting MCP progress notifications. |
| `impri_report_result` | `update_item` (status completed/failed + metadata_patch) | ✅ (optionally add a `report_result` alias with a clearer schema) |
| `impri_inbox_status` (pending count, recent) | `list_pending_approvals`, `list_collections` (counts per status) | ✅ |
| `impri_create_watcher` | – | ❌ 🚧 Add `create_watcher` (`watchers:manage`). |
| `impri_list_watchers` | – | ❌ 🚧 Add `list_watchers`. |
| `impri_list_watcher_presets` | – | ❌ 🚧 Add `list_watcher_presets`. |
| `impri_create_watcher_from_preset` | – | ❌ 🚧 Add `create_watcher_from_preset`. |
| (none) | `list_collections`, `create_collection`, `get_collection`, `list_items`, `get_item`, `search_items`, `approve_item`, `reject_item`, `list_available_actions`, `execute_custom_action`, `get_item_history` | ✅ (14 tools vs 8) |

| MCP platform feature | Impri | staq.ly | Build | Size |
|---|---|---|---|---|
| Transports | Stdio npm package `npx @impri/mcp` (thin HTTP client), plus stateless HTTP `/mcp` (POST only; anonymous `tools/list`) | 🟡 HTTP `/mcp` and stdio **only via `docker exec` against the local DB**. There is no stdio client for a remote or cloud instance. | `mcp-stdio --remote <url>` mode, or a tiny npm wrapper `@staqly/mcp` (`npx`) that proxies stdio to HTTP `/mcp` with `STAQLY_API_KEY`. Needed for Claude Desktop, Cursor and Windsurf users who don't run Docker locally. Already listed as next improvement #6. | S–M |
| outputSchema on tools | Yes (validated by ajv tests) | ❌ Handlers return `(…, any, error)`, so go-sdk emits no outputSchema. | Give the handlers typed output structs so go-sdk publishes outputSchema and `structuredContent`. | S |
| Untrusted wrapping | `<untrusted-external-content>` for `payload.untrusted` | ❌ | See §1.1. | S |
| Registry metadata | `/.well-known/mcp/server-card.json`, `server.json` (MCP registry), `smithery.yaml` | ❌ | Add a server card endpoint and `server.json` (only meaningful once a remote stdio package exists). | S |
| Rules and channels tools | None | – | Optional read-only `list_rules`. Never expose rule creation to agents by default (it could auto-approve). | S |

### 1.11 SDKs, CLI, integrations, install docs

| Impri feature | How it works in Impri | staq.ly status | What we need to build | Size |
|---|---|---|---|---|
| TypeScript SDK | `@impri/sdk`. <ul><li>Actions: `createAction` (auto idempotency key), `getAction`, `listActions`, `decide`, `bulkDecide`, `reportResult`, `awaitDecision`, `approvalGate`.</li><li>Watchers: CRUD, `listWatcherPresets`, `createWatcherFromPreset`.</li><li>Keys: `createKey`, `listKeys`, `revokeKey`.</li><li>Project: `getProject`, `updateProject`, `rotateWebhookSecret`, `exportProject`, `eraseProjectData`.</li><li>Audit: `listAudit`, `exportAudit`.</li><li>Notification channels: CRUD and `test`.</li><li>Plus `verifyWebhook`, typed errors and an `isUntrusted` getter.</li></ul> | 🟡 `@staqly/sdk`. <ul><li>Collections: list, get, create, update, delete.</li><li>Items: list, get, create, `createItemWithFile`, update, delete.</li><li>Decisions and actions: approve, reject, runAction, availableActions, itemHistory.</li><li>Also uploadMedia, listEvents, `verifyWebhook`, `StaqlyError`.</li><li>Webhooks: list, create, delete only.</li></ul> **Missing:** bulk actions, item versions, webhook get/update/test, deliveries and retry, API keys, workspace and usage, rules, watchers and presets, notification channels, export, `awaitDecision`, an `approvalGate` helper, pagination iterators. | Add the methods. Add `awaitDecision(id, {timeoutMs})` (loops the long-poll) and `approvalGate({collection, title, content, metadata}, fn)` (create → await → run fn with edited values → report result). Add `iterItems()` async iterator and auto-generate from OpenAPI where possible. | M |
| Python SDK | `impri` (sync and async). The same surface plus `iter_actions`, `iter_watchers`, `iter_audit`, a `@requires_approval` decorator, `approval_gate` and `verify_webhook`. About 216 tests. | ❌ | New `packages/sdk-python` (`staqly`): an `httpx` client with sync and async variants, items, collections, decisions, webhooks, events, rules, watchers, `await_decision`, the `@requires_approval(collection=…)` decorator, `verify_webhook`, typed errors and iterators. Publish to PyPI. | L |
| CLI | `impri`: `init` (`--cloud --signup --demo`), `login`, `status`, `push` (`--wait --timeout`, stdin body, exit code 1 on reject), `inbox`, `list`, `get`, `approve`, `reject`, `tail` (poll stream), `presets`, `watch add <preset>`, `watchers list/get/delete`, `keys list/create/revoke`. Config `~/.impri/config.json` (0600); env overrides; exit code 2 on conflict. | ❌ Only operator subcommands: `server backup`, `recovery-key`, `workspaces`, `set-plan`, `connect-link`, plus Make targets. | New `apps/api/cmd/staqly` (Go, single static binary, also in the image): <ul><li>`init` / `login` (URL + key → `~/.config/staqly/config.json` 0600)</li><li>`status`, `collections ls/create`</li><li>`push <collection> --title --content/-f file/--stdin --meta k=v --wait --timeout` (exit 0 approved, 1 rejected or expired, 2 conflict)</li><li>`inbox`, `get`, `approve`, `reject -m`, `run <action>`</li><li>`tail` (event stream)</li><li>`watchers ls/add --preset/run/pause`, `presets`, `rules ls`, `keys ls/create/revoke`</li><li>`export`</li></ul> Local mode needs no key. | M–L |
| LangChain / LangGraph | `integrations/langchain`: Python tool and approval wrapper | ❌ | `integrations/langchain` (Python, on the Python SDK): `StaqlyApprovalTool` plus a `requires_approval` wrapper for tools, and a LangGraph interrupt example. | M |
| CrewAI | `ImpriApprovalTool` and an `ImpriApprovalCallback` that gates steps and tasks | ❌ | `integrations/crewai`: tool plus step callback. | M |
| OpenAI Agents SDK | `make_guardrail()` and the `requires_approval` decorator | ❌ | `integrations/openai-agents` (thin, on the Python SDK). | S |
| Claude Agent SDK | `withImpriApproval({tool, kind, title, preview, editable})` → `GatedTool.handle(block)` creates, waits, then executes or returns a rejection `tool_result`; `makeToolResult` | ❌ | `integrations/claude-agent-sdk` (TS, on `@staqly/sdk`): `withStaqlyApproval()` plus a **PreToolUse hook** example for Claude Code / Agent SDK (`canUseTool` → create item → await). | M |
| n8n | Docs only: HTTP Request + Wait + IF + result; no custom node | ✅ Better: an importable demo workflow, `setup-demo.sh`, networking docs, signed-webhook verify node. | Optional: an n8n **community node** `n8n-nodes-staqly` (create item, wait for decision via resume URL, trigger on webhook events). It depends on the per-item `callback_url` (§1.1). | L |
| Make and Zapier | Docs only (HTTP module plus custom webhook) | ❌ docs | Add a docs page with JSON templates. | S |
| Install: Claude Code, Codex | `claude mcp add impri -e … -- npx @impri/mcp`; Codex config; a CLAUDE.md snippet "call push_action, then await_decision before side effects" | ✅ Docs and web pages for Claude Code and Codex (HTTP and stdio). | Add the "tell the agent to use it" CLAUDE.md / AGENTS.md snippet once `await_decision` exists. | S |
| Install: Cursor | `.cursor/mcp.json` (npx), `.cursorrules` snippet | ❌ | Docs, plus a web page `integrations/cursor` (`{"mcpServers":{"staqly":{"url":"http://localhost:3333/mcp"}}}`; headers for cloud). | S |
| Install: Windsurf | `~/.codeium/windsurf/mcp_config.json` | ❌ | Docs, plus a web page `integrations/windsurf` (`serverUrl`). | S |
| Install: Claude Desktop | `claude_desktop_config.json` | ❌ | Docs. Claude Desktop needs stdio or `mcp-remote`, which depends on the remote stdio proxy. | S |
| Cookbook | 10 recipes: email gate, DB write, social post, code exec, idempotent batch, watcher triage, multi-step shared keys, webhook receiver, programmatic reject, key rotation; plus `examples/cookbook/*` runnable | 🟡 README examples plus the n8n demo | `docs/cookbook.md` plus `examples/` (content review loop, regenerate-with-feedback, publish-after-approve, watcher → triage collection, key rotation, webhook receiver in Node and Python). | M |
| Webhook verify snippets | Python and Node in the docs; SDK helpers | 🟡 TS `verifyWebhook`; README documents the scheme | Add Python and Go snippets to the docs. | S |

### 1.12 Web inbox and dashboard UX

| Impri feature | How it works in Impri | staq.ly status | What we need to build | Size |
|---|---|---|---|---|
| Keyboard | `j`/`k`, `Enter`/`Space`, `a`, `r`, `e`, `x`, `/` (focus search), `Shift+A`, `Shift+R`, `Esc`, `?` | 🟡 `j`/`k`/arrows, `a`, `r`, `e`, `x`, `o`/`Enter`, `Shift+A`, `Esc`, `?`, `⌘K` palette. **Missing `Shift+R` (bulk reject) and `/` (search).** | Add `Shift+R` (bulk reject with a shared feedback prompt) and `/`. | S |
| Filters | Status tabs, `q` search (300 ms debounce), kind dropdown, time range (24 h / 7 d / 30 d), a "Filtered" chip with clear-all, a filtered empty state | 🟡 Inbox: status, collection, type, clear filters. FTS search exists in the API and in collection pages, but **not in the inbox**; there is no time range in the inbox. | Add a search box (`q`) and a time-range select to the inbox. | S |
| Bulk | Sticky bar Approve N / Reject N, select-all-visible, editable items excluded, optimistic removal and restore on error, mobile full-width bar | ✅ Bulk approve plus checkbox selection (verify the reject path and select-all). | Exclude or warn on items with an `editable` whitelist, once that exists. | S |
| Editable fields and diff in detail | Per-field "Edit" toggles with a live `- / +` diff; "Approve with edits" | 🟡 `item-edit-form` exists; no diff. | Live diff (see §1.1). | S |
| Copy ID and target URL | Copy buttons | ✅ `CopyButton` for the item ID on the detail page | – | – |
| Load more | Cursor `has_more` | ✅ Infinite query with auto-fetch | – | – |
| Live updates | Polling | ✅ Polling every 20 s (same) | Optional SSE `/v1/events/stream` (next improvement #2). | M |
| Getting started and demo | `GettingStarted.vue`: "Send a test approval" button (kind `demo`), checklist; `impri init --demo` | 🟡 `make seed`; setup flow | Optional: an empty-state "Create a sample item" button in the inbox. | S |
| Watchers, Rules, Notifications, Audit, Billing, Usage screens | `WatchersScreen`, `WatcherPresetsDialog`, `NotificationsScreen` (76 KB), `AuditScreen`, `BillingScreen`, `UsageScreen` | 🟡 Activity (audit) ✅, Plan & usage ✅, Webhooks ✅, API keys ✅. **Missing: Watchers 🚧, Rules 🚧, Notifications, Billing (Stripe).** | Pages: `/watchers`, `/rules`, `/settings/notifications`, plus an export button in `/activity`. | M each |
| PWA / mobile | Manifest plus `sw.js` (push) | 🟡 Responsive, with a dedicated mobile swipe inbox and QR connect; **not installable** (no manifest or service worker) | Manifest, icons, `sw.js` (part of web push). | S |
| i18n | None (English only) | None (English only) | ➖ Not a parity gap. | – |
| Recovery-code UI | Shield icon, setup banner, "Lost your key?" | ✅ Recovery form, rotate in Settings | – | – |

---

## 2. Prioritised implementation plan (parallel work packages)

Principle: each package owns its own new files. Shared hot spots are:
- `httpapi/server.go` (route mounting)
- `app/events.go` (`emit`)
- `app/items.go`
- `mcpserver/mcpserver.go`
- `openapi.yaml`
- migrations numbering

**Conventions to avoid conflicts:**
- Each WP mounts its routes from its own `httpapi/routes_<wp>.go` through a `func (s *Server) mount<WP>(r chi.Router)`, and adds a single line to `server.go`.
- Each WP adds its OpenAPI paths in one contiguous block, tagged with the WP.
- Migration numbers are reserved up front:

| Migration | Package |
|---|---|
| 00005 | WP-1 |
| 00006 | WP-2 |
| 00007 | WP-3 |
| 00008 | WP-5 |
| 00009 | WP-8 |
| 00010 | WP-4 |

- `emit()` gets **one** extension point now: `s.hooks []func(ctx, q, Event)`, called inside the transaction. WP-2 (notifications), WP-1 (callback_url) and WP-5 (metrics) register hooks instead of editing `emit`.

### Priority 0: finish what is in flight (🚧, the rules and watchers owners)

**WP-0 Rules and watchers completion.** Files: `app/rules.go`, `app/watchers.go`, `app/feeds.go`, `httpapi/routes_rules_watchers.go`, `web/app/(app)/{rules,watchers}`, `openapi.yaml` (rules and watchers block).
- REST wiring and OpenAPI for rules, watchers and presets.
- Web pages: rules editor with priority ordering and a "test against an item" dry-run endpoint `POST /v1/rules/test`; watchers list, status, run-now, pause/resume; preset picker.
- Add the `hn-show-ask` preset and the `url_hosts` rule condition.
- Emit `watcher.updated`, `watcher.deleted`, `watcher.degraded`, `watcher.paused`, `watcher.run`.
- Add `WATCHERS_ENABLED`.
- Optional in this WP: watcher `window` and `jitter` plus workspace `timezone`; scoring `{pattern, points}` plus `min_score`.

### Priority 1: core agent loop parity (highest value, unblocks SDK, MCP and CLI)

**WP-1 Approval core.** Files: `app/items.go` (only this WP edits it in P1), new `app/await.go`, `httpapi/routes_core.go`, migration 00005, OpenAPI items block.
1. `expires_in` / `expires_at` on create, a collection `default_expiry_seconds`, and a new `expired` status. Migration: rebuild the `items` status CHECK if there is one, and add `expired` to the default `allowed_statuses`.
2. An `editable` whitelist on the item plus a collection default; enforce it in `RunAction` (422).
3. Edit diff: a unified diff for `content` and a JSON diff for `metadata`, stored in the event `data.diff` and `data.final`.
4. `GET /v1/items/{id}/await?timeout=≤30`, a long-poll on an in-process `sync.Map[itemID]chan` signalled after commit in `mutateItem`.
5. Optional per-item `callback_url`: an outbox delivery to an ad-hoc URL with the workspace signing secret. Needs a `webhook_deliveries.target_url` nullable column, or a synthetic hidden webhook row.
6. `POST /v1/items/{id}/result` sugar.
7. `data.channel` on decision events, from a request header `X-Staqly-Channel` or the principal kind.

**WP-6 MCP parity** (depends on WP-1 items 4 and 6, and on WP-0 for watchers). Files: `mcpserver/mcpserver.go`, `cmd/mcp-stdio`.
- New tools: `await_decision` (progress notifications), `report_result`, `list_watchers`, `create_watcher`, `list_watcher_presets`, `create_watcher_from_preset`, `list_rules` (read only).
- Add `expires_in` and `editable` args to `create_item`.
- Untrusted wrapping.
- `mcp-stdio --remote URL` (proxy mode with `STAQLY_API_KEY`).
- `/.well-known/mcp/server-card.json`.

### Priority 2: notifications and approve-from-anywhere (biggest functional gap)

**WP-2 Notification channel framework plus outbound notifiers.** Files: new `app/notify/` (`channel.go`, `telegram.go`, `slack.go`, `discord.go`, `ntfy.go`, `email.go`), `app/channels.go`, `httpapi/routes_channels.go`, migration 00006, `web/app/(app)/settings/notifications`.
- `notification_channels` and `notification_queue` tables, enqueued through the `emit` hook for pending items and escalations.
- A worker with a per-channel digest window, auto-disable after N failures, and the SSRF-guarded client (reuse `safeClient`).
- CRUD plus `/test`; masked secrets.
- Telegram, Discord and Slack send media by upload (our media is private).
- ntfy with an `Authorization` option; SMTP email.
- Optional quiet hours.
- Scope `notifications:manage`.
- Wire the rule `escalate.channel_id` (one line in `rules.go`, coordinate with WP-0).

**WP-3 Interactive chat approvals plus one-tap links** (depends on WP-2). Files: `httpapi/routes_integrations.go`, `app/chatdecide.go`, `app/decidelinks.go`, migration 00007 (`decision_links`: hashed single-use tokens).
- Telegram callbacks, with webhook mode plus **`getUpdates` long-poll mode for local installs**, a `ForceReply` reject reason, and a `/start <code>` connect flow.
- Slack interactions (BYO app; Socket Mode for local) with a reject-reason modal.
- Discord interactions (Ed25519, PING) with a modal.
- Signed one-tap `GET/POST /v1/decide/{token}` used by ntfy `Actions` and email.
- Every path calls `RunAction`, so rules, version guard, webhooks and events stay consistent; the actor is `Telegram: @user` etc. and the channel is tagged.
- Approver allowlists per channel.

**WP-4 Web push plus PWA.** Files: `app/push.go`, `httpapi/routes_push.go`, migration 00010, `apps/web/public/{sw.js,manifest.webmanifest,icons}`, a `web/components/settings/push-toggle.tsx`.
- VAPID keys auto-generated into `/data` when no env is set.
- Register as a WP-2 notifier type `web_push`, or directly through the emit hook if WP-2 isn't ready.
- Docs: works on `localhost` and HTTPS only.

### Priority 3: trust, compliance, operations

**WP-5 Observability and hardening.** Files: new `internal/metrics`, `httpapi/ratelimit.go`, `httpapi/middleware_obs.go`, `cmd/server/main.go` (Sentry init), `apps/web` Sentry config, migration 00008 (only if a canary table is needed).
- `/ready` with the checks listed in §1.9.
- `/metrics` (opt-in, token or separate address).
- Sentry (API and web, no-op when unset).
- `X-Request-Id` response header.
- Per-principal and per-route rate limits with `429` and `Retry-After`.
- Rate limits on `/sessions/recover` and `/device-links/redeem`.

**WP-7 Audit export, retention, GDPR.** Files: `app/export.go`, `app/retention.go`, `httpapi/routes_export.go`, `app/events.go` (filter additions only; coordinate with the WP-2 hook), `web/components/activity` (export button), `web/components/settings` (danger zone).
- Events filters: `since`, `until`, type prefix, `actor_id`.
- `GET /v1/events/export` (NDJSON/CSV, formula-safe).
- `GET /v1/workspace/export` (streamed ZIP with media).
- `DELETE /v1/workspace/data` with a tombstone event; cloud `DELETE /v1/workspace`.
- Retention env and a daily prune job.
- `webhook.*` events.
- Optional hash chain (`events.hash`/`prev_hash` plus `/v1/events/verify`).

**WP-9 Self-host and deploy kit.** Files: `deploy/` (new), `apps/api/Dockerfile`, `docs/backup.md`, `docs/cloud.md`.
- Optional Litestream (`LITESTREAM_REPLICA_URL`).
- An uploads sync recipe.
- A systemd backup timer.
- `deploy/Caddyfile`, `compose.prod.yml`.
- A status-page recommendation (cloud).

### Priority 4: ecosystem

**WP-10 TypeScript SDK completion.** Files: `packages/sdk-typescript/**`. Add every missing method from §1.11, plus `awaitDecision`, `approvalGate`, iterators, and rules, watchers and channels once those APIs land (regenerate types from OpenAPI).

**WP-11 Python SDK.** Files: `packages/sdk-python/**` (new).
- Sync and async `httpx` clients, models, errors.
- `await_decision`, `@requires_approval`, `verify_webhook`, iterators.
- Tests against a live container (`scripts/e2e.sh` hook).

**WP-12 CLI.** Files: `apps/api/cmd/staqly/**` (new; HTTP only, no internal imports, apart from shared types if desired) plus a Dockerfile line. Commands as listed in §1.11.

**WP-13 Framework integrations and install docs** (depends on WP-10 and WP-11).
- Packages: `integrations/{claude-agent-sdk,langchain,crewai,openai-agents}`.
- Docs: `docs/install/{cursor,windsurf,claude-desktop}.md`, plus web pages `integrations/{cursor,windsurf}`.
- Agent instruction snippets for CLAUDE.md / AGENTS.md / `.cursorrules`.
- `docs/cookbook.md` and `examples/`.
- Make/Zapier docs.
- Optional n8n community node (needs WP-1 `callback_url`).

### Priority 5: cloud monetisation

**WP-8 Stripe billing plus operator API.** Files: `app/billing.go`, `app/admin.go`, `httpapi/routes_billing.go`, migration 00009, `app/plans.go` (price IDs), `web/components/settings/plan-*`.
- Checkout, portal and webhook.
- `OPERATOR_WORKSPACE_ID` with `/v1/admin/stats` and `/v1/admin/workspaces/{id}/plan`.
- Billing is disabled when `STRIPE_SECRET_KEY` is unset (local is always unlocked).

### Small UX items (bundle into whichever web WP is free)

- Inbox: `/` search, `q` box, time range, `Shift+R`, diff view, a "sample item" empty state.

### Dependency summary

| Package | Depends on |
|---|---|
| WP-0, WP-1, WP-5, WP-9, WP-12 (core commands) | Nothing; can start immediately |
| WP-2 | Only the shared `emit` hook (land that first; a 10-line PR) |
| WP-3 | WP-2 |
| WP-4 | WP-2 (soft) |
| WP-6 | WP-0 + WP-1 |
| WP-7 | Nothing (touches `events.go` filters; coordinate with the hook PR) |
| WP-10 | WP-1 / WP-0 / WP-2 APIs (incremental) |
| WP-11, WP-13 | WP-1 |
| WP-8 | Nothing (cloud only; lowest priority) |

---

## 3. Impri features that are not applicable (or deliberately different)

| Impri feature | Why it's not applicable or different for staq.ly |
|---|---|
| Single per-project `webhook_secret` with `rotate-webhook-secret`; single `action.updated` event; unsigned channel webhooks | We already have per-endpoint secrets, typed events, a transactional outbox, event and delivery IDs, and always-signed requests. Only add a per-endpoint **secret rotation** endpoint (`POST /v1/webhooks/{id}/rotate-secret` with a dual-secret grace period), which is S. |
| Agent self-approval with the same scope | Deliberately prevented (`items:write` vs `items:review`). |
| UI auth by pasting an admin API key into `localStorage` | We use HttpOnly session cookies with CSRF and keyless local mode; we keep that. |
| argon2 key hashing | Unnecessary for 256-bit random keys; SHA-256 is adequate (documented in architecture.md). |
| Redis for a multi-instance rate limit | Single Go process with a SQLite writer by design. |
| `reddit_search` watcher kind (Reddit JSON API) | The RSS search preset covers it; it avoids API-terms issues. |
| `require_n_approvers` as shipped | Dead code in Impri. Implement only if multi-approver becomes a real requirement (see §1.1), and do it properly. |
| Slack OAuth "Add to Slack" shared app | Needs a published Slack app owned by the operator, which makes sense only for the hosted cloud. Self-host and local use BYO app or Socket Mode. |
| Product feedback endpoint and admin feedback inbox; activation funnel tied to `kind = 'demo'` | SaaS growth tooling for the Impri operator; at most an optional cloud nicety. |
| Marketing and SEO docs (about 150 use-case pages), privacy and terms for `impri.dev` | Content marketing, not product parity; only needed if we run a public cloud. |
| Fly.io-specific deploy (`fly.toml`, `fly-backup.sh`), GitLab CI | Platform choices; we have Docker Compose and Make. A generic Caddy/compose prod kit is enough. |
| Cloudflare Worker status page | Only for a hosted cloud; any uptime monitor will do. |
| Watcher "triage" as the only item type | Our watchers write into a chosen collection as normal items, a superset. |
| Impri's action `kind` free-text taxonomy | Collections, `type` and `source` replace it; rules match on those instead of `kind_pattern`. |
| i18n | Impri has none either; not a parity gap. |
