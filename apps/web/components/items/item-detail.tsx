"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowLeft,
  Download,
  Eraser,
  ExternalLink,
  History,
  Layers,
  MoreHorizontal,
  PencilLine,
  Trash2,
  TriangleAlert,
} from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { toast } from "sonner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { CopyButton } from "@/components/common/copy-button";
import { ErrorState, EmptyState } from "@/components/common/error-state";
import { StatusBadge } from "@/components/common/status-badge";
import { ExternalContentBadge } from "@/components/common/external-badge";
import { TYPE_LABELS, TypeIcon } from "@/components/common/type-icon";
import { api, ApiError, mediaUrl, type Item, type ItemType } from "@/lib/api";
import { toastApiError } from "@/lib/notify";
import { formatBytes, formatDateTime, pluralize, prettyJSON, timeAgo } from "@/lib/format";
import { invalidateAfterItemChange, qk, removeItemFromLists, useCollection, useItem } from "@/lib/queries";
import { cn } from "@/lib/utils";
import { useIsMobile } from "@/hooks/use-mobile";
import { EventTimeline } from "./event-timeline";
import { ItemActionBar } from "./item-actions";
import { ItemEditForm } from "./item-edit-form";
import { ItemPreview } from "./item-preview";

function Prop({ label, children, mono }: { label: string; children: React.ReactNode; mono?: boolean }) {
  return (
    <div className="grid grid-cols-[110px_1fr] items-baseline gap-2 py-1.5 text-sm">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className={cn("min-w-0 break-words", mono && "font-mono text-xs")}>{children}</dd>
    </div>
  );
}

function ItemHistory({ id, version }: { id: string; version: number }) {
  const events = useQuery({ queryKey: [...qk.itemEvents(id), version], queryFn: () => api.itemEvents(id) });
  if (events.isLoading) return <Skeleton className="h-40" />;
  if (events.error) return <ErrorState error={events.error} onRetry={() => events.refetch()} />;
  const list = events.data?.data ?? [];
  if (!list.length) return <EmptyState icon={<History />} title="No history yet" />;
  return <EventTimeline events={list} />;
}

function ItemVersions({ id, version }: { id: string; version: number }) {
  const versions = useQuery({
    queryKey: [...qk.itemVersions(id), version],
    queryFn: async () => (await api.itemVersions(id)).data ?? [],
  });
  const [open, setOpen] = useState<number | null>(null);
  if (versions.isLoading) return <Skeleton className="h-40" />;
  if (versions.error) return <ErrorState error={versions.error} onRetry={() => versions.refetch()} />;
  const list = (versions.data ?? []).toSorted((a, b) => (b.version ?? 0) - (a.version ?? 0));
  if (!list.length)
    return (
      <EmptyState
        icon={<Layers />}
        title="No previous versions"
        description="A snapshot is stored before every change to this item."
      />
    );
  return (
    <ul className="divide-y rounded-lg border" data-testid="item-versions">
      {list.map((v) => {
        const snap = v.snapshot;
        const isOpen = open === v.version;
        return (
          <li key={v.version} className="text-sm">
            <button
              type="button"
              className="flex w-full items-center gap-3 px-3 py-2.5 text-left hover:bg-muted/50"
              onClick={() => setOpen(isOpen ? null : (v.version ?? null))}
              aria-expanded={isOpen}
            >
              <span className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">v{v.version}</span>
              <span className="min-w-0 flex-1 truncate">{snap?.title || "Untitled"}</span>
              {snap?.status && <StatusBadge status={snap.status} />}
              <span className="hidden text-xs text-muted-foreground sm:inline" title={formatDateTime(v.created_at)}>
                replaced {timeAgo(v.created_at)}
              </span>
            </button>
            {isOpen && snap && (
              <div className="border-t bg-muted/30 px-3 py-3">
                <div className="mb-2 text-xs text-muted-foreground">
                  Changed by <span className="font-mono">{v.actor}</span> · {formatDateTime(v.created_at)}
                </div>
                <pre className="scrollbar-thin max-h-72 overflow-auto rounded-md border bg-background p-3 font-mono text-[11.5px]">
                  {prettyJSON({
                    title: snap.title,
                    status: snap.status,
                    content: snap.type === "json" && snap.content ? JSON.parse(snap.content) : snap.content,
                    url: snap.url || undefined,
                    metadata: snap.metadata,
                    feedback: snap.feedback || undefined,
                    media: snap.media?.original_name,
                  })}
                </pre>
              </div>
            )}
          </li>
        );
      })}
    </ul>
  );
}

function MetadataSummary({ metadata }: { metadata: Item["metadata"] }) {
  const entries = Object.entries(metadata ?? {});
  if (!entries.length) return <div className="text-sm text-muted-foreground">No metadata</div>;
  return (
    <dl className="divide-y">
      {entries.slice(0, 30).map(([k, v]) => (
        <div key={k} className="grid grid-cols-[110px_1fr] gap-2 py-1.5 text-sm">
          <dt className="truncate font-mono text-xs text-muted-foreground" title={k}>
            {k}
          </dt>
          <dd className="min-w-0 font-mono text-xs break-words">{typeof v === "string" ? v : JSON.stringify(v)}</dd>
        </div>
      ))}
    </dl>
  );
}

export function ItemDetail({
  id,
  variant = "page",
  onDeleted,
}: {
  id: string;
  variant?: "page" | "sheet";
  onDeleted?: () => void;
}) {
  const item = useItem(id);
  const qc = useQueryClient();
  const router = useRouter();
  const collection = useCollection(item.data?.collection_id ?? "");
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [confirmPurge, setConfirmPurge] = useState(false);
  const [purgeAck, setPurgeAck] = useState(false);
  const [tab, setTab] = useState("details");
  const isMobile = useIsMobile();

  const del = useMutation({
    mutationFn: () => api.deleteItem(id),
    onSuccess: () => {
      removeItemFromLists(qc, id);
      qc.removeQueries({ queryKey: qk.item(id) });
      invalidateAfterItemChange(qc);
      toast.success("Item deleted");
      if (onDeleted) onDeleted();
      else router.push(item.data ? `/collections/${item.data.collection_id}` : "/collections");
    },
    onError: (e) => toastApiError(e),
  });

  const purge = useMutation({
    mutationFn: () => api.purgeItem(id),
    onSuccess: (r) => {
      setConfirmPurge(false);
      removeItemFromLists(qc, id);
      qc.removeQueries({ queryKey: ["item", id] });
      invalidateAfterItemChange(qc);
      toast.success("Item purged", {
        description: `${pluralize(r.events_deleted, "event")} erased${r.media_deleted ? ", file deleted" : ""}.`,
      });
      if (onDeleted) onDeleted();
      else router.push(item.data ? `/collections/${item.data.collection_id}` : "/collections");
    },
    onError: (e) => toastApiError(e, "Could not purge the item"),
  });

  if (item.isLoading) {
    return (
      <div className="grid gap-4">
        <Skeleton className="h-8 w-1/2" />
        <Skeleton className="h-[360px]" />
      </div>
    );
  }
  if (item.error) {
    const notFound = item.error instanceof ApiError && item.error.status === 404;
    return (
      <ErrorState
        error={item.error}
        title={notFound ? "Item not found" : "Could not load item"}
        onRetry={notFound ? undefined : () => item.refetch()}
      />
    );
  }
  const it = item.data!;
  const sheet = variant === "sheet";

  return (
    <div className="grid gap-5" data-testid="item-detail">
      <div className="flex flex-col gap-3">
        {!sheet && (
          <Link
            href={`/collections/${it.collection_id}`}
            className="inline-flex w-fit items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
          >
            <ArrowLeft className="size-3" /> {collection.data?.name ?? "Collection"}
          </Link>
        )}
        <div className="flex items-start gap-3">
          <div className="min-w-0 flex-1">
            <h1 className={cn("font-semibold tracking-tight break-words", sheet ? "pr-8 text-lg" : "text-xl")}>
              {it.title || "Untitled"}
            </h1>
            <div className="mt-1.5 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
              <StatusBadge status={it.status} />
              <ExternalContentBadge item={it} />
              <span className="inline-flex items-center gap-1">
                <TypeIcon type={it.type} className="size-3.5" /> {TYPE_LABELS[it.type as ItemType] ?? it.type}
              </span>
              <span>·</span>
              <span title={formatDateTime(it.created_at)}>created {timeAgo(it.created_at)}</span>
              {it.source && (
                <>
                  <span>·</span>
                  <span>via {it.source}</span>
                </>
              )}
              <span className="font-mono opacity-70">v{it.version}</span>
            </div>
          </div>
          <div className={cn("flex items-center gap-1", sheet && "mr-8")}>
            {sheet && (
              <Button asChild variant="ghost" size="sm">
                <Link href={`/items/${it.id}`}>
                  <ExternalLink /> Open
                </Link>
              </Button>
            )}
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="icon-sm" aria-label="More item options">
                  <MoreHorizontal />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onSelect={() => setTab("edit")}>
                  <PencilLine /> Edit item
                </DropdownMenuItem>
                {it.media && (
                  <DropdownMenuItem asChild>
                    <a href={mediaUrl(it.media.id, { download: true })} download={it.media.original_name}>
                      <Download /> Download file
                    </a>
                  </DropdownMenuItem>
                )}
                <DropdownMenuSeparator />
                <DropdownMenuItem variant="destructive" onSelect={() => setConfirmDelete(true)}>
                  <Trash2 /> Delete item
                </DropdownMenuItem>
                <DropdownMenuItem
                  variant="destructive"
                  onSelect={() => {
                    setPurgeAck(false);
                    setConfirmPurge(true);
                  }}
                >
                  <Eraser /> Purge permanently
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>
        {!isMobile && <ItemActionBar item={it} />}
        {it.feedback && (
          <div className="rounded-lg border-l-2 border-brand/70 bg-muted/50 px-3 py-2 text-sm">
            <div className="mb-0.5 text-xs font-medium text-muted-foreground">Latest feedback</div>
            <div className="whitespace-pre-wrap">{it.feedback}</div>
          </div>
        )}
      </div>

      <div className={cn("grid gap-6", !sheet && "lg:grid-cols-[minmax(0,1fr)_300px]")}>
        <div className="grid min-w-0 content-start gap-5">
          <ItemPreview item={it} compact={sheet} />
          <Tabs value={tab} onValueChange={setTab}>
            <TabsList className="max-sm:w-full">
              <TabsTrigger value="details">Details</TabsTrigger>
              <TabsTrigger value="edit">Edit</TabsTrigger>
              <TabsTrigger value="history">History</TabsTrigger>
              <TabsTrigger value="versions">Versions</TabsTrigger>
            </TabsList>
            <TabsContent value="details" className="pt-3">
              <div className="grid gap-4 sm:grid-cols-2">
                <section>
                  <h3 className="mb-1 text-xs font-medium tracking-wide text-muted-foreground uppercase">Metadata</h3>
                  <MetadataSummary metadata={it.metadata} />
                </section>
                {sheet && <ItemProps item={it} collectionName={collection.data?.name} />}
              </div>
            </TabsContent>
            <TabsContent value="edit" className="pt-3">
              <ItemEditForm item={it} />
            </TabsContent>
            <TabsContent value="history" className="pt-3">
              <ItemHistory id={it.id} version={it.version} />
            </TabsContent>
            <TabsContent value="versions" className="pt-3">
              <ItemVersions id={it.id} version={it.version} />
            </TabsContent>
          </Tabs>
        </div>
        {!sheet && (
          <aside className="h-fit rounded-xl border bg-card p-4 lg:sticky lg:top-4">
            <ItemProps item={it} collectionName={collection.data?.name} />
          </aside>
        )}
      </div>

      {isMobile && (
        <>
          {!sheet && <div className="h-16" aria-hidden />}
          <div
            className={cn(
              "z-30 border-t bg-background/90 px-3 py-2.5 backdrop-blur-xl supports-[backdrop-filter]:bg-background/75 empty:hidden",
              sheet
                ? "sticky -bottom-[max(1.25rem,env(safe-area-inset-bottom))] -mx-4 pb-[max(0.625rem,env(safe-area-inset-bottom))]"
                : "fixed inset-x-0 bottom-(--mobile-chrome-bottom)",
            )}
            data-testid="item-sticky-actions"
          >
            <ItemActionBar item={it} compact className="mx-auto max-w-lg" />
          </div>
        </>
      )}

      <AlertDialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete this item?</AlertDialogTitle>
            <AlertDialogDescription>
              “{it.title}” will be removed from its collection. Its event history is kept.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={() => del.mutate()} disabled={del.isPending}>
              Delete item
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={confirmPurge} onOpenChange={(o) => !purge.isPending && setConfirmPurge(o)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Purge “{it.title || "Untitled"}” permanently?</AlertDialogTitle>
            <AlertDialogDescription asChild>
              <div className="grid gap-2">
                <p>
                  Unlike Delete, purge also <strong className="text-foreground">erases its history</strong>: every event
                  about it disappears from the activity log, pending webhook deliveries about it are dropped, its file
                  is deleted unless another item uses it, and finished workspace exports expire.
                </p>
                <p>Use it to remove personal data. Systems that already received the data are not affected.</p>
              </div>
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="flex gap-2 rounded-lg border border-rose-500/40 bg-rose-500/10 p-2.5 text-xs">
            <TriangleAlert className="size-4 shrink-0 text-rose-600" aria-hidden />
            This cannot be undone.
          </div>
          <div className="flex items-center gap-2">
            <Checkbox id="purge-ack" checked={purgeAck} onCheckedChange={(v) => setPurgeAck(v === true)} />
            <Label htmlFor="purge-ack" className="font-normal">
              I understand the item and its history are erased
            </Label>
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={purge.isPending}>Cancel</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={(e) => {
                e.preventDefault();
                purge.mutate();
              }}
              disabled={!purgeAck || purge.isPending}
            >
              {purge.isPending ? "Purging…" : "Purge permanently"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function ItemProps({ item: it, collectionName }: { item: Item; collectionName?: string }) {
  return (
    <section>
      <h3 className="mb-1 text-xs font-medium tracking-wide text-muted-foreground uppercase">Properties</h3>
      <dl className="divide-y">
        <Prop label="ID" mono>
          <span className="flex min-w-0 items-center gap-1">
            <span className="min-w-0 break-all">{it.id}</span>
            <CopyButton value={it.id} size="icon-xs" label="Copy item ID" className="shrink-0" />
          </span>
        </Prop>
        <Prop label="Collection">
          <Link href={`/collections/${it.collection_id}`} className="hover:underline">
            {collectionName ?? it.collection_id}
          </Link>
        </Prop>
        <Prop label="Status">
          <StatusBadge status={it.status} />
        </Prop>
        <Prop label="Source">{it.source || <span className="text-muted-foreground">—</span>}</Prop>
        <Prop label="External ref" mono>
          {it.external_ref || <span className="font-sans text-muted-foreground">—</span>}
        </Prop>
        {it.url && (
          <Prop label="URL" mono>
            <a
              href={it.url}
              target="_blank"
              rel="noopener noreferrer nofollow"
              className="break-all text-brand hover:underline"
            >
              {it.url}
            </a>
          </Prop>
        )}
        {it.media && (
          <Prop label="File">
            <span className="break-all">{it.media.original_name}</span>
            <span className="block text-xs text-muted-foreground">
              {it.media.mime_type} · {formatBytes(it.media.size_bytes)}
            </span>
          </Prop>
        )}
        <Prop label="Created">{formatDateTime(it.created_at)}</Prop>
        <Prop label="Updated">{formatDateTime(it.updated_at)}</Prop>
        <Prop label="Decided">
          {it.decided_at ? formatDateTime(it.decided_at) : <span className="text-muted-foreground">—</span>}
        </Prop>
        <Prop label="Version" mono>
          {it.version}
        </Prop>
      </dl>
    </section>
  );
}
