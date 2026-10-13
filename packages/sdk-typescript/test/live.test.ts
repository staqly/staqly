// Live integration tests against a real server. Skipped unless STAQLY_LIVE=1.
//
//   STAQLY_LIVE=1 STAQLY_API_URL=http://127.0.0.1:3333 npm test            # local mode (keyless)
//   STAQLY_LIVE=1 STAQLY_API_URL=http://127.0.0.1:3334 npm test            # cloud mode: a workspace is created
//   STAQLY_LIVE=1 STAQLY_API_URL=https://… STAQLY_API_KEY=stk_… npm test   # cloud mode with your key
//
// Webhook signatures are checked end to end when the server may deliver to
// loopback (WEBHOOK_ALLOW_LOOPBACK=true); otherwise only the delivery record is.
import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import {
  ConflictError,
  type Item,
  NotFoundError,
  PermissionDeniedError,
  PlanLimitError,
  SCOPES,
  Staqly,
  ValidationError,
  verifyWebhook,
  type WebhookEvent,
} from "../src/index.js";

const live = process.env.STAQLY_LIVE === "1";
const PNG = Uint8Array.from(
  Buffer.from(
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==",
    "base64",
  ),
);

describe.skipIf(!live)("live", () => {
  let c: Staqly;
  let admin: Staqly | undefined; // cloud: the signup browser session (all scopes)
  let mode: "local" | "cloud";
  const name = `SDK-TS ${Date.now()}`;
  let colId = "";
  const received: { headers: Record<string, string | string[] | undefined>; body: string }[] = [];
  let receiver: Server;
  let receiverURL = "";

  beforeAll(async () => {
    const probe = new Staqly({ apiKey: null });
    const setup = await probe.http.request<{ mode: "local" | "cloud" }>({ method: "GET", path: "/v1/setup" });
    mode = setup.mode;
    let apiKey = process.env.STAQLY_API_KEY;
    if (mode === "cloud" && !apiKey) {
      // Signup returns a default key (no admin, no rules:manage) and a session
      // cookie. Use the session to mint a key that can also manage rules.
      const res = await probe.http.request<Response>({
        method: "POST",
        path: "/v1/workspaces",
        body: { name: `sdk-ts-${Date.now()}` },
        noAuth: true,
        raw: true,
      });
      const ws = (await res.json()) as { api_key: { key: string }; csrf_token: string };
      expect(ws.api_key.key).toMatch(/^stk_/);
      const cookie = res.headers
        .getSetCookie()
        .map((v) => v.split(";")[0])
        .join("; ");
      admin = new Staqly({ apiKey: null, defaultHeaders: { Cookie: cookie, "X-CSRF-Token": ws.csrf_token } });
      const key = await admin.apiKeys.create({ name: "sdk live", scopes: SCOPES.filter((s) => s !== "admin") });
      apiKey = key.key;
    }
    c = new Staqly({ apiKey: apiKey ?? null });
    receiver = createServer((req, res) => {
      const chunks: Buffer[] = [];
      req.on("data", (d) => chunks.push(d));
      req.on("end", () => {
        received.push({ headers: req.headers, body: Buffer.concat(chunks).toString("utf8") });
        res.writeHead(200).end("ok");
      });
    });
    await new Promise<void>((r) => receiver.listen(0, "127.0.0.1", r));
    receiverURL = `http://127.0.0.1:${(receiver.address() as AddressInfo).port}/hook`;
  });

  afterAll(async () => {
    receiver?.close();
    if (colId) await c.collections.delete(colId, name).catch(() => undefined);
  });

  it("health, whoami, workspace, usage, plans", async () => {
    expect((await c.health()).status).toBe("ok");
    const me = await c.whoami();
    expect(me.mode).toBe(mode);
    const ws = await c.workspace.get();
    expect(ws.id).toMatch(/^ws_/);
    const usage = await c.workspace.usage();
    expect(usage.plan.id).toBe(mode === "local" ? "unlocked" : "free");
    const plans = await c.plans.list();
    expect(plans.map((p) => p.id)).toContain("free");
  });

  it("collections: create, get by name, update", async () => {
    const col = await c.collections.create({ name, description: "SDK live test" });
    colId = col.id;
    expect((await c.collections.get(name)).id).toBe(colId);
    expect((await c.collections.update(colId, { color: "#ff0000" })).color).toBe("#ff0000");
    expect((await c.collections.list()).some((x) => x.id === colId)).toBe(true);
    await expect(c.collections.get("no such collection 123")).rejects.toBeInstanceOf(NotFoundError);
  });

  it("Instagram avatar flow: upload → wait (approved by someone else) → report publication", async () => {
    const item = await c.items.upload(name, PNG, {
      filename: "avatar.png",
      title: "New avatar",
      content: "Caption: hello",
      metadata: { account: "@acme", score: 0.5 },
      editable: ["content", "metadata.caption"],
      expires_in: "24h",
    });
    expect(item.type).toBe("image");
    expect(item.status).toBe("pending");
    expect(item.media?.mime_type).toBe("image/png");
    expect(item.expires_at).toBeTruthy();
    expect(await c.media.download(item.media!.id)).toEqual(PNG);

    // A reviewer approves from "another process" while we wait.
    const reviewer = new Staqly({ apiKey: c.http.apiKey ?? null });
    const timer = setTimeout(() => {
      void reviewer.items.approve(item.id, { feedback: "love it", edits: { content: "Caption: hello!" } });
    }, 1500);
    const started = Date.now();
    const decided = await c.items.waitForDecision(item.id, { timeout: 30_000 });
    clearTimeout(timer);
    expect(decided.status).toBe("approved");
    expect(decided.content).toBe("Caption: hello!");
    expect(decided.feedback).toBe("love it");
    expect(Date.now() - started).toBeLessThan(15_000);

    const done = await c.items.reportResult(item.id, {
      status: "completed",
      result: { post_url: "https://instagram.com/p/123" },
    });
    expect(done.status).toBe("completed");
    expect(done.metadata.result).toEqual({ post_url: "https://instagram.com/p/123" });

    // the approval event carries the diff of the reviewer's edits
    const history = await c.items.history(item.id);
    const approved = history.data.find((e) => e.type === "item.approved");
    expect(approved).toBeDefined();
    expect((approved!.data as { diff?: Record<string, unknown> }).diff).toHaveProperty("content");
  });

  it("legacy publication report via items.update", async () => {
    const it = await c.items.create(colId, { title: "post 2", content: "x" });
    await c.items.approve(it.id);
    const done = await c.items.update(it.id, { status: "completed", metadata_patch: { post_url: "https://x/2" } });
    expect(done.status).toBe("completed");
    expect(done.metadata.post_url).toBe("https://x/2");
  });

  it("editable fields are enforced; double decisions conflict", async () => {
    const it = await c.items.create(name, { title: "locked", content: "body", editable: ["title"] });
    const err = (await c.items.approve(it.id, { edits: { content: "changed" } }).catch((e) => e)) as ValidationError;
    expect(err).toBeInstanceOf(ValidationError);
    expect(err.code).toBe("not_editable");
    const rejected = await c.items.reject(it.id, { feedback: "too short" });
    expect(rejected.status).toBe("rejected");
    await expect(c.items.approve(it.id)).rejects.toBeInstanceOf(ConflictError);
    expect((await c.items.waitForDecision(it.id, { timeout: 5_000 })).status).toBe("rejected");
  });

  it("list, search, iterate, bulk actions, versions", async () => {
    const made: Item[] = [];
    for (let i = 0; i < 5; i++) {
      made.push(await c.items.create(name, { title: `zebra note ${i}`, content: `stripes ${i}`, source: "sdk-ts" }));
    }
    const page = await c.items.list({ collection: name, limit: 2 });
    expect(page.data).toHaveLength(2);
    expect(page.next_cursor).toBeTruthy();
    const all: Item[] = [];
    for await (const it of c.items.iterate({ collection: name, source: "sdk-ts", limit: 2 })) all.push(it);
    expect(all.map((i) => i.id).toSorted()).toEqual(made.map((i) => i.id).toSorted());
    const found = await c.items.search("zebr", { collection_id: colId });
    expect(found.data.length).toBe(5);
    const bulk = await c.items.bulkAction(
      made.slice(0, 2).map((i) => i.id),
      "reject",
      { feedback: "bulk" },
    );
    expect(bulk.every((r) => r.ok)).toBe(true);
    const actions = await c.items.listActions(made[2]!.id);
    expect(actions.map((a) => a.name)).toContain("approve");
    await c.items.update(made[2]!.id, { title: "renamed" });
    expect((await c.items.versions(made[2]!.id)).length).toBeGreaterThan(0);
    await c.items.delete(made[4]!.id);
    await expect(c.items.get(made[4]!.id)).rejects.toBeInstanceOf(NotFoundError);
  });

  it("events: list and iterate", async () => {
    const page = await c.events.list({ collection_id: colId, type: "item.created", limit: 3 });
    expect(page.data.length).toBe(3);
    let n = 0;
    for await (const e of c.events.iterate({ collection_id: colId, type: "item.created", limit: 3 })) {
      expect(e.type).toBe("item.created");
      n++;
    }
    expect(n).toBeGreaterThanOrEqual(8);
  });

  it("webhooks: create, test, deliveries, verify signature end to end", async () => {
    const hook = await c.webhooks.create({ url: receiverURL, events: ["*"], description: "sdk-ts live" });
    expect(hook.secret).toMatch(/^whsec_/);
    expect((await c.webhooks.get(hook.id)).url).toBe(receiverURL);
    const delivery = await c.webhooks.test(hook.id);
    expect(delivery.event_type).toBe("webhook.test");
    let status = "pending";
    for (let i = 0; i < 40 && status === "pending"; i++) {
      await new Promise((r) => setTimeout(r, 250));
      const list = await c.webhooks.deliveries.list({ webhook_id: hook.id });
      status = list.find((d) => d.id === delivery.id)?.status ?? "pending";
    }
    if (status === "succeeded") {
      const got = received.find((r) => r.headers["x-staqly-delivery"] === delivery.id)!;
      const ev: WebhookEvent = await verifyWebhook(hook.secret, got.body, got.headers);
      expect(ev.type).toBe("webhook.test");
      await expect(verifyWebhook("whsec_wrong", got.body, got.headers)).rejects.toThrow();
      const retried = await c.webhooks.deliveries.retry(delivery.id);
      expect(retried.status).toBe("pending");
    } else {
      console.warn(`webhook delivery ${status}: server does not deliver to loopback; signature check skipped`);
    }
    const rotated = await c.webhooks.rotateSecret(hook.id);
    expect(rotated.secret).not.toBe(hook.secret);
    expect((await c.webhooks.update(hook.id, { active: false })).active).toBe(false);
    await c.webhooks.delete(hook.id);
  });

  it("rules: auto-approve on metadata", async () => {
    const rule = await c.rules.create({
      name: "auto-approve confident",
      action: "auto_approve",
      match: { collection_ids: [colId], conditions: [{ path: "score", op: "gte", value: 0.9 }] },
      params: { feedback: "auto" },
    });
    try {
      const hi = await c.items.create(colId, { title: "confident", metadata: { score: 0.95 } });
      const lo = await c.items.create(colId, { title: "unsure", metadata: { score: 0.2 } });
      expect(hi.status).toBe("approved");
      expect(lo.status).toBe("pending");
      const d = await c.requestApproval(colId, { title: "gate", metadata: { score: 0.99 } });
      expect(d.approved).toBe(true);
      expect((await c.rules.get(rule.id)).times_applied).toBeGreaterThanOrEqual(2);
      expect((await c.rules.disable(rule.id)).enabled).toBe(false);
    } finally {
      await c.rules.delete(rule.id);
    }
  });

  it("watchers: presets and CRUD (no fetch assertions)", async () => {
    const presets = await c.watchers.listPresets();
    expect(presets.length).toBeGreaterThan(0);
    const w = await c.watchers.create({
      name: "example",
      kind: "url_diff",
      url: "https://example.com/",
      collection: name,
      every: "1h",
    });
    expect(w.collection_id).toBe(colId);
    expect((await c.watchers.pause(w.id)).status).toBe("paused");
    expect((await c.watchers.resume(w.id)).status).not.toBe("paused");
    expect((await c.watchers.list()).some((x) => x.id === w.id)).toBe(true);
    await c.watchers.delete(w.id);
  });

  it("notification channels: CRUD, test, deliveries", async () => {
    const ch = await c.notificationChannels.create({
      name: "sdk phone",
      type: "ntfy",
      config: {
        topic: `sdk-${Date.now()}`,
        server_url: receiverURL.replace("/hook", ""),
        access_token: "secret-token",
      },
      events: ["item.created"],
      collection_ids: [colId],
    });
    expect(ch.type).toBe("ntfy");
    expect(JSON.stringify(ch.config)).not.toContain("secret-token"); // secrets come back masked
    expect((await c.notificationChannels.get(ch.id)).name).toBe("sdk phone");
    expect((await c.notificationChannels.list()).some((x) => x.id === ch.id)).toBe(true);
    const result = await c.notificationChannels.test(ch.id);
    expect(typeof result.ok).toBe("boolean");
    expect(Array.isArray(await c.notificationChannels.deliveries(ch.id, { limit: 10 }))).toBe(true);
    expect((await c.notificationChannels.disable(ch.id)).enabled).toBe(false);
    expect((await c.notificationChannels.update(ch.id, { digest_window_seconds: 60 })).digest_window_seconds).toBe(60);
    await c.notificationChannels.delete(ch.id);
    await expect(c.notificationChannels.get(ch.id)).rejects.toBeInstanceOf(NotFoundError);
  });

  it("exports: stream events, export the workspace, purge an item", async () => {
    const events = [];
    for await (const e of c.exports.events({ collection_id: colId, type: "item.created" })) events.push(e);
    expect(events.length).toBeGreaterThanOrEqual(8);
    expect(events[0]!.created_at <= events.at(-1)!.created_at).toBe(true); // oldest first
    const csv = await (await c.exports.eventsResponse({ format: "csv", collection_id: colId })).text();
    expect(csv.split("\n")[0]).toContain("created_at,id,type");

    const owner = admin ?? c; // workspace export + purge need admin
    const job = await owner.exports.exportWorkspace({ timeout: 60_000, pollInterval: 250 });
    expect(job.status).toBe("ready");
    const zip = await owner.exports.downloadWorkspaceExport(job.id);
    expect([zip[0], zip[1]]).toEqual([0x50, 0x4b]); // "PK"

    const doomed = await c.items.create(colId, { title: "to purge" });
    const purged = await owner.items.purge(doomed.id);
    expect(purged.item_id).toBe(doomed.id);
    expect(purged.events_deleted).toBeGreaterThanOrEqual(1);
    await expect(c.items.get(doomed.id)).rejects.toBeInstanceOf(NotFoundError);
    expect((await c.ready()).status).toBe("ok");
  });

  it("cloud: plan limits and scopes", async () => {
    if (mode !== "cloud") return;
    const hooks = [];
    try {
      const max = (await c.workspace.usage()).plan.limits.webhooks;
      const existing = (await c.webhooks.list()).length;
      for (let i = existing; i < max; i++) hooks.push(await c.webhooks.create({ url: "https://example.com/hook" }));
      const err = (await c.webhooks.create({ url: "https://example.com/hook" }).catch((e) => e)) as PlanLimitError;
      expect(err).toBeInstanceOf(PlanLimitError);
      expect(err.status).toBe(402);
      expect(err.limit).toBe("webhooks");
      expect(err.max).toBe(max);
      expect(err.plan).toBe("free");
    } finally {
      for (const h of hooks) await c.webhooks.delete(h.id);
    }
    // the automation key is not admin: key management is forbidden
    await expect(c.apiKeys.list()).rejects.toBeInstanceOf(PermissionDeniedError);
    if (admin) {
      const keys = await admin.apiKeys.list();
      expect(keys.length).toBeGreaterThanOrEqual(2);
      const extra = await admin.apiKeys.create({ name: "temp", scopes: ["items:read"], expires_at: "2099-01-01" });
      const rotated = await admin.apiKeys.rotate(extra.id);
      expect(rotated.key).toMatch(/^stk_/);
      expect(rotated.id).not.toBe(extra.id);
      const ro = new Staqly({ apiKey: rotated.key! });
      await expect(ro.collections.create({ name: "nope" })).rejects.toBeInstanceOf(PermissionDeniedError);
      await admin.apiKeys.revoke(rotated.id);
      await expect(ro.items.list()).rejects.toMatchObject({ status: 401 });
      expect((await admin.workspace.rename("SDK live renamed")).name).toBe("SDK live renamed");
    }
  });
});
