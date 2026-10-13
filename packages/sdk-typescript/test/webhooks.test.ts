import { createHmac } from "node:crypto";
import { describe, expect, it } from "vitest";
import { computeWebhookSignature, isValidWebhook, verifyWebhook, WebhookVerificationError } from "../src/index.js";
import * as sub from "../src/webhooks.js";

const secret = "whsec_test";
const body = JSON.stringify({
  id: "evt_1",
  type: "item.approved",
  created_at: "2026-01-01T00:00:00Z",
  workspace_id: "ws_1",
  delivery_id: "dlv_1",
  attempt: 1,
  actor: { type: "local", id: "local", name: "Local" },
  data: { item: { id: "itm_1", status: "approved" }, collection: { id: "col_1", name: "C" } },
});
const now = 1_800_000_000;
const sign = (s: string, ts: number, b: string) =>
  `sha256=${createHmac("sha256", s).update(`${ts}.${b}`).digest("hex")}`;
const headers = (sig = sign(secret, now, body), ts: number | string = now) => ({
  "x-staqly-signature": sig,
  "x-staqly-timestamp": String(ts),
  "x-staqly-event": "item.approved",
});

describe("verifyWebhook", () => {
  it("accepts a valid delivery and returns the parsed event", async () => {
    const ev = await verifyWebhook(secret, body, headers(), { now });
    expect(ev.type).toBe("item.approved");
    expect(ev.data.item).toMatchObject({ id: "itm_1" });
  });

  it("accepts bytes and a Fetch Headers object", async () => {
    const h = new Headers({ "X-Staqly-Signature": sign(secret, now, body), "X-Staqly-Timestamp": String(now) });
    const ev = await verifyWebhook(secret, new TextEncoder().encode(body), h, { now });
    expect(ev.id).toBe("evt_1");
  });

  it("accepts Node-style headers with mixed case and arrays", async () => {
    const ev = await verifyWebhook(
      secret,
      body,
      { "X-Staqly-Signature": [sign(secret, now, body)], "X-Staqly-Timestamp": String(now) },
      { now },
    );
    expect(ev.id).toBe("evt_1");
  });

  it("matches the server's signing scheme", async () => {
    expect(await computeWebhookSignature(secret, now, body)).toBe(sign(secret, now, body));
  });

  const bad: [string, () => Promise<unknown>][] = [
    ["tampered body", () => verifyWebhook(secret, `${body} `, headers(), { now })],
    ["wrong secret", () => verifyWebhook("whsec_other", body, headers(), { now })],
    [
      "expired timestamp",
      () => verifyWebhook(secret, body, headers(sign(secret, now - 301, body), now - 301), { now }),
    ],
    ["future timestamp", () => verifyWebhook(secret, body, headers(sign(secret, now + 301, body), now + 301), { now })],
    ["timestamp swapped", () => verifyWebhook(secret, body, headers(sign(secret, now, body), now - 1), { now })],
    ["missing signature", () => verifyWebhook(secret, body, { "x-staqly-timestamp": String(now) }, { now })],
    [
      "missing timestamp",
      () => verifyWebhook(secret, body, { "x-staqly-signature": sign(secret, now, body) }, { now }),
    ],
    ["malformed signature", () => verifyWebhook(secret, body, headers("sha256=zz"), { now })],
    ["no prefix", () => verifyWebhook(secret, body, headers(sign(secret, now, body).slice(7)), { now })],
    ["empty secret", () => verifyWebhook("", body, headers(), { now })],
  ];
  for (const [name, fn] of bad) {
    it(`rejects: ${name}`, async () => {
      await expect(fn()).rejects.toBeInstanceOf(WebhookVerificationError);
    });
  }

  it("tolerance can be widened or disabled", async () => {
    const old = now - 3600;
    await expect(
      verifyWebhook(secret, body, headers(sign(secret, old, body), old), { now, tolerance: 7200 }),
    ).resolves.toBeTruthy();
    await expect(
      verifyWebhook(secret, body, headers(sign(secret, old, body), old), { now, tolerance: 0 }),
    ).resolves.toBeTruthy();
  });

  it("rejects a valid signature over a non-JSON body", async () => {
    await expect(verifyWebhook(secret, "nope", headers(sign(secret, now, "nope")), { now })).rejects.toThrow(/JSON/);
  });

  it("isValidWebhook returns booleans", async () => {
    expect(await isValidWebhook(secret, body, headers(), { now })).toBe(true);
    expect(await isValidWebhook(secret, "x", headers(), { now })).toBe(false);
  });

  it("is exported from the edge-friendly subpath", () => {
    expect(sub.verifyWebhook).toBe(verifyWebhook);
  });
});
