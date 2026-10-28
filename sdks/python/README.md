# staqly

Official Python SDK for **[staqly](https://github.com/staqly/staqly)**, the local-first scheduler
for posts your agents write: posts live in collections, go out to connected channels at their
time, on a schedule, or right away, and your own automation can be a channel too.

- Sync `Staqly` and async `AsyncStaqly` clients with identical APIs, built on `httpx`.
- Typed pydantic v2 models, checked against the API's OpenAPI contract, with `py.typed` and
  `mypy --strict` clean.
- Retries with exponential backoff and jitter, `Retry-After` handling, automatic idempotency keys
  and timeouts.
- Typed errors, auto-paginating generators, webhook verification and `wait_for_published`.
- Python ≥ 3.10.

```bash
pip install staqly        # or: uv add staqly
```

## Contents

- [Quickstart](#quickstart)
- [Configuration](#configuration)
- [Collections](#collections)
- [Channels and providers](#channels-and-providers)
- [Posts](#posts)
- [Your own automation](#your-own-automation)
- [Schedules](#schedules)
- [Jobs](#jobs)
- [Media](#media)
- [Analytics](#analytics)
- [Events](#events)
- [Webhooks](#webhooks)
- [Alerts](#alerts)
- [Exports and purge](#exports-and-purge)
- [Workspace, plans, billing and API keys](#workspace-plans-billing-and-api-keys)
- [Sessions and device links](#sessions-and-device-links)
- [Pagination](#pagination)
- [Errors](#errors)
- [Retries, timeouts and idempotency](#retries-timeouts-and-idempotency)
- [Async usage](#async-usage)
- [Example: an agent that writes and publishes](#example-an-agent-that-writes-and-publishes)

## Quickstart

**Staqly Community Edition.** A staqly server on your machine, started with `staqly serve` (or the `ghcr.io/staqly/staqly` Docker image),
wants its access token, which it prints when it starts. Put it in `STAQLY_API_KEY`
(`export STAQLY_API_KEY=$(staqly serve token)`, with Docker `$(docker exec staqly server token)`):

```python
from staqly import Staqly

staqly = Staqly()  # STAQLY_API_URL (Cloud: https://app.staqly.ai) and STAQLY_API_KEY

channel = staqly.channels.list()[0]  # connected in the dashboard
post = staqly.posts.create(
    "Blog posts",
    content="Launch day! Three things we shipped this week...",
    targets=[{"channel_id": channel.id}],
    publish_at="2026-11-03T09:00:00Z",
    status="scheduled",
)

# or publish right away and wait for the platform's answer
now = staqly.posts.create(
    "Blog posts", content="Hello", targets=[{"channel_id": channel.id}], publish_now=True
)
done = staqly.posts.wait_for_published(now.id)
print(done.status, done.targets[0].external_url)
```

**Cloud mode.** Use a hosted instance with an API key (`stk_...`):

```python
staqly = Staqly(base_url="https://staqly.example.com", api_key=os.environ["STAQLY_API_KEY"])
```

Don't have a workspace yet? Create one programmatically. It starts on a free 7-day trial (Pro); when it ends the workspace is read-only until a plan is paid for (`billing.checkout(plan=..., interval=...)`). The response
holds an admin key (scope `admin`, also the dashboard sign-in credential) and a single-use recovery
key, both shown only once:

```python
created = Staqly(base_url="https://staqly.example.com").workspaces.create(name="My agents")
print(created.api_key.key)  # stk_... store it now
```

Use the client as a context manager (`with Staqly() as staqly:`), or call `staqly.close()`, to
release connections.

## Configuration

```python
Staqly(
    base_url="http://localhost:3333",  # default: $STAQLY_API_URL, then http://localhost:3333
    api_key="stk_...",  # default: $STAQLY_API_KEY; "" = ignore the env var
    timeout=60.0,  # seconds per request (or an httpx.Timeout)
    max_retries=2,  # 408/429/5xx/network errors
    default_headers={"X-Team": "growth"},
    user_agent="my-agent/1.0",  # appended to "staqly-sdk-python/0.1.0"
    http_client=httpx.Client(proxy="http://proxy:8080"),  # your own client/transport
)
```

Every method accepts `request_options=` for per-request overrides:

```python
staqly.posts.list(status="scheduled", request_options={"timeout": 5, "max_retries": 0, "headers": {...}})
staqly.posts.create("Posts", content="x", request_options={"idempotency_key": f"job-{job_id}"})
```

`staqly.health()` checks liveness and `staqly.ready()` checks readiness (database, migrations and
workers); `staqly.live()` is the process-only probe (`/livez`). `staqly.setup()` reports the server
mode, whether the workspace exists (local) and whether signup is open (cloud). `staqly.whoami()`
returns the server mode, the workspace, and the calling principal with its scopes.
`staqly.metrics(token=None)` returns the Prometheus text (from this machine, or with the server's
`METRICS_TOKEN`) and `staqly.openapi()` the server's OpenAPI YAML. `staqly.request("GET", "/v1/...")` calls any endpoint with
the same auth, retries and error handling.

## Collections

Collections group posts and carry the schedules that publish them.

```python
col = staqly.collections.create(
    "Blog posts",
    description="Everything about the product",
    processing_timeout_seconds=3600,  # how long your own automation may take to report a result
)
staqly.collections.list(include_archived=False)
staqly.collections.get("Blog posts")  # ID or exact name
staqly.collections.list_posts(col.id, status="draft", limit=50)  # one PostPage
staqly.collections.update(col.id, color="#e1306c", clear_processing_timeout=True)
staqly.collections.archive(col.id)  # unarchive(col.id)
staqly.collections.delete(col.id, confirm="Blog posts")  # permanent; the name must match
```

## Channels and providers

A channel is a connected account. `providers.list()` is the catalog: what staqly can publish to,
how each platform authenticates (`oauth2`, `token` or `none`), its limits and the fields it needs.

```python
providers = staqly.providers.list()
telegram = next(p for p in providers if p.id == "telegram")
telegram.credential_fields  # [ProviderField(key="bot_token", kind="secret", required=True), ...]

# token platforms and your own automation connect here
tg = staqly.channels.connect(
    "telegram",
    name="Announcements",
    fields={"bot_token": TOKEN, "chat_id": "@acme"},  # stored sealed
)
mine = staqly.channels.connect("custom", name="My pipeline")  # see "Your own automation"

# OAuth platforms: open the link in a browser signed in to the platform
url = staqly.channels.oauth_start("mastodon", instance="mastodon.social")
staqly.providers.set_app("mastodon", client_id=..., client_secret=...)  # the workspace's own developer app

staqly.channels.list()
staqly.channels.get(tg.id)
staqly.channels.update(tg.id, name="News", settings={"silent": True})  # defaults for new targets
staqly.channels.update(tg.id, fields={"bot_token": "fresh"})  # reconnect
staqly.channels.test(tg.id)  # ok / error / profile: checks the credentials against the platform
staqly.channels.options(slack_channel_id, "channel")  # the choices of a remote setting, from the platform
staqly.providers.check("instagram")  # this server's setup for an OAuth platform, without an account
code = staqly.channels.telegram_connect()  # send code.command in the chat
staqly.channels.telegram_connect_status(code.code)  # connected yet?
staqly.channels.disable(tg.id)  # targets on it fail without trying; enable(id) turns it back on
staqly.channels.remove(tg.id)  # targets on it go with it
```

A channel's `status` is `connected`, `needs_reconnect` (the platform refused the credentials; an
alert and the `channel.needs_reconnect` event tell you) or `disabled`.

## Posts

```python
from staqly.models import PostTargetInput

media = staqly.media.upload("cover.png")
post = staqly.posts.create(
    "Blog posts",  # id or exact name
    content="Launch day!",
    media=[media.id],
    targets=[
        {"channel_id": tg.id},
        PostTargetInput(channel_id=bsky.id, content="Launch day! (short)", settings={"lang": "en"}),
    ],
    tags=["launch"],
    metadata={"campaign": "q4"},
    source="my-agent",
)

staqly.posts.schedule(post.id, "2026-11-03T09:00:00Z")  # scheduled: goes out then (a datetime works too)
staqly.posts.cancel(post.id)  # back to draft, the time kept as a note
staqly.posts.publish(post.id)  # now: the post becomes publishing
staqly.posts.wait_for_published(post.id, timeout=60)  # published or failed (check targets[].error)
staqly.posts.retry_target(post.id, bsky.id)  # try a failed target again

staqly.posts.update(
    post.id, content="Edited", expected_version=post.version
)  # drafts, scheduled and failed posts
staqly.posts.update(post.id, clear_publish_at=True)  # unschedule
staqly.posts.preview(post.id)  # the text each target carries and what its provider objects to
staqly.posts.duplicate(post.id)  # a new draft with the same content and targets
staqly.posts.history(post.id)  # events, newest first
staqly.posts.delete(post.id)  # history is kept; purge(id) removes it too (admin)

# list and search
page = staqly.posts.list(collection_id="Blog posts", status="scheduled", limit=50)
staqly.posts.list(publish_after="2026-11-01", publish_before="2026-11-08")  # the calendar
staqly.posts.list(channel_id=tg.id, tag="launch")
staqly.posts.search("launch")
for p in staqly.posts.iterate(status="draft"):
    print(p.content)
staqly.posts.tags()  # [Tag(name, count)], most used first
```

Statuses: `draft`, `scheduled`, `publishing`, `published`, `failed` (`staqly.models.PostStatus`).
Scheduling and publishing need the `posts:publish` scope; creating and editing need
`posts:write`. A published or publishing post cannot be edited (`ConflictError`, code
`not_editable`); `expected_version` makes an edit conditional (code `post_changed`).

## Your own automation

A channel of provider `custom` is **you**: when a post with such a target publishes, staqly
emits `post.publish_requested`, lists the target under `work.list()` and waits for your result
(until the collection's processing timeout). Report what you did:

```python
for w in staqly.work.list():
    if w.kind == "target" and w.channel.id == mine.id:
        url = my_platform.publish(w.post.content)
        staqly.posts.report_result(w.post.id, mine.id, status="published", url=url)
        # or: status="failed", error="API 500"
    if w.kind == "posts":
        ...  # a schedule with source "agent" found nothing to publish: write new drafts into w.collection
```

The same two situations arrive as webhooks (`post.publish_requested`, `schedule.posts_requested`)
for push instead of polling.

## Schedules

A schedule publishes a collection's waiting drafts: once at a time, or on a cadence.

```python
from staqly.models import ScheduleRule

s = staqly.schedules.create(
    "Blog posts",
    name="Weekday mornings",
    kind="recurring",
    rule=ScheduleRule(
        times=["09:00"], weekdays=[1, 2, 3, 4, 5], tz="Europe/Warsaw"
    ),  # 0 = Sunday; or every_minutes=360
    channels=[tg.id],  # added to posts without their own targets
    pick="next",  # the oldest waiting draft; "all"; or "selected" with post_ids
    source="agent",  # when nothing waits: ask an agent (schedule.posts_requested); "endpoint" fetches from endpoint_url
)
staqly.schedules.create("Launch", name="Launch day", kind="once", run_at="2026-11-03T09:00:00Z", pick="all")
staqly.schedules.list(collection_id="Blog posts")
staqly.schedules.get(s.id)
staqly.schedules.update(s.id, rule={"times": ["08:00", "17:30"]})  # the next run is recomputed
staqly.schedules.disable(s.id)
staqly.schedules.enable(s.id)
staqly.schedules.run(s.id)  # now; the regular next run is unchanged
staqly.schedules.delete(s.id)
```

## Jobs

Publishing, schedule runs and sign-in refreshes run as jobs with retries (at most 3 attempts,
1, 5 and 15 minutes apart).

```python
staqly.jobs.stats()  # queued, running, failed, done_24h, by_kind
staqly.jobs.list(status="failed")  # JobPage: data, next_cursor
for j in staqly.jobs.iterate(kind="publish_post"):
    print(j.last_error)
staqly.jobs.retry(job_id)  # a failed job, now
staqly.jobs.cancel(job_id)  # a queued job
```

## Media

```python
media = staqly.media.upload("clip.mp4")  # path, bytes or binary file object; 415 for SVG/HTML
staqly.media.upload(png_bytes, filename="a.png", content_type="image/png")
data = staqly.media.download(media.id)  # bytes
staqly.media.url(media.id)  # absolute URL (needs auth)
staqly.media.delete(media.id)  # posts that use it lose it
staqly.media.replace(media.id, "clip-edited.mp4")  # the edit takes the original's place in drafts
```

### Design editor, AI and stock photos

The dashboard's design tools are reachable with an API key too (`media:read` / `media:write`):

```python
import time

staqly.design.brand()  # colors, fonts, logos
staqly.design.set_brand(colors=["#22c55e"], fonts=["Inter"], logos=[logo.id])

project = staqly.media_projects.create(open("design.json", "rb"), name="Banner", source_ids=[photo.id])
staqly.media_projects.file(project.id)  # the design, as bytes

if staqly.ai.status().configured:
    staqly.ai.text("Our new release is out today", instruction="Make it shorter")
    job = staqly.ai.jobs.create("generate", prompt="a lighthouse at dawn", width=1024, height=1024)
    while not (job := staqly.ai.jobs.get(job.id)).finished_at:
        time.sleep(2)
    if job.status == "succeeded":
        image = staqly.ai.jobs.accept(job.id)  # a library file to put in a post

photo = staqly.stock.photos(query="mountains").data[0]  # Pixabay, when the server has a key
staqly.stock.use(photo.id)  # copy it into the media library
```

Uploads are read into memory. Per-file limits depend on the plan.

## Analytics

How publishing went over a range of days in the workspace's time zone (`from_` and `to` both
included; default the last 30 days, at most 366). Needs `posts:read`.

```python
from datetime import date

a = staqly.analytics.get()  # the last 30 days
print(a.totals.targets_published, a.totals.success_rate, a.previous.success_rate)
staqly.analytics.get(from_=date(2026, 10, 1), to="2026-10-28")  # str or date
staqly.analytics.get(days=7)  # the last 7 days up to today (or `to`)
staqly.analytics.get(channel=tg.id, collection=col.id, source="claude-code")
for ch in a.channels:
    print(ch.name, ch.published, ch.failed, ch.metrics.engagements if ch.metrics else None)
# also: a.queue, a.daily, a.sources, a.posting_times.best, a.top_posts.posts, a.failures, a.feedback
```

## Events

```python
page = staqly.events.list(type="post.published", collection_id=col.id, limit=100)
for ev in staqly.events.iterate(post_id=post.id):
    print(ev.type, ev.actor.name, ev.data)
staqly.events.list(channel_id=tg.id)
staqly.events.list(schedule_id=s.id, agent="claude-code")
```

Event types: `post.created`, `post.updated`, `post.scheduled`, `post.cancelled`,
`post.publish_requested`, `post.target_published`, `post.published`, `post.failed`,
`post.deleted`, `post.purged`, `channel.connected`, `channel.needs_reconnect`,
`channel.disabled`, `channel.removed`, `schedule.ran`, `schedule.posts_requested`,
`collection.*`, `webhook.*`.

## Webhooks

```python
hook = staqly.webhooks.create(
    "https://my-app.example.com/staqly",
    events=[
        "post.published",
        "post.failed",
        "post.publish_requested",
    ],  # or "*" / "post.*"; unknown patterns: 422
    collection_ids=[col.id],  # empty = all collections
)
hook.secret  # whsec_... keep it to verify deliveries

staqly.webhooks.test(hook.id)
staqly.webhooks.deliveries.list(webhook_id=hook.id, status="failed")
staqly.webhooks.deliveries.retry(delivery_id)
staqly.webhooks.rotate_secret(hook.id)
staqly.webhooks.update(hook.id, active=False)
staqly.webhooks.list()
staqly.webhooks.get(hook.id)
staqly.webhooks.delete(hook.id)
```

### Verifying deliveries

Every delivery is signed. The `X-Staqly-Signature` header is
`sha256=` + hex(HMAC-SHA256(secret, f"{X-Staqly-Timestamp}.{raw_body}")). `verify_webhook` checks
the signature with a constant-time comparison, rejects timestamps more than 5 minutes off
(`tolerance=`, in seconds), and returns a typed `WebhookEvent`. It raises
`WebhookVerificationError` otherwise. **Always pass the raw request body.**

Deliveries are at-least-once and can arrive out of order, so de-duplicate them on
`event.id` (also sent as `X-Staqly-Event-Id`) and order them by `event.created_at`
([delivery guarantees](https://docs.staqly.ai/automation/webhooks#delivery-guarantees)).

**FastAPI:**

```python
from fastapi import FastAPI, HTTPException, Request
from staqly import WebhookVerificationError, verify_webhook

app = FastAPI()


@app.post("/staqly")
async def staqly_webhook(request: Request) -> dict[str, bool]:
    try:
        event = verify_webhook(SECRET, await request.body(), request.headers)
    except WebhookVerificationError:
        raise HTTPException(status_code=400, detail="invalid signature")
    if event.type == "post.publish_requested" and event.post:
        await publish_myself(event.post, event.data["channel"])  # event.post is a typed Post
    return {"ok": True}
```

**Flask:**

```python
from flask import Flask, abort, request
from staqly import WebhookVerificationError, verify_webhook

app = Flask(__name__)


@app.post("/staqly")
def staqly_webhook():
    try:
        event = verify_webhook(SECRET, request.get_data(), request.headers)
    except WebhookVerificationError:
        abort(400)
    print(event.type, event.data)
    return "", 204
```

`is_valid_webhook()` returns a boolean. `compute_signature(secret, ts, body)` helps you write
tests for your handler.

## Alerts

Alerts push a message to Telegram, Slack, Discord, ntfy or email when a post is published or
fails, or a channel needs reconnecting. Managing them needs the `alerts:manage` scope. Secrets in
`config` are write-only and come back masked.

```python
a = staqly.alerts.create(
    "My phone",
    type="ntfy",  # telegram | slack | discord | ntfy | email
    config={"topic": "acme-posts"},  # type-specific settings
    events=["post.published", "post.failed", "channel.needs_reconnect"],  # the default
    collection_ids=[col.id],
    digest_window_seconds=300,  # batch messages (0 = immediately)
)
result = staqly.alerts.test(a.id)  # result.ok / result.error
staqly.alerts.deliveries(a.id, limit=20)
staqly.alerts.update(a.id, config={"priority": 4})  # config keys merge; None removes one
staqly.alerts.disable(a.id)
staqly.alerts.enable(a.id)
staqly.alerts.list()
staqly.alerts.delete(a.id)
```

Going over the plan's limit raises `PlanLimitError` with `limit == "alerts"`.

## Exports and purge

```python
# stream the activity log, oldest first, without buffering it
for ev in staqly.exports.events(from_="2026-10-01", type_prefix="post."):
    print(ev.type)
staqly.exports.save_events("events.csv", format="csv")  # or "ndjson"; streamed to disk

# full workspace export (admin): ZIP with collections, posts, channels (no credentials), schedules, events, media
job = staqly.exports.export_workspace("workspace.zip", timeout=600)  # create, wait, save
# or step by step: create_workspace_export() → wait_for_workspace_export(id)
#                  → save_workspace_export(id, path) / download_workspace_export(id) (bytes)

# delete a post and its whole history (admin); posts.delete() keeps the history
staqly.posts.purge(post.id)  # PurgeResult(post_id, events_deleted, media_deleted)
```

The workspace export works on every plan; on Starter it is data only (`includes_media` is `False`: no
media files in the ZIP). The event export needs a plan with data export (Plus and above: `PlanLimitError`,
`limit == "data_export"`) and `events:read`; without `admin` it leaves out credential and session audit events
(`api_key.*`, `session.*`, `recovery_key.*`, device links). Only one workspace export can run at a
time. Starting another raises `ConflictError` with `details["export_id"]`. A failed or expired job
raises `ExportFailedError`.

## Workspace, plans, billing and API keys

```python
staqly.workspace.get()
staqly.workspace.rename("Acme agents")  # admin
u = staqly.workspace.usage()  # u.plan.limits, u.usage, u.period, u.posts_by_status, u.webhook_deliveries
staqly.workspace.access_token().token  # local: what agents send as Bearer
staqly.workspace.rotate_access_token()  # local: new token (+ sessions_revoked); the old one stops at once
staqly.workspace.rotate_recovery_key().recovery_key  # dashboard session only; shown once
staqly.workspace.delete(confirm="Acme agents")  # cloud, dashboard session only; permanent
staqly.workspace.agents()  # admin: MCP clients and API connections seen, latest first
staqly.workspace.forget_agent(agent_id)  # admin; it comes back if it talks to the workspace again

# Pro subscription through Stripe (cloud, admin); open the URLs in a browser
staqly.billing.get()  # enabled, plan, subscription_status, plan_expires_at, suspended, has_customer
staqly.billing.checkout(
    plan="plus", interval="year"
).url  # Stripe Checkout; a paying workspace is switched instead
staqly.billing.portal().url  # payment method, invoices, cancel
staqly.plans.list()  # Starter, Plus, Pro, Business: price_month, price_year in currency (eur, per user; yearly twelve months at 20 % off or more), limits (-1 = unlimited)

key = staqly.api_keys.create(
    "writer", scopes=["collections:read", "posts:read", "posts:write", "media:write"]
)
key.key  # stk_... shown once
staqly.api_keys.list()
staqly.api_keys.rotate(key.id)
staqly.api_keys.revoke(key.id)
```

Scopes (`staqly.models.Scope`): `collections:read|write|delete`, `posts:read|write|delete|publish`,
`channels:read|manage`, `schedules:manage`, `media:read|write`, `webhooks:manage`,
`alerts:manage`, `events:read`, `admin`. Writing and publishing are separate: `posts:write`
creates and edits, `posts:publish` schedules, publishes, cancels, retries a target or reports a
result. Deleting needs its own scope: `posts:delete` (posts, media), `collections:delete`
(collections); `:write` alone answers 403.

## Sessions and device links

These endpoints back the dashboard. Most of them need a browser session rather than an API key
(API keys get 403), so they are mainly useful for tooling around a self-hosted install.

```python
staqly.sessions.current()  # same as whoami(), includes the csrf_token
staqly.sessions.list()  # admin: active browser sessions (Session.current marks this one)
staqly.sessions.revoke("ses_q2vbm4kz7xw3dn5hjr6c")
staqly.sessions.logout()  # revoke the current session
s = staqly.sessions.sign_in_with_api_key(
    "stk_..."
)  # admin API key (local: the access token) -> session cookie + csrf_token
r = staqly.sessions.recover("staqly-recovery-K7QMZ-3XKD5-VNPW2-HCY4T-BRFA6-JLE5S-O2UQM-4ZXRA")
r.api_key.key, r.recovery_key  # both shown once; every other session is signed out

link = staqly.device_links.create()  # one-time "connect a device" URL, 5 minutes, single use
staqly.device_links.redeem(
    "q7mz3xkd5vnpw2hcy4tbrfa6jle5so2u"
)  # token from the URL fragment -> session cookie
```

`sign_in_with_api_key()`, `recover()` and `redeem()` store the session cookie in the client's cookie jar. Unsafe requests
on a session need the CSRF token: pass `default_headers={"X-CSRF-Token": s.csrf_token}` or
`request_options={"headers": {...}}`.

## Pagination

`posts.list()`, `jobs.list()` and `events.list()` return one page (`.data`, `.next_cursor`).
`iterate()` follows the cursors lazily:

```python
page = staqly.posts.list(status="published", limit=100)
while page.next_cursor:
    page = staqly.posts.list(status="published", limit=100, cursor=page.next_cursor)

for post in staqly.posts.iterate(status="published"):  # async: `async for`
    ...
```

## Errors

Every error subclasses `staqly.StaqlyError`. HTTP errors subclass `APIError` and carry `status`,
`code`, `message`, `fields`, `details`, `request_id`, `body` and `response`. `request_id` is the
server's `X-Request-Id`; include it in bug reports.

| Class | When |
|---|---|
| `BadRequestError` | 400 |
| `AuthenticationError` | 401 missing/invalid/revoked key |
| `PlanLimitError` | 402 `plan_limit_exceeded`, with `.limit`, `.max`, `.used`, `.plan` |
| `PermissionDeniedError` | 403 missing scope, or the action needs a dashboard session (`.is_not_local` is deprecated: always `False` now) |
| `NotFoundError` | 404 |
| `ConflictError` | 409, e.g. the post is already publishing (`not_editable`) or the version changed (`post_changed`) |
| `GoneError` | 410 |
| `PayloadTooLargeError` / `UnsupportedMediaTypeError` | 413 / 415 |
| `ValidationError` | 422, with `.fields` (`targets.<channel_id>.content` when a provider's limit is broken) |
| `RateLimitError` | 429, with `.retry_after` |
| `InternalServerError` | 5xx |
| `APIConnectionError` / `TimeoutError` | no response / request timed out (`TimeoutError` is also a builtin `TimeoutError`) |
| `WaitTimeoutError` | `wait_for_published` / `wait_for_workspace_export` gave up (`.id`, `.post`) |
| `ExportFailedError` | a workspace export job failed or expired (`.job`) |
| `WebhookVerificationError` | bad signature or timestamp |

```python
from staqly import PlanLimitError

try:
    staqly.webhooks.create(url)
except PlanLimitError as e:
    print(f"{e.limit}: {e.used}/{e.max} on the {e.plan} plan")
```

## Retries, timeouts and idempotency

- Cloud servers rate-limit each key (600 requests per minute, bursts of 100). A 429 comes with
  `Retry-After`, which the retry logic honours.
- Requests that fail with 408, 429, 500, 502, 503 or 504, or a network error or timeout, are
  retried up to `max_retries` times (default 2). The delay is 0.5 s · 2ⁿ, capped at 8 s, with
  jitter. A `Retry-After` header is honoured if it is 60 s or less. A longer one raises
  `RateLimitError` immediately.
- Every `POST`, `PUT`, `PATCH` or `DELETE` with a JSON body gets an automatic `Idempotency-Key`, reused
  across its retries. The server then replays the first response instead of applying the request
  twice. Set `request_options={"idempotency_key": ...}` to choose the key yourself. Keys are kept
  for 24 h. The server compares JSON bodies by content (key order and spacing don't
  matter), so a key can be shared with the TypeScript SDK for the same request.
- Multipart uploads are not covered by server-side idempotency, so they are retried only on 429.
- `timeout` (default 60 s) applies to each attempt.

## Async usage

`AsyncStaqly` has the same API. Await the methods and use `async for` on iterators:

```python
import asyncio
from staqly import AsyncStaqly


async def main() -> None:
    async with AsyncStaqly() as staqly:
        post = await staqly.posts.create(
            "Drafts", content="Hello", targets=[{"channel_id": "ch_1"}], publish_now=True
        )
        done = await staqly.posts.wait_for_published(post.id)
        async for p in staqly.posts.iterate(status="published"):
            print(p.content)


asyncio.run(main())
```

## Example: an agent that writes and publishes

An agent drafts a post with a picture, schedules it for the morning, and a second process (or a
webhook) follows what happened:

```python
from datetime import datetime, timedelta

from staqly import Staqly

with Staqly() as staqly:  # or Staqly(base_url=..., api_key=...) in cloud mode
    # 1. a draft with media and every connected channel
    channels = [c for c in staqly.channels.list() if c.status == "connected"]
    cover = staqly.media.upload("out/cover.png")
    post = staqly.posts.create(
        "Blog posts",
        content="Fresh look for autumn 🍂 Three things we changed and why.",
        media=[cover.id],
        targets=[{"channel_id": c.id} for c in channels],
        tags=["autumn"],
        source="my-agent",
    )

    # 2. tomorrow at nine
    nine = (datetime.now().astimezone() + timedelta(days=1)).replace(
        hour=9, minute=0, second=0, microsecond=0
    )
    staqly.posts.schedule(post.id, nine)

    # 3. later: did it go out?
    result = staqly.posts.wait_for_published(post.id, timeout=26 * 3600)
    for t in result.targets:
        print(t.channel_name, t.status, t.external_url or t.error)
```

## Development

```bash
uv sync                     # .venv with dev tools
make check                  # gen (sync client) + ruff + mypy --strict + pytest + build
STAQLY_LIVE=1 STAQLY_API_URL=http://127.0.0.1:3333 uv run pytest tests/test_live.py
```

## License

Apache-2.0 — see [LICENSE](LICENSE) and [NOTICE](NOTICE).
