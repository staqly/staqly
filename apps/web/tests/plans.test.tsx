import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { AddItemDialog } from "@/components/collections/add-item-dialog";
import { PlanUsagePage, meterLevel } from "@/components/settings/plan-usage-page";
import { WebhookFormDialog } from "@/components/webhooks/webhook-form-dialog";
import { describePlanLimit } from "@/lib/notify";
import { nav } from "./navigation";
import { PLANS, UNLOCKED, usage } from "./plans-fixtures";
import { mockApi, renderWithProviders } from "./utils";

const LIMIT_402 = {
  status: 402,
  body: {
    error: {
      code: "plan_limit_exceeded",
      message: "The Free plan allows 3 webhooks; upgrade the workspace plan to continue.",
      details: { limit: "webhooks", max: 3, used: 3, plan: "free" },
    },
  },
};

describe("plan & usage page", () => {
  it("cloud: shows the current plan, usage meters with warning levels and the plan cards", async () => {
    mockApi({
      "GET /v1/workspace/usage": usage(PLANS[0], {
        items_per_month: 85,
        webhooks: 3,
        api_keys: 1,
        storage_bytes: 200 << 20,
      }),
      "GET /v1/plans": { data: PLANS },
    });
    renderWithProviders(<PlanUsagePage />, { mode: "cloud" });

    expect(await screen.findByRole("heading", { name: /free plan/i })).toBeInTheDocument();
    const items = await screen.findByTestId("meter-Items this month");
    expect(items).toHaveAttribute("data-level", "warn"); // 85% → amber
    expect(within(items).getByText("85")).toBeInTheDocument();
    expect(within(items).getByText(/100/)).toBeInTheDocument();
    expect(within(items).getByText(/almost full/i)).toBeInTheDocument();

    const hooks = screen.getByTestId("meter-Webhooks");
    expect(hooks).toHaveAttribute("data-level", "full"); // 3/3 → red
    expect(within(hooks).getByText(/limit reached/i)).toBeInTheDocument();
    expect(within(hooks).getByRole("progressbar")).toHaveAttribute("aria-valuetext", "3 of 3");

    expect(screen.getByTestId("meter-API keys")).toHaveAttribute("data-level", "ok");
    expect(within(screen.getByTestId("meter-Storage")).getByText("200 MB")).toBeInTheDocument();

    // Plan cards with the current plan marked; upgrades are not purchasable yet.
    expect(await screen.findByTestId("plan-free")).toHaveAttribute("data-current", "true");
    expect(within(screen.getByTestId("plan-free")).getByText("Current plan")).toBeInTheDocument();
    const upgrades = screen.getAllByRole("button", { name: /upgrade — coming soon/i });
    expect(upgrades).toHaveLength(2);
    upgrades.forEach((b) => expect(b).toBeDisabled());
  });

  it("shows ∞ for unlimited limits", async () => {
    mockApi({
      "GET /v1/workspace/usage": usage(PLANS[2], { items_per_month: 4321, webhooks: 12, api_keys: 7 }),
      "GET /v1/plans": { data: PLANS },
    });
    renderWithProviders(<PlanUsagePage />, { mode: "cloud" });
    const items = await screen.findByTestId("meter-Items this month");
    expect(items).toHaveAttribute("data-level", "unlimited");
    expect(within(items).getByText("4,321")).toBeInTheDocument();
    expect(within(items).getByLabelText("unlimited")).toHaveTextContent("∞");
    // Storage is still limited on Team.
    expect(screen.getByTestId("meter-Storage")).toHaveAttribute("data-level", "ok");
  });

  it("local: unlocked, no limits and no plan cards", async () => {
    const api = mockApi({
      "GET /v1/workspace/usage": usage(UNLOCKED, { items_per_month: 7, webhooks: 1 }),
    });
    renderWithProviders(<PlanUsagePage />, { mode: "local" });
    expect(await screen.findByText(/unlocked — local install, no limits/i)).toBeInTheDocument();
    const items = await screen.findByTestId("meter-Items this month");
    expect(within(items).getByText("7")).toBeInTheDocument();
    expect(within(items).getByText("∞")).toBeInTheDocument();
    for (const m of ["Webhooks", "API keys", "Storage"]) {
      expect(screen.getByTestId(`meter-${m}`)).toHaveAttribute("data-level", "unlimited");
    }
    expect(screen.queryByTestId("plan-grid")).not.toBeInTheDocument();
    expect(api.find("GET", "/v1/plans")).toHaveLength(0);
  });

  it("shows rule and watcher meters with the plan's watcher interval", async () => {
    mockApi({
      "GET /v1/workspace/usage": usage(PLANS[0], { rules: 5, watchers: 2 }),
      "GET /v1/plans": { data: PLANS },
    });
    renderWithProviders(<PlanUsagePage />, { mode: "cloud" });
    const rules = await screen.findByTestId("meter-Rules");
    expect(rules).toHaveAttribute("data-level", "full");
    expect(within(rules).getByRole("progressbar")).toHaveAttribute("aria-valuetext", "5 of 5");
    const watchers = screen.getByTestId("meter-Watchers");
    expect(watchers).toHaveAttribute("data-level", "ok");
    expect(within(watchers).getByRole("progressbar")).toHaveAttribute("aria-valuetext", "2 of 3");
    expect(watchers).toHaveTextContent("Active only · checks every 15 min at most");
  });

  it("local: rule and watcher meters are unlimited", async () => {
    mockApi({ "GET /v1/workspace/usage": usage(UNLOCKED, { rules: 4, watchers: 9 }) });
    renderWithProviders(<PlanUsagePage />, { mode: "local" });
    expect(await screen.findByTestId("meter-Rules")).toHaveAttribute("data-level", "unlimited");
    expect(screen.getByTestId("meter-Watchers")).toHaveAttribute("data-level", "unlimited");
    expect(within(screen.getByTestId("meter-Watchers")).getByText("9")).toBeInTheDocument();
  });

  it("meter levels: amber above 80%, red at 100%", () => {
    expect(meterLevel(80, 100)).toBe("ok");
    expect(meterLevel(81, 100)).toBe("warn");
    expect(meterLevel(100, 100)).toBe("full");
    expect(meterLevel(5, -1)).toBe("unlimited");
  });
});

describe("402 plan_limit_exceeded", () => {
  it("creating a webhook over the limit shows a friendly message with a View plans action", async () => {
    mockApi({
      "GET /v1/collections": { data: [] },
      "POST /v1/webhooks": LIMIT_402,
    });
    const user = userEvent.setup();
    renderWithProviders(<WebhookFormDialog open onOpenChange={() => {}} webhook={null} />, { mode: "cloud" });
    await user.type(await screen.findByLabelText("Endpoint URL"), "https://example.com/hook");
    await user.click(screen.getByRole("button", { name: /create webhook/i }));

    expect(await screen.findByText("Plan limit reached")).toBeInTheDocument();
    expect(screen.getByText("Your Free plan includes 3 webhooks (3 used).")).toBeInTheDocument();
    const view = screen.getByRole("button", { name: "View plans" });
    // Without a registered app router the action falls back to a full navigation; register one like <Providers> does.
    const { setNavigator } = await import("@/lib/notify");
    setNavigator((href) => nav.push(href));
    fireEvent.click(view);
    await waitFor(() => expect(nav.push).toHaveBeenCalledWith("/settings/plan"));
    setNavigator(null);
  });

  it("creating an item over the monthly quota explains the limit", async () => {
    mockApi({
      "POST /v1/collections/col_1/items": {
        status: 402,
        body: {
          error: {
            code: "plan_limit_exceeded",
            message: "The Free plan allows 100 items per month; upgrade the workspace plan to continue.",
            details: { limit: "items_per_month", max: 100, used: 100, plan: "free" },
          },
        },
      },
    });
    const user = userEvent.setup();
    renderWithProviders(<AddItemDialog collectionId="col_1" open onOpenChange={() => {}} />, { mode: "cloud" });
    await user.click(await screen.findByRole("tab", { name: /text/i }));
    await user.type(screen.getByLabelText("Text"), "hello");
    await user.click(screen.getByRole("button", { name: /add item/i }));
    expect(await screen.findByText("Plan limit reached")).toBeInTheDocument();
    expect(screen.getByText("Your Free plan includes 100 items per month (100 used).")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "View plans" })).toBeInTheDocument();
  });

  it("describes byte limits in human units", () => {
    expect(describePlanLimit({ limit: "max_upload_bytes", max: 25 << 20, plan: "free" }, "x")).toBe(
      "Your Free plan accepts files up to 25 MB.",
    );
    expect(describePlanLimit({ limit: "storage_bytes", max: 1 << 30, used: 1 << 30, plan: "free" }, "x")).toBe(
      "Your Free plan includes 1 GB of storage (1 GB used).",
    );
    expect(describePlanLimit(undefined, "fallback")).toBe("fallback");
  });
});
