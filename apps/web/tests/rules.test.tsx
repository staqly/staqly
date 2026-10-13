import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { EventTimeline } from "@/components/items/event-timeline";
import { RulesPage } from "@/components/rules/rules-page";
import type { Rule, StashEvent } from "@/lib/api";
import {
  RULE_TEMPLATES,
  coerceValue,
  reorderPriorities,
  ruleFormSchema,
  summarizeRule,
  templateValues,
  valuesToInput,
} from "@/lib/rules";
import { collection } from "./fixtures";
import { PLANS, UNLOCKED, usage } from "./plans-fixtures";
import { mockApi, renderWithProviders, type Call } from "./utils";

function rule(over: Partial<Rule> = {}): Rule {
  return {
    id: "rul_1",
    name: "Confident avatars",
    priority: 10,
    enabled: true,
    match: { collection_ids: ["col_1"], types: ["image"], conditions: [{ path: "score", op: "gte", value: 0.9 }] },
    action: "auto_approve",
    params: {},
    times_applied: 12,
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-01T00:00:00Z",
    ...over,
  };
}

function issues(v: unknown) {
  const r = ruleFormSchema.safeParse(v);
  return r.success ? [] : r.error.issues.map((i) => `${i.path.join(".")}: ${i.message}`);
}

const COLLECTIONS = { data: [collection(), collection({ id: "col_2", name: "Research" })] };

const name = (id: string) => ({ col_1: "Instagram Avatar", col_2: "Research" })[id] ?? id;

describe("rule summary sentence", () => {
  it("reads like the spec example", () => {
    expect(summarizeRule(rule(), name)).toBe(
      "When a new image in Instagram Avatar has metadata.score ≥ 0.9 → auto-approve",
    );
  });

  it("covers every matcher and action", () => {
    expect(
      summarizeRule(
        {
          match: {
            collection_ids: ["col_1", "col_2"],
            types: ["image", "video"],
            source_pattern: "n8n-*",
            text_contains: "sale",
            conditions: [
              { path: "tags", op: "in", value: ["a", "b"] },
              { path: "flagged", op: "exists" },
            ],
          },
          action: "auto_reject",
          params: { feedback: "Off-brand" },
        },
        name,
      ),
    ).toBe(
      "When a new image or video in Instagram Avatar or Research from a source matching “n8n-*” mentioning “sale” has metadata.tags in [“a”, “b”] and metadata.flagged → auto-reject with “Off-brand”",
    );
    expect(summarizeRule({ match: {}, action: "expire", params: { after_seconds: 86400 } })).toBe(
      "When a new item arrives → expire if not reviewed within 1 day",
    );
    expect(summarizeRule({ match: {}, action: "set_status", params: { status: "processing" } })).toBe(
      "When a new item arrives → set status to Processing",
    );
    expect(summarizeRule({ match: { text_contains: "urgent" }, action: "escalate", params: { note: "Call me" } })).toBe(
      "When a new item mentioning “urgent” → escalate (“Call me”)",
    );
  });
});

describe("rule form model", () => {
  it("builds request bodies with typed condition values", () => {
    const v = templateValues(RULE_TEMPLATES.find((t) => t.id === "escalate-urgent")!);
    expect(valuesToInput(v, { priority: 30 })).toEqual({
      name: "Escalate urgent items",
      enabled: true,
      priority: 30,
      match: { conditions: [{ path: "priority", op: "in", value: ["urgent", "high"] }] },
      action: "escalate",
      params: { note: "Needs a human today" },
    });
    expect(valuesToInput(templateValues(RULE_TEMPLATES.find((t) => t.id === "expire-24h")!)).params).toEqual({
      after_seconds: 86400,
    });
    expect(coerceValue("0.9")).toBe(0.9);
    expect(coerceValue("true")).toBe(true);
    expect(coerceValue("007")).toBe("007");
    expect(coerceValue("cats")).toBe("cats");
  });

  it("validates like the backend", () => {
    const base = templateValues(RULE_TEMPLATES[0]);
    expect(issues(base)).toEqual([]);
    expect(issues({ ...base, name: " " })).toContain("name: Give the rule a name");
    expect(issues({ ...base, conditions: [{ path: "score", op: "gt", value: "high", list: [] }] })).toContain(
      "conditions.0.value: Enter a number",
    );
    expect(issues({ ...base, conditions: [{ path: "tag", op: "not_in", value: "", list: [] }] })).toContain(
      "conditions.0.list: Add at least one value",
    );
    expect(issues({ ...base, conditions: [{ path: "a b", op: "exists", value: "", list: [] }] })[0]).toMatch(
      /dot notation/,
    );
    expect(issues({ ...base, action: "expire", expire_amount: 30, expire_unit: "minutes" })).toEqual([]);
    expect(issues({ ...base, action: "expire", expire_amount: 91, expire_unit: "days" })).toContain(
      "expire_amount: Between 1 minute and 90 days",
    );
  });

  it("renumbers priorities only where needed", () => {
    expect(
      reorderPriorities([
        { id: "b", priority: 20 },
        { id: "a", priority: 10 },
        { id: "c", priority: 30 },
      ]),
    ).toEqual([
      { id: "b", priority: 10 },
      { id: "a", priority: 20 },
    ]);
  });
});

describe("rules page", () => {
  it("builds a rule with the visual builder and sends the right body", async () => {
    const api = mockApi({
      "GET /v1/rules": { data: [] },
      "GET /v1/collections": COLLECTIONS,
      "GET /v1/workspace/usage": usage(PLANS[0], { rules: 2 }),
      "POST /v1/rules": (c: Call) => ({ status: 201, body: { ...rule(), ...(c.body as object) } }),
    });
    const user = userEvent.setup();
    renderWithProviders(<RulesPage />, { mode: "cloud" });

    expect(await screen.findByText("No rules yet")).toBeInTheDocument();
    expect(within(await screen.findByTestId("usage-chip-rules")).getByText("2")).toBeInTheDocument();
    expect(screen.getByTestId("usage-chip-rules")).toHaveTextContent("2 / 5 rules");
    expect(within(screen.getByTestId("rule-templates")).getAllByRole("button")).toHaveLength(4);

    await user.click(screen.getAllByRole("button", { name: /new rule/i })[0]);
    const sheet = await screen.findByRole("dialog");
    await user.type(within(sheet).getByLabelText("Name"), "Confident avatars");
    await user.click(await within(sheet).findByRole("button", { name: "Instagram Avatar" }));
    await user.click(within(sheet).getByRole("button", { name: /^image$/i }));
    await user.click(within(sheet).getByRole("button", { name: /add condition/i }));
    await user.type(within(sheet).getByLabelText("Condition 1 path"), "score");
    expect(within(sheet).getByLabelText("Condition 1 operator")).toHaveValue("gte");
    await user.type(within(sheet).getByLabelText("Condition 1 value"), "0.9");

    expect(within(sheet).getByTestId("rule-summary")).toHaveTextContent(
      "When a new image in Instagram Avatar has metadata.score ≥ 0.9 → auto-approve",
    );
    // Value input adapts to the operator.
    await user.selectOptions(within(sheet).getByLabelText("Condition 1 operator"), "exists");
    expect(within(sheet).queryByLabelText("Condition 1 value")).not.toBeInTheDocument();
    expect(within(sheet).getByText(/any value/i)).toBeInTheDocument();
    await user.selectOptions(within(sheet).getByLabelText("Condition 1 operator"), "gte");
    expect(within(sheet).getByLabelText("Condition 1 value")).toHaveValue("0.9");

    await user.click(within(sheet).getByRole("button", { name: /create rule/i }));
    await waitFor(() => expect(api.find("POST", "/v1/rules")).toHaveLength(1));
    expect(api.find("POST", "/v1/rules")[0].body).toEqual({
      name: "Confident avatars",
      enabled: true,
      priority: 10,
      match: { collection_ids: ["col_1"], types: ["image"], conditions: [{ path: "score", op: "gte", value: 0.9 }] },
      action: "auto_approve",
      params: {},
    });
    expect(await screen.findByText("Rule created")).toBeInTheDocument();
  });

  it("starts from a template, blocks invalid values and shows the plan-limit toast on 402", async () => {
    const api = mockApi({
      "GET /v1/rules": { data: [] },
      "GET /v1/collections": COLLECTIONS,
      "GET /v1/workspace/usage": usage(PLANS[0], { rules: 5 }),
      "GET /v1/plans": { data: PLANS },
      "POST /v1/rules": {
        status: 402,
        body: {
          error: {
            code: "plan_limit_exceeded",
            message: "The Free plan allows 5 rules; upgrade the workspace plan to continue.",
            details: { limit: "rules", max: 5, used: 5, plan: "free" },
          },
        },
      },
    });
    const user = userEvent.setup();
    renderWithProviders(<RulesPage />, { mode: "cloud" });
    expect(await screen.findByTestId("usage-chip-rules")).toHaveAttribute("data-level", "full");

    await user.click(await screen.findByRole("button", { name: /escalate urgent items/i }));
    const sheet = await screen.findByRole("dialog");
    expect(within(sheet).getByLabelText("Name")).toHaveValue("Escalate urgent items");
    expect(within(sheet).getByRole("radio", { name: /escalate/i })).toHaveAttribute("aria-checked", "true");
    const chips = within(sheet).getAllByTestId("chip");
    expect(chips.map((c) => c.textContent)).toEqual(["urgent", "high"]);
    // Remove both values: an "in" condition needs at least one.
    await user.click(within(sheet).getByRole("button", { name: "Remove urgent" }));
    await user.click(within(sheet).getByRole("button", { name: "Remove high" }));
    await user.click(within(sheet).getByRole("button", { name: /create rule/i }));
    expect(await within(sheet).findByText("Add at least one value")).toBeInTheDocument();
    expect(api.find("POST", "/v1/rules")).toHaveLength(0);

    await user.type(within(sheet).getByLabelText("Condition 1 values"), "urgent{Enter}");
    await user.click(within(sheet).getByRole("button", { name: /create rule/i }));
    await waitFor(() => expect(api.find("POST", "/v1/rules")).toHaveLength(1));
    expect(await screen.findByText("Plan limit reached")).toBeInTheDocument();
    expect(screen.getByText("Your Free plan includes 5 rules (5 used).")).toBeInTheDocument();
  });

  it("lists rules with summary, applied count, toggles and reorders by priority", async () => {
    const rules = [
      rule(),
      rule({
        id: "rul_2",
        name: "Mute test runs",
        priority: 20,
        enabled: false,
        match: { source_pattern: "test-*" },
        action: "auto_reject",
        params: { feedback: "Muted" },
        times_applied: 1,
      }),
    ];
    const api = mockApi({
      "GET /v1/rules": { data: rules },
      "GET /v1/collections": COLLECTIONS,
      "GET /v1/workspace/usage": usage(UNLOCKED, { rules: 2 }),
      "PATCH /v1/rules/*": (c: Call) => ({ ...rules.find((r) => c.path.endsWith(r.id))!, ...(c.body as object) }),
    });
    const user = userEvent.setup();
    renderWithProviders(<RulesPage />, { mode: "local" });

    const rows = await screen.findAllByTestId("rule-row");
    expect(rows).toHaveLength(2);
    expect(within(rows[0]).getByTestId("rule-summary-line")).toHaveTextContent(
      "When a new image in Instagram Avatar has metadata.score ≥ 0.9 → auto-approve",
    );
    expect(within(rows[0]).getByText("Applied 12 times")).toBeInTheDocument();
    expect(within(rows[1]).getByText("Applied 1 time")).toBeInTheDocument();
    expect(within(rows[1]).getByText("Off")).toBeInTheDocument();
    expect(screen.getByTestId("usage-chip-rules")).toHaveTextContent(/2 rules\s*· no limit/);

    await user.click(within(rows[1]).getByRole("switch", { name: /enable mute test runs/i }));
    await waitFor(() => expect(api.find("PATCH", "/v1/rules/rul_2")[0]?.body).toEqual({ enabled: true }));

    await user.click(within(rows[1]).getByRole("button", { name: /move mute test runs up/i }));
    await waitFor(() =>
      expect(api.calls.filter((c) => c.method === "PATCH" && "priority" in (c.body as object))).toHaveLength(2),
    );
    const prio = api.calls.filter((c) => c.method === "PATCH" && "priority" in (c.body as object));
    expect(prio.map((c) => [c.path, c.body])).toEqual([
      ["/v1/rules/rul_2", { priority: 10 }],
      ["/v1/rules/rul_1", { priority: 20 }],
    ]);
  });
});

const ev = (over: Partial<StashEvent>): StashEvent => ({
  id: "evt",
  seq: 1,
  type: "item.created",
  collection_id: "col_1",
  item_id: "itm_1",
  actor: { type: "system", id: "rul_1", name: "Rule: Confident avatars" },
  data: {},
  created_at: "2026-10-01T10:00:00Z",
  ...over,
});

describe("history timeline for automation events", () => {
  it("labels rule, watcher and expiry actors and the new event types", () => {
    renderWithProviders(
      <EventTimeline
        events={[
          ev({
            id: "e1",
            type: "item.approved",
            data: { feedback: "Score above 0.9", rule: { name: "Confident avatars" } },
          }),
          ev({ id: "e2", type: "item.escalated", data: { note: "Needs a human today" } }),
          ev({ id: "e3", type: "item.rule_applied", data: { expires_at: "2026-10-02T10:00:00Z" } }),
          ev({
            id: "e4",
            type: "item.expired",
            actor: { type: "system", id: "expiry", name: "Expiry" },
            data: { feedback: "Expired before review" },
          }),
          ev({
            id: "e5",
            type: "item.created",
            actor: { type: "system", id: "wat_1", name: "Watcher: HN front page" },
          }),
        ]}
      />,
    );
    expect(screen.getByText("Escalated")).toBeInTheDocument();
    expect(screen.getByText("Rule applied")).toBeInTheDocument();
    expect(screen.getByText("Expired")).toBeInTheDocument();
    expect(screen.getByText("Needs a human today")).toBeInTheDocument();
    expect(screen.getByText("Expired before review")).toBeInTheDocument();
    expect(screen.getByText(/expires if still pending/i)).toBeInTheDocument();

    const actors = screen.getAllByTestId("actor-automation");
    expect(actors).toHaveLength(5);
    expect(actors[0]).toHaveTextContent("RuleConfident avatars");
    expect(actors[0]).toHaveAttribute("href", "/rules");
    expect(actors[3]).toHaveTextContent("Expiry timer");
    expect(actors[4]).toHaveTextContent("WatcherHN front page");
    expect(actors[4]).toHaveAttribute("href", "/watchers");
  });
});
