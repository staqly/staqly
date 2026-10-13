import {
  Archive,
  Bot,
  Check,
  CirclePlus,
  Cog,
  FolderPlus,
  KeyRound,
  Laptop,
  ListChecks,
  Radar,
  Siren,
  Timer,
  TimerOff,
  Pencil,
  RefreshCw,
  Repeat,
  Send,
  Trash2,
  User,
  X,
  Zap,
} from "lucide-react";
import Link from "next/link";
import type { StashEvent } from "@/lib/api";
import { formatDateTime, timeAgo } from "@/lib/format";
import { cn } from "@/lib/utils";
import { StatusBadge } from "@/components/common/status-badge";

const LABELS: Record<string, string> = {
  "item.created": "Created",
  "item.updated": "Edited",
  "item.status_changed": "Status changed",
  "item.approved": "Approved",
  "item.rejected": "Rejected",
  "item.deleted": "Deleted",
  "item.archived": "Archived",
  "item.regenerate_requested": "Regeneration requested",
  "item.publish_requested": "Publish requested",
  "item.retry_requested": "Retry requested",
  "collection.created": "Collection created",
  "collection.updated": "Collection updated",
  "collection.archived": "Collection archived",
  "collection.deleted": "Collection deleted",
  "api_key.created": "API key created",
  "api_key.revoked": "API key revoked",
  "recovery_key.rotated": "Recovery key rotated",
  "workspace.created": "Workspace created",
  "webhook.test": "Webhook test",
  "item.escalated": "Escalated",
  "item.rule_applied": "Rule applied",
  "item.expired": "Expired",
  "rule.created": "Rule created",
  "rule.updated": "Rule updated",
  "rule.deleted": "Rule deleted",
  "watcher.created": "Watcher created",
  "item.purged": "Purged",
  "workspace.export_requested": "Workspace export requested",
  "workspace.exported": "Workspace exported",
};

export function eventLabel(type: string) {
  if (LABELS[type]) return LABELS[type];
  const tail = type.split(".").slice(1).join(" ").replace(/_/g, " ");
  return tail ? tail.charAt(0).toUpperCase() + tail.slice(1) : type;
}

function eventIcon(type: string) {
  if (type === "item.escalated") return { Icon: Siren, cls: "text-amber-600 bg-amber-500/12 dark:text-amber-400" };
  if (type === "item.expired") return { Icon: TimerOff, cls: "text-rose-600 bg-rose-500/10 dark:text-rose-400" };
  if (type === "item.rule_applied")
    return { Icon: Timer, cls: "text-violet-600 bg-violet-500/10 dark:text-violet-400" };
  if (type.startsWith("rule.")) return { Icon: ListChecks, cls: "text-muted-foreground bg-muted" };
  if (type.startsWith("watcher.")) return { Icon: Radar, cls: "text-muted-foreground bg-muted" };
  if (type === "item.approved") return { Icon: Check, cls: "text-emerald-600 bg-emerald-500/10 dark:text-emerald-400" };
  if (type === "item.rejected") return { Icon: X, cls: "text-rose-600 bg-rose-500/10 dark:text-rose-400" };
  if (type === "item.created") return { Icon: CirclePlus, cls: "text-sky-600 bg-sky-500/10 dark:text-sky-400" };
  if (type === "item.updated") return { Icon: Pencil, cls: "text-muted-foreground bg-muted" };
  if (type === "item.status_changed")
    return { Icon: Repeat, cls: "text-violet-600 bg-violet-500/10 dark:text-violet-400" };
  if (type.endsWith("deleted")) return { Icon: Trash2, cls: "text-muted-foreground bg-muted" };
  if (type.endsWith("archived")) return { Icon: Archive, cls: "text-muted-foreground bg-muted" };
  if (type.includes("regenerate") || type.includes("retry"))
    return { Icon: RefreshCw, cls: "text-brand bg-brand-soft" };
  if (type.includes("publish")) return { Icon: Send, cls: "text-brand bg-brand-soft" };
  if (type.startsWith("collection.")) return { Icon: FolderPlus, cls: "text-muted-foreground bg-muted" };
  if (type.startsWith("api_key.") || type.startsWith("recovery_key."))
    return { Icon: KeyRound, cls: "text-muted-foreground bg-muted" };
  if (type.startsWith("item.")) return { Icon: Zap, cls: "text-brand bg-brand-soft" };
  return { Icon: Cog, cls: "text-muted-foreground bg-muted" };
}

/** System actors created by automation: "Rule: <name>", "Watcher: <name>", "Expiry". */
export function systemActorKind(actor: StashEvent["actor"]): "rule" | "watcher" | "expiry" | null {
  if (actor.type !== "system") return null;
  if (actor.name.startsWith("Rule: ")) return "rule";
  if (actor.name.startsWith("Watcher: ")) return "watcher";
  if (actor.id === "expiry" || actor.name === "Expiry") return "expiry";
  return null;
}

export function ActorLabel({ actor }: { actor: StashEvent["actor"] }) {
  const auto = systemActorKind(actor);
  if (auto) {
    const Icon = auto === "rule" ? ListChecks : auto === "watcher" ? Radar : Timer;
    const name = auto === "rule" ? actor.name.slice(6) : auto === "watcher" ? actor.name.slice(9) : "Expiry timer";
    const href = auto === "rule" ? "/rules" : auto === "watcher" ? "/watchers" : undefined;
    const body = (
      <>
        <Icon className="size-3" />
        <span className="text-muted-foreground/80">
          {auto === "rule" ? "Rule" : auto === "watcher" ? "Watcher" : "Automation"}
        </span>
        <span className="max-w-48 truncate font-medium text-foreground/80">{name}</span>
      </>
    );
    return href ? (
      <Link
        href={href}
        data-testid="actor-automation"
        className="inline-flex items-center gap-1 rounded-md bg-muted/70 px-1.5 py-0.5 text-muted-foreground hover:bg-muted"
        title={`${actor.name} (${actor.id})`}
      >
        {body}
      </Link>
    ) : (
      <span
        data-testid="actor-automation"
        className="inline-flex items-center gap-1 rounded-md bg-muted/70 px-1.5 py-0.5 text-muted-foreground"
      >
        {body}
      </span>
    );
  }
  const Icon = actor.type === "api_key" ? Bot : actor.type === "session" ? User : actor.type === "local" ? Laptop : Cog;
  const kind =
    actor.type === "api_key"
      ? "API key"
      : actor.type === "session"
        ? "Dashboard"
        : actor.type === "local"
          ? "Local"
          : "System";
  return (
    <span className="inline-flex items-center gap-1 text-muted-foreground" title={`${kind}: ${actor.id}`}>
      <Icon className="size-3" />
      <span className="max-w-48 truncate">{actor.name || kind}</span>
    </span>
  );
}

interface EventData {
  feedback?: string;
  previous_status?: string;
  action?: string;
  edited?: boolean;
  item?: { id?: string; title?: string; status?: string };
  collection?: { id?: string; name?: string };
  rule?: { id?: string; name?: string; action?: string };
  note?: string;
  expires_at?: string;
}

export function eventData(e: StashEvent): EventData {
  return (e.data ?? {}) as EventData;
}

export function EventTimeline({ events, showItem = false }: { events: StashEvent[]; showItem?: boolean }) {
  return (
    <ol className="relative" data-testid="event-timeline">
      {events.map((e, i) => {
        const { Icon, cls } = eventIcon(e.type);
        const d = eventData(e);
        const statusChanged = d.previous_status && d.item?.status && d.previous_status !== d.item.status;
        return (
          <li key={e.id} className="relative flex gap-3 pb-5 last:pb-0">
            {i < events.length - 1 && (
              <span className="absolute top-7 bottom-0 left-[13px] w-px bg-border" aria-hidden />
            )}
            <span className={cn("relative z-10 flex size-7 shrink-0 items-center justify-center rounded-full", cls)}>
              <Icon className="size-3.5" />
            </span>
            <div className="min-w-0 flex-1 pt-0.5">
              <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm">
                <span className="font-medium">{eventLabel(e.type)}</span>
                {statusChanged && (
                  <span className="inline-flex items-center gap-1">
                    <StatusBadge status={d.previous_status!} />
                    <span className="text-muted-foreground">→</span>
                    <StatusBadge status={d.item!.status!} />
                  </span>
                )}
                {d.edited && <span className="text-xs text-muted-foreground">(with edits)</span>}
              </div>
              {showItem && e.item_id && (
                <Link
                  href={`/items/${e.item_id}`}
                  className="mt-0.5 block truncate text-sm text-foreground/80 hover:underline"
                >
                  {d.item?.title || e.item_id}
                </Link>
              )}
              {showItem && !e.item_id && e.collection_id && e.type !== "collection.deleted" && (
                <Link
                  href={`/collections/${e.collection_id}`}
                  className="mt-0.5 block truncate text-sm text-foreground/80 hover:underline"
                >
                  {d.collection?.name || e.collection_id}
                </Link>
              )}
              {showItem && e.item_id && d.collection?.name && (
                <div className="truncate text-xs text-muted-foreground">in {d.collection.name}</div>
              )}
              {e.type === "item.rule_applied" && d.expires_at && (
                <div className="mt-1 text-sm text-muted-foreground">
                  Expires if still pending{" "}
                  <time
                    dateTime={d.expires_at}
                    title={formatDateTime(d.expires_at)}
                    className="font-medium text-foreground/80"
                  >
                    {timeAgo(d.expires_at)}
                  </time>
                </div>
              )}
              {d.note && (
                <blockquote className="mt-1.5 rounded-md border-l-2 border-amber-500/70 bg-amber-500/8 px-3 py-1.5 text-sm whitespace-pre-wrap">
                  {d.note}
                </blockquote>
              )}
              {d.feedback && (
                <blockquote className="mt-1.5 rounded-md border-l-2 border-brand/60 bg-muted/50 px-3 py-1.5 text-sm whitespace-pre-wrap">
                  {d.feedback}
                </blockquote>
              )}
              <div className="mt-1 flex flex-wrap items-center gap-x-2 text-xs text-muted-foreground">
                <ActorLabel actor={e.actor} />
                <span>·</span>
                <time dateTime={e.created_at} title={formatDateTime(e.created_at)}>
                  {timeAgo(e.created_at)}
                </time>
                <span className="font-mono opacity-60">{e.type}</span>
              </div>
            </div>
          </li>
        );
      })}
    </ol>
  );
}
