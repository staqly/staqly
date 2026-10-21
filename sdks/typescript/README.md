# @staqly/sdk

Official TypeScript / JavaScript SDK for **[staqly](https://github.com/staqly/staqly)**, the
local-first scheduler for posts your agents write: posts live in collections, go out to
connected channels at their time, on a schedule, or right away, and your own automation can be
a channel too.

- Typed end to end: models are generated from the API's OpenAPI contract.
- Works on Node ≥ 20.19, Bun, Deno, browsers and edge runtimes (Cloudflare Workers, Vercel/Next.js
  edge). It uses only `fetch` and Web Crypto and has **no runtime dependencies**.
- ESM-only and tree-shakeable, with a standalone `@staqly/sdk/webhooks` entry point. CommonJS code
  can still `require("@staqly/sdk")` on Node ≥ 20.19 or ≥ 22.12, which load ES modules from `require()`.
- Retries with exponential backoff and jitter, `Retry-After` handling, automatic idempotency keys,
  timeouts and `AbortSignal` support.
- Typed errors, auto-paginating iterators, webhook verification and `waitForPublished`.

```bash
npm install @staqly/sdk
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
- [Events](#events)
- [Webhooks](#webhooks)
- [Alerts](#alerts)
- [Exports and purge](#exports-and-purge)
- [Workspace, plans, API keys](#workspace-plans-and-api-keys)
- [Sessions and device links](#sessions-and-device-links)
- [Pagination](#pagination)
- [Errors](#errors)
- [Retries, timeouts and idempotency](#retries-timeouts-and-idempotency)
- [Example: an agent that writes and publishes](#example-an-agent-that-writes-and-publishes)
- [Types](#types)

## Quickstart

**Staqly Community Edition.** A staqly server on your machine, started with `staqly serve` (or the `ghcr.io/staqly/staqly` Docker image),
wants its access token, which it prints when it starts. Put it in `STAQLY_API_KEY`
(`export STAQLY_API_KEY=$(staqly serve token)`, with Docker `$(docker exec staqly server token)`):

```ts
import { Staqly } from "@staqly/sdk";

const staqly = new Staqly(); // $STAQLY_API_URL (Staqly Cloud: https://app.staqly.ai; default http://localhost:3333) and $STAQLY_API_KEY

const [channel] = await staqly.channels.list(); // connected in the dashboard
const post = await staqly.posts.create({
  collection: "Blog posts",
  content: "Launch day! Three things we shipped this week...",
  targets: [{ channel_id: channel.id }],
  publish_at: "2026-11-03T09:00:00Z",
  status: "scheduled",
});

// or publish right away and wait for the platform's answer
const now = await staqly.posts.create({ collection: "Blog posts", content: "Hello", targets: [{ channel_id: channel.id }], publish_now: true });
const done = await staqly.posts.waitForPublished(now.id);
console.log(done.status, done.targets[0].external_url);
```

**Cloud mode.** Use a hosted instance with an API key (`stk_...`):

```ts
const staqly = new Staqly({
  baseUrl: "https://staqly.example.com",
  apiKey: process.env.STAQLY_API_KEY,
});
```

Don't have a workspace yet? Create one programmatically. It starts on a free 7-day trial (Pro); when it ends the workspace is read-only until a plan is paid for (`billing.checkout({ plan, interval })`). The response
holds an admin key (scope `admin`, also the dashboard sign-in credential) and a single-use recovery
key, both shown only once:

```ts
const { workspace, api_key, recovery_key } = await new Staqly({ baseUrl: "https://staqly.example.com" })
  .workspaces.create({ name: "My agents" });
console.log(api_key.key); // stk_... store it now
```

## Configuration

```ts
new Staqly({
  baseUrl: "http://localhost:3333", // default: $STAQLY_API_URL, then http://localhost:3333
  apiKey: "stk_...",                  // default: $STAQLY_API_KEY; null = ignore the env var
  timeout: 60_000,                  // per-request timeout in ms
  maxRetries: 2,                    // 408/429/5xx/network errors
  fetch: myFetch,                   // custom fetch (tests, proxies, tracing)
  defaultHeaders: { "X-Team": "growth" },
  userAgent: "my-agent/1.0",        // appended to "staqly-sdk-ts/0.1.0"
});
```

Every method takes an optional last argument with per-request overrides:

```ts
await staqly.posts.list({ status: "scheduled" }, { timeout: 5_000, maxRetries: 0, signal, headers: { ... } });
await staqly.posts.create({ collection: "Posts", content: "x" }, { idempotencyKey: `job-${jobId}` });
```

`staqly.health()` checks liveness and the database, `staqly.live()` is the bare liveness probe
(`/livez`) and `staqly.ready()` checks readiness (database, migrations and workers).
`staqly.whoami()` returns the server mode, the workspace, and the calling principal with its
scopes. `staqly.setup()` reports the mode and whether the workspace exists or signup is open,
`staqly.openapi()` returns the server's OpenAPI YAML, and `staqly.metrics({ token })` returns the
Prometheus metrics (from the server's machine, or with the operator's `METRICS_TOKEN`).

Request bodies and query parameters use the API's `snake_case` names, exactly as in the REST
docs. SDK options such as `timeout` and `idempotencyKey` are `camelCase`.

## Collections

Collections group posts and carry the schedules that publish them.

```ts
const col = await staqly.collections.create({
  name: "Blog posts",
  description: "Everything about the product",
  processing_timeout_seconds: 3600, // how long your own automation may take to report a result (null = server default)
});
await staqly.collections.list();                       // add { include_archived: true } for archived ones
await staqly.collections.get("Blog posts");            // ID or exact name
await staqly.collections.update(col.id, { color: "#e1306c" });
await staqly.collections.archive(col.id);               // unarchive(col.id)
await staqly.collections.delete(col.id, "Blog posts");  // permanent; the name must match
await staqly.collections.listPosts("Blog posts", { status: "draft" }); // one page, same filters as posts.list
```

## Channels and providers

A channel is a connected account. `providers.list()` is the catalog: what staqly can publish to,
how each platform authenticates (`oauth2`, `token` or `none`), its limits and the fields it needs.

```ts
const providers = await staqly.providers.list();
providers.find((p) => p.id === "telegram")!.credential_fields; // [{ key: "bot_token", kind: "secret", required: true }, ...]

// token platforms and your own automation connect here
const tg = await staqly.channels.connect({
  provider: "telegram",
  name: "Announcements",
  fields: { bot_token: process.env.TELEGRAM_TOKEN!, chat_id: "@acme" }, // stored sealed, never returned
});
const mine = await staqly.channels.connect({ provider: "custom", name: "My pipeline" }); // see "Your own automation"

// OAuth platforms: open the link in a browser signed in to the platform
const url = await staqly.channels.oauthStart("mastodon", { instance: "mastodon.social" });
await staqly.providers.setApp("mastodon", { client_id, client_secret }); // the workspace's own developer app

await staqly.channels.list(); await staqly.channels.get(tg.id);
await staqly.channels.update(tg.id, { name: "News", settings: { silent: true } }); // defaults for new targets
await staqly.channels.update(tg.id, { fields: { bot_token: "fresh" } });           // reconnect
await staqly.channels.test(tg.id);      // { ok, error, profile }: checks the credentials against the platform
await staqly.channels.disable(tg.id);   // targets on it fail without trying; enable(id) turns it back on
await staqly.channels.remove(tg.id);    // targets on it go with it
```

A channel's `status` is `connected`, `needs_reconnect` (the platform refused the credentials; an
alert and the `channel.needs_reconnect` event tell you) or `disabled`.

## Posts

```ts
// a draft: content, media, the channels it goes to
const media = await staqly.media.upload(await fileFromPath("cover.png"));
const post = await staqly.posts.create({
  collection: "Blog posts",           // id or exact name
  content: "Launch day!",
  media: [media.id],
  targets: [
    { channel_id: tg.id },
    { channel_id: bsky.id, content: "Launch day! (short version)", settings: { lang: "en" } }, // per-channel text and settings
  ],
  tags: ["launch"],
  metadata: { campaign: "q4" },
  source: "my-agent",
});

await staqly.posts.schedule(post.id, "2026-11-03T09:00:00Z"); // scheduled: goes out then (a Date works too)
await staqly.posts.cancel(post.id);                             // back to draft, the time kept as a note
await staqly.posts.publish(post.id);                            // now: the post becomes publishing
await staqly.posts.waitForPublished(post.id, { timeout: 60_000 }); // published or failed (check targets[].error)
await staqly.posts.retryTarget(post.id, bsky.id);               // try a failed target again

await staqly.posts.update(post.id, { content: "Edited", expected_version: post.version }); // drafts, scheduled and failed posts
await staqly.posts.update(post.id, { publish_at: null });       // unschedule
await staqly.posts.preview(post.id);                            // the text each target carries and what its provider objects to
await staqly.posts.duplicate(post.id);                          // a new draft with the same content and targets
await staqly.posts.history(post.id);                            // events, newest first

// feedback from preview pages: threads, some on a passage (quote, part, anchor_start/anchor_end)
const comments = await staqly.posts.comments(post.id);           // oldest first; replies carry parent_id
await staqly.posts.addComment(post.id, { body: "Fixed, thanks", parent_id: comments[0].id }); // reply as the team
await staqly.posts.resolveComment(post.id, comments[0].id);     // reopenComment(id, commentId) undoes it
await staqly.posts.delete(post.id);                             // history is kept; purge(id) removes it too (admin)

// list and search
await staqly.posts.list({ collection_id: "Blog posts", status: "scheduled", limit: 50 });
await staqly.posts.list({ publish_after: "2026-11-01", publish_before: "2026-11-08" }); // the calendar
await staqly.posts.list({ channel_id: tg.id, tag: "launch" });
await staqly.posts.search("launch");
for await (const p of staqly.posts.iterate({ status: "draft" })) console.log(p.content);
await staqly.posts.tags(); // [{ name, count }], most used first
```

Statuses: `draft`, `scheduled`, `publishing`, `published`, `failed`. Scheduling and publishing
need the `posts:publish` scope; creating and editing need `posts:write`. A published or
publishing post cannot be edited (`ConflictError`, code `not_editable`); `expected_version` makes
an edit conditional (code `post_changed`).

## Your own automation

A channel of provider `custom` is **you**: when a post with such a target publishes, staqly
emits `post.publish_requested`, lists the target under `work.list()` and waits for your result
(until the collection's processing timeout). Report what you did:

```ts
for (const w of await staqly.work.list()) {
  if (w.kind === "target" && w.channel!.id === mine.id) {
    const url = await myPlatform.publish(w.post!.content);
    await staqly.posts.reportResult(w.post!.id, mine.id, { status: "published", url });
    // or: { status: "failed", error: "API 500" }
  }
  if (w.kind === "posts") {
    // a schedule with source "agent" found nothing to publish: write new drafts into w.collection
  }
}
```

The same two situations arrive as webhooks (`post.publish_requested`, `schedule.posts_requested`)
for push instead of polling.

## Schedules

A schedule publishes a collection's waiting drafts: once at a time, or on a cadence.

```ts
const s = await staqly.schedules.create({
  collection: "Blog posts",
  name: "Weekday mornings",
  kind: "recurring",
  rule: { times: ["09:00"], weekdays: [1, 2, 3, 4, 5], tz: "Europe/Warsaw" }, // 0 = Sunday; or { every_minutes: 360 }
  channels: [tg.id],      // added to posts without their own targets
  pick: "next",           // the oldest waiting draft; "all"; or "selected" with post_ids
  source: "agent",        // when nothing waits: ask an agent (schedule.posts_requested); "endpoint" fetches from endpoint_url
});
await staqly.schedules.create({ collection: "Launch", name: "Launch day", kind: "once", run_at: "2026-11-03T09:00:00Z", pick: "all" });
await staqly.schedules.list({ collection_id: "Blog posts" }); await staqly.schedules.get(s.id);
await staqly.schedules.update(s.id, { rule: { times: ["08:00", "17:30"] } }); // the next run is recomputed
await staqly.schedules.disable(s.id); await staqly.schedules.enable(s.id);
await staqly.schedules.run(s.id);     // now; the regular next run is unchanged
await staqly.schedules.delete(s.id);
```

## Jobs

Publishing, schedule runs and sign-in refreshes run as jobs with retries (at most 3 attempts,
1, 5 and 15 minutes apart).

```ts
await staqly.jobs.stats();                                 // { queued, running, failed, done_24h, by_kind }
await staqly.jobs.list({ status: "failed" });              // { data, next_cursor }
for await (const j of staqly.jobs.iterate({ kind: "publish_post" })) console.log(j.last_error);
await staqly.jobs.retry(jobId);                            // a failed job, now
await staqly.jobs.cancel(jobId);                           // a queued job
```

## Media

```ts
import { fileFromPath } from "@staqly/sdk/node"; // Node/Bun/Deno only
const media = await staqly.media.upload(await fileFromPath("clip.mp4"));      // 415 for SVG/HTML
await staqly.media.upload(bytes, { filename: "a.png", contentType: "image/png" }); // Blob, File, ArrayBuffer or Uint8Array
const bytes = await staqly.media.download(media.id);        // Uint8Array
const res = await staqly.media.downloadResponse(media.id);  // raw Response (streaming)
staqly.media.url(media.id);                                 // absolute URL (needs auth)
await staqly.media.delete(media.id);                        // posts that use it lose it
```

## Events

The workspace activity log, newest first:

```ts
const page = await staqly.events.list({ type: "post.published", collection_id: col.id, limit: 100 });
for await (const ev of staqly.events.iterate({ post_id: post.id })) console.log(ev.type, ev.actor.name);
await staqly.events.list({ channel_id: tg.id }); await staqly.events.list({ schedule_id: s.id, agent: "claude-code" });
```

Event types: `post.created`, `post.updated`, `post.scheduled`, `post.cancelled`,
`post.publish_requested`, `post.target_published`, `post.published`, `post.failed`,
`post.deleted`, `post.purged`, `channel.connected`, `channel.needs_reconnect`,
`channel.disabled`, `channel.removed`, `schedule.ran`, `schedule.posts_requested`,
`collection.*`, `webhook.*`.

## Webhooks

```ts
const hook = await staqly.webhooks.create({
  url: "https://my-app.example.com/staqly",
  events: ["post.published", "post.failed", "post.publish_requested"], // or "*" / "post.*"; unknown patterns: 422
  collection_ids: [col.id],                                             // empty = all collections
});
hook.secret; // whsec_... keep it to verify deliveries

await staqly.webhooks.test(hook.id);                                 // queue a webhook.test delivery
await staqly.webhooks.deliveries.list({ webhook_id: hook.id, status: "failed" });
await staqly.webhooks.deliveries.retry(deliveryId);
await staqly.webhooks.rotateSecret(hook.id);
await staqly.webhooks.update(hook.id, { active: false });
await staqly.webhooks.list(); await staqly.webhooks.get(hook.id); await staqly.webhooks.delete(hook.id);
```

### Verifying deliveries

Every delivery is signed. The `X-Staqly-Signature` header is
`sha256=` + hex(HMAC-SHA256(secret, `${X-Staqly-Timestamp}.${rawBody}`)). `verifyWebhook` checks
the signature with a constant-time comparison, rejects timestamps more than 5 minutes off
(`tolerance`, in seconds), and returns the parsed event. It throws `WebhookVerificationError`
otherwise. **Always pass the raw body**. JSON that was parsed and re-serialised will not match.

Deliveries are at-least-once and can arrive out of order, so de-duplicate them on `event.id` (also
sent as `X-Staqly-Event-Id`) and order them by `event.created_at`
([delivery guarantees](https://docs.staqly.ai/automation/webhooks#delivery-guarantees)).

**Next.js route handler (App Router; works on the edge runtime):**

```ts
// app/api/staqly/route.ts
import { verifyWebhook, WebhookVerificationError, type PostEventData } from "@staqly/sdk/webhooks";

export async function POST(req: Request) {
  const body = await req.text();
  try {
    const event = await verifyWebhook<PostEventData>(process.env.STAQLY_WEBHOOK_SECRET!, body, req.headers);
    if (event.type === "post.publish_requested") await publishMyself(event.data.post, event.data.channel!);
    return new Response("ok");
  } catch (e) {
    if (e instanceof WebhookVerificationError) return new Response("invalid signature", { status: 400 });
    throw e;
  }
}
```

**Express:**

```ts
import express from "express";
import { verifyWebhook } from "@staqly/sdk";

const app = express();
app.post("/staqly", express.raw({ type: "application/json" }), async (req, res) => {
  try {
    const event = await verifyWebhook(process.env.STAQLY_WEBHOOK_SECRET!, req.body, req.headers);
    console.log(event.type, event.data);
    res.sendStatus(200);
  } catch {
    res.sendStatus(400);
  }
});
```

**Cloudflare Workers / Hono / Bun.serve:** use the same call with `await request.text()` and
`request.headers`. `isValidWebhook()` returns a boolean, and `computeWebhookSignature()` helps you
write tests for your handler.

## Alerts

Alerts push a message to Telegram, Slack, Discord, ntfy or email when a post is published or
fails, or a channel needs reconnecting. Managing them needs the `alerts:manage` scope. Secrets in
`config` are write-only and come back masked.

```ts
const a = await staqly.alerts.create({
  name: "My phone",
  type: "ntfy",                                   // telegram | slack | discord | ntfy | email
  config: { topic: "acme-posts" },                // type-specific settings
  events: ["post.published", "post.failed", "channel.needs_reconnect"], // the default
  collection_ids: [col.id],
  digest_window_seconds: 300,                     // batch messages (0 = immediately)
});
const { ok, error } = await staqly.alerts.test(a.id); // send a test message now
await staqly.alerts.deliveries(a.id, { limit: 20 });
await staqly.alerts.update(a.id, { config: { priority: 4 } }); // config keys merge; null removes one
await staqly.alerts.disable(a.id); await staqly.alerts.enable(a.id);
await staqly.alerts.list(); await staqly.alerts.delete(a.id);
```

Going over the plan's limit raises `PlanLimitError` with `limit: "alerts"`.

## Exports and purge

```ts
// stream the activity log, oldest first, without buffering it
for await (const ev of staqly.exports.events({ from: "2026-10-01", type_prefix: "post." })) console.log(ev.type);

// CSV or NDJSON straight to a file (Node)
import { saveResponse } from "@staqly/sdk/node";
await saveResponse(await staqly.exports.eventsResponse({ format: "csv" }), "events.csv");

// full workspace export (admin): ZIP with collections, posts, channels (no credentials), schedules, events, media
const job = await staqly.exports.exportWorkspace({ timeout: 10 * 60_000 }); // create + wait until ready
await saveResponse(await staqly.exports.workspaceExportResponse(job.id), "workspace.zip");
// or step by step: createWorkspaceExport() → waitForWorkspaceExport(id) → downloadWorkspaceExport(id)

// delete a post and its whole history (admin); posts.delete keeps the history
await staqly.posts.purge(post.id); // { post_id, events_deleted, media_deleted }
```

The workspace export works on every plan; on Starter it is data only (`includes_media: false`: no
media files in the ZIP). The event export needs a plan with data export (Plus and above: `PlanLimitError`,
`limit: "data_export"`) and `events:read`; without `admin` it leaves out credential and session audit events
(`api_key.*`, `session.*`, `recovery_key.*`, device links). Only one workspace export can run at a
time. Starting another returns a `ConflictError` with `details.export_id`. A failed or expired job
throws `ExportFailedError`.

## Workspace, plans and API keys

```ts
await staqly.workspace.get();
await staqly.workspace.rename("Acme agents"); // admin
const { plan, usage, period, posts_by_status, webhook_deliveries } = await staqly.workspace.usage();
await staqly.plans.list(); // Starter, Plus, Pro, Business: price_month, price_year in currency (eur, per user; yearly twelve months at 20 % off or more), limits (-1 = unlimited)
await staqly.workspace.agents(); // admin: MCP clients and API connections seen, latest first
await staqly.workspace.forgetAgent(agentId); // admin; it comes back if it talks to the workspace again

// Billing (cloud with Stripe; admin scope)
await staqly.billing.get(); // { enabled, plan, subscription_status, plan_expires_at, suspended, has_customer }
const { url } = await staqly.billing.checkout({ plan: "plus", interval: "year" }); // open in a browser to pay; a paying workspace is switched instead
await staqly.billing.portal(); // { url } of the Stripe customer portal

// API keys (cloud; admin scope)
const key = await staqly.apiKeys.create({ name: "writer", scopes: ["collections:read", "posts:read", "posts:write", "media:write"], expires_at: "2027-01-01" });
key.key; // stk_... shown once
await staqly.apiKeys.list(); await staqly.apiKeys.rotate(key.id); await staqly.apiKeys.revoke(key.id);
```

Scopes (also exported as `SCOPES`): `collections:read|write|delete`, `posts:read|write|delete|publish`,
`channels:read|manage`, `schedules:manage`, `media:read|write`, `webhooks:manage`,
`alerts:manage`, `events:read`, `admin`. Writing and publishing are separate: `posts:write`
creates and edits, `posts:publish` schedules, publishes, cancels, retries a target or reports a
result. Deleting needs its own scope: `posts.delete()` and `media.delete()` need `posts:delete`,
`collections.delete()` needs `collections:delete` (`:write` alone answers 403).

## Sessions and device links

Browser sessions are cookies. Methods that create one (`sessions.signInWithApiKey`,
`sessions.recover`, `deviceLinks.redeem`)
only help with a cookie-aware `fetch` (a browser with credentials, or a custom `fetch` with a cookie
jar). Methods marked "session" answer 403 to API keys.

```ts
await staqly.sessions.current();            // same as whoami(), plus the CSRF token
await staqly.sessions.list();               // admin
await staqly.sessions.revoke("ses_q2vbm4kz7xw3dn5hjr6c"); // admin
await staqly.sessions.logout();             // revoke the current session
await staqly.sessions.signInWithApiKey("stk_..."); // admin key (local: the access token) -> new session (no auth)
const { api_key, recovery_key } = await staqly.sessions.recover("staqly-recovery-K7QX2-MB4ZD-W9PLT-R3HVN-6YCJE-A5FUG-T2KWS-8NDQB");
await staqly.deviceLinks.create();          // session or local: one-time sign-in link (5 min)
await staqly.deviceLinks.redeem(token);     // token from the link fragment (no auth)
await staqly.workspace.accessToken();       // local: { token } agents send as Bearer
await staqly.workspace.rotateAccessToken(); // local: { token, sessions_revoked }; the old token stops at once
await staqly.workspace.rotateRecoveryKey(); // session: new recovery key, shown once
await staqly.workspace.delete("Acme agents"); // session, cloud: deletes everything; the name must match
```

## Pagination

`posts.list()`, `jobs.list()` and `events.list()` return one page: `{ data, next_cursor }`. Pass
`next_cursor` back as `cursor` to get the next page, or let `iterate()` follow the cursors lazily:

```ts
let page = await staqly.posts.list({ status: "published", limit: 100 });
while (page.next_cursor) page = await staqly.posts.list({ status: "published", limit: 100, cursor: page.next_cursor });

for await (const post of staqly.posts.iterate({ status: "published" })) {
  if (done(post)) break; // stops fetching
}
```

## Errors

Every error extends `StaqlyError`. HTTP errors extend `APIError` and carry `status`, `code`,
`message`, `fields`, `details`, `requestId`, `headers` and `body`. `requestId` is the server's
`X-Request-Id`; include it in bug reports.

| Class | When |
|---|---|
| `BadRequestError` | 400 malformed request |
| `AuthenticationError` | 401 missing/invalid/revoked key |
| `PlanLimitError` | 402 `plan_limit_exceeded`, with `limit`, `max`, `used`, `plan` |
| `PermissionDeniedError` | 403 missing scope, or the action needs a dashboard session (`isNotLocal` is deprecated: always false now) |
| `NotFoundError` | 404 |
| `ConflictError` | 409, e.g. the post is already publishing (`not_editable`) or the version changed (`post_changed`) |
| `GoneError` | 410 |
| `PayloadTooLargeError` / `UnsupportedMediaTypeError` | 413 / 415 uploads |
| `ValidationError` | 422, with `fields` (`targets.<channel_id>.content` when a provider's limit is broken) |
| `RateLimitError` | 429, with `retryAfter` (seconds) |
| `InternalServerError` | 5xx |
| `APIConnectionError` / `TimeoutError` | no response / request timed out |
| `APIUserAbortError` | your `AbortSignal` fired |
| `WaitTimeoutError` | `waitForPublished` / `waitForWorkspaceExport` gave up (`id`, `post`) |
| `ExportFailedError` | a workspace export job failed or expired (`job`) |
| `WebhookVerificationError` | bad signature or timestamp |

```ts
import { ConflictError, PlanLimitError } from "@staqly/sdk";
try {
  await staqly.webhooks.create({ url });
} catch (e) {
  if (e instanceof PlanLimitError) console.log(`${e.limit}: ${e.used}/${e.max} on ${e.plan}`);
  else throw e;
}
```

## Retries, timeouts and idempotency

- Cloud servers rate-limit each key (600 requests per minute, bursts of 100). A 429 comes with
  `Retry-After`, which the retry logic honours.
- Requests that fail with 408, 429, 500, 502, 503 or 504, or a network error or timeout, are
  retried up to `maxRetries` times (default 2). The delay is 0.5 s · 2ⁿ, capped at 8 s, with
  jitter. A `Retry-After` header is honoured if it is 60 s or less. A longer one raises
  `RateLimitError` immediately.
- Every `POST`, `PUT`, `PATCH` or `DELETE` with a JSON body gets an automatic `Idempotency-Key`, reused
  across its retries. The server then replays the first response instead of applying the request
  twice. Pass `{ idempotencyKey }` to choose the key yourself, for example to de-duplicate across
  process restarts. Keys are kept for 24 h. The server compares JSON bodies by content (key order and spacing don't
  matter), so a key can be shared with the Python SDK for the same request.
- Multipart uploads (`media.upload`) are not covered by server-side idempotency,
  so they are retried only on 429.
- `timeout` (default 60 s) applies to each attempt.

## Example: an agent that writes and publishes

An agent drafts a post with a picture, schedules it for the morning, and a second process (or a
webhook) follows what happened:

```ts
import { Staqly } from "@staqly/sdk";
import { fileFromPath } from "@staqly/sdk/node";

const staqly = new Staqly(); // or { baseUrl, apiKey } in cloud mode

// 1. a draft with media and two channels
const channels = await staqly.channels.list();
const cover = await staqly.media.upload(await fileFromPath("out/cover.png"));
const post = await staqly.posts.create({
  collection: "Blog posts",
  content: "Fresh look for autumn 🍂 Three things we changed and why.",
  media: [cover.id],
  targets: channels.filter((c) => c.status === "connected").map((c) => ({ channel_id: c.id })),
  tags: ["autumn"],
  source: "my-agent",
});

// 2. tomorrow at nine, local time
const nine = new Date(); nine.setDate(nine.getDate() + 1); nine.setHours(9, 0, 0, 0);
await staqly.posts.schedule(post.id, nine);

// 3. later: did it go out?
const result = await staqly.posts.waitForPublished(post.id, { timeout: 26 * 3600_000 });
for (const t of result.targets) console.log(t.channel_name, t.status, t.external_url || t.error);
```

## Types

All models come from the OpenAPI contract: `Post`, `PostTarget`, `Collection`, `Channel`,
`Provider`, `Schedule`, `Job`, `Alert`, `Event`, `Webhook`, `Delivery`, `Workspace`, `APIKey`,
`Plan` and more. Webhook payloads are typed by `WebhookEvent<D>` and `PostEventData`. Low-level
`components` and `paths` are exported for anything else.

## License

Apache-2.0 — see [LICENSE](LICENSE) and [NOTICE](NOTICE).
