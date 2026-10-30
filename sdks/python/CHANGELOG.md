# Changelog

All notable changes to the `staqly` Python package are documented here. The
project follows [Semantic Versioning](https://semver.org/).

## 0.1.0 — unreleased

First release, rebuilt for the posts pivot (2026-10-19): staqly is a scheduler for posts your
agents write, no longer a review inbox.

### Added
- The rest of what an API key can do: `channels.options`, `channels.telegram_connect` /
  `telegram_connect_status`, `providers.check`, `media.replace`, and the design editor's endpoints:
  `media_projects` (list, get, create, update, delete, file), `design` (brand, set_brand), `ai`
  (status, text, `jobs`: create, get, result, accept, discard) and `stock` (photos, use). Models
  `ChannelOption`, `TelegramConnect`, `ProviderCheck`, `MediaProject`, `DesignBrand`, `StockPage`,
  `StockPhoto`, `AIStatus`, `AIOperation`, `AIUsage`, `AIJob`.
- Local mode needs its access token: pass it as `api_key` or `STAQLY_API_KEY` (`staqly serve token`);
  `workspace.access_token()` and `workspace.rotate_access_token()` (models `AccessToken`,
  `AccessTokenRotated`); `sessions.sign_in_with_api_key` takes the token in local mode.
  `PermissionDeniedError.is_not_local` is deprecated (servers no longer send `not_local`).
- Resources `posts` (create, update, schedule, publish, cancel, duplicate, retry_target,
  report_result, preview, history, tags, purge, `wait_for_published`), `channels` (connect, update,
  enable/disable, remove, test, `oauth_start`), `providers` (list, set_app, delete_app), `schedules`
  (CRUD, enable/disable, run), `jobs` (list, iterate, stats, get, retry, cancel), `work` and `alerts`.
- `title` on `Post`; `posts.create` requires it (`title=...`, right after the collection; at most 300
  characters: platforms with a title publish it, the others show it in staqly) and `posts.update`
  takes it (it can't be emptied). `ProviderField.from_post`: the field (title, tags) is filled
  from the post.
- `repeat_every` on `Post` and on `posts.create` / `posts.update` (`1d` to `6d`, `1w`, `2w`, `1mo`; `""`
  stops it); `ProviderField.when` for fields that apply only to one choice of another field.
- Models `Post`, `PostTarget`, `PostTargetInput`, `Channel`, `Provider`, `Schedule`, `ScheduleRule`,
  `Job`, `JobStats`, `Alert`, `WorkItem`; `WebhookEvent.post`.
- `analytics.get(from_=..., to=..., days=..., channel=..., collection=..., source=...)`
  (`GET /v1/analytics`): totals against the period before, the queue, daily counts, channels,
  sources, posting times, top posts, failures and feedback for a range of days (`from_` / `to` as
  `str` or `date`). Models `Analytics`, `AnalyticsRange`, `AnalyticsTotals`, `AnalyticsMetrics`,
  `AnalyticsQueue`, `AnalyticsDay`, `ChannelAnalytics`, `MetricsCoverage`, `AnalyticsSource`,
  `AnalyticsTimeSlot`, `PostingTimes`, `TopPosts`, `TopPost`, `TopPostChannel`, `AnalyticsFailure`,
  `AnalyticsFeedback`.

### Removed
- `items`, `watchers`, `notification_channels`, `decision_links`, `request_approval`,
  `requires_approval`, `RejectedError`; `collections.set_actions` and `list_items` (now `list_posts`).

### Earlier (review-inbox era)
- Watchers on JSON and XML sources: `kind="json"` / `"xml"` with `extract` (`WatcherExtract` or a
  dict: `format`, `items`, `title`, optional `url`, `id`, `content`, `published`, `author` paths)
  on `watchers.create()` / `update()`, and `watchers.preview(url, ...)` (`POST /v1/watchers/preview`:
  `WatcherPreview` with `detected_kind`, sample `entries`, `suggestions` of item paths).
  `Watcher.extract`.
- Scheduled publishing: `publish_at` on `items.create()`, `items.approve()` (approve only),
  `items.update()` (`""` clears), `request_approval()` / `requires_approval()`; `publish_after` /
  `publish_before` filters and the `publish_asc` / `publish_desc` sorts on `items.list()`;
  `Item.publish_at`.
- Sync `Staqly` (httpx.Client) and async `AsyncStaqly` (httpx.AsyncClient) with an identical
  surface (the sync client is generated from the async one).
- Resources: `collections`, `items`, `media`, `events`, `webhooks` (+ `deliveries`),
  `watchers` (+ run), `workspace`, `workspaces` (cloud signup), `plans`,
  `api_keys`, `notification_channels`, `exports`, `billing`, `sessions`, `device_links`,
  `decision_links`; plus `health()`, `ready()`, `live()`, `setup()`, `metrics()`, `openapi()`, `whoami()` and a
  low-level `request()`.
- `sessions.sign_in_with_api_key()` (`POST /v1/sessions/api-key`, cloud): exchanges an `admin`
  API key for a session cookie; returns `SessionCreated`.
- `sessions.recover()` returns `SessionRecovered`: the CSRF token, a new admin API key
  (`api_key.key`) and the next recovery key. Recovery keys are single use; the key returned by
  `workspaces.create()` is an admin key (`"Admin key"`, scopes `["admin"]`).
- `collections.list_items()`, `workspace.set_plan()`, `workspace.delete()`,
  `workspace.rotate_recovery_key()`.
- `exports.events()` streams the NDJSON activity export; `exports.save_events()` (CSV/NDJSON to a
  file); workspace export jobs (`export_workspace`, `wait_for_workspace_export`,
  `save_workspace_export`, `download_workspace_export`); `items.purge()`.
- `items.upload()` from a path, bytes or file object; `items.search()`, `bulk_action()`,
  `history()`, `versions()`, `list_actions()`, `report_result()`, `unarchive()`, `summarize()`;
  `expected_version` on `approve`/`reject`/`action` (409 on a stale version).
- `workspace.agents()` and `workspace.forget_agent()` (`/v1/agents`, admin); `workspace.usage()`
  includes `items_by_status` and `webhook_deliveries`; `decision_links.preview()`; `webhooks.deliveries.list(item_id=...)`.
- `items.wait_for_decision()` on the server long-poll; `items.wait()`.
- `request_approval()` and the `@requires_approval` decorator (support `expires_in`, `editable`).
- Lazy pagination generators: `items.iterate()`, `events.iterate()` (sync and async).
- pydantic v2 models checked against `apps/api/openapi.yaml` by the test suite; `py.typed`.
- Typed errors, retries with backoff + jitter, `Retry-After`, automatic `Idempotency-Key`,
  `STAQLY_API_URL` / `STAQLY_API_KEY`.
- `verify_webhook()` → typed `WebhookEvent` (raises `WebhookVerificationError`),
  `is_valid_webhook()`, `compute_signature()`.

### Fixed
- `request_approval()` / `requires_approval()` read `item.decision`: an approval in a collection
  that publishes automatically (the item is already `processing`) is approved, instead of
  reading as not approved and raising `RejectedError`.
- One-tap decision link errors (`decision_links.decide()`) are typed with the server's reason
  (`ConflictError` "This item is already approved.", `GoneError`), not `http_error`;
  `DecisionResult.error` added.

### Removed
- Removed: rules (the API no longer has them).
- Removed: watcher presets, `watchers.list_presets()`, `watchers.create_from_preset()` and the
  `WatcherPreset` model (the API no longer has `/v1/watcher-presets` or
  `/v1/watchers/from-preset`, nor `preset_id` on a watcher); create watchers with a feed or page URL.
- Removed: custom collection actions. A collection's `actions` is now its flow from the fixed
  catalog (`regenerate`, `publish`, `retry`, `archive`; `ActionName`), set with `ActionFlow`
  (`name`, `allowed_statuses`, `auto_run`) or plain dicts in `collections.create()`, `update()`
  and `set_actions()`; full `ActionDef` objects from a GET are still accepted. `ActionDef` gains
  `auto_run` (publish starts right after approval).

### Security
- **Breaking (server):** deleting needs its own scope. `Scope` gains `items:delete`
  (`items.delete()`, `media.delete()`) and `collections:delete` (`collections.delete()`);
  `items:write` and `collections:write` no longer allow deleting (403, `PermissionDeniedError`).
  Existing keys are not widened: rotate or recreate a key with the delete scope it needs.
  `admin` keys, dashboard sessions and local mode keep every scope; `items.purge()` stays
  `admin`.
- `verify_webhook` accepts only ASCII digits in `X-Staqly-Timestamp`;
  `is_valid_webhook` never raises, whatever the input.
- The API key is stored privately and redacted from `repr()` of the clients and
  their HTTP transport (`client.api_key` stays available read-only).
