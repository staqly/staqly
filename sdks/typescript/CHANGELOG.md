# Changelog

All notable changes to `@staqly/sdk` are documented here. The project follows
[Semantic Versioning](https://semver.org/).

## 0.1.0 — unreleased

First public release: a rewrite of the internal 1.0.0 client.

### Added
- Resource namespaces: `collections`, `items`, `media`, `events`, `webhooks` (+ `deliveries`),
  `watchers` (+ run), `workspace`, `workspaces` (cloud signup),
  `plans`, `apiKeys`, `billing`, `notificationChannels`, `exports`, `sessions`, `deviceLinks`,
  `decisionLinks`;
  plus `health()`, `live()`, `ready()`, `setup()`, `openapi()`, `metrics()` and `whoami()`.
- `workspace.setPlan()`, `workspace.rotateRecoveryKey()`, `workspace.delete()`,
  `collections.listItems()`: every operation in `openapi.yaml` has a method, except the chat
  integration callbacks (Telegram, Slack, Discord), the Stripe webhook (`/v1/billing/webhook`),
  `/mcp` and the HTML decision page.
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
  `items.history()`, `items.listActions()`, `items.reportResult()`.
- `items.waitForDecision()` on the server long-poll (`/v1/items/{id}/wait`), `items.wait()`.
- `requestApproval()` and `requiresApproval()` approval gates (support `expires_in`, `editable`).
- Auto-paginating async iterators: `items.iterate()`, `events.iterate()`.
- Typed errors (`AuthenticationError`, `PlanLimitError`, `ValidationError`, `RateLimitError`, ...).
- Retries with exponential backoff + jitter, `Retry-After`, automatic `Idempotency-Key`,
  per-request timeouts and `AbortSignal`, custom `fetch`, `STAQLY_API_URL` / `STAQLY_API_KEY`.
- `verifyWebhook()` returns the typed event and throws `WebhookVerificationError`; timestamp
  tolerance; `computeWebhookSignature()`, `isValidWebhook()`; edge-safe `@staqly/sdk/webhooks` entry.
- `@staqly/sdk/node` with `fileFromPath()` and `saveResponse()`.
- ESM-only package built with `tsc` (TypeScript 7); Node ≥ 20.19 (CommonJS callers can
  `require()` it there). oxlint, vitest unit tests, live integration tests (`STAQLY_LIVE=1`).

### Removed
- Removed: rules (the API no longer has them).
- Removed: watcher presets, `watchers.listPresets()`, `watchers.createFromPreset()` and the
  `WatcherPreset` type (the API no longer has `/v1/watcher-presets` or
  `/v1/watchers/from-preset`, nor `preset_id` on a watcher); create watchers with a feed or page URL.

### Security
- The API key is a private field (`#apiKey`, read through the `http.apiKey` getter): not
  enumerable, and redacted from `util.inspect`/`console.log` and `JSON.stringify`.
- Raw (streamed) responses — `media.downloadResponse`, event/workspace exports — stay
  abortable through `options.signal` until the body has been consumed.

### Changed (from the internal 1.0.0 client)
- Flat methods (`createItem`, `approve`, `listWebhooks`, ...) moved into resource namespaces.
- `verifyWebhook(secret, body, headers)` now takes the request headers and returns the parsed
  event (it used to return a boolean); use `isValidWebhook()` for a boolean.
- `StaqlyError` is now the base class; HTTP failures are `APIError` subclasses.
