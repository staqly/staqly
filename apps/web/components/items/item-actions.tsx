"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Check, ChevronDown, MoreHorizontal, Pencil, Play, X } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Kbd } from "@/components/ui/kbd";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { api, ApiError, type ActionBody, type ActionDef, type Item, type ItemFields } from "@/lib/api";
import { toastApiError } from "@/lib/notify";
import { prettyJSON } from "@/lib/format";
import { invalidateAfterItemChange, qk, syncItemInCache, useItemActions } from "@/lib/queries";
import { cn } from "@/lib/utils";

export const REJECT_ACTION: ActionDef = {
  name: "reject",
  label: "Reject",
  event: "item.rejected",
  requires_feedback: true,
  builtin: true,
};

/** Reject always asks for feedback in the UI so agents know what to change. */
export function needsFeedback(action: ActionDef) {
  return action.requires_feedback || action.name === "reject";
}

export function conflictMessage(err: ApiError) {
  return /modified|concurrent/i.test(err.message)
    ? "This item changed since you loaded it. The latest version has been loaded."
    : `Already decided — ${err.message}`;
}

/** Mutation that runs approve/reject/custom actions with consistent cache updates & errors. */
export function useRunAction(opts?: { onDone?: (item: Item, action: string) => void; silent?: boolean }) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ item, action, body }: { item: Item; action: string; body?: ActionBody }) =>
      api.runAction(item.id, action, body),
    onSuccess: (updated, { action }) => {
      syncItemInCache(qc, updated);
      invalidateAfterItemChange(qc, updated.id);
      if (!opts?.silent) {
        const verb = action === "approve" ? "Approved" : action === "reject" ? "Rejected" : "Action sent";
        toast.success(`${verb}: ${updated.title}`);
      }
      opts?.onDone?.(updated, action);
    },
    onError: (err, { item }) => {
      if (err instanceof ApiError && err.status === 409) {
        toast.warning(conflictMessage(err));
        void qc.invalidateQueries({ queryKey: qk.item(item.id) });
        void qc.invalidateQueries({ queryKey: qk.itemsAll });
        invalidateAfterItemChange(qc, item.id);
        return;
      }
      toastApiError(err);
    },
  });
}

export function FeedbackDialog({
  open,
  onOpenChange,
  action,
  itemTitle,
  pending,
  onSubmit,
  count,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  action: ActionDef | null;
  itemTitle?: string;
  pending?: boolean;
  onSubmit: (feedback: string) => void;
  count?: number;
}) {
  const [feedback, setFeedback] = useState("");
  const required = action ? needsFeedback(action) : false;
  const canSubmit = !pending && (!required || feedback.trim().length > 0);
  const isReject = action?.name === "reject";

  function submit() {
    if (!canSubmit) return;
    onSubmit(feedback.trim());
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (!o) setFeedback("");
        onOpenChange(o);
      }}
    >
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>
            {action?.label ?? "Action"}
            {count && count > 1 ? ` ${count} items` : ""}
          </DialogTitle>
          <DialogDescription>
            {isReject
              ? "Tell the agent what to change. Feedback is stored on the item and sent with the webhook."
              : required
                ? "This action requires feedback for the agent."
                : "Optional note sent with the action event."}
            {itemTitle && <span className="mt-1 block truncate font-medium text-foreground">{itemTitle}</span>}
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-2">
          <Label htmlFor="action-feedback">Feedback{required ? "" : " (optional)"}</Label>
          <Textarea
            id="action-feedback"
            // oxlint-disable-next-line jsx-a11y/no-autofocus -- first field of a modal dialog (WAI-ARIA dialog pattern)
            autoFocus
            rows={4}
            value={feedback}
            placeholder={isReject ? "e.g. Background is too busy, make the face larger" : "Add a note…"}
            onChange={(e) => setFeedback(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
                e.preventDefault();
                submit();
              }
            }}
          />
          <p className="text-xs text-muted-foreground">
            <Kbd>Enter</Kbd> to submit · <Kbd>Shift</Kbd>+<Kbd>Enter</Kbd> for a new line
          </p>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button variant={isReject ? "destructive" : "default"} disabled={!canSubmit} onClick={submit}>
            {pending ? "Sending…" : (action?.label ?? "Submit")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function editableContent(item: Item) {
  return item.type === "text" || item.type === "json" || item.type === "link";
}

/** Approve with edits ("e" in the inbox). Edits are applied atomically with the decision. */
export function EditApproveDialog({
  item,
  open,
  onOpenChange,
  pending,
  onSubmit,
}: {
  item: Item | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  pending?: boolean;
  onSubmit: (edits: ItemFields, feedback: string) => void;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        {item && (
          <EditApproveForm
            key={item.id + item.version}
            item={item}
            pending={pending}
            onSubmit={onSubmit}
            onCancel={() => onOpenChange(false)}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}

function EditApproveForm({
  item,
  pending,
  onSubmit,
  onCancel,
}: {
  item: Item;
  pending?: boolean;
  onSubmit: (edits: ItemFields, feedback: string) => void;
  onCancel: () => void;
}) {
  const [title, setTitle] = useState(item.title);
  const [content, setContent] = useState(item.type === "json" ? prettyJSON(item.content) : item.content);
  const [url, setUrl] = useState(item.url);
  const [feedback, setFeedback] = useState("");
  const [error, setError] = useState<string | null>(null);

  function submit(e: React.FormEvent) {
    e.preventDefault();
    const edits: ItemFields = {};
    if (title !== item.title) edits.title = title;
    if (editableContent(item)) {
      if (item.type === "json") {
        try {
          const parsed = JSON.parse(content || "null");
          if (JSON.stringify(parsed) !== item.content) edits.content = parsed;
        } catch {
          setError("Content must be valid JSON");
          return;
        }
      } else if (content !== item.content) {
        edits.content = content;
      }
    }
    if (item.type === "link" && url !== item.url) edits.url = url;
    onSubmit(edits, feedback.trim());
  }

  return (
    <form onSubmit={submit} className="grid gap-4">
      <DialogHeader>
        <DialogTitle>Edit & approve</DialogTitle>
        <DialogDescription>Changes are saved together with the approval in one step.</DialogDescription>
      </DialogHeader>
      <div className="grid gap-2">
        <Label htmlFor="edit-title">Title</Label>
        <Input
          id="edit-title"
          // oxlint-disable-next-line jsx-a11y/no-autofocus -- first field of a modal dialog (WAI-ARIA dialog pattern)
          autoFocus
          value={title}
          onChange={(e) => setTitle(e.target.value)}
        />
      </div>
      {item.type === "link" && (
        <div className="grid gap-2">
          <Label htmlFor="edit-url">URL</Label>
          <Input id="edit-url" value={url} onChange={(e) => setUrl(e.target.value)} className="font-mono text-xs" />
        </div>
      )}
      {editableContent(item) && (
        <div className="grid gap-2">
          <Label htmlFor="edit-content">{item.type === "json" ? "JSON" : "Content"}</Label>
          <Textarea
            id="edit-content"
            rows={10}
            value={content}
            onChange={(e) => {
              setContent(e.target.value);
              setError(null);
            }}
            className={cn("max-h-[45vh]", item.type === "json" && "font-mono text-xs")}
            aria-invalid={!!error}
          />
          {error && <p className="text-sm text-destructive">{error}</p>}
        </div>
      )}
      <div className="grid gap-2">
        <Label htmlFor="edit-feedback">Note (optional)</Label>
        <Input id="edit-feedback" value={feedback} onChange={(e) => setFeedback(e.target.value)} />
      </div>
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={pending}>
          <Check /> {pending ? "Approving…" : "Save & approve"}
        </Button>
      </DialogFooter>
    </form>
  );
}

/**
 * Action buttons for an item, driven by GET /v1/items/{id}/actions so the
 * buttons always reflect the caller's scopes and the item's current status.
 */
export function ItemActionBar({
  item,
  className,
  size = "sm",
  showEdit = true,
  onDone,
  compact = false,
}: {
  item: Item;
  className?: string;
  size?: "sm" | "default" | "xs";
  showEdit?: boolean;
  onDone?: (item: Item, action: string) => void;
  /** Touch layout: big Reject / Approve plus a "more" menu (used in sticky mobile bars). */
  compact?: boolean;
}) {
  const actions = useItemActions(item.id, item.version);
  const run = useRunAction({ onDone });
  const [feedbackFor, setFeedbackFor] = useState<ActionDef | null>(null);
  const [editing, setEditing] = useState(false);

  const list = actions.data ?? [];
  const approve = list.find((a) => a.name === "approve");
  const reject = list.find((a) => a.name === "reject");
  const custom = list.filter((a) => !a.builtin);

  function trigger(action: ActionDef) {
    if (needsFeedback(action)) {
      setFeedbackFor(action);
      return;
    }
    run.mutate({ item, action: action.name, body: { expected_version: item.version } });
  }

  if (actions.isLoading) {
    return <div className={cn("h-8 w-48 animate-pulse rounded-lg bg-muted", compact && "h-12 w-full", className)} />;
  }

  const dialogs = (
    <>
      <FeedbackDialog
        open={!!feedbackFor}
        onOpenChange={(o) => !o && setFeedbackFor(null)}
        action={feedbackFor}
        itemTitle={item.title}
        pending={run.isPending}
        onSubmit={(feedback) => {
          const action = feedbackFor!;
          run.mutate(
            { item, action: action.name, body: { feedback, expected_version: item.version } },
            { onSettled: () => setFeedbackFor(null) },
          );
        }}
      />
      <EditApproveDialog
        item={editing ? item : null}
        open={editing}
        onOpenChange={setEditing}
        pending={run.isPending}
        onSubmit={(edits, feedback) =>
          run.mutate(
            {
              item,
              action: "approve",
              body: {
                ...(Object.keys(edits).length ? { edits } : {}),
                ...(feedback ? { feedback } : {}),
                expected_version: item.version,
              },
            },
            { onSettled: () => setEditing(false) },
          )
        }
      />
    </>
  );

  if (compact) {
    if (list.length === 0) return null;
    const more = (approve && showEdit) || custom.length > 0;
    return (
      <div className={cn("flex items-center gap-2", className)} data-testid="item-actions-compact">
        {reject && (
          <Button
            variant="destructive"
            className="h-12 flex-1 rounded-xl text-[15px] pointer-coarse:h-12"
            onClick={() => trigger(reject)}
            disabled={run.isPending}
          >
            <X className="size-5" /> Reject
          </Button>
        )}
        {approve && (
          <Button
            className="h-12 flex-[1.4] rounded-xl bg-emerald-600 text-[15px] text-white hover:bg-emerald-600/90 pointer-coarse:h-12 dark:bg-emerald-500 dark:text-emerald-950"
            onClick={() => trigger(approve)}
            disabled={run.isPending}
          >
            <Check className="size-5" /> Approve
          </Button>
        )}
        {!approve && !reject && custom.length > 0 && (
          <Button
            variant="outline"
            className="h-12 flex-1 rounded-xl pointer-coarse:h-12"
            onClick={() => trigger(custom[0])}
            disabled={run.isPending}
          >
            <Play /> {custom[0].label}
          </Button>
        )}
        {more && (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="outline"
                className="size-12 shrink-0 rounded-xl pointer-coarse:size-12"
                aria-label="More actions"
                disabled={run.isPending}
              >
                <MoreHorizontal className="size-5" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" side="top" className="min-w-56">
              {approve && showEdit && (
                <DropdownMenuItem onSelect={() => setEditing(true)}>
                  <Pencil /> Edit & approve
                </DropdownMenuItem>
              )}
              {custom.length > 0 && <DropdownMenuLabel>Collection actions</DropdownMenuLabel>}
              {custom.map((a) => (
                <DropdownMenuItem key={a.name} onSelect={() => trigger(a)}>
                  <Play />
                  <span className="flex-1">{a.label}</span>
                  {a.set_status && <span className="text-xs text-muted-foreground">→ {a.set_status}</span>}
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        )}
        {dialogs}
      </div>
    );
  }

  return (
    <div className={cn("flex flex-wrap items-center gap-1.5", className)}>
      {approve && (
        <Button size={size} onClick={() => trigger(approve)} disabled={run.isPending}>
          <Check /> Approve
        </Button>
      )}
      {approve && showEdit && (
        <Button size={size} variant="outline" onClick={() => setEditing(true)} disabled={run.isPending}>
          <Pencil /> Edit & approve
        </Button>
      )}
      {reject && (
        <Button size={size} variant="destructive" onClick={() => trigger(reject)} disabled={run.isPending}>
          <X /> Reject
        </Button>
      )}
      {custom.length > 0 &&
        custom.length <= 2 &&
        custom.map((a) => (
          <Button key={a.name} size={size} variant="outline" onClick={() => trigger(a)} disabled={run.isPending}>
            <Play /> {a.label}
          </Button>
        ))}
      {custom.length > 2 && (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button size={size} variant="outline" disabled={run.isPending}>
              Actions <ChevronDown />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="min-w-52">
            <DropdownMenuLabel>Collection actions</DropdownMenuLabel>
            {custom.map((a) => (
              <DropdownMenuItem key={a.name} onSelect={() => trigger(a)}>
                <Play />
                <span className="flex-1">{a.label}</span>
                {a.set_status && <span className="text-xs text-muted-foreground">→ {a.set_status}</span>}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      )}
      {list.length === 0 && (
        <span className="text-xs text-muted-foreground">No actions available in status “{item.status}”.</span>
      )}

      {dialogs}
    </div>
  );
}
