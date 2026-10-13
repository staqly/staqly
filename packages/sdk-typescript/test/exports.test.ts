import { describe, expect, it } from "vitest";
import { ExportFailedError, InternalServerError, WaitTimeoutError } from "../src/index.js";
import { client, json } from "./helpers.js";

const ev = (id: string) => ({
  id,
  seq: 1,
  type: "item.created",
  collection_id: null,
  item_id: null,
  actor: { type: "local", id: "l", name: "L" },
  data: {},
  created_at: "2026-01-01T00:00:00Z",
});

function chunked(...parts: string[]): Response {
  const enc = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    start(ctrl) {
      for (const p of parts) ctrl.enqueue(enc.encode(p));
      ctrl.close();
    },
  });
  return new Response(body, { headers: { "Content-Type": "application/x-ndjson" } });
}

describe("exports", () => {
  it("streams NDJSON events split across chunks", async () => {
    const lines = [ev("e1"), ev("e2"), ev("e3")].map((e) => JSON.stringify(e));
    const all = `${lines.join("\n")}\n`;
    const { c, calls } = client(chunked(all.slice(0, 10), all.slice(10, 150), all.slice(150)));
    const ids: string[] = [];
    for await (const e of c.exports.events({ from: "2026-10-01", type_prefix: "item." })) ids.push(e.id);
    expect(ids).toEqual(["e1", "e2", "e3"]);
    expect(calls[0]!.path).toBe("/v1/events/export");
    expect(Object.fromEntries(calls[0]!.url.searchParams)).toEqual({
      from: "2026-10-01",
      type_prefix: "item.",
      format: "ndjson",
    });
  });

  it("handles a final line without newline", async () => {
    const { c } = client(chunked(JSON.stringify(ev("a")), "\n", JSON.stringify(ev("b"))));
    const ids: string[] = [];
    for await (const e of c.exports.events()) ids.push(e.id);
    expect(ids).toEqual(["a", "b"]);
  });

  it("eventsResponse returns the raw CSV response", async () => {
    const { c, calls } = client(new Response("created_at,id\n", { headers: { "Content-Type": "text/csv" } }));
    const res = await c.exports.eventsResponse({ format: "csv", item_id: "itm_1" });
    expect(await res.text()).toBe("created_at,id\n");
    expect(calls[0]!.url.searchParams.get("format")).toBe("csv");
  });

  it("export errors are typed", async () => {
    const { c } = client(json({ error: { code: "internal", message: "x" } }, 500));
    await expect(c.exports.events({}, { maxRetries: 0 }).next()).rejects.toBeInstanceOf(InternalServerError);
  });

  it("exportWorkspace waits until ready, then downloads", async () => {
    const job = { id: "exp_1", size_bytes: 0, created_at: "t", started_at: null, finished_at: null, expires_at: "t" };
    const { c, calls } = client(
      json({ ...job, status: "pending" }, 202),
      json({ ...job, status: "running" }),
      json({ ...job, status: "ready", download_url: "/v1/workspace/exports/exp_1/download" }),
      new Response(new Uint8Array([0x50, 0x4b]), { headers: { "Content-Type": "application/zip" } }),
    );
    const ready = await c.exports.exportWorkspace({ pollInterval: 100 });
    expect(ready.status).toBe("ready");
    expect(await c.exports.downloadWorkspaceExport(ready.id)).toEqual(new Uint8Array([0x50, 0x4b]));
    expect(calls.map((x) => `${x.method} ${x.path}`)).toEqual([
      "POST /v1/workspace/export",
      "GET /v1/workspace/exports/exp_1",
      "GET /v1/workspace/exports/exp_1",
      "GET /v1/workspace/exports/exp_1/download",
    ]);
  });

  it("failed exports throw ExportFailedError", async () => {
    const { c } = client(json({ id: "exp_1", status: "failed", error: "disk full" }));
    const err = (await c.exports.waitForWorkspaceExport("exp_1").catch((e) => e)) as ExportFailedError;
    expect(err).toBeInstanceOf(ExportFailedError);
    expect(err.job.error).toBe("disk full");
  });

  it("times out", async () => {
    const { c } = client(json({ id: "exp_1", status: "running" }));
    await expect(c.exports.waitForWorkspaceExport("exp_1", { timeout: 1, pollInterval: 100 })).rejects.toBeInstanceOf(
      WaitTimeoutError,
    );
  });
});
