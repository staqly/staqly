"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ArrowRight, CircleCheck, CircleX, Plus, Repeat, Siren, TimerOff, Trash2, type LucideIcon } from "lucide-react";
import {
  Controller,
  useFieldArray,
  useForm,
  useWatch,
  type Control,
  type FieldErrors,
  type UseFormRegister,
} from "react-hook-form";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Field, FieldDescription, FieldError, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { ChipsInput } from "@/components/common/chips-input";
import { NativeSelect } from "@/components/common/native-select";
import { ToggleChip } from "@/components/common/toggle-chip";
import { TYPE_LABELS, TypeIcon } from "@/components/common/type-icon";
import { api, ITEM_TYPES, type Rule, type RuleAction } from "@/lib/api";
import { toastApiError } from "@/lib/notify";
import { qk, useChannels, useCollections } from "@/lib/queries";
import {
  EXPIRE_PRESETS,
  RULE_ACTIONS,
  RULE_OPS,
  SET_STATUS_OPTIONS,
  defaultRuleValues,
  emptyCondition,
  ruleFormSchema,
  ruleToValues,
  summarizeRule,
  valueKind,
  valuesToInput,
  type RuleFormValues,
} from "@/lib/rules";
import { statusMeta } from "@/lib/status";
import { cn } from "@/lib/utils";

const ACTION_ICONS: Record<RuleAction, { Icon: LucideIcon; cls: string }> = {
  auto_approve: { Icon: CircleCheck, cls: "text-emerald-600 dark:text-emerald-400" },
  auto_reject: { Icon: CircleX, cls: "text-rose-600 dark:text-rose-400" },
  set_status: { Icon: Repeat, cls: "text-violet-600 dark:text-violet-400" },
  expire: { Icon: TimerOff, cls: "text-orange-600 dark:text-orange-400" },
  escalate: { Icon: Siren, cls: "text-amber-600 dark:text-amber-400" },
};

export function actionIcon(action: RuleAction) {
  return ACTION_ICONS[action] ?? ACTION_ICONS.escalate;
}

export interface RuleSheetState {
  rule?: Rule | null;
  /** Prefilled values for a new rule (templates). */
  initial?: RuleFormValues;
}

export function RuleFormSheet({
  open,
  onOpenChange,
  state,
  nextPriority,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  state: RuleSheetState;
  nextPriority: number;
}) {
  const editing = state.rule ?? null;
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="gap-0 data-[side=right]:w-full data-[side=right]:sm:max-w-xl">
        <SheetHeader className="border-b pt-[max(1rem,env(safe-area-inset-top))]">
          <SheetTitle>{editing ? "Edit rule" : "New rule"}</SheetTitle>
          <SheetDescription>Runs once when a new item arrives with status pending.</SheetDescription>
        </SheetHeader>
        {open && (
          <RuleForm
            key={editing?.id ?? "new"}
            rule={editing}
            initial={state.initial}
            nextPriority={nextPriority}
            onDone={() => onOpenChange(false)}
          />
        )}
      </SheetContent>
    </Sheet>
  );
}

function Section({ title, hint, children }: { title: string; hint?: string; children: React.ReactNode }) {
  return (
    <section className="grid gap-3">
      <div>
        <h3 className="text-sm font-semibold">{title}</h3>
        {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
      </div>
      {children}
    </section>
  );
}

function RuleForm({
  rule,
  initial,
  nextPriority,
  onDone,
}: {
  rule: Rule | null;
  initial?: RuleFormValues;
  nextPriority: number;
  onDone: () => void;
}) {
  const qc = useQueryClient();
  const collections = useCollections(true);
  const form = useForm<RuleFormValues>({
    resolver: zodResolver(ruleFormSchema),
    defaultValues: rule ? ruleToValues(rule) : (initial ?? defaultRuleValues()),
    mode: "onTouched",
  });
  const { register, control, handleSubmit, formState, setValue } = form;
  const conditions = useFieldArray({ control, name: "conditions" });
  const values = useWatch({ control }) as RuleFormValues;
  const action = values.action;

  const collectionName = (id: string) => collections.data?.find((c) => c.id === id)?.name ?? id;
  const visibleCollections = (collections.data ?? []).filter(
    (c) => !c.archived || values.collection_ids?.includes(c.id),
  );

  const save = useMutation({
    mutationFn: (v: RuleFormValues) =>
      rule ? api.updateRule(rule.id, valuesToInput(v)) : api.createRule(valuesToInput(v, { priority: nextPriority })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.rules });
      void qc.invalidateQueries({ queryKey: qk.usage });
      toast.success(rule ? "Rule saved" : "Rule created", {
        description: rule ? undefined : "It applies to new items from now on.",
      });
      onDone();
    },
    onError: (e) => toastApiError(e, rule ? "Could not save the rule" : "Could not create the rule"),
  });

  let summary = "";
  try {
    summary = summarizeRule(valuesToInput({ ...defaultRuleValues(), ...values }), collectionName);
  } catch {
    summary = "";
  }

  return (
    <form
      onSubmit={handleSubmit((v) => save.mutate(v))}
      noValidate
      className="flex min-h-0 flex-1 flex-col"
      data-testid="rule-form"
    >
      <div className="grid flex-1 content-start gap-6 overflow-y-auto overscroll-contain p-4">
        <Field data-invalid={!!formState.errors.name}>
          <FieldLabel htmlFor="rule-name">Name</FieldLabel>
          <Input
            id="rule-name"
            {...register("name")}
            placeholder="Auto-approve confident avatars"
            aria-invalid={!!formState.errors.name}
            autoComplete="off"
          />
          <FieldError errors={[formState.errors.name]} />
        </Field>

        <Section title="When a new item…" hint="Leave a filter empty to match everything.">
          <Field>
            <FieldLabel>In collections</FieldLabel>
            <Controller
              control={control}
              name="collection_ids"
              render={({ field }) => (
                <div className="flex flex-wrap gap-1.5" role="group" aria-label="Collections">
                  {visibleCollections.map((c) => {
                    const on = field.value.includes(c.id);
                    return (
                      <ToggleChip
                        key={c.id}
                        pressed={on}
                        color={c.color}
                        onClick={() =>
                          field.onChange(on ? field.value.filter((x) => x !== c.id) : [...field.value, c.id])
                        }
                      >
                        {c.name}
                      </ToggleChip>
                    );
                  })}
                  {!visibleCollections.length && (
                    <span className="text-xs text-muted-foreground">
                      No collections yet — the rule applies everywhere.
                    </span>
                  )}
                </div>
              )}
            />
          </Field>
          <Field>
            <FieldLabel>Of type</FieldLabel>
            <Controller
              control={control}
              name="types"
              render={({ field }) => (
                <div className="flex flex-wrap gap-1.5" role="group" aria-label="Types">
                  {ITEM_TYPES.map((t) => {
                    const on = field.value.includes(t);
                    return (
                      <ToggleChip
                        key={t}
                        pressed={on}
                        onClick={() => field.onChange(on ? field.value.filter((x) => x !== t) : [...field.value, t])}
                      >
                        <span className="inline-flex items-center gap-1.5">
                          {!on && <TypeIcon type={t} className="size-3.5" aria-hidden />}
                          {TYPE_LABELS[t]}
                        </span>
                      </ToggleChip>
                    );
                  })}
                </div>
              )}
            />
          </Field>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field data-invalid={!!formState.errors.source_pattern}>
              <FieldLabel htmlFor="rule-source">Source matches</FieldLabel>
              <Input
                id="rule-source"
                {...register("source_pattern")}
                placeholder="e.g. n8n-*"
                className="font-mono"
                autoComplete="off"
              />
              <FieldDescription className="text-xs">
                Glob: <code>*</code> any text, <code>?</code> one character.
              </FieldDescription>
              <FieldError errors={[formState.errors.source_pattern]} />
            </Field>
            <Field data-invalid={!!formState.errors.text_contains}>
              <FieldLabel htmlFor="rule-text">Title or content contains</FieldLabel>
              <Input id="rule-text" {...register("text_contains")} placeholder="e.g. urgent" autoComplete="off" />
              <FieldDescription className="text-xs">Case-insensitive.</FieldDescription>
              <FieldError errors={[formState.errors.text_contains]} />
            </Field>
          </div>
        </Section>

        <Section
          title="…and its metadata"
          hint="All conditions must hold. Paths use dot notation, e.g. score or post.likes."
        >
          {conditions.fields.length > 0 && (
            <ul className="grid gap-2" data-testid="conditions">
              {conditions.fields.map((f, i) => (
                <ConditionRow
                  key={f.id}
                  index={i}
                  control={control}
                  errors={formState.errors}
                  register={register}
                  onRemove={() => conditions.remove(i)}
                  onOpChange={(op) => {
                    // Keep a sensible value when switching between value kinds.
                    const kind = valueKind(op);
                    const cur = form.getValues(`conditions.${i}`);
                    if (kind === "list" && !cur.list.length && cur.value.trim()) {
                      setValue(`conditions.${i}.list`, [cur.value.trim()]);
                    }
                  }}
                />
              ))}
            </ul>
          )}
          <div>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => conditions.append(emptyCondition())}
              disabled={conditions.fields.length >= 10}
            >
              <Plus /> Add condition
            </Button>
          </div>
        </Section>

        <Section title="Then">
          <Controller
            control={control}
            name="action"
            render={({ field }) => (
              <div role="radiogroup" aria-label="Action" className="grid gap-2 sm:grid-cols-2">
                {RULE_ACTIONS.map((a) => {
                  const { Icon, cls } = actionIcon(a.value);
                  const on = field.value === a.value;
                  return (
                    <button
                      key={a.value}
                      type="button"
                      role="radio"
                      aria-checked={on}
                      onClick={() => field.onChange(a.value)}
                      className={cn(
                        "flex items-start gap-2.5 rounded-xl border p-3 text-left transition-colors",
                        on ? "border-brand/60 bg-brand-soft/60 ring-1 ring-brand/30" : "hover:bg-muted/60",
                      )}
                    >
                      <Icon className={cn("mt-0.5 size-4 shrink-0", cls)} />
                      <span className="min-w-0">
                        <span className="block text-sm font-medium">{a.label}</span>
                        <span className="block text-xs text-muted-foreground">{a.description}</span>
                      </span>
                    </button>
                  );
                })}
              </div>
            )}
          />

          {(action === "auto_approve" || action === "auto_reject") && (
            <Field data-invalid={!!formState.errors.feedback}>
              <FieldLabel htmlFor="rule-feedback">
                Feedback{" "}
                {action === "auto_approve" && <span className="font-normal text-muted-foreground">(optional)</span>}
              </FieldLabel>
              <Textarea
                id="rule-feedback"
                rows={2}
                {...register("feedback")}
                placeholder={
                  action === "auto_reject" ? "Why it was rejected — agents read this" : "Note stored with the decision"
                }
              />
              <FieldError errors={[formState.errors.feedback]} />
            </Field>
          )}
          {action === "set_status" && (
            <Field>
              <FieldLabel htmlFor="rule-status">New status</FieldLabel>
              <NativeSelect id="rule-status" {...register("status")} className="sm:max-w-60">
                {SET_STATUS_OPTIONS.map((s) => (
                  <option key={s} value={s}>
                    {statusMeta(s).label}
                  </option>
                ))}
              </NativeSelect>
              <FieldDescription className="text-xs">Approve and reject have their own actions.</FieldDescription>
            </Field>
          )}
          {action === "expire" && (
            <Field data-invalid={!!formState.errors.expire_amount}>
              <FieldLabel htmlFor="rule-expire">Expire if not reviewed within</FieldLabel>
              <div className="flex flex-wrap gap-1.5" role="group" aria-label="Expiry presets">
                {EXPIRE_PRESETS.map((p) => {
                  const on = values.expire_amount === p.amount && values.expire_unit === p.unit;
                  return (
                    <ToggleChip
                      key={p.label}
                      pressed={on}
                      onClick={() => {
                        setValue("expire_amount", p.amount, { shouldValidate: true });
                        setValue("expire_unit", p.unit, { shouldValidate: true });
                      }}
                    >
                      {p.label}
                    </ToggleChip>
                  );
                })}
              </div>
              <div className="flex gap-2 sm:max-w-72">
                <Input
                  id="rule-expire"
                  type="number"
                  inputMode="numeric"
                  min={1}
                  className="w-24"
                  {...register("expire_amount", { valueAsNumber: true })}
                  aria-invalid={!!formState.errors.expire_amount}
                />
                <NativeSelect {...register("expire_unit")} aria-label="Unit" className="flex-1">
                  <option value="minutes">minutes</option>
                  <option value="hours">hours</option>
                  <option value="days">days</option>
                </NativeSelect>
              </div>
              <FieldError errors={[formState.errors.expire_amount]} />
            </Field>
          )}
          {action === "escalate" && (
            <Field data-invalid={!!formState.errors.note}>
              <FieldLabel htmlFor="rule-note">
                Note <span className="font-normal text-muted-foreground">(optional)</span>
              </FieldLabel>
              <Textarea id="rule-note" rows={2} {...register("note")} placeholder="Needs a human today" />
              <FieldDescription className="text-xs">
                Sent with <code>item.escalated</code> to webhooks; the item stays in the Inbox.
              </FieldDescription>
              <FieldError errors={[formState.errors.note]} />
            </Field>
          )}
          {action === "escalate" && <EscalateChannelField register={register} current={values.channel_id} />}
        </Section>

        <label className="flex items-center justify-between gap-3 rounded-xl border p-3">
          <span>
            <span className="block text-sm font-medium">Enabled</span>
            <span className="block text-xs text-muted-foreground">Disabled rules are skipped.</span>
          </span>
          <Controller
            control={control}
            name="enabled"
            render={({ field }) => (
              <Switch checked={field.value} onCheckedChange={field.onChange} aria-label="Enabled" />
            )}
          />
        </label>
      </div>

      <div className="grid gap-3 border-t bg-background/95 p-4 pb-[max(1rem,env(safe-area-inset-bottom))] backdrop-blur">
        <p
          className="flex items-start gap-2 rounded-lg bg-muted/60 px-3 py-2 text-[13px] leading-snug"
          aria-live="polite"
          data-testid="rule-summary"
        >
          <ArrowRight className="mt-0.5 size-3.5 shrink-0 text-brand" aria-hidden />
          <span className="min-w-0 break-words">{summary}</span>
        </p>
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={onDone}>
            Cancel
          </Button>
          <Button type="submit" disabled={save.isPending}>
            {save.isPending ? "Saving…" : rule ? "Save rule" : "Create rule"}
          </Button>
        </div>
      </div>
    </form>
  );
}

function ConditionRow({
  index,
  control,
  errors,
  register,
  onRemove,
  onOpChange,
}: {
  index: number;
  control: Control<RuleFormValues>;
  errors: FieldErrors<RuleFormValues>;
  register: ReturnType<typeof useForm<RuleFormValues>>["register"];
  onRemove: () => void;
  onOpChange: (op: RuleFormValues["conditions"][number]["op"]) => void;
}) {
  const op = useWatch({ control, name: `conditions.${index}.op` });
  const kind = valueKind(op);
  const err = errors.conditions?.[index];
  const opField = register(`conditions.${index}.op`);
  return (
    <li className="grid gap-2 rounded-xl border bg-muted/20 p-2.5" data-testid="condition-row">
      <div className="grid gap-2 sm:grid-cols-[minmax(0,1fr)_9.5rem]">
        <div
          className="flex min-w-0 items-center rounded-lg border border-input bg-background focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/50 aria-invalid:border-destructive dark:bg-input/30"
          aria-invalid={!!err?.path}
        >
          <span className="pl-2.5 font-mono text-xs text-muted-foreground select-none">metadata.</span>
          <input
            {...register(`conditions.${index}.path`)}
            aria-label={`Condition ${index + 1} path`}
            aria-invalid={!!err?.path}
            placeholder="score"
            autoComplete="off"
            autoCapitalize="off"
            spellCheck={false}
            className="h-8 min-w-0 flex-1 bg-transparent pr-2.5 font-mono text-base outline-none placeholder:text-muted-foreground/70 md:text-sm pointer-coarse:h-11"
          />
        </div>
        <NativeSelect
          {...opField}
          onChange={(e) => {
            void opField.onChange(e);
            onOpChange(e.target.value as RuleFormValues["conditions"][number]["op"]);
          }}
          aria-label={`Condition ${index + 1} operator`}
        >
          {RULE_OPS.map((o) => (
            <option key={o.value} value={o.value}>
              {o.symbol === o.label ? o.label : `${o.label} (${o.symbol})`}
            </option>
          ))}
        </NativeSelect>
      </div>
      <div className="flex items-start gap-2">
        <div className="min-w-0 flex-1">
          {kind === "number" && (
            <Input
              type="text"
              inputMode="decimal"
              {...register(`conditions.${index}.value`)}
              aria-label={`Condition ${index + 1} value`}
              aria-invalid={!!err?.value}
              placeholder="0.9"
              className="font-mono"
            />
          )}
          {kind === "text" && (
            <Input
              {...register(`conditions.${index}.value`)}
              aria-label={`Condition ${index + 1} value`}
              aria-invalid={!!err?.value}
              placeholder={op === "contains" ? "text or list element" : "value, number or true/false"}
            />
          )}
          {kind === "list" && (
            <Controller
              control={control}
              name={`conditions.${index}.list`}
              render={({ field }) => (
                <ChipsInput
                  value={field.value}
                  onChange={field.onChange}
                  label={`Condition ${index + 1} values`}
                  placeholder="Type a value, press Enter"
                  invalid={!!err?.list}
                />
              )}
            />
          )}
          {kind === "none" && (
            <p className="flex h-8 items-center text-xs text-muted-foreground pointer-coarse:h-11">
              Matches when the key is present with any value.
            </p>
          )}
        </div>
        <Button
          type="button"
          size="icon"
          variant="ghost"
          onClick={onRemove}
          aria-label={`Remove condition ${index + 1}`}
          className="text-muted-foreground"
        >
          <Trash2 />
        </Button>
      </div>
      <FieldError errors={[err?.path, err?.value, err?.list as { message?: string } | undefined]} className="text-xs" />
    </li>
  );
}

/** Escalate → optional single notification channel (params.channel_id). */
function EscalateChannelField({ register, current }: { register: UseFormRegister<RuleFormValues>; current: string }) {
  const channels = useChannels();
  const list = channels.data ?? [];
  const missing = !!current && !channels.isLoading && !list.some((c) => c.id === current);
  return (
    <Field>
      <FieldLabel htmlFor="rule-channel">
        Notify channel <span className="font-normal text-muted-foreground">(optional)</span>
      </FieldLabel>
      <NativeSelect id="rule-channel" {...register("channel_id")} className="sm:max-w-80" data-testid="rule-channel">
        <option value="">All channels subscribed to item.escalated</option>
        {list.map((c) => (
          <option key={c.id} value={c.id}>
            {c.name} ({c.type}){c.enabled ? "" : " · paused"}
          </option>
        ))}
        {missing && <option value={current}>Deleted channel ({current})</option>}
      </NativeSelect>
      <FieldDescription className="text-xs">
        {channels.error
          ? "Could not load notification channels."
          : list.length
            ? "A chosen channel gets the escalation even if it isn't subscribed to item.escalated."
            : "No notification channels yet — add one under Settings → Notifications."}
      </FieldDescription>
    </Field>
  );
}
