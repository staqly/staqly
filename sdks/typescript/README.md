# @staqly/sdk

Official TypeScript / JavaScript SDK for **[staqly](https://github.com/staqly/staqly)**, the
local-first workspace where AI agents store generated content (items in collections) and
humans review it (approve, reject with feedback, custom actions).

- Typed end to end: models are generated from the API's OpenAPI contract.
- Works on Node ≥ 20.19, Bun, Deno, browsers and edge runtimes (Cloudflare Workers, Vercel/Next.js
  edge). It uses only `fetch` and Web Crypto and has **no runtime dependencies**.
- ESM-only and tree-shakeable, with a standalone `@staqly/sdk/webhooks` entry point. CommonJS code
  can still `require("@staqly/sdk")` on Node ≥ 20.19 or ≥ 22.12, which load ES modules from `require()`.
- Retries with exponential backoff and jitter, `Retry-After` handling, automatic idempotency keys,
  timeouts and `AbortSignal` support.
- Typed errors, auto-paginating iterators, webhook verification, `waitForDecision` and an approval-gate helper.

```bash
npm install @staqly/sdk
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
- [Workspace, plans, API keys](#workspace-plans-and-api-keys)
- [Sessions, device links and decision links](#sessions-device-links-and-decision-links)
- [Pagination](#pagination)
- [Errors](#errors)
- [Retries, timeouts and idempotency](#retries-timeouts-and-idempotency)
- [Example: Instagram avatar flow](#example-instagram-avatar-flow)
- [Types](#types)

## Quickstart

**Local mode.** A staqly server on your machine, started with `staqly serve` (or the `ghcr.io/staqly/staqly` Docker image),
needs no credentials:

```ts
import { Staqly } from "@staqly/sdk";

const staqly = new Staqly(); // http://localhost:3333

await staqly.collections.create({ name: "Blog drafts" });
const item = await staqly.items.create("Blog drafts", {
  title: "Why local-first?",
  content: "# Draft\n...",
  metadata: { model: "gpt-x", score: 0.82 },
});

const decided = await staqly.items.waitForDecision(item.id); // blocks until a human decides
if (decided.status === "approved") publish(decided.content); // reviewer edits included
else console.log("Rejected:", decided.feedback);
```

**Cloud mode.** Use a hosted instance with an API key (`stk_...`):

```ts
const staqly = new Staqly({
  baseUrl: "https://staqly.example.com",
  apiKey: process.env.STAQLY_API_KEY,
});
```

Don't have a workspace yet? Create one programmatically. It starts on the Free plan, and the key
is shown only once:

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
await staqly.items.list({ status: "pending" }, { timeout: 5_000, maxRetries: 0, signal, headers: { ... } });
await staqly.items.create("Posts", { title: "x" }, { idempotencyKey: `job-${jobId}` });
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

```ts
const col = await staqly.collections.create({
  name: "Instagram",
  description: "Avatar and post candidates",
  actions: [
    // approve and reject are built in; add your own:
    { name: "publish", label: "Publish", event: "item.publish_requested", requires_feedback: false,
      set_status: "processing", allowed_statuses: ["approved"] },
  ],
});
await staqly.collections.list();                       // add { include_archived: true } for archived ones
await staqly.collections.get("Instagram");             // ID or exact name
await staqly.collections.update(col.id, { color: "#e1306c" });
await staqly.collections.setActions(col.id, [...]);
await staqly.collections.archive(col.id);               // unarchive(col.id)
await staqly.collections.delete(col.id, "Instagram");   // permanent; the name must match
await staqly.collections.listItems("Instagram", { status: "pending" }); // one page, same filters as items.list
```

## Items

```ts
// create: collection ID or name; content is a string or any JSON value
const item = await staqly.items.create("Instagram", {
  title: "Caption idea",
  content: "Golden hour 🌅",
  metadata: { score: 0.91 },
  source: "caption-agent",
  external_ref: "job-123",
  expires_in: "24h",                    // undecided items become "expired"
  editable: ["content", "metadata.caption"], // what reviewers may change before approving
});

// upload a file and create the item in one request (type inferred: image/video/file)
import { fileFromPath } from "@staqly/sdk/node"; // Node/Bun/Deno only
const img = await staqly.items.upload("Instagram", await fileFromPath("avatar.png"), {
  title: "New avatar",
  metadata: { prompt: "..." },
});
// ...or pass a Blob, File, ArrayBuffer or Uint8Array:
await staqly.items.upload("Instagram", bytes, { filename: "a.png", contentType: "image/png" });

await staqly.items.get(item.id);
await staqly.items.update(item.id, { metadata_patch: { reviewed_by: "bot" } }); // merge keys (null removes one)
await staqly.items.delete(item.id);                                              // history is kept

// decisions (need the items:review scope)
await staqly.items.approve(item.id, { feedback: "Ship it", edits: { title: "Better title" } });
await staqly.items.reject(item.id, { feedback: "Too dark, brighten it" });
await staqly.items.action(item.id, "publish", { payload: { channel: "ig" } });   // custom action
await staqly.items.listActions(item.id);            // actions allowed in the current status
await staqly.items.bulkAction(["itm_1", "itm_2"], "reject", { feedback: "duplicates" });
await staqly.items.approve(item.id, { expected_version: item.version });         // optimistic concurrency

// after acting on an approved item, record the outcome
await staqly.items.reportResult(item.id, { status: "completed", result: { post_url: "https://..." } });
await staqly.items.reportResult(item.id, { status: "failed", error: "Instagram API 500" });
await staqly.items.reject(item.id, { feedback: "Too dark", follow_up: "regenerate" }); // reject and ask for a new version
await staqly.items.update(item.id, { tags: ["client-a", "urgent"] }); // labels, any status, no new version
await staqly.items.list({ decision: ["approved"], run: "failed", tag: "client-a", archived: false });
await staqly.items.unarchive(item.id); // back to review, its run or its decision

// history
await staqly.items.history(item.id);   // events, newest first
await staqly.items.versions(item.id);  // snapshots before each change

// list and search
await staqly.items.list({ collection: "Instagram", status: "pending", limit: 50 });
await staqly.items.search("golden hour", { status: "approved" });
for await (const it of staqly.items.iterate({ status: "pending" })) console.log(it.title);
```

Statuses: `draft`, `pending` (awaiting review), `approved`, `rejected`, `expired`, `processing`,
`completed`, `failed`, `archived`. `items.update` cannot set `approved` or `rejected`. Use
`approve` and `reject` for that.

## Waiting for a human decision

```ts
import { WaitTimeoutError } from "@staqly/sdk";

try {
  const item = await staqly.items.waitForDecision(id, { timeout: 30 * 60_000 }); // default 10 min
  switch (item.status) {
    case "approved": /* go */ break;
    case "rejected": /* item.feedback says why */ break;
    case "expired":  /* nobody answered in time (expires_in) */ break;
  }
} catch (e) {
  if (e instanceof WaitTimeoutError) console.log("still pending", e.item);
}
```

`waitForDecision` uses the server's long-poll endpoint (`GET /v1/items/{id}/wait`, up to 60 s per
round) and re-issues it until the item leaves `pending`, so it does not busy-poll. It returns as
soon as the item is approved, rejected or expired, or when an action moves it on. Pass
`signal` to cancel the wait, or `onPoll` to log progress. `items.wait(id, { timeout })` runs a
single round.

For long-running services, push beats waiting: subscribe a [webhook](#webhooks) to
`item.approved`, `item.rejected` and `item.expired`.

## Approval gates

`requestApproval` creates an item, waits for the decision and returns it. It never throws on a
rejection:

```ts
const d = await staqly.requestApproval("Outbox", {
  title: "Send the October newsletter?",
  content: draft,
  expires_in: "4h",
  editable: ["content"],
});
if (d.approved) await send(d.item.content); // the reviewer's edited version
else if (d.expired) console.log("no answer in time");
else console.log("rejected:", d.feedback);
```

`requiresApproval` wraps a function so that every call asks a human first. The function runs only
after approval. The item is then reported `completed`, with the function's return value stored as
`result`, or `failed` if the function throws. A rejection or expiry throws `RejectedError`:

```ts
const publish = staqly.requiresApproval(postToInstagram, {
  collection: "Instagram",
  title: (post) => `Publish "${post.caption}"?`,
  content: (post) => post,           // default: the arguments as JSON
  metadata: (post) => ({ account: post.account }),
  expires_in: "24h",
});
await publish({ caption: "Hello", account: "@acme" }); // waits for a human, then posts
```

## Media

```ts
const media = await staqly.media.upload(file, { filename: "clip.mp4" }); // 415 for SVG/HTML
await staqly.items.create("Videos", { title: "Teaser", media_id: media.id });
const bytes = await staqly.media.download(media.id);        // Uint8Array
const res = await staqly.media.downloadResponse(media.id);  // raw Response (streaming)
staqly.media.url(media.id);                                 // absolute URL (needs auth)
await staqly.media.delete(media.id);
```

## Events

The workspace activity log, newest first:

```ts
const page = await staqly.events.list({ type: "item.approved", collection_id: col.id, limit: 100 });
for await (const ev of staqly.events.iterate({ item_id: item.id })) console.log(ev.type, ev.actor.name);
```

## Webhooks

```ts
const hook = await staqly.webhooks.create({
  url: "https://my-app.example.com/staqly",
  events: ["item.approved", "item.rejected", "item.expired"], // or "*" / "item.*"
  collection_ids: [col.id],                                    // empty = all collections
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

Deliveries are at-least-once, so de-duplicate them on `event.id` (also sent as `X-Staqly-Event-Id`).

**Next.js route handler (App Router; works on the edge runtime):**

```ts
// app/api/staqly/route.ts
import { verifyWebhook, WebhookVerificationError, type ItemEventData } from "@staqly/sdk/webhooks";

export async function POST(req: Request) {
  const body = await req.text();
  try {
    const event = await verifyWebhook<ItemEventData>(process.env.STAQLY_WEBHOOK_SECRET!, body, req.headers);
    if (event.type === "item.approved") await publish(event.data.item);
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

## Watchers

Watchers turn RSS feeds and web pages into items on a schedule:

```ts
await staqly.watchers.create({
  name: "Hacker News", kind: "rss", url: "https://news.ycombinator.com/rss", collection: "News",
});
const w = await staqly.watchers.create({
  name: "Competitor pricing", kind: "url_diff", url: "https://example.com/pricing",
  collection: "Research", every: "6h", keywords: ["price"],
});
await staqly.watchers.run(w.id);   // check now
await staqly.watchers.pause(w.id); await staqly.watchers.resume(w.id);
await staqly.watchers.update(w.id, { every: "1h" }); await staqly.watchers.delete(w.id);
```

Leave out `kind` and staqly reads the URL once to pick it (`rss` for a feed, `url_diff`
otherwise). `collections: ["News", "Reading"]` adds each new entry to several collections
(it takes precedence over `collection`).

## Notification channels

Channels push review requests to Telegram, Slack, Discord, ntfy or email, with Approve/Reject
buttons or one-tap links. Managing them needs the `channels:manage` scope. Secrets in `config` are
write-only and come back masked.

```ts
const ch = await staqly.notificationChannels.create({
  name: "My phone",
  type: "ntfy",                                   // telegram | slack | discord | ntfy | email
  config: { topic: "acme-reviews" },              // type-specific settings
  events: ["item.created", "item.escalated"],     // the default
  collection_ids: [col.id],
  digest_window_seconds: 300,                     // batch messages (0 = immediately)
});
const { ok, error } = await staqly.notificationChannels.test(ch.id); // send a test message now
await staqly.notificationChannels.deliveries(ch.id, { limit: 20 });
await staqly.notificationChannels.update(ch.id, { config: { priority: 4 } }); // config keys merge
await staqly.notificationChannels.disable(ch.id); await staqly.notificationChannels.enable(ch.id);
await staqly.notificationChannels.list(); await staqly.notificationChannels.delete(ch.id);
```

On the Free plan you can have 2 channels. Going over the limit raises `PlanLimitError` with
`limit: "notification_channels"`.

## Exports and purge

```ts
// stream the activity log, oldest first, without buffering it
for await (const ev of staqly.exports.events({ from: "2026-10-01", type_prefix: "item." })) console.log(ev.type);

// CSV or NDJSON straight to a file (Node)
import { saveResponse } from "@staqly/sdk/node";
await saveResponse(await staqly.exports.eventsResponse({ format: "csv" }), "events.csv");

// full workspace export (admin): ZIP with collections, items + versions, events, media, settings
const job = await staqly.exports.exportWorkspace({ timeout: 10 * 60_000 }); // create + wait until ready
await saveResponse(await staqly.exports.workspaceExportResponse(job.id), "workspace.zip");
// or step by step: createWorkspaceExport() → waitForWorkspaceExport(id) → downloadWorkspaceExport(id)

// delete an item and its whole history (admin); items.delete keeps the history
await staqly.items.purge(item.id); // { item_id, events_deleted, media_deleted }
```

Only one workspace export can run at a time. Starting another returns a `ConflictError` with
`details.export_id`. A failed or expired job throws `ExportFailedError`.

## Workspace, plans and API keys

```ts
await staqly.workspace.get();
await staqly.workspace.rename("Acme agents"); // admin
const { plan, usage, period } = await staqly.workspace.usage();
await staqly.workspace.setPlan("free"); // admin, cloud; other plans throw PlanLimitError (402)
await staqly.plans.list(); // limits: -1 = unlimited

// Billing (cloud with Stripe; admin scope)
await staqly.billing.get(); // { enabled, plan, subscription_status, plan_expires_at, suspended, has_customer }
const { url } = await staqly.billing.checkout(); // open in a browser to pay for Pro
await staqly.billing.portal(); // { url } of the Stripe customer portal

// API keys (cloud; admin scope)
const key = await staqly.apiKeys.create({ name: "n8n", scopes: ["items:read", "items:write"], expires_at: "2027-01-01" });
key.key; // stk_... shown once
await staqly.apiKeys.list(); await staqly.apiKeys.rotate(key.id); await staqly.apiKeys.revoke(key.id);
```

Scopes: `collections:read`, `collections:write`, `items:read`, `items:write`, `items:review`,
`actions:execute`, `webhooks:manage`, `watchers:manage`, `channels:manage`, `admin` (also exported
as `SCOPES`).

## Sessions, device links and decision links

Browser sessions are cookies. Methods that create one (`sessions.recover`, `deviceLinks.redeem`)
only help with a cookie-aware `fetch` (a browser with credentials, or a custom `fetch` with a cookie
jar). Methods marked "session" answer 403 to API keys.

```ts
await staqly.sessions.current();            // same as whoami(), plus the CSRF token
await staqly.sessions.list();               // admin
await staqly.sessions.revoke("ses_q2vbm4kz7xw3dn5hjr6c"); // admin
await staqly.sessions.logout();             // revoke the current session
await staqly.sessions.recover("staqly-recovery-K7QX2-MB4ZD-W9PLT-R3HVN-6YCJE-A5FUG-T2KWS-8NDQB"); // recovery key -> new session (no auth)
await staqly.deviceLinks.create();          // session or local: one-time sign-in link (5 min)
await staqly.deviceLinks.redeem(token);     // token from the link fragment (no auth)
await staqly.workspace.rotateRecoveryKey(); // session: new recovery key, shown once
await staqly.workspace.delete("Acme agents"); // session, cloud: deletes everything; the name must match

// One-tap decision links from notification channels (the token is the credential)
await staqly.decisionLinks.decide(token, { feedback: "Wrong tone" }); // approve or reject, single use
```

## Pagination

`items.list()` and `events.list()` return one page: `{ data, next_cursor }`. Pass `next_cursor`
back as `cursor` to get the next page, or let `iterate()` follow the cursors lazily for you:

```ts
let page = await staqly.items.list({ status: "approved", limit: 100 });
while (page.next_cursor) page = await staqly.items.list({ status: "approved", limit: 100, cursor: page.next_cursor });

for await (const item of staqly.items.iterate({ status: "approved" })) {
  if (done(item)) break; // stops fetching
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
| `PermissionDeniedError` | 403 missing scope; `isNotLocal` when a local server refused a non-local caller |
| `NotFoundError` | 404 |
| `ConflictError` | 409, e.g. the item was already decided or the version changed |
| `GoneError` | 410 |
| `PayloadTooLargeError` / `UnsupportedMediaTypeError` | 413 / 415 uploads |
| `ValidationError` | 422, with `fields` (code `not_editable` for edits outside `editable`) |
| `RateLimitError` | 429, with `retryAfter` (seconds) |
| `InternalServerError` | 5xx |
| `APIConnectionError` / `TimeoutError` | no response / request timed out |
| `APIUserAbortError` | your `AbortSignal` fired |
| `WaitTimeoutError` | `waitForDecision` / `waitForWorkspaceExport` gave up (`itemId`, `item`) |
| `ExportFailedError` | a workspace export job failed or expired (`job`) |
| `RejectedError` | a `requiresApproval` call was rejected or expired |
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
- Every `POST`, `PATCH` or `DELETE` with a JSON body gets an automatic `Idempotency-Key`, reused
  across its retries. The server then replays the first response instead of applying the request
  twice. Pass `{ idempotencyKey }` to choose the key yourself, for example to de-duplicate across
  process restarts. Keys are kept for 24 h.
- Multipart uploads (`items.upload`, `media.upload`) are not covered by server-side idempotency,
  so they are retried only on 429.
- `timeout` (default 60 s) applies to each attempt. `items.wait` raises it automatically to
  outlast the server-side long poll.

## Example: Instagram avatar flow

An agent generates an avatar. A human approves it, possibly editing the caption, and the agent
then publishes it and records where:

```ts
import { Staqly } from "@staqly/sdk";
import { fileFromPath } from "@staqly/sdk/node";

const staqly = new Staqly(); // or { baseUrl, apiKey } in cloud mode

// 1. store the generated image for review
const item = await staqly.items.upload("Instagram", await fileFromPath("out/avatar.png"), {
  title: "New profile picture",
  content: "Caption: Fresh look for autumn 🍂",
  metadata: { account: "@acme", prompt: "autumn portrait, warm light" },
  editable: ["content"],
  expires_in: "24h",
});

// 2. wait for the human (or get a webhook instead)
const decided = await staqly.items.waitForDecision(item.id, { timeout: 24 * 3600_000 });
if (decided.status !== "approved") {
  console.log(`not publishing (${decided.status}): ${decided.feedback}`);
  process.exit(0);
}

// 3. publish with the approved (possibly edited) caption, then report the outcome
try {
  const image = await staqly.media.download(item.media!.id); // Uint8Array
  const post = await instagram.setAvatar(image, decided.content);
  await staqly.items.reportResult(item.id, { status: "completed", result: { post_url: post.url } });
} catch (err) {
  await staqly.items.reportResult(item.id, { status: "failed", error: String(err) });
}
// equivalent low-level form: items.update(id, { status: "completed", metadata_patch: { post_url } })
```

## Types

All models come from the OpenAPI contract: `Item`, `Collection`, `Event`, `Webhook`, `Delivery`,
`Watcher`, `Workspace`, `APIKey`, `Plan` and more. Webhook payloads are typed by
`WebhookEvent<D>` and `ItemEventData`. Low-level `components` and `paths` are exported for
anything else.

## License

Apache-2.0 — see [LICENSE](LICENSE) and [NOTICE](NOTICE).
