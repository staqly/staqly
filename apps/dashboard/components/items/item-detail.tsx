'use client'

import { useQuery } from '@tanstack/react-query'
import { ArrowLeft, ExternalLink, History, Layers, MoreHorizontal } from 'lucide-react'
import Link from 'next/link'
import { useRouter } from 'next/navigation'
import { useState } from 'react'

import { ErrorState, EmptyState } from '@/components/common/error-state'
import { ExternalContentBadge } from '@/components/common/external-badge'
import { IdChip } from '@/components/common/id-chip'
import { DecisionState, ItemState, RunState, StatusBadge } from '@/components/common/status-badge'
import { TYPE_LABELS, TypeIcon } from '@/components/common/type-icon'
import { Button } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useIsMobile } from '@/hooks/use-mobile'
import { api, ApiError, type Item, type ItemType } from '@/lib/api'
import { formatBytes, formatDateTime, prettyJSON, timeAgo } from '@/lib/format'
import { qk, useCollection, useItem } from '@/lib/queries'
import { safeHttpUrl } from '@/lib/safe'
import { cn } from '@/lib/utils'

import { EventTimeline } from './event-timeline'
import { FailedNote } from './failed-note'
import { FeedbackNote } from './feedback-note'
import { ItemActionBar } from './item-actions'
import { ItemDeliveries } from './item-deliveries'
import { ItemEditForm } from './item-edit-form'
import { useItemOptions } from './item-options'
import { ItemPreview } from './item-preview'
import { ItemSummary } from './item-summary'
import { TagList, TagsPopover } from './item-tags'
import { MetadataCard } from './metadata-card'
import { ProcessingNote } from './processing-note'

function Prop({ label, children, mono }: { label: string; children: React.ReactNode; mono?: boolean }) {
  return (
    <div className="grid grid-cols-[96px_1fr] items-baseline gap-2 py-1.5 text-sm">
      <dt className="truncate text-xs text-muted-foreground" title={label}>
        {label}
      </dt>
      <dd className={cn('min-w-0 break-words', mono && 'font-mono text-xs')}>{children}</dd>
    </div>
  )
}

function ItemHistory({ id, version }: { id: string; version: number }) {
  const events = useQuery({ queryKey: [...qk.itemEvents(id), version], queryFn: () => api.itemEvents(id) })

  if (events.isLoading) {
    return <Skeleton className="h-40" />
  }

  if (events.error) {
    return <ErrorState error={events.error} onRetry={() => events.refetch()} />
  }

  const list = events.data?.data ?? []

  if (!list.length) {
    return <EmptyState icon={<History />} title="No history yet" />
  }

  return <EventTimeline events={list} />
}

function ItemVersions({ id, version }: { id: string; version: number }) {
  const versions = useQuery({
    queryKey: [...qk.itemVersions(id), version],
    queryFn: async () => (await api.itemVersions(id)).data ?? [],
  })
  const [open, setOpen] = useState<number | null>(null)

  if (versions.isLoading) {
    return <Skeleton className="h-40" />
  }

  if (versions.error) {
    return <ErrorState error={versions.error} onRetry={() => versions.refetch()} />
  }

  const list = (versions.data ?? []).toSorted((a, b) => (b.version ?? 0) - (a.version ?? 0))

  if (!list.length) {
    return (
      <EmptyState
        icon={<Layers />}
        title="No previous versions"
        description="A snapshot is stored before every change to this item."
      />
    )
  }

  return (
    <ul className="divide-y rounded-lg border" data-testid="item-versions">
      {list.map((v) => {
        const snap = v.snapshot
        const isOpen = open === v.version

        return (
          <li key={v.version} className="text-sm">
            <button
              type="button"
              className="flex w-full items-center gap-3 px-3 py-2.5 text-left hover:bg-muted/50"
              onClick={() => setOpen(isOpen ? null : (v.version ?? null))}
              aria-expanded={isOpen}
            >
              <span className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">v{v.version}</span>
              <span className="min-w-0 flex-1 truncate">{snap?.title || 'Untitled'}</span>
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
                    content: snap.type === 'json' && snap.content ? JSON.parse(snap.content) : snap.content,
                    url: snap.url || undefined,
                    metadata: snap.metadata,
                    feedback: snap.feedback || undefined,
                    media: snap.media?.original_name,
                  })}
                </pre>
              </div>
            )}
          </li>
        )
      })}
    </ul>
  )
}

// Mirrors the API (items.go lockedStatuses), plus processing: an automation is working on it.
const CONTENT_LOCKED = new Set(['approved', 'processing', 'completed', 'archived'])

export function ItemDetail({
  id,
  variant = 'page',
  onDeleted,
}: {
  id: string
  variant?: 'page' | 'sheet'
  onDeleted?: () => void
}) {
  const item = useItem(id)
  const router = useRouter()
  const collection = useCollection(item.data?.collection_id ?? '')
  const [tab, setTab] = useState('details')
  const isMobile = useIsMobile()

  const options = useItemOptions(item.data, () => {
    if (onDeleted) {
      onDeleted()
    } else {
      router.push(item.data ? `/collections/${item.data.collection_id}` : '/collections')
    }
  })

  if (item.isLoading) {
    return (
      <div className="grid gap-4">
        <Skeleton className="h-8 w-1/2" />
        <Skeleton className="h-[360px]" />
      </div>
    )
  }

  if (item.error) {
    const notFound = item.error instanceof ApiError && item.error.status === 404

    return (
      <ErrorState
        error={item.error}
        title={notFound ? 'Item not found' : 'Could not load item'}
        onRetry={notFound ? undefined : () => item.refetch()}
      />
    )
  }

  const it = item.data!
  const sheet = variant === 'sheet'

  return (
    <div className="grid gap-5" data-testid="item-detail">
      <div className="flex flex-col gap-3">
        {!sheet && (
          <Link
            href={`/collections/${it.collection_id}`}
            className="inline-flex w-fit items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
          >
            <ArrowLeft className="size-3" /> {collection.data?.name ?? 'Collection'}
          </Link>
        )}
        <div className="flex items-start gap-3">
          <div className="min-w-0 flex-1">
            <h1 className={cn('font-semibold tracking-tight break-words', sheet ? 'pr-8 text-lg' : 'text-xl')}>
              {it.title || 'Untitled'}
            </h1>
            <div className="mt-1.5 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
              <ItemState item={it} />
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
            <TagList tags={it.tags} className="mt-1.5" />
          </div>
          <div className={cn('flex flex-wrap items-center justify-end gap-1.5', sheet && 'mr-8')}>
            {/* Actions top right, next to the title. Phones get the sticky bar at the bottom instead. */}
            {!isMobile && <ItemActionBar item={it} size="sm" menuExtra={options.menu} />}
            {sheet && (
              <Button asChild variant="ghost" size="sm">
                <Link href={`/items/${it.id}`}>
                  <ExternalLink /> Open
                </Link>
              </Button>
            )}
            {/* Phones have no action bar up here, so they keep a menu of their own. */}
            {isMobile && (
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="ghost" size="icon-sm" aria-label="More item options">
                    <MoreHorizontal />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">{options.menu}</DropdownMenuContent>
              </DropdownMenu>
            )}
          </div>
        </div>
        {it.status === 'processing' && <ProcessingNote item={it} />}
        {it.status === 'failed' && <FailedNote item={it} />}
      </div>

      {/* grid-cols-1 is minmax(0, 1fr): long unbroken text (a webhook error, a URL) wraps instead of widening the page. */}
      <div className="grid grid-cols-1 gap-6">
        <div className="grid min-w-0 grid-cols-1 content-start gap-5">
          <ItemSummary item={it} />
          <ItemPreview item={it} compact={sheet} />
          {/* Edit disappears once the content locks (a decision made here): fall back to Details. */}
          <Tabs
            value={tab === 'edit' && CONTENT_LOCKED.has(it.status) ? 'details' : tab}
            onValueChange={setTab}
            className="min-w-0"
          >
            <TabsList className="max-sm:w-full">
              <TabsTrigger value="details">Details</TabsTrigger>
              {/* Nothing to edit once the content is locked (after a decision, or while an automation works). */}
              {!CONTENT_LOCKED.has(it.status) && <TabsTrigger value="edit">Edit</TabsTrigger>}
              <TabsTrigger value="history">History</TabsTrigger>
              <TabsTrigger value="versions">Versions</TabsTrigger>
            </TabsList>
            <TabsContent value="details" className="min-w-0 pt-3">
              <div className="grid grid-cols-1 gap-4">
                <div className="rounded-xl border bg-card p-4">
                  <ItemProps item={it} collectionName={collection.data?.name} />
                </div>
                <MetadataCard metadata={it.metadata} />
                <FeedbackNote item={it} />
                {it.status !== 'pending' && it.status !== 'draft' && <ItemDeliveries item={it} />}
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
      </div>

      {isMobile && (
        <>
          {!sheet && <div className="h-16" aria-hidden />}
          <div
            className={cn(
              'z-30 border-t bg-background/90 px-3 py-2.5 backdrop-blur-xl supports-[backdrop-filter]:bg-background/75 empty:hidden',
              sheet
                ? 'sticky -bottom-[max(1.25rem,env(safe-area-inset-bottom))] -mx-4 pb-[max(0.625rem,env(safe-area-inset-bottom))]'
                : 'fixed inset-x-0 bottom-(--mobile-chrome-bottom)',
            )}
            data-testid="item-sticky-actions"
          >
            <ItemActionBar item={it} compact className="mx-auto max-w-lg" />
          </div>
        </>
      )}

      {options.dialogs}
    </div>
  )
}

function ItemProps({ item: it, collectionName }: { item: Item; collectionName?: string }) {
  return (
    <section>
      <h3 className="mb-1 text-xs font-medium tracking-wide text-muted-foreground uppercase">Properties</h3>
      <dl className="divide-y">
        <Prop label="ID" mono>
          <IdChip id={it.id} label="item" />
        </Prop>
        <Prop label="Collection">
          <Link href={`/collections/${it.collection_id}`} className="hover:underline">
            {collectionName ?? it.collection_id}
          </Link>
        </Prop>
        <Prop label="Tags">
          <span className="flex flex-wrap items-center gap-1.5">
            {it.tags?.length ? <TagList tags={it.tags} /> : <span className="text-muted-foreground">—</span>}
            {/* Labels, not content: always edited here (saved at once, no new version), whatever the status. */}
            <TagsPopover item={it} />
          </span>
        </Prop>
        <Prop label="Decision">
          <DecisionState item={it} />
        </Prop>
        <Prop label="Run">
          <RunState item={it} />
        </Prop>
        {it.archived_at && <Prop label="Archived">{formatDateTime(it.archived_at)}</Prop>}
        <Prop label="Source">{it.source || <span className="text-muted-foreground">—</span>}</Prop>
        <Prop label="External ref" mono>
          {it.external_ref || <span className="font-sans text-muted-foreground">—</span>}
        </Prop>
        {it.url && (
          <Prop label="URL" mono>
            {safeHttpUrl(it.url) ? (
              <a
                href={safeHttpUrl(it.url)!}
                target="_blank"
                rel="noopener noreferrer nofollow"
                title={it.url}
                className="block truncate text-brand hover:underline"
              >
                {it.url}
              </a>
            ) : (
              <span className="block truncate" title={it.url}>
                {it.url}
              </span>
            )}
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
  )
}
