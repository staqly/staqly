"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Plus, X } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { api, ApiError, type Webhook, type WebhookInput } from "@/lib/api";
import { toastApiError } from "@/lib/notify";
import { qk, useCollections } from "@/lib/queries";

export const COMMON_WEBHOOK_EVENTS: { value: string; hint: string }[] = [
  { value: "*", hint: "Every event" },
  { value: "item.*", hint: "Every item event" },
  { value: "item.created", hint: "Agent added an item" },
  { value: "item.approved", hint: "Reviewer approved" },
  { value: "item.rejected", hint: "Reviewer rejected (with feedback)" },
  { value: "item.regenerate_requested", hint: "Regenerate action" },
  { value: "item.publish_requested", hint: "Publish action" },
  { value: "item.retry_requested", hint: "Retry action" },
  { value: "item.status_changed", hint: "Status set via API" },
  { value: "item.escalated", hint: "A rule escalated an item" },
  { value: "item.expired", hint: "Rejected after a rule's expiry" },
  { value: "item.updated", hint: "Fields edited" },
  { value: "item.deleted", hint: "Item deleted" },
  { value: "collection.*", hint: "Collection changes" },
];

const PATTERN_RE = /^[^\s/]{1,100}$/;

export function WebhookFormDialog({
  open,
  onOpenChange,
  webhook,
  onSaved,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  webhook?: Webhook | null;
  onSaved?: (w: Webhook) => void;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90svh] overflow-y-auto sm:max-w-xl">
        {open && (
          <WebhookForm
            key={webhook?.id ?? "new"}
            webhook={webhook ?? null}
            onCancel={() => onOpenChange(false)}
            onSaved={(w) => {
              onOpenChange(false);
              onSaved?.(w);
            }}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}

function WebhookForm({
  webhook,
  onCancel,
  onSaved,
}: {
  webhook: Webhook | null;
  onCancel: () => void;
  onSaved: (w: Webhook) => void;
}) {
  const qc = useQueryClient();
  const collections = useCollections(true);
  const [url, setUrl] = useState(webhook?.url ?? "");
  const [description, setDescription] = useState(webhook?.description ?? "");
  const [events, setEvents] = useState<string[]>(webhook?.events ?? ["item.approved", "item.rejected"]);
  const [custom, setCustom] = useState("");
  const [collectionIds, setCollectionIds] = useState<string[]>(webhook?.collection_ids ?? []);
  const [timeoutMs, setTimeoutMs] = useState(String(webhook?.timeout_ms ?? 10000));
  const [maxAttempts, setMaxAttempts] = useState(String(webhook?.max_attempts ?? 8));
  const [active, setActive] = useState(webhook?.active ?? true);
  const [errors, setErrors] = useState<Record<string, string>>({});

  const save = useMutation({
    mutationFn: (body: WebhookInput) => (webhook ? api.updateWebhook(webhook.id, body) : api.createWebhook(body)),
    onSuccess: (w) => {
      void qc.invalidateQueries({ queryKey: qk.webhooks });
      toast.success(webhook ? "Webhook updated" : "Webhook created");
      onSaved(w);
    },
    onError: (e) => {
      if (e instanceof ApiError && e.fields) setErrors(e.fields);
      toastApiError(e);
    },
  });

  function addCustom() {
    const v = custom.trim();
    if (!v) return;
    if (!PATTERN_RE.test(v)) {
      setErrors((x) => ({ ...x, events: "Patterns cannot contain spaces or slashes" }));
      return;
    }
    if (!events.includes(v)) setEvents([...events, v]);
    setCustom("");
  }

  function submit(e: React.FormEvent) {
    e.preventDefault();
    const errs: Record<string, string> = {};
    if (!/^https?:\/\/[^\s]+$/i.test(url.trim())) errs.url = "Enter an absolute http(s) URL";
    if (!events.length) errs.events = "Subscribe to at least one event (use * for all)";
    const t = Number(timeoutMs);
    if (!Number.isInteger(t) || t < 1000 || t > 60000) errs.timeout_ms = "Between 1000 and 60000 ms";
    const m = Number(maxAttempts);
    if (!Number.isInteger(m) || m < 1 || m > 20) errs.max_attempts = "Between 1 and 20";
    setErrors(errs);
    if (Object.keys(errs).length) return;
    save.mutate({
      url: url.trim(),
      description: description.trim(),
      events,
      collection_ids: collectionIds,
      timeout_ms: t,
      max_attempts: m,
      active,
    });
  }

  const extraEvents = events.filter((e) => !COMMON_WEBHOOK_EVENTS.some((c) => c.value === e));

  return (
    <form onSubmit={submit} className="grid gap-4" noValidate>
      <DialogHeader>
        <DialogTitle>{webhook ? "Edit webhook" : "New webhook"}</DialogTitle>
        <DialogDescription>
          staq.ly POSTs signed JSON to this URL when matching events happen, retrying with backoff on failure.
        </DialogDescription>
      </DialogHeader>
      <div className="grid gap-2">
        <Label htmlFor="wh-url">Endpoint URL</Label>
        <Input
          id="wh-url"
          // oxlint-disable-next-line jsx-a11y/no-autofocus -- first field of a modal dialog (WAI-ARIA dialog pattern)
          autoFocus
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          placeholder="http://host.docker.internal:5678/webhook/staqly"
          className="font-mono text-sm"
          aria-invalid={!!errors.url}
        />
        {errors.url && <p className="text-sm text-destructive">{errors.url}</p>}
      </div>
      <div className="grid gap-2">
        <Label htmlFor="wh-desc">Description</Label>
        <Input
          id="wh-desc"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="n8n review loop"
        />
      </div>
      <fieldset className="grid gap-2">
        <legend className="mb-2 text-sm font-medium">Events</legend>
        <div className="grid gap-1 sm:grid-cols-2">
          {COMMON_WEBHOOK_EVENTS.map((ev) => (
            <label key={ev.value} className="flex items-start gap-2 rounded-md p-1 hover:bg-muted/40">
              <Checkbox
                checked={events.includes(ev.value)}
                aria-label={ev.value}
                onCheckedChange={(v) =>
                  setEvents((cur) => (v ? [...cur, ev.value] : cur.filter((x) => x !== ev.value)))
                }
                className="mt-0.5"
              />
              <span className="min-w-0">
                <span className="font-mono text-xs">{ev.value}</span>
                <span className="block text-xs text-muted-foreground">{ev.hint}</span>
              </span>
            </label>
          ))}
        </div>
        {extraEvents.length > 0 && (
          <div className="flex flex-wrap gap-1">
            {extraEvents.map((e) => (
              <span
                key={e}
                className="inline-flex items-center gap-1 rounded-md bg-muted py-0.5 pr-1 pl-1.5 font-mono text-xs"
              >
                {e}
                <button
                  type="button"
                  onClick={() => setEvents(events.filter((x) => x !== e))}
                  aria-label={`Remove ${e}`}
                >
                  <X className="size-3" />
                </button>
              </span>
            ))}
          </div>
        )}
        <div className="flex gap-2">
          <Input
            value={custom}
            onChange={(e) => setCustom(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                addCustom();
              }
            }}
            placeholder="Custom event or pattern, e.g. item.my_action_requested"
            className="font-mono text-xs"
            aria-label="Custom event pattern"
          />
          <Button type="button" variant="outline" size="sm" onClick={addCustom} className="h-8">
            <Plus /> Add
          </Button>
        </div>
        {errors.events && <p className="text-sm text-destructive">{errors.events}</p>}
      </fieldset>
      <fieldset className="grid gap-2">
        <legend className="mb-1 text-sm font-medium">Collections</legend>
        <p className="text-xs text-muted-foreground">Leave all unchecked to receive events from every collection.</p>
        <div className="grid max-h-36 gap-1 overflow-y-auto sm:grid-cols-2">
          {(collections.data ?? []).map((c) => (
            <label key={c.id} className="flex items-center gap-2 rounded-md p-1 text-sm hover:bg-muted/40">
              <Checkbox
                checked={collectionIds.includes(c.id)}
                onCheckedChange={(v) => setCollectionIds((cur) => (v ? [...cur, c.id] : cur.filter((x) => x !== c.id)))}
              />
              <span className="truncate">{c.name}</span>
            </label>
          ))}
        </div>
        {errors.collection_ids && <p className="text-sm text-destructive">{errors.collection_ids}</p>}
      </fieldset>
      <div className="grid gap-4 sm:grid-cols-3">
        <div className="grid gap-2">
          <Label htmlFor="wh-timeout">Timeout (ms)</Label>
          <Input
            id="wh-timeout"
            type="number"
            min={1000}
            max={60000}
            step={500}
            value={timeoutMs}
            onChange={(e) => setTimeoutMs(e.target.value)}
            aria-invalid={!!errors.timeout_ms}
          />
          {errors.timeout_ms && <p className="text-xs text-destructive">{errors.timeout_ms}</p>}
        </div>
        <div className="grid gap-2">
          <Label htmlFor="wh-attempts">Max attempts</Label>
          <Input
            id="wh-attempts"
            type="number"
            min={1}
            max={20}
            value={maxAttempts}
            onChange={(e) => setMaxAttempts(e.target.value)}
            aria-invalid={!!errors.max_attempts}
          />
          {errors.max_attempts && <p className="text-xs text-destructive">{errors.max_attempts}</p>}
        </div>
        <div className="grid gap-2">
          <Label htmlFor="wh-active">Active</Label>
          <div className="flex h-8 items-center">
            <Switch id="wh-active" checked={active} onCheckedChange={setActive} />
          </div>
        </div>
      </div>
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Saving…" : webhook ? "Save webhook" : "Create webhook"}
        </Button>
      </DialogFooter>
    </form>
  );
}
