# Changelog

All notable changes to `@staqly/sdk` are documented here. The project follows
[Semantic Versioning](https://semver.org/).

## 0.1.0 — unreleased

First public release, rebuilt for the posts pivot (2026-10-19): staqly is a scheduler for posts
your agents write, no longer a review inbox.

### Added
- Local mode needs its access token: pass it as `apiKey` or `STAQLY_API_KEY` (`staqly serve token`);
  `workspace.accessToken()` and `workspace.rotateAccessToken()`; `sessions.signInWithApiKey` takes the
  token in local mode. `PermissionDeniedError.isNotLocal` is deprecated (servers no longer send `not_local`).
- Resource namespaces `posts` (create, update, schedule, publish, cancel, duplicate, retryTarget,
  reportResult, preview, history, tags, purge, `waitForPublished`), `channels` (connect, update,
  enable/disable, remove, test, `oauthStart`), `providers` (list, setApp, deleteApp), `schedules`
  (CRUD, enable/disable, run), `jobs` (list, stats, get, retry, cancel), `work` and `alerts`.
- `PostEventData` for `post.*` webhook payloads; `SCOPES` lists the 16 scopes.
- `repeat_every` on posts (`RepeatEvery`: `1d` to `6d`, `1w`, `2w`, `1mo`): once published, a copy is
  scheduled that much later; `posts.update(id, { repeat_every: null })` stops it.
- `analytics.get({ from, to, days, channel, collection, source })` (`GET /v1/analytics`): totals against the
  period before, the queue, daily counts, channels, sources, posting times, top posts, failures and
  feedback for a range of days. Types `Analytics`, `AnalyticsParams`, `AnalyticsTotals`,
  `AnalyticsMetrics`, `ChannelAnalytics`, `AnalyticsTimeSlot`.

### Removed
- `items`, `watchers`, `notificationChannels`, `decisionLinks`, `requestApproval`,
  `requiresApproval`, `RejectedError`, `ItemEventData`; `collections.setActions` and `listItems`
  (now `listPosts`).

### Earlier (review-inbox era)
- Watchers on JSON and XML sources: `kind: "json" | "xml"` with `extract` (`WatcherExtract`: `format`,
  `items`, `title`, optional `url`, `id`, `content`, `published`, `author` paths) on
  `watchers.create()` / `update()`, and `watchers.preview(input)` (`POST /v1/watchers/preview`:
  `detected_kind`, sample `entries`, `suggestions` of item paths) to test a source first.
  Types `WatcherKind`, `WatcherExtract`, `WatcherPreviewInput`, `WatcherPreview`,
  `WatcherPreviewEntry`, `WatcherPathSuggestion`.
- Resource namespaces: `collections`, `items`, `media`, `events`, `webhooks` (+ `deliveries`),
  `watchers` (+ run), `workspace`, `workspaces` (cloud signup),
  `plans`, `apiKeys`, `billing`, `notificationChannels`, `exports`, `sessions`, `deviceLinks`,
  `decisionLinks`;
  plus `health()`, `live()`, `ready()`, `setup()`, `openapi()`, `metrics()` and `whoami()`.
- `workspace.setPlan()`, `workspace.rotateRecoveryKey()`, `workspace.delete()`,
  `collections.listItems()`: every operation in `openapi.yaml` has a method, except the chat
  integration callbacks (Telegram, Slack, Discord), the Stripe webhook (`/v1/billing/webhook`)
  and `/mcp`. `decisionLinks.preview()` returns the HTML decision page.
- `sessions.signInWithApiKey()` (`POST /v1/sessions/api-key`, cloud): exchanges an `admin` API key
  for a browser session; returns `SessionCreated`.
- `sessions.recover()` returns `SessionRecovered`: the CSRF token, a new admin API key
  (`api_key.key`) and the next recovery key. Recovery keys are single use; the key returned by
  `workspaces.create()` is an admin key (`"Admin key"`, scopes `["admin"]`).
- `billing.get()`, `billing.checkout()`, `billing.portal()` (Stripe subscription; cloud, `admin`).
- `exports.events()` streams the NDJSON activity export; `exports.eventsResponse()` (CSV/NDJSON);
  workspace export jobs (`exportWorkspace`, `waitForWorkspaceExport`, `downloadWorkspaceExport`);
  `items.purge()`; `saveResponse()` in `@staqly/sdk/node`.
- `items.upload()` (multipart create), `items.search()`, `items.bulkAction()`, `items.versions()`,
  `items.history()`, `items.listActions()`, `items.reportResult()`, `items.unarchive()`,
  `items.summarize()`; `expected_version` on `approve`/`reject`/`action` (409 on a stale version).
- `workspace.agents()` and `workspace.forgetAgent()` (`/v1/agents`, admin); `workspace.usage()`
  includes `items_by_status` and `webhook_deliveries`.
- `items.waitForDecision()` on the server long-poll (`/v1/items/{id}/wait`), `items.wait()`.
- `requestApproval()` and `requiresApproval()` approval gates (support `expires_in`, `editable`,
  `publish_at`).
- Scheduled publishing: `publish_at` on `items.create()`, `items.approve()` (approve only) and
  `items.update()` (`null` clears), `publish_after` / `publish_before` filters and the
  `publish_asc` / `publish_desc` sorts on `items.list()`; `Item.publish_at`.
- Auto-paginating async iterators: `items.iterate()`, `events.iterate()`.
- Typed errors (`AuthenticationError`, `PlanLimitError`, `ValidationError`, `RateLimitError`, ...).
- Retries with exponential backoff + jitter, `Retry-After`, automatic `Idempotency-Key`,
  per-request timeouts and `AbortSignal`, custom `fetch`, `STAQLY_API_URL` / `STAQLY_API_KEY`.
- `verifyWebhook()` returns the typed event and throws `WebhookVerificationError`; timestamp
  tolerance; `computeWebhookSignature()`, `isValidWebhook()`; edge-safe `@staqly/sdk/webhooks` entry.
- `@staqly/sdk/node` with `fileFromPath()` and `saveResponse()`.
- ESM-only package built with `tsc` (TypeScript 7); Node ≥ 20.19 (CommonJS callers can
  `require()` it there). oxlint, vitest unit tests, live integration tests (`STAQLY_LIVE=1`).

### Fixed
- `requestApproval()` / `requiresApproval()` read `item.decision`: an approval in a collection
  that publishes automatically (the item is already `processing`) is approved, instead of
  reading as not approved and throwing `RejectedError`.
- One-tap decision link errors (`decisionLinks.decide()`) are typed with the server's reason
  (`ConflictError` "This item is already approved.", `GoneError`), not `http_error`.

### Removed
- Removed: rules (the API no longer has them).
- Removed: watcher presets, `watchers.listPresets()`, `watchers.createFromPreset()` and the
  `WatcherPreset` type (the API no longer has `/v1/watcher-presets` or
  `/v1/watchers/from-preset`, nor `preset_id` on a watcher); create watchers with a feed or page URL.
- Removed: custom collection actions. A collection's `actions` is now its flow from the fixed
  catalog (`regenerate`, `publish`, `retry`, `archive`; `ActionName`), set with
  `ActionFlowInput` (`{ name, allowed_statuses?, auto_run? }`) in `collections.create()`,
  `update()` and `setActions()`; full `ActionDef` objects from a GET are still accepted.
  `ActionDef` gains `auto_run` (publish starts right after approval).

### Security
- **Breaking (server):** deleting needs its own scope. `Scope` and `SCOPES` gain
  `items:delete` (`items.delete()`, `media.delete()`) and `collections:delete`
  (`collections.delete()`); `items:write` and `collections:write` no longer allow deleting
  (403 `forbidden`). Existing keys are not widened: rotate or recreate a key with the delete
  scope it needs. `admin` keys, dashboard sessions and local mode keep every scope;
  `items.purge()` stays `admin`.
- The API key is a private field (`#apiKey`, read through the `http.apiKey` getter): not
  enumerable, and redacted from `util.inspect`/`console.log` and `JSON.stringify`.
- Raw (streamed) responses — `media.downloadResponse`, event/workspace exports — stay
  abortable through `options.signal` until the body has been consumed.

### Changed (from the internal 1.0.0 client)
- Flat methods (`createItem`, `approve`, `listWebhooks`, ...) moved into resource namespaces.
- `verifyWebhook(secret, body, headers)` now takes the request headers and returns the parsed
  event (it used to return a boolean); use `isValidWebhook()` for a boolean.
- `StaqlyError` is now the base class; HTTP failures are `APIError` subclasses.
