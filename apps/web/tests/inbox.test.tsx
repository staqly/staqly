import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it } from "vitest";
import { Inbox } from "@/components/inbox/inbox";
import type { Item } from "@/lib/api";
import { APPROVE, REGENERATE, REJECT, collection, item } from "./fixtures";
import { nav } from "./navigation";
import { mockApi, renderWithProviders } from "./utils";

const ITEMS: Item[] = [
  item({ id: "itm_1", title: "Avatar one" }),
  item({ id: "itm_2", title: "Avatar two", version: 3 }),
  item({ id: "itm_3", title: "Caption three", type: "text", media: null, content: "Hello" }),
];

function setup(extra: Record<string, unknown> = {}) {
  return mockApi({
    "GET /v1/items": { data: ITEMS },
    "GET /v1/collections": { data: [collection({ status_counts: { pending: 3 }, item_count: 3 })] },
    "GET /v1/items/*": (c: { path: string }) =>
      c.path.endsWith("/actions") ? { data: [APPROVE, REJECT, REGENERATE] } : ITEMS.find((i) => c.path.endsWith(i.id)),
    "GET /v1/events": { data: [] },
    "POST /v1/items/itm_1/actions/approve": item({ id: "itm_1", title: "Avatar one", status: "approved", version: 2 }),
    "POST /v1/items/itm_2/actions/approve": item({ id: "itm_2", title: "Avatar two", status: "approved", version: 4 }),
    "POST /v1/items/itm_1/actions/reject": item({ id: "itm_1", title: "Avatar one", status: "rejected", version: 2 }),
    ...extra,
  });
}

async function ready() {
  const preview = await screen.findByTestId("inbox-preview");
  await within(preview).findByRole("button", { name: /^approve/i });
  return preview;
}

describe("approval inbox", () => {
  beforeEach(() => nav.set("/inbox"));

  it("lists pending items oldest first and previews the first one", async () => {
    const api = setup();
    renderWithProviders(<Inbox />, { mode: "local" });
    const preview = await ready();
    expect(within(preview).getByRole("heading", { name: "Avatar one" })).toBeInTheDocument();
    const q = api.find("GET", "/v1/items")[0].query;
    expect(q.get("status")).toBe("pending");
    expect(q.get("sort")).toBe("created_asc");
    expect(screen.getByTestId("inbox-count-label")).toHaveTextContent("3 pending");
    // Custom collection actions are offered too.
    expect(within(preview).getByRole("button", { name: /regenerate/i })).toBeInTheDocument();
  });

  it("approves via the button and advances to the next item", async () => {
    const api = setup();
    const user = userEvent.setup();
    renderWithProviders(<Inbox />, { mode: "local" });
    const preview = await ready();
    await user.click(within(preview).getByRole("button", { name: /^approve/i }));
    await waitFor(() => expect(api.find("POST", "/v1/items/itm_1/actions/approve")).toHaveLength(1));
    expect(api.find("POST", "/v1/items/itm_1/actions/approve")[0].body).toEqual({ expected_version: 1 });
    expect(await screen.findByRole("heading", { name: "Avatar two" })).toBeInTheDocument();
    expect(screen.queryByRole("option", { name: /avatar one/i })).not.toBeInTheDocument();
  });

  it("supports j/k navigation and the a shortcut", async () => {
    const api = setup();
    renderWithProviders(<Inbox />, { mode: "local" });
    await ready();
    fireEvent.keyDown(window, { key: "j" });
    expect(await screen.findByRole("heading", { name: "Avatar two" })).toBeInTheDocument();
    fireEvent.keyDown(window, { key: "k" });
    expect(await screen.findByRole("heading", { name: "Avatar one" })).toBeInTheDocument();
    fireEvent.keyDown(window, { key: "j" });
    await screen.findByRole("heading", { name: "Avatar two" });
    await within(screen.getByTestId("inbox-preview")).findByRole("button", { name: /^approve/i });
    fireEvent.keyDown(window, { key: "a" });
    await waitFor(() => expect(api.find("POST", "/v1/items/itm_2/actions/approve")).toHaveLength(1));
    expect(api.find("POST", "/v1/items/itm_2/actions/approve")[0].body).toEqual({ expected_version: 3 });
  });

  it("r opens a feedback box that requires text and Enter submits it", async () => {
    const api = setup();
    const user = userEvent.setup();
    renderWithProviders(<Inbox />, { mode: "local" });
    await ready();
    fireEvent.keyDown(window, { key: "r" });
    const dialog = await screen.findByRole("dialog");
    const submit = within(dialog).getByRole("button", { name: "Reject" });
    expect(submit).toBeDisabled();
    const box = within(dialog).getByLabelText("Feedback");
    await user.type(box, "{Enter}");
    expect(api.find("POST", "/v1/items/itm_1/actions/reject")).toHaveLength(0);
    await user.type(box, "Face too small{Enter}");
    await waitFor(() => expect(api.find("POST", "/v1/items/itm_1/actions/reject")).toHaveLength(1));
    expect(api.find("POST", "/v1/items/itm_1/actions/reject")[0].body).toEqual({
      feedback: "Face too small",
      expected_version: 1,
    });
  });

  it("e edits before approving and sends edits", async () => {
    const api = setup();
    const user = userEvent.setup();
    renderWithProviders(<Inbox />, { mode: "local" });
    await ready();
    fireEvent.keyDown(window, { key: "e" });
    const dialog = await screen.findByRole("dialog");
    const title = within(dialog).getByLabelText("Title");
    await user.clear(title);
    await user.type(title, "Avatar final");
    await user.click(within(dialog).getByRole("button", { name: /save & approve/i }));
    await waitFor(() => expect(api.find("POST", "/v1/items/itm_1/actions/approve")).toHaveLength(1));
    expect(api.find("POST", "/v1/items/itm_1/actions/approve")[0].body).toEqual({
      edits: { title: "Avatar final" },
      expected_version: 1,
    });
  });

  it("x selects items and shift+A bulk approves them", async () => {
    const api = setup({
      "POST /v1/items/bulk-actions": {
        data: [
          { id: "itm_1", ok: true, item: item({ id: "itm_1", status: "approved" }) },
          { id: "itm_2", ok: false, error: { code: "conflict", message: "already decided" } },
        ],
      },
    });
    renderWithProviders(<Inbox />, { mode: "local" });
    await ready();
    fireEvent.keyDown(window, { key: "x" });
    fireEvent.keyDown(window, { key: "j" });
    await screen.findByRole("heading", { name: "Avatar two" });
    fireEvent.keyDown(window, { key: "x" });
    expect(await screen.findByTestId("bulk-bar")).toHaveTextContent("2 selected");
    fireEvent.keyDown(window, { key: "A", shiftKey: true });
    await waitFor(() => expect(api.find("POST", "/v1/items/bulk-actions")).toHaveLength(1));
    expect(api.find("POST", "/v1/items/bulk-actions")[0].body).toEqual({ ids: ["itm_1", "itm_2"], action: "approve" });
    expect(await screen.findByText(/1 item skipped \(1 already decided\)/)).toBeInTheDocument();
  });

  it("handles 409 conflicts gracefully", async () => {
    setup({
      "POST /v1/items/itm_1/actions/approve": {
        status: 409,
        body: {
          error: { code: "conflict", message: "cannot approve an item that is approved (allowed from: pending)" },
        },
      },
    });
    const user = userEvent.setup();
    renderWithProviders(<Inbox />, { mode: "local" });
    const preview = await ready();
    await user.click(within(preview).getByRole("button", { name: /^approve/i }));
    expect(await screen.findByText(/Already decided/)).toBeInTheDocument();
  });

  it("shows the shortcut legend", async () => {
    setup();
    renderWithProviders(<Inbox />, { mode: "local" });
    await ready();
    fireEvent.keyDown(window, { key: "?" });
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("Approve selected")).toBeInTheDocument();
    expect(within(dialog).getByText("Reject with feedback")).toBeInTheDocument();
  });
});
