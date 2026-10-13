import { z } from "zod";
import {
  ITEM_TYPES,
  type ItemType,
  type Rule,
  type RuleAction,
  type RuleCondition,
  type RuleInput,
  type RuleOp,
} from "./api";
import { formatInterval, truncate } from "./format";
import { statusMeta } from "./status";

// Pure helpers for the approval-rule builder: form model, validation that
// mirrors apps/api/internal/app/rules.go, request bodies and the one-line
// summary sentence shown in the list and the editor.

export const RULE_ACTIONS: { value: RuleAction; label: string; description: string }[] = [
  {
    value: "auto_approve",
    label: "Auto-approve",
    description: "Approve immediately; webhooks get a normal item.approved.",
  },
  { value: "auto_reject", label: "Auto-reject", description: "Reject immediately with your feedback." },
  { value: "set_status", label: "Set status", description: "Move the item out of review, e.g. to processing." },
  { value: "expire", label: "Expire", description: "Mark it expired if nobody reviews it in time." },
  {
    value: "escalate",
    label: "Escalate",
    description: "Keep it pending and notify: item.escalated goes to webhooks and notification channels.",
  },
];

export const RULE_OPS: { value: RuleOp; label: string; symbol: string }[] = [
  { value: "eq", label: "equals", symbol: "=" },
  { value: "ne", label: "does not equal", symbol: "≠" },
  { value: "gt", label: "greater than", symbol: ">" },
  { value: "gte", label: "at least", symbol: "≥" },
  { value: "lt", label: "less than", symbol: "<" },
  { value: "lte", label: "at most", symbol: "≤" },
  { value: "contains", label: "contains", symbol: "contains" },
  { value: "in", label: "is one of", symbol: "in" },
  { value: "not_in", label: "is not one of", symbol: "not in" },
  { value: "exists", label: "is set", symbol: "exists" },
];

/** Statuses set_status may use (pending/approved/rejected have dedicated actions). */
export const SET_STATUS_OPTIONS = ["draft", "processing", "completed", "failed", "archived"] as const;
export type SetStatus = (typeof SET_STATUS_OPTIONS)[number];

export const NUMERIC_OPS: RuleOp[] = ["lt", "lte", "gt", "gte"];
export const LIST_OPS: RuleOp[] = ["in", "not_in"];

export type ValueKind = "number" | "text" | "list" | "none";
export function valueKind(op: RuleOp): ValueKind {
  if (op === "exists") return "none";
  if (NUMERIC_OPS.includes(op)) return "number";
  if (LIST_OPS.includes(op)) return "list";
  return "text";
}

export const EXPIRE_UNITS = { minutes: 60, hours: 3600, days: 86400 } as const;
export type ExpireUnit = keyof typeof EXPIRE_UNITS;
export const EXPIRE_MIN_SECONDS = 60;
export const EXPIRE_MAX_SECONDS = 90 * 86400;
export const EXPIRE_PRESETS: { label: string; amount: number; unit: ExpireUnit }[] = [
  { label: "1 h", amount: 1, unit: "hours" },
  { label: "6 h", amount: 6, unit: "hours" },
  { label: "24 h", amount: 24, unit: "hours" },
  { label: "3 days", amount: 3, unit: "days" },
  { label: "7 days", amount: 7, unit: "days" },
];

/** Split seconds into the largest whole unit for the picker. */
export function splitSeconds(seconds: number): { amount: number; unit: ExpireUnit } {
  if (seconds >= 86400 && seconds % 86400 === 0) return { amount: seconds / 86400, unit: "days" };
  if (seconds >= 3600 && seconds % 3600 === 0) return { amount: seconds / 3600, unit: "hours" };
  return { amount: Math.round(seconds / 60), unit: "minutes" };
}

const conditionSchema = z
  .object({
    path: z
      .string()
      .trim()
      .min(1, "Enter a metadata key, e.g. score or post.likes")
      .max(100, "At most 100 characters")
      .regex(/^[^\s.]+(\.[^\s.]+)*$/, "Use dot notation without spaces, e.g. post.likes"),
    op: z.enum(["eq", "ne", "lt", "lte", "gt", "gte", "contains", "in", "not_in", "exists"]),
    value: z.string(),
    list: z.array(z.string()),
  })
  .superRefine((c, ctx) => {
    const kind = valueKind(c.op);
    if (kind === "number" && (c.value.trim() === "" || !Number.isFinite(Number(c.value)))) {
      ctx.addIssue({ code: "custom", path: ["value"], message: "Enter a number" });
    }
    if (kind === "text" && c.value.trim() === "") {
      ctx.addIssue({ code: "custom", path: ["value"], message: "Enter a value" });
    }
    if (kind === "list" && c.list.length === 0) {
      ctx.addIssue({ code: "custom", path: ["list"], message: "Add at least one value" });
    }
  });

export const ruleFormSchema = z
  .object({
    name: z.string().trim().min(1, "Give the rule a name").max(100, "At most 100 characters"),
    enabled: z.boolean(),
    collection_ids: z.array(z.string()),
    types: z.array(z.enum(ITEM_TYPES as [ItemType, ...ItemType[]])),
    source_pattern: z.string().trim().max(200, "At most 200 characters"),
    text_contains: z.string().trim().max(200, "At most 200 characters"),
    conditions: z.array(conditionSchema).max(10, "At most 10 conditions"),
    action: z.enum(["auto_approve", "auto_reject", "set_status", "expire", "escalate"]),
    feedback: z.string().max(2000, "At most 2000 characters"),
    status: z.enum(SET_STATUS_OPTIONS),
    expire_amount: z.number(),
    expire_unit: z.enum(["minutes", "hours", "days"]),
    note: z.string().max(2000, "At most 2000 characters"),
    channel_id: z.string(),
  })
  .superRefine((v, ctx) => {
    if (v.action === "expire") {
      const secs = v.expire_amount * EXPIRE_UNITS[v.expire_unit];
      if (
        !Number.isFinite(secs) ||
        !Number.isInteger(v.expire_amount) ||
        secs < EXPIRE_MIN_SECONDS ||
        secs > EXPIRE_MAX_SECONDS
      ) {
        ctx.addIssue({ code: "custom", path: ["expire_amount"], message: "Between 1 minute and 90 days" });
      }
    }
  });

export type RuleFormValues = z.infer<typeof ruleFormSchema>;
export type ConditionFormValue = RuleFormValues["conditions"][number];

export function emptyCondition(): ConditionFormValue {
  return { path: "", op: "gte", value: "", list: [] };
}

export function defaultRuleValues(): RuleFormValues {
  return {
    name: "",
    enabled: true,
    collection_ids: [],
    types: [],
    source_pattern: "",
    text_contains: "",
    conditions: [],
    action: "auto_approve",
    feedback: "",
    status: "processing",
    expire_amount: 24,
    expire_unit: "hours",
    note: "",
    channel_id: "",
  };
}

function valueToString(v: unknown): string {
  if (v === undefined || v === null) return "";
  return typeof v === "string" ? v : typeof v === "object" ? JSON.stringify(v) : String(v);
}

/** Existing rule → editable form values. */
export function ruleToValues(r: Rule): RuleFormValues {
  const d = defaultRuleValues();
  const after = r.params.after_seconds ? splitSeconds(r.params.after_seconds) : null;
  return {
    name: r.name,
    enabled: r.enabled,
    collection_ids: r.match.collection_ids ?? [],
    types: r.match.types ?? [],
    source_pattern: r.match.source_pattern ?? "",
    text_contains: r.match.text_contains ?? "",
    conditions: (r.match.conditions ?? []).map((c) => ({
      path: c.path,
      op: c.op,
      value: Array.isArray(c.value) ? "" : valueToString(c.value),
      list: Array.isArray(c.value) ? c.value.map(valueToString) : [],
    })),
    action: r.action,
    feedback: r.params.feedback ?? "",
    status: (SET_STATUS_OPTIONS as readonly string[]).includes(r.params.status ?? "")
      ? (r.params.status as SetStatus)
      : d.status,
    expire_amount: after?.amount ?? d.expire_amount,
    expire_unit: after?.unit ?? d.expire_unit,
    note: r.params.note ?? "",
    channel_id: r.params.channel_id ?? "",
  };
}

/**
 * Typed comparison value: "0.9" → 0.9, "true" → true, anything else stays a
 * string. The API compares eq/ne/in with fmt.Sprint, so typed JSON values
 * match metadata exactly; numeric ops need real numbers.
 */
export function coerceValue(raw: string): string | number | boolean {
  const s = raw.trim();
  if (s === "true") return true;
  if (s === "false") return false;
  if (s !== "" && Number.isFinite(Number(s)) && String(Number(s)) === s) return Number(s);
  return s;
}

/** Users often type "metadata.score"; paths are already relative to metadata. */
export function normalizePath(path: string) {
  return path.trim().replace(/^metadata\./, "");
}

export function conditionToApi(c: ConditionFormValue): RuleCondition {
  const path = normalizePath(c.path);
  switch (valueKind(c.op)) {
    case "none":
      return { path, op: c.op };
    case "number":
      return { path, op: c.op, value: Number(c.value) };
    case "list":
      return { path, op: c.op, value: c.list.map(coerceValue) };
    default:
      return { path, op: c.op, value: c.op === "contains" ? c.value.trim() : coerceValue(c.value) };
  }
}

/** Form values → POST/PATCH /v1/rules body (match and params are replaced as a whole). */
export function valuesToInput(v: RuleFormValues, extra?: { priority?: number }): RuleInput {
  const match: RuleInput["match"] = {};
  if (v.collection_ids.length) match.collection_ids = v.collection_ids;
  if (v.types.length) match.types = v.types;
  if (v.source_pattern.trim()) match.source_pattern = v.source_pattern.trim();
  if (v.text_contains.trim()) match.text_contains = v.text_contains.trim();
  if (v.conditions.length) match.conditions = v.conditions.map(conditionToApi);

  const params: RuleInput["params"] = {};
  const feedback = v.feedback.trim();
  switch (v.action) {
    case "auto_approve":
    case "auto_reject":
      if (feedback) params.feedback = feedback;
      break;
    case "set_status":
      params.status = v.status;
      break;
    case "expire":
      params.after_seconds = v.expire_amount * EXPIRE_UNITS[v.expire_unit];
      break;
    case "escalate":
      if (v.note.trim()) params.note = v.note.trim();
      if (v.channel_id) params.channel_id = v.channel_id;
      break;
  }
  return {
    name: v.name.trim(),
    enabled: v.enabled,
    ...(extra?.priority !== undefined ? { priority: extra.priority } : {}),
    match,
    action: v.action,
    params,
  };
}

// ---- summary sentence ----

const TYPE_NOUNS: Record<ItemType, string> = {
  image: "image",
  video: "video",
  text: "text",
  link: "link",
  json: "JSON item",
  file: "file",
};

function orList(parts: string[]) {
  if (parts.length <= 1) return parts.join("");
  return parts.slice(0, -1).join(", ") + " or " + parts[parts.length - 1];
}

function fmtValue(v: unknown) {
  if (typeof v === "string") return `“${truncate(v, 40)}”`;
  return valueToString(v);
}

export function describeCondition(c: RuleCondition): string {
  const path = `metadata.${normalizePath(c.path)}`;
  const op = RULE_OPS.find((o) => o.value === c.op);
  if (c.op === "exists") return path;
  if (c.op === "in" || c.op === "not_in") {
    const list = Array.isArray(c.value) ? c.value.map(fmtValue).join(", ") : fmtValue(c.value);
    return `${path} ${c.op === "in" ? "in" : "not in"} [${list}]`;
  }
  if (c.op === "contains") return `${path} contains ${fmtValue(c.value)}`;
  return `${path} ${op?.symbol ?? c.op} ${typeof c.value === "number" ? c.value : fmtValue(c.value)}`;
}

export function describeAction(action: RuleAction, params: RuleInput["params"] = {}): string {
  switch (action) {
    case "auto_approve":
      return "auto-approve";
    case "auto_reject":
      return params.feedback ? `auto-reject with “${truncate(params.feedback, 48)}”` : "auto-reject";
    case "set_status":
      return `set status to ${params.status ? statusMeta(params.status).label : "…"}`;
    case "expire":
      return params.after_seconds ? `expire if not reviewed within ${formatInterval(params.after_seconds)}` : "expire";
    case "escalate":
      return params.note ? `escalate (“${truncate(params.note, 48)}”)` : "escalate";
  }
}

/**
 * Human-readable rule, e.g.
 * "When a new image in Instagram Avatar has metadata.score ≥ 0.9 → auto-approve".
 */
export function summarizeRule(
  input: Pick<RuleInput, "match" | "action" | "params">,
  collectionName: (id: string) => string = (id) => id,
): string {
  const m = input.match ?? {};
  const types = m.types ?? [];
  const noun = types.length ? orList(types.map((t) => TYPE_NOUNS[t] ?? t)) : "item";
  let s = `When a new ${noun}`;
  if (m.collection_ids?.length) s += ` in ${orList(m.collection_ids.map(collectionName))}`;
  if (m.source_pattern)
    s += ` from ${m.source_pattern.includes("*") || m.source_pattern.includes("?") ? "a source matching " : ""}“${m.source_pattern}”`;
  if (m.text_contains) s += ` mentioning “${truncate(m.text_contains, 40)}”`;
  if (m.conditions?.length) s += ` has ${m.conditions.map(describeCondition).join(" and ")}`;
  if (!m.collection_ids?.length && !m.source_pattern && !m.text_contains && !m.conditions?.length) s += " arrives";
  return `${s} → ${input.action ? describeAction(input.action, input.params) : "…"}`;
}

// ---- templates ----

export interface RuleTemplate {
  id: string;
  title: string;
  description: string;
  values: Partial<RuleFormValues>;
}

export const RULE_TEMPLATES: RuleTemplate[] = [
  {
    id: "high-score",
    title: "Auto-approve high scores",
    description: "Trust your model when it is confident: metadata.score ≥ 0.9.",
    values: {
      name: "Auto-approve high scores",
      types: ["image"],
      conditions: [{ path: "score", op: "gte", value: "0.9", list: [] }],
      action: "auto_approve",
      feedback: "Score above 0.9",
    },
  },
  {
    id: "noisy-source",
    title: "Reject a noisy source",
    description: "Silence a source you no longer want to review.",
    values: {
      name: "Reject a noisy source",
      source_pattern: "test-*",
      action: "auto_reject",
      feedback: "Source is muted",
    },
  },
  {
    id: "expire-24h",
    title: "Expire after 24 h",
    description: "Expire items nobody reviewed within a day.",
    values: { name: "Expire after 24 h", action: "expire", expire_amount: 24, expire_unit: "hours" },
  },
  {
    id: "escalate-urgent",
    title: "Escalate urgent items",
    description: "Ping webhooks & notifications when something is urgent.",
    values: {
      name: "Escalate urgent items",
      conditions: [{ path: "priority", op: "in", value: "", list: ["urgent", "high"] }],
      action: "escalate",
      note: "Needs a human today",
    },
  },
];

export function templateValues(t: RuleTemplate): RuleFormValues {
  return { ...defaultRuleValues(), ...t.values, conditions: structuredClone(t.values.conditions ?? []) };
}

/** Priority for a rule appended after the existing ones. */
export function nextPriority(rules: Pick<Rule, "priority">[]) {
  return rules.length ? Math.min(100000, Math.max(...rules.map((r) => r.priority)) + 10) : 10;
}

/**
 * New priorities for a reordered list: 10, 20, 30… Only rules whose priority
 * changes are returned, so a move costs as few PATCH requests as possible.
 */
export function reorderPriorities(ordered: Pick<Rule, "id" | "priority">[]): { id: string; priority: number }[] {
  return ordered
    .map((r, i) => ({ id: r.id, priority: (i + 1) * 10, old: r.priority }))
    .filter((r) => r.priority !== r.old)
    .map(({ id, priority }) => ({ id, priority }));
}

export function moveItem<T>(list: T[], from: number, to: number): T[] {
  if (to < 0 || to >= list.length || from === to) return list;
  const next = [...list];
  const [x] = next.splice(from, 1);
  next.splice(to, 0, x);
  return next;
}
