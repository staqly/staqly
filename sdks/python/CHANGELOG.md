# Changelog

All notable changes to the `staqly` Python package are documented here. The
project follows [Semantic Versioning](https://semver.org/).

## 0.1.0 — unreleased

First release.

### Added
- Sync `Staqly` (httpx.Client) and async `AsyncStaqly` (httpx.AsyncClient) with an identical
  surface (the sync client is generated from the async one).
- Resources: `collections`, `items`, `media`, `events`, `webhooks` (+ `deliveries`),
  `watchers` (+ run), `workspace`, `workspaces` (cloud signup), `plans`,
  `api_keys`, `notification_channels`, `exports`, `billing`, `sessions`, `device_links`,
  `decision_links`; plus `health()`, `ready()`, `live()`, `setup()`, `metrics()`, `openapi()`, `whoami()` and a
  low-level `request()`.
- `collections.list_items()`, `workspace.set_plan()`, `workspace.delete()`,
  `workspace.rotate_recovery_key()`.
- `exports.events()` streams the NDJSON activity export; `exports.save_events()` (CSV/NDJSON to a
  file); workspace export jobs (`export_workspace`, `wait_for_workspace_export`,
  `save_workspace_export`, `download_workspace_export`); `items.purge()`.
- `items.upload()` from a path, bytes or file object; `items.search()`, `bulk_action()`,
  `history()`, `versions()`, `list_actions()`, `report_result()`.
- `items.wait_for_decision()` on the server long-poll; `items.wait()`.
- `request_approval()` and the `@requires_approval` decorator (support `expires_in`, `editable`).
- Lazy pagination generators: `items.iterate()`, `events.iterate()` (sync and async).
- pydantic v2 models checked against `apps/api/openapi.yaml` by the test suite; `py.typed`.
- Typed errors, retries with backoff + jitter, `Retry-After`, automatic `Idempotency-Key`,
  `STAQLY_API_URL` / `STAQLY_API_KEY`.
- `verify_webhook()` → typed `WebhookEvent` (raises `WebhookVerificationError`),
  `is_valid_webhook()`, `compute_signature()`.

### Removed
- Removed: rules (the API no longer has them).
- Removed: watcher presets, `watchers.list_presets()`, `watchers.create_from_preset()` and the
  `WatcherPreset` model (the API no longer has `/v1/watcher-presets` or
  `/v1/watchers/from-preset`, nor `preset_id` on a watcher); create watchers with a feed or page URL.

### Security
- `verify_webhook` accepts only ASCII digits in `X-Staqly-Timestamp`;
  `is_valid_webhook` never raises, whatever the input.
- The API key is stored privately and redacted from `repr()` of the clients and
  their HTTP transport (`client.api_key` stays available read-only).
