import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ActivityFeed } from "@/components/activity/activity-feed";
import { Inbox } from "@/components/inbox/inbox";
import { ItemDetail } from "@/components/items/item-detail";
import { RuleFormSheet } from "@/components/rules/rule-form-sheet";
import { DataPrivacyPage } from "@/components/settings/data-privacy-page";
import { SettingsNav } from "@/components/settings/settings-nav";
import { SystemPage, checkOk } from "@/components/settings/system-page";
import type { WorkspaceExport } from "@/lib/api";
import { browser } from "@/lib/browser";
import { valuesToInput, defaultRuleValues } from "@/lib/rules";
import { APPROVE, REJECT, collection, item } from "./fixtures";
import { nav } from "./navigation";
import { mockApi, renderWithProviders } from "./utils";

function exportJob(over: Partial<WorkspaceExport> = {}): WorkspaceExport {
  return {
    id: "exp_1",
    status: "pending",
    size_bytes: 0,
    created_at: "2026-10-13T10:00:00Z",
    started_at: null,
    finished_at: null,
    expires_at: "2026-10-14T10:00:00Z",
    ...over,
  };
}

const READY = exportJob({
  status: "ready",
  size_bytes: 4096,
  started_at: "2026-10-13T10:00:01Z",
  finished_at: "2026-10-13T10:00:02Z",
  download_url: "/v1/workspace/exports/exp_1/download",
});

describe("settings navigation", () => {
  it("has Notifications, Data & privacy and System sections", () => {
    nav.set("/settings/data");
    renderWithProviders(<SettingsNav />);
    expect(screen.getByRole("link", { name: "Notifications" })).toHaveAttribute("href", "/settings/notifications");
    expect(screen.getByRole("link", { name: "Data & privacy" })).toHaveAttribute("aria-current", "page");
    expect(screen.getByRole("link", { name: "System" })).toHaveAttribute("href", "/settings/system");
  });
});

describe("activity export", () => {
  beforeEach(() => nav.set("/activity", "type=item.approved&actor_type=api_key&collection=col_1"));

  it("exports NDJSON and CSV for the current filters through the proxy", async () => {
    mockApi({ "GET /v1/events": { data: [] }, "GET /v1/collections": { data: [collection()] } });
    const user = userEvent.setup();
    renderWithProviders(<ActivityFeed />, { mode: "cloud" });
    await user.click(await screen.findByRole("button", { name: /export/i }));
    const menu = await screen.findByRole("menu");
    expect(within(menu).getByText(/matching the current filters/)).toBeInTheDocument();
    const ndjson = new URL(
      within(menu)
        .getByRole("menuitem", { name: /ndjson/i })
        .getAttribute("href")!,
      "http://x",
    );
    expect(ndjson.pathname).toBe("/api/v1/events/export");
    expect(Object.fromEntries(ndjson.searchParams)).toEqual({
      format: "ndjson",
      type: "item.approved",
      actor_type: "api_key",
      collection_id: "col_1",
    });
    const csv = within(menu).getByRole("menuitem", { name: /csv/i });
    expect(csv.getAttribute("href")).toContain("format=csv");
    expect(csv).toHaveAttribute("download");
  });
});

describe("data & privacy", () => {
  beforeEach(() => nav.set("/settings/data"));

  it("starts a workspace export, polls it and offers the ZIP download", async () => {
    const api = mockApi({
      "POST /v1/workspace/export": { status: 202, body: exportJob() },
      "GET /v1/workspace/exports/exp_1": READY,
    });
    const user = userEvent.setup();
    renderWithProviders(<DataPrivacyPage />, { mode: "local" });
    expect(screen.getByTestId("export-all-csv")).toHaveAttribute("href", "/api/v1/events/export?format=csv");
    await user.click(screen.getByRole("button", { name: /export workspace/i }));
    await waitFor(() => expect(api.find("POST", "/v1/workspace/export")).toHaveLength(1));
    const link = await screen.findByRole("link", { name: /download zip/i });
    expect(link).toHaveAttribute("href", "/api/v1/workspace/exports/exp_1/download");
    expect(screen.getByTestId("export-status")).toHaveTextContent(/4 KB · available until/);
    expect(api.find("GET", "/v1/workspace/exports/exp_1").length).toBeGreaterThan(0);
  });

  it("shows the export that is already running on 409", async () => {
    mockApi({
      "POST /v1/workspace/export": {
        status: 409,
        body: { error: { code: "conflict", message: "an export is already running", details: { export_id: "exp_9" } } },
      },
      "GET /v1/workspace/exports/exp_9": exportJob({
        id: "exp_9",
        status: "running",
        started_at: "2026-10-13T10:00:01Z",
      }),
    });
    const user = userEvent.setup();
    renderWithProviders(<DataPrivacyPage />, { mode: "cloud" });
    await user.click(screen.getByRole("button", { name: /export workspace/i }));
    expect(await screen.findByText("An export is already running")).toBeInTheDocument();
    expect(await screen.findByText(/Building your archive/)).toBeInTheDocument();
    expect(screen.getByTestId("export-status")).toHaveAttribute("data-status", "running");
    expect(screen.getByRole("button", { name: /export again/i })).toBeDisabled();
  });

  it("cloud: deletes the workspace only after typing its name, then goes to /setup", async () => {
    const api = mockApi({ "DELETE /v1/workspace": { status: 204 } });
    const assign = vi.spyOn(browser, "assign").mockImplementation(() => {});
    const user = userEvent.setup();
    renderWithProviders(<DataPrivacyPage />, { mode: "cloud" });
    expect(screen.queryByTestId("local-delete")).not.toBeInTheDocument();
    await user.click(within(screen.getByTestId("danger-zone")).getByRole("button", { name: /delete workspace/i }));
    const dialog = await screen.findByRole("dialog");
    const confirm = within(dialog).getByRole("button", { name: "Delete workspace" });
    expect(confirm).toBeDisabled();
    await user.type(within(dialog).getByLabelText(/to confirm/), "Test W");
    expect(confirm).toBeDisabled();
    await user.type(within(dialog).getByLabelText(/to confirm/), "S");
    expect(confirm).toBeEnabled();
    await user.click(confirm);
    await waitFor(() => expect(api.find("DELETE", "/v1/workspace")).toHaveLength(1));
    expect(api.find("DELETE", "/v1/workspace")[0].body).toEqual({ confirm: "Test WS" });
    await waitFor(() => expect(assign).toHaveBeenCalledWith("/setup"));
    assign.mockRestore();
  });

  it("local: explains deleting the data volume instead of offering a delete button", () => {
    mockApi({});
    renderWithProviders(<DataPrivacyPage />, { mode: "local" });
    expect(screen.queryByTestId("danger-zone")).not.toBeInTheDocument();
    expect(screen.getByTestId("local-delete")).toHaveTextContent("docker compose down -v");
    expect(screen.queryByRole("button", { name: /delete workspace/i })).not.toBeInTheDocument();
  });
});

describe("purge an item", () => {
  it("requires an explicit acknowledgement and calls the purge endpoint", async () => {
    const api = mockApi({
      "GET /v1/items/itm_1": item({ title: "Avatar v1" }),
      "GET /v1/items/itm_1/actions": { data: [APPROVE, REJECT] },
      "GET /v1/items/itm_1/events": { data: [] },
      "GET /v1/items/itm_1/versions": { data: [] },
      "GET /v1/collections/col_1": collection(),
      "POST /v1/items/itm_1/purge": { item_id: "itm_1", events_deleted: 3, media_deleted: true },
    });
    const user = userEvent.setup();
    renderWithProviders(<ItemDetail id="itm_1" />, { mode: "local" });
    await user.click(await screen.findByRole("button", { name: "More item options" }));
    await user.click(await screen.findByRole("menuitem", { name: /purge permanently/i }));
    const dialog = await screen.findByRole("alertdialog");
    expect(dialog).toHaveTextContent(/erases its history/);
    const purge = within(dialog).getByRole("button", { name: /purge permanently/i });
    expect(purge).toBeDisabled();
    await user.click(within(dialog).getByRole("checkbox"));
    await user.click(purge);
    await waitFor(() => expect(api.find("POST", "/v1/items/itm_1/purge")).toHaveLength(1));
    expect(await screen.findByText("Item purged")).toBeInTheDocument();
    expect(screen.getByText("3 events erased, file deleted.")).toBeInTheDocument();
    expect(nav.push).toHaveBeenCalledWith("/collections/col_1");
  });
});

describe("system status", () => {
  it("shows readiness checks from /readyz (503 still renders the checks)", async () => {
    mockApi({
      "GET /readyz": {
        status: 503,
        body: { status: "unavailable", checks: { database: "ok", migrations: "pending (3 < 4)", workers: "stale" } },
      },
      "GET /health": { status: "ok", version: "1.4.0" },
    });
    renderWithProviders(<SystemPage />, { mode: "local" });
    const card = await screen.findByTestId("readiness");
    expect(card).toHaveAttribute("data-status", "unavailable");
    expect(card).toHaveTextContent("Not ready");
    expect(card).toHaveTextContent("pending (3 < 4)");
    expect(await within(card).findByText("v1.4.0")).toBeInTheDocument();
    expect(checkOk("workers", "not_started")).toBe(true);
    expect(checkOk("workers", "stale")).toBe(false);
  });
});

describe("expired filter", () => {
  it("the inbox has an Expired tab that queries status=expired", async () => {
    nav.set("/inbox");
    const api = mockApi({
      "GET /v1/items": { data: [] },
      "GET /v1/collections": { data: [collection()] },
    });
    const user = userEvent.setup();
    const first = renderWithProviders(<Inbox />, { mode: "local" });
    const tabs = await screen.findAllByRole("radio", { name: "Expired" });
    await user.click(tabs[0]);
    expect(nav.replace).toHaveBeenCalledWith("/inbox?status=expired", { scroll: false });
    first.unmount();
    // The URL now carries the filter; a fresh render queries expired items.
    renderWithProviders(<Inbox />, { mode: "local" });
    await waitFor(() =>
      expect(api.find("GET", "/v1/items").some((c) => c.query.get("status") === "expired")).toBe(true),
    );
    expect((await screen.findAllByText("No expired items")).length).toBeGreaterThan(0);
  });
});

describe("escalate to one channel", () => {
  it("sends params.channel_id when a channel is chosen", async () => {
    const api = mockApi({
      "GET /v1/collections": { data: [collection()] },
      "GET /v1/notification-channels": {
        data: [
          {
            id: "nch_1",
            name: "On-call phone",
            type: "ntfy",
            enabled: true,
            config: {},
            events: [],
            collection_ids: [],
            digest_window_seconds: 0,
            fail_count: 0,
            last_error: "",
            interactive: false,
            created_at: "",
            updated_at: "",
          },
        ],
      },
      "POST /v1/rules": (c: { body: unknown }) => ({ status: 201, body: { id: "rul_1", ...(c.body as object) } }),
    });
    const user = userEvent.setup();
    renderWithProviders(<RuleFormSheet open onOpenChange={() => {}} state={{}} nextPriority={10} />, { mode: "local" });
    const sheet = await screen.findByRole("dialog");
    await user.type(within(sheet).getByLabelText("Name"), "Wake me up");
    await user.click(within(sheet).getByRole("radio", { name: /escalate/i }));
    const select = await within(sheet).findByLabelText(/notify channel/i);
    expect(within(select).getByRole("option", { name: /all channels subscribed/i })).toBeInTheDocument();
    await within(sheet).findByRole("option", { name: /on-call phone/i });
    await user.selectOptions(select, "nch_1");
    await user.click(within(sheet).getByRole("button", { name: /create rule/i }));
    await waitFor(() => expect(api.find("POST", "/v1/rules")).toHaveLength(1));
    expect((api.find("POST", "/v1/rules")[0].body as { params: unknown }).params).toEqual({ channel_id: "nch_1" });
  });

  it("omits channel_id when all subscribed channels should be notified", () => {
    const v = { ...defaultRuleValues(), name: "x", action: "escalate" as const, note: "hi" };
    expect(valuesToInput(v).params).toEqual({ note: "hi" });
    expect(valuesToInput({ ...v, channel_id: "nch_2" }).params).toEqual({ note: "hi", channel_id: "nch_2" });
  });
});
