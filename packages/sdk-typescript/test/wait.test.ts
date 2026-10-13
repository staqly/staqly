import { describe, expect, it } from "vitest";
import { ConflictError, RejectedError, WaitTimeoutError } from "../src/index.js";
import { client, item, json } from "./helpers.js";

describe("waitForDecision", () => {
  it("long-polls until decided", async () => {
    const { c, calls } = client(
      json({ decided: false, item: item() }),
      json({ decided: true, item: item({ status: "approved" }) }),
    );
    const seen: string[] = [];
    const out = await c.items.waitForDecision("itm_1", { timeout: 120_000, onPoll: (i) => seen.push(i.status) });
    expect(out.status).toBe("approved");
    expect(seen).toEqual(["pending", "approved"]);
    expect(calls.map((x) => x.url.pathname + x.url.search)).toEqual([
      "/v1/items/itm_1/wait?timeout=60",
      "/v1/items/itm_1/wait?timeout=60",
    ]);
  });

  it("asks the server to wait only for the remaining time", async () => {
    const { c, calls } = client(json({ decided: true, item: item({ status: "rejected" }) }));
    await c.items.waitForDecision("itm_1", { timeout: 4_500 });
    expect(calls[0]!.url.searchParams.get("timeout")).toBe("5");
  });

  it("treats expired as decided", async () => {
    const { c } = client(json({ decided: true, item: item({ status: "expired" }) }));
    expect((await c.items.waitForDecision("itm_1")).status).toBe("expired");
  });

  it("throws WaitTimeoutError when still pending at the deadline", async () => {
    const { c } = client(json({ decided: false, item: item() }));
    const err = (await c.items.waitForDecision("itm_1", { timeout: 1 }).catch((e) => e)) as WaitTimeoutError;
    expect(err).toBeInstanceOf(WaitTimeoutError);
    expect(err.itemId).toBe("itm_1");
    expect(err.item?.status).toBe("pending");
  });

  it("can be aborted", async () => {
    const ctrl = new AbortController();
    ctrl.abort();
    const { c } = client(json({ decided: false, item: item() }));
    await expect(c.items.waitForDecision("itm_1", { signal: ctrl.signal })).rejects.toThrow(/abort/i);
  });
});

describe("requestApproval", () => {
  it("creates the item and waits", async () => {
    const { c, calls } = client(
      json(item(), 201),
      json({ decided: true, item: item({ status: "rejected", feedback: "no" }) }),
    );
    const d = await c.requestApproval("Outbox", { title: "Send?", content: "hi", expires_in: 3600 });
    expect(d).toMatchObject({ approved: false, expired: false, status: "rejected", feedback: "no" });
    expect(calls[0]!.body).toEqual({ title: "Send?", content: "hi", expires_in: 3600 });
  });

  it("returns immediately when a rule already decided the item", async () => {
    const { c, calls } = client(json(item({ status: "approved" }), 201));
    const d = await c.requestApproval("Outbox", { title: "x" });
    expect(d.approved).toBe(true);
    expect(calls).toHaveLength(1);
  });
});

describe("requiresApproval", () => {
  it("runs the function after approval and reports the result", async () => {
    const { c, calls } = client(
      json(item(), 201),
      json({ decided: true, item: item({ status: "approved" }) }),
      json(item({ status: "completed" })),
    );
    const publish = c.requiresApproval(async (caption: string, n: number) => ({ url: `https://x/${caption}/${n}` }), {
      collection: "Instagram",
      title: (caption) => `Publish ${caption}?`,
      metadata: (_c, n) => ({ n }),
      editable: ["content"],
    });
    expect(await publish("hello", 2)).toEqual({ url: "https://x/hello/2" });
    expect(calls[0]!.body).toEqual({
      title: "Publish hello?",
      content: ["hello", 2],
      metadata: { n: 2 },
      editable: ["content"],
    });
    expect(calls[2]!.path).toBe("/v1/items/itm_1/result");
    expect(calls[2]!.body).toEqual({ status: "completed", result: { url: "https://x/hello/2" } });
  });

  it("throws RejectedError without running the function", async () => {
    let ran = false;
    const { c } = client(
      json(item(), 201),
      json({ decided: true, item: item({ status: "rejected", feedback: "nope" }) }),
    );
    const gated = c.requiresApproval(
      async () => {
        ran = true;
      },
      { collection: "X", title: "t" },
    );
    const err = (await gated().catch((e) => e)) as RejectedError;
    expect(err).toBeInstanceOf(RejectedError);
    expect(err.feedback).toBe("nope");
    expect(ran).toBe(false);
  });

  it("reports failures and rethrows", async () => {
    const { c, calls } = client(
      json(item(), 201),
      json({ decided: true, item: item({ status: "approved" }) }),
      json(item({ status: "failed" })),
    );
    const gated = c.requiresApproval(
      () => {
        throw new Error("API down");
      },
      { collection: "X", title: "t" },
    );
    await expect(gated()).rejects.toThrow("API down");
    expect(calls[2]!.body).toEqual({ status: "failed", error: "API down" });
  });

  it("surfaces conflicts from reporting", async () => {
    const { c } = client(
      json(item(), 201),
      json({ decided: true, item: item({ status: "approved" }) }),
      json({ error: { code: "conflict", message: "x" } }, 409),
    );
    const gated = c.requiresApproval(() => 1, { collection: "X", title: "t" });
    await expect(gated()).rejects.toBeInstanceOf(ConflictError);
  });
});
