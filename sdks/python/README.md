# staqly

Official Python SDK for **[staqly](https://github.com/staqly/staqly)**, the local-first
workspace where AI agents store generated content (items in collections) and humans review it
(approve, reject with feedback, custom actions).

- Sync `Staqly` and async `AsyncStaqly` clients with identical APIs, built on `httpx`.
- Typed pydantic v2 models, checked against the API's OpenAPI contract, with `py.typed` and
  `mypy --strict` clean.
- Retries with exponential backoff and jitter, `Retry-After` handling, automatic idempotency keys
  and timeouts.
- Typed errors, auto-paginating generators, webhook verification, `wait_for_decision` and approval
  gates, including a `@requires_approval` decorator.
- Python ≥ 3.10.

```bash
pip install staqly        # or: uv add staqly
```

## Contents

- [Quickstart](#quickstart)
- [Configuration](#configuration)
- [Collections](#collections)
- [Items](#items)
- [Waiting for a human decision](#waiting-for-a-human-decision)
- [Approval gates](#approval-gates)
- [Media](#media)
- [Events](#events)
- [Webhooks](#webhooks)
- [Watchers](#watchers)
- [Notification channels](#notification-channels)
- [Exports and purge](#exports-and-purge)
- [Workspace, plans, billing and API keys](#workspace-plans-billing-and-api-keys)
- [Sessions, device links and decision links](#sessions-device-links-and-decision-links)
- [Pagination](#pagination)
- [Errors](#errors)
- [Retries, timeouts and idempotency](#retries-timeouts-and-idempotency)
- [Async usage](#async-usage)
- [Example: Instagram avatar flow](#example-instagram-avatar-flow)

## Quickstart

**Local mode.** A staqly server on your machine, started with `staqly serve` (or the `ghcr.io/staqly/staqly` Docker image),
needs no credentials:

```python
from staqly import Staqly

staqly = Staqly()  # http://localhost:3333

staqly.collections.create("Blog drafts")
item = staqly.items.create(
    "Blog drafts",
    title="Why local-first?",
    content="# Draft\n...",
    metadata={"model": "gpt-x", "score": 0.82},
)

decided = staqly.items.wait_for_decision(item.id)  # blocks until a human decides
if decided.status == "approved":
    publish(decided.content)  # reviewer edits included
else:
    print("Rejected:", decided.feedback)
```

**Cloud mode.** Use a hosted instance with an API key (`stk_...`):

```python
staqly = Staqly(base_url="https://staqly.example.com", api_key=os.environ["STAQLY_API_KEY"])
```

Don't have a workspace yet? Create one programmatically. It starts on the Free plan, and the key
is shown only once:

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
staqly.items.list(status="pending", request_options={"timeout": 5, "max_retries": 0, "headers": {...}})
staqly.items.create("Posts", title="x", request_options={"idempotency_key": f"job-{job_id}"})
```

`staqly.health()` checks liveness and `staqly.ready()` checks readiness (database, migrations and
workers); `staqly.live()` is the process-only probe (`/livez`). `staqly.setup()` reports the server
mode, whether the workspace exists (local) and whether signup is open (cloud). `staqly.whoami()`
returns the server mode, the workspace, and the calling principal with its scopes.
`staqly.metrics(token=None)` returns the Prometheus text (from this machine, or with the server's
`METRICS_TOKEN`) and `staqly.openapi()` the server's OpenAPI YAML. `staqly.request("GET", "/v1/...")` calls any endpoint with
the same auth, retries and error handling.

## Collections

```python
from staqly.models import ActionDef

col = staqly.collections.create(
    "Instagram",
    description="Avatar and post candidates",
    actions=[  # approve and reject are built in; add your own
        ActionDef(name="publish", label="Publish", set_status="processing", allowed_statuses=["approved"]),
    ],
)
staqly.collections.list(include_archived=False)
staqly.collections.get("Instagram")  # ID or exact name
staqly.collections.list_items(col.id, status="pending", limit=50)  # one ItemPage
staqly.collections.update(col.id, color="#e1306c")
staqly.collections.set_actions(col.id, [...])
staqly.collections.archive(col.id)  # unarchive(col.id)
staqly.collections.delete(col.id, confirm="Instagram")  # permanent; the name must match
```

## Items

```python
item = staqly.items.create(
    "Instagram",  # collection ID or name
    title="Caption idea",
    content="Golden hour 🌅",  # str or any JSON value
    metadata={"score": 0.91},
    source="caption-agent",
    external_ref="job-123",
    expires_in="24h",  # undecided items become "expired" (seconds or duration)
    editable=["content", "metadata.caption"],  # what reviewers may change before approving
)

# upload a file and create the item in one request: path, bytes or binary file object
img = staqly.items.upload("Instagram", "avatar.png", title="New avatar", metadata={"prompt": "..."})
staqly.items.upload("Instagram", png_bytes, filename="a.png")

staqly.items.get(item.id)
staqly.items.update(item.id, metadata_patch={"reviewed_by": "bot"})  # merge keys (None removes one)
staqly.items.delete(item.id)  # history is kept

# decisions (need the items:review scope)
staqly.items.approve(item.id, feedback="Ship it", edits={"title": "Better title"})
staqly.items.reject(item.id, feedback="Too dark, brighten it")
staqly.items.action(item.id, "publish", payload={"channel": "ig"})  # custom action
staqly.items.list_actions(item.id)
staqly.items.bulk_action(["itm_1", "itm_2"], "reject", feedback="duplicates")
staqly.items.approve(item.id, expected_version=item.version)  # optimistic concurrency

# after acting on an approved item, record the outcome
staqly.items.report_result(item.id, status="completed", result={"post_url": "https://..."})
staqly.items.report_result(item.id, status="failed", error="Instagram API 500")
staqly.items.reject(item.id, feedback="Too dark", follow_up="regenerate")  # reject and ask for a new version
staqly.items.update(item.id, tags=["client-a", "urgent"])  # labels, any status, no new version
staqly.items.list(decision=["approved"], run="failed", tag="client-a", archived=False)
staqly.items.unarchive(item.id)  # back to review, its run or its decision

staqly.items.history(item.id)  # events, newest first
staqly.items.versions(item.id)  # snapshots before each change

# list and search
page = staqly.items.list(collection="Instagram", status="pending", limit=50)
hits = staqly.items.search("golden hour", status="approved")
for it in staqly.items.iterate(status="pending", created_after=date(2026, 10, 1)):
    print(it.title)
```

Statuses: `draft`, `pending` (awaiting review), `approved`, `rejected`, `expired`, `processing`,
`completed`, `failed`, `archived`. They are available as `staqly.models.ItemStatus`. `update()`
cannot set `approved` or `rejected`. Use `approve()` and `reject()` for that.

## Waiting for a human decision

```python
from staqly import WaitTimeoutError

try:
    item = staqly.items.wait_for_decision(item_id, timeout=30 * 60)  # seconds, default 600
except WaitTimeoutError as e:
    print("still pending", e.item)
else:
    match item.status:
        case "approved":
            ...
        case "rejected":
            print(item.feedback)
        case "expired":
            ...  # nobody answered within expires_in
```

`wait_for_decision` uses the server's long-poll endpoint (`GET /v1/items/{id}/wait`, up to 60 s
per round) and re-issues it until the item leaves `pending`, so it does not busy-poll. It returns
as soon as the item is approved, rejected or expired, or when an action moves it on.
`on_poll=callback` lets you log progress. `items.wait(id, timeout=30)` runs a single round.

For long-running services, push beats waiting: subscribe a [webhook](#webhooks) to
`item.approved`, `item.rejected` and `item.expired`.

## Approval gates

`request_approval` creates an item, waits for the decision and returns a `Decision`. It never
raises on a rejection:

```python
d = staqly.request_approval(
    "Outbox",
    title="Send the October newsletter?",
    content=draft,
    expires_in="4h",
    editable=["content"],
)
if d.approved:
    send(d.item.content)  # the reviewer's edited version
elif d.expired:
    print("no answer in time")
else:
    print("rejected:", d.feedback)
```

`@requires_approval` makes every call ask a human first. The function runs only after approval.
The item is then reported `completed`, with the return value stored as `result`, or `failed` if
the function raises. A rejection or expiry raises `RejectedError`:

```python
from staqly import RejectedError


@staqly.requires_approval(
    "Outbox",
    title=lambda to, subject, **_: f"Email {to}: {subject}",
    metadata=lambda to, **_: {"recipient": to},
    expires_in="1h",
)
def send_email(to: str, subject: str, body: str) -> str:
    return mailer.send(to, subject, body)


try:
    send_email("ceo@example.com", "Q3 numbers", body)
except RejectedError as e:
    print("blocked:", e.feedback)
```

With `AsyncStaqly`, decorate `async def` functions the same way.

## Media

```python
media = staqly.media.upload("clip.mp4")  # 415 for SVG/HTML
staqly.items.create("Videos", title="Teaser", media_id=media.id)
data = staqly.media.download(media.id)  # bytes
staqly.media.url(media.id)  # absolute URL (needs auth)
staqly.media.delete(media.id)
```

Uploads are read into memory. Per-file limits depend on the plan: 25 MB on Free.

## Events

```python
page = staqly.events.list(type="item.approved", collection_id=col.id, limit=100)
for ev in staqly.events.iterate(item_id=item.id):
    print(ev.type, ev.actor.name, ev.data)
```

## Webhooks

```python
hook = staqly.webhooks.create(
    "https://my-app.example.com/staqly",
    events=["item.approved", "item.rejected", "item.expired"],  # or "*" / "item.*"
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

Deliveries are at-least-once, so de-duplicate them on `event.id` (also sent as
`X-Staqly-Event-Id`).

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
    if event.type == "item.approved" and event.item:
        await publish(event.item)  # event.item is a typed Item; event.data["diff"] has reviewer edits
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

## Watchers

```python
staqly.watchers.create("Hacker News", kind="rss", url="https://news.ycombinator.com/rss", collection="News")
w = staqly.watchers.create(
    "Competitor pricing",
    kind="url_diff",
    url="https://example.com/pricing",
    collection="Research",
    every="6h",
    keywords=["price"],
)
# kind left out: detected from the URL; a list of collections gets every new entry
staqly.watchers.create(
    "Go releases", url="https://github.com/golang/go/releases.atom", collection=["Releases", "Go"]
)
staqly.watchers.run(w.id)  # check now
staqly.watchers.pause(w.id)
staqly.watchers.resume(w.id)
staqly.watchers.update(w.id, every="1h")
staqly.watchers.delete(w.id)
```

## Notification channels

Channels push review requests to Telegram, Slack, Discord, ntfy or email, with Approve/Reject
buttons or one-tap links. Managing them needs the `channels:manage` scope. Secrets in `config` are
write-only and come back masked.

```python
ch = staqly.notification_channels.create(
    "My phone",
    type="ntfy",  # telegram | slack | discord | ntfy | email
    config={"topic": "acme-reviews"},  # type-specific settings
    events=["item.created", "item.escalated"],  # the default
    collection_ids=[col.id],
    digest_window_seconds=300,  # batch messages (0 = immediately)
)
result = staqly.notification_channels.test(ch.id)  # result.ok / result.error
staqly.notification_channels.deliveries(ch.id, limit=20)
staqly.notification_channels.update(ch.id, config={"priority": 4})  # config keys merge
staqly.notification_channels.disable(ch.id)
staqly.notification_channels.enable(ch.id)
staqly.notification_channels.list()
staqly.notification_channels.delete(ch.id)
```

On the Free plan you can have 2 channels. Going over the limit raises `PlanLimitError` with
`limit == "notification_channels"`.

## Exports and purge

```python
# stream the activity log, oldest first, without buffering it
for ev in staqly.exports.events(from_="2026-10-01", type_prefix="item."):
    print(ev.type)
staqly.exports.save_events("events.csv", format="csv")  # or "ndjson"; streamed to disk

# full workspace export (admin): ZIP with collections, items + versions, events, media, settings
job = staqly.exports.export_workspace("workspace.zip", timeout=600)  # create, wait, save
# or step by step: create_workspace_export() → wait_for_workspace_export(id)
#                  → save_workspace_export(id, path) / download_workspace_export(id) (bytes)

# delete an item and its whole history (admin); items.delete() keeps the history
staqly.items.purge(item.id)  # PurgeResult(item_id, events_deleted, media_deleted)
```

Only one workspace export can run at a time. Starting another raises `ConflictError` with
`details["export_id"]`. A failed or expired job raises `ExportFailedError`.

## Workspace, plans, billing and API keys

```python
staqly.workspace.get()
staqly.workspace.rename("Acme agents")  # admin
u = staqly.workspace.usage()  # u.plan.limits, u.usage.items_per_month, u.period
staqly.workspace.set_plan("free")  # admin, cloud: switch to Free (lifts a trial suspension)
staqly.workspace.rotate_recovery_key().recovery_key  # dashboard session only; shown once
staqly.workspace.delete(confirm="Acme agents")  # cloud, dashboard session only; permanent

# Pro subscription through Stripe (cloud, admin); open the URLs in a browser
staqly.billing.get()  # enabled, plan, subscription_status, plan_expires_at, suspended, has_customer
staqly.billing.checkout().url  # Stripe Checkout for Pro
staqly.billing.portal().url  # payment method, invoices, cancel
staqly.plans.list()  # limits: -1 = unlimited

key = staqly.api_keys.create("n8n", scopes=["items:read", "items:write"], expires_at="2027-01-01")
key.key  # stk_... shown once
staqly.api_keys.list()
staqly.api_keys.rotate(key.id)
staqly.api_keys.revoke(key.id)
```

## Sessions, device links and decision links

These endpoints back the dashboard. Most of them need a browser session rather than an API key
(API keys get 403), so they are mainly useful for tooling around a self-hosted install.

```python
staqly.sessions.current()  # same as whoami(), includes the csrf_token
staqly.sessions.list()  # admin: active browser sessions (Session.current marks this one)
staqly.sessions.revoke("ses_q2vbm4kz7xw3dn5hjr6c")
staqly.sessions.logout()  # revoke the current session
# recovery key -> session cookie + csrf_token
s = staqly.sessions.recover("staqly-recovery-K7QMZ-3XKD5-VNPW2-HCY4T-BRFA6-JLE5S-O2UQM-4ZXRA")

link = staqly.device_links.create()  # one-time "connect a device" URL, 5 minutes, single use
# token from the URL fragment -> session cookie
staqly.device_links.redeem("q7mz3xkd5vnpw2hcy4tbrfa6jle5so2u")

# one-tap links from notification channels (public: the token is the credential)
html = staqly.decision_links.preview("dl_tiptzca7tyr7yzgxiuqh")  # confirmation page, does not decide
staqly.decision_links.decide("dl_tiptzca7tyr7yzgxiuqh", feedback="Off-brand")  # DecisionResult; single use
```

`recover()` and `redeem()` store the session cookie in the client's cookie jar. Unsafe requests
on a session need the CSRF token: pass `default_headers={"X-CSRF-Token": s.csrf_token}` or
`request_options={"headers": {...}}`. The inbound chat-provider callbacks
(`/v1/integrations/*`), the Stripe webhook (`/v1/billing/webhook`) and `/mcp` are not wrapped:
providers and MCP clients call them directly.

## Pagination

`items.list()` and `events.list()` return one page (`.data`, `.next_cursor`). `iterate()` follows
the cursors lazily:

```python
page = staqly.items.list(status="approved", limit=100)
while page.next_cursor:
    page = staqly.items.list(status="approved", limit=100, cursor=page.next_cursor)

for item in staqly.items.iterate(status="approved"):  # async: `async for`
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
| `PermissionDeniedError` | 403 missing scope; `.is_not_local` when a local server refused a non-local caller |
| `NotFoundError` | 404 |
| `ConflictError` | 409, e.g. the item was already decided or the version changed |
| `GoneError` | 410 |
| `PayloadTooLargeError` / `UnsupportedMediaTypeError` | 413 / 415 |
| `ValidationError` | 422, with `.fields` (code `not_editable` for edits outside `editable`) |
| `RateLimitError` | 429, with `.retry_after` |
| `InternalServerError` | 5xx |
| `APIConnectionError` / `TimeoutError` | no response / request timed out (`TimeoutError` is also a builtin `TimeoutError`) |
| `WaitTimeoutError` | `wait_for_decision` / `wait_for_workspace_export` gave up (`.item_id`, `.item`) |
| `ExportFailedError` | a workspace export job failed or expired (`.job`) |
| `RejectedError` | a `@requires_approval` call was rejected or expired |
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
- Every `POST`, `PATCH` or `DELETE` with a JSON body gets an automatic `Idempotency-Key`, reused
  across its retries. The server then replays the first response instead of applying the request
  twice. Set `request_options={"idempotency_key": ...}` to choose the key yourself. Keys are kept
  for 24 h.
- Multipart uploads are not covered by server-side idempotency, so they are retried only on 429.
- `timeout` (default 60 s) applies to each attempt. `items.wait` raises it automatically to
  outlast the server-side long poll.

## Async usage

`AsyncStaqly` has the same API. Await the methods and use `async for` on iterators:

```python
import asyncio
from staqly import AsyncStaqly


async def main() -> None:
    async with AsyncStaqly() as staqly:
        item = await staqly.items.create("Drafts", title="Hello")
        decided = await staqly.items.wait_for_decision(item.id)
        async for it in staqly.items.iterate(status="approved"):
            print(it.title)


asyncio.run(main())
```

## Example: Instagram avatar flow

An agent generates an avatar. A human approves it, possibly editing the caption, and the agent
then publishes it and records where:

```python
from staqly import Staqly

with Staqly() as staqly:  # or Staqly(base_url=..., api_key=...) in cloud mode
    # 1. store the generated image for review
    item = staqly.items.upload(
        "Instagram",
        "out/avatar.png",
        title="New profile picture",
        content="Caption: Fresh look for autumn 🍂",
        metadata={"account": "@acme", "prompt": "autumn portrait, warm light"},
        editable=["content"],
        expires_in="24h",
    )

    # 2. wait for the human (or get a webhook instead)
    decided = staqly.items.wait_for_decision(item.id, timeout=24 * 3600)
    if decided.status != "approved":
        raise SystemExit(f"not publishing ({decided.status}): {decided.feedback}")

    # 3. publish with the approved (possibly edited) caption, then report the outcome
    try:
        image = staqly.media.download(item.media.id)
        post = instagram.set_avatar(image, caption=decided.content)
    except Exception as exc:
        staqly.items.report_result(item.id, status="failed", error=str(exc))
        raise
    staqly.items.report_result(item.id, status="completed", result={"post_url": post.url})
    # equivalent low-level form:
    # staqly.items.update(item.id, status="completed", metadata_patch={"post_url": post.url})
```

## Development

```bash
uv sync                     # .venv with dev tools
make check                  # gen (sync client) + ruff + mypy --strict + pytest + build
STAQLY_LIVE=1 STAQLY_API_URL=http://127.0.0.1:3333 uv run pytest tests/test_live.py
```

## License

Apache-2.0 — see [LICENSE](LICENSE) and [NOTICE](NOTICE).
