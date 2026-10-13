# Changelog

All notable changes to `@staqly/sdk` are documented here. The project follows
[Semantic Versioning](https://semver.org/).

## 0.1.0 — unreleased

First public release: a rewrite of the internal 1.0.0 client.

### Added
- Resource namespaces: `collections`, `items`, `media`, `events`, `webhooks` (+ `deliveries`),
  `rules`, `watchers` (+ presets, from-preset, run), `workspace`, `workspaces` (cloud signup),
  `plans`, `apiKeys`, `notificationChannels`, `exports`; plus `health()`, `ready()` and `whoami()`.
- `exports.events()` streams the NDJSON activity export; `exports.eventsResponse()` (CSV/NDJSON);
  workspace export jobs (`exportWorkspace`, `waitForWorkspaceExport`, `downloadWorkspaceExport`);
  `items.purge()`; `saveResponse()` in `@staqly/sdk/node`.
- `items.upload()` (multipart create), `items.search()`, `items.bulkAction()`, `items.versions()`,
  `items.history()`, `items.listActions()`, `items.reportResult()`.
- `items.waitForDecision()` on the server long-poll (`/v1/items/{id}/wait`), `items.wait()`.
- `requestApproval()` and `requiresApproval()` approval gates (support `expires_in`, `editable`).
- Auto-paginating async iterators: `items.iterate()`, `events.iterate()`.
- Typed errors (`AuthenticationError`, `PlanLimitError`, `ValidationError`, `RateLimitError`, …).
- Retries with exponential backoff + jitter, `Retry-After`, automatic `Idempotency-Key`,
  per-request timeouts and `AbortSignal`, custom `fetch`, `STAQLY_API_URL` / `STAQLY_API_KEY`.
- `verifyWebhook()` returns the typed event and throws `WebhookVerificationError`; timestamp
  tolerance; `computeWebhookSignature()`, `isValidWebhook()`; edge-safe `@staqly/sdk/webhooks` entry.
- `@staqly/sdk/node` with `fileFromPath()` and `saveResponse()`.
- ESM-only package built with `tsc` (TypeScript 7); Node ≥ 20.19 (CommonJS callers can
  `require()` it there). oxlint, vitest unit tests, live integration tests (`STAQLY_LIVE=1`).

### Changed (from the internal 1.0.0 client)
- Flat methods (`createItem`, `approve`, `listWebhooks`, …) moved into resource namespaces.
- `verifyWebhook(secret, body, headers)` now takes the request headers and returns the parsed
  event (it used to return a boolean); use `isValidWebhook()` for a boolean.
- `StaqlyError` is now the base class; HTTP failures are `APIError` subclasses.
