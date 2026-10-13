'use client'

import {
  Check,
  ChevronLeft,
  ChevronRight,
  ExternalLink,
  Inbox as InboxIcon,
  MoreHorizontal,
  Pencil,
  Play,
  SlidersHorizontal,
  X,
} from 'lucide-react'
import Link from 'next/link'
import { memo, useRef, useState } from 'react'

import { EmptyState, ErrorState } from '@/components/common/error-state'
import { ExternalContentBadge } from '@/components/common/external-badge'
import { StatusBadge } from '@/components/common/status-badge'
import { TYPE_LABELS, TypeIcon } from '@/components/common/type-icon'
import { ItemPreview, ItemThumb } from '@/components/items/item-preview'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { ITEM_TYPES, type ActionDef, type Collection, type Item, type ItemQuery } from '@/lib/api'
import { formatDateTime, timeAgo } from '@/lib/format'
import { cn } from '@/lib/utils'

export const STATUS_TABS = [
  { value: 'pending', label: 'Pending' },
  { value: 'approved', label: 'Approved' },
  { value: 'rejected', label: 'Rejected' },
  { value: 'expired', label: 'Expired' },
] as const

interface Props {
  items: Item[]
  active?: Item
  activeIndex: number
  setActiveId: (id: string) => void
  move: (delta: number) => void
  status: NonNullable<ItemQuery['status']>
  collectionId: string
  type?: ItemQuery['type']
  setParam: (key: string, value: string | undefined) => void
  clearFilters: () => void
  collections?: Collection[]
  collectionName: (id: string) => string
  countLabel: string
  loading: boolean
  error: unknown
  onRetry: () => void
  hasNextPage: boolean
  fetchingNext: boolean
  fetchNext: () => void
  available: ActionDef[]
  custom: ActionDef[]
  actionsLoading: boolean
  busy: boolean
  approve: () => void
  reject: () => void
  edit: () => void
  runAction: (a: ActionDef) => void
}

/** A row of the "Up next" queue; memoised so moving between items re-renders only the rows that changed. */
const QueueRow = memo(function QueueRow({
  item,
  active,
  collectionName,
  time,
  showStatus,
  onPick,
}: {
  item: Item
  active: boolean
  collectionName: string
  time: string
  showStatus: boolean
  onPick: (id: string) => void
}) {
  return (
    <div className="border-b [contain-intrinsic-size:auto_64px] [content-visibility:auto] last:border-b-0" role="none">
      <button
        type="button"
        role="option"
        aria-selected={active}
        data-item-id={item.id}
        onClick={() => onPick(item.id)}
        className={cn(
          'flex min-h-16 w-full items-center gap-3 px-3 py-2 text-left',
          active ? 'bg-accent' : 'active:bg-muted/60',
        )}
      >
        <ItemThumb item={item} className="size-11" />
        <span className="min-w-0 flex-1">
          <span className="block truncate text-sm font-medium">{item.title || 'Untitled'}</span>
          <span className="flex items-center gap-1.5 truncate text-xs text-muted-foreground">
            <ExternalContentBadge item={item} compact />
            {collectionName} · {time}
          </span>
        </span>
        {showStatus && <StatusBadge status={item.status} />}
      </button>
    </div>
  )
})

/**
 * Phone layout of the inbox: one large card at a time, swipe or tap arrows to
 * move, and a sticky action bar (Reject / Approve / more) above the tab bar.
 */
export function MobileInbox(p: Props) {
  const [filtersOpen, setFiltersOpen] = useState(false)
  const touch = useRef<{ x: number; y: number } | null>(null)
  const top = useRef<HTMLDivElement>(null)
  const { items, active, setActiveId } = p
  const canApprove = p.available.some((a) => a.name === 'approve')
  const canReject = p.available.some((a) => a.name === 'reject')
  const activeFilters = (p.collectionId ? 1 : 0) + (p.type ? 1 : 0)
  const showBar = !!active && (canApprove || canReject || p.custom.length > 0)

  function go(delta: number) {
    p.move(delta)
    top.current?.scrollIntoView({ block: 'start', behavior: 'smooth' })
  }

  function pick(id: string) {
    setActiveId(id)
    top.current?.scrollIntoView({ block: 'start', behavior: 'smooth' })
  }

  return (
    <div className="flex flex-col" data-testid="inbox-mobile">
      <div ref={top} className="scroll-mt-(--mobile-chrome-top)" />
      <div className="grid gap-3 px-4 pt-4 pb-2">
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-baseline gap-2">
            <h1 className="text-2xl font-semibold tracking-tight">Inbox</h1>
            <span className="text-sm text-muted-foreground tabular-nums" data-testid="inbox-count-label">
              {p.countLabel}
            </span>
          </div>
          <Button variant="outline" onClick={() => setFiltersOpen(true)} aria-label="Filters" className="relative">
            <SlidersHorizontal /> Filters
            {activeFilters > 0 && (
              <span className="ml-0.5 flex size-5 items-center justify-center rounded-full bg-brand text-[11px] font-semibold text-brand-foreground">
                {activeFilters}
              </span>
            )}
          </Button>
        </div>
        <ToggleGroup
          type="single"
          variant="outline"
          value={p.status}
          onValueChange={(v) => v && p.setParam('status', v === 'pending' ? undefined : v)}
          aria-label="Status filter"
          className="w-full"
        >
          {STATUS_TABS.map((t) => (
            <ToggleGroupItem key={t.value} value={t.value} className="flex-1">
              {t.label}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </div>

      {p.loading ? (
        <div className="grid gap-3 px-4 pt-2">
          <Skeleton className="h-6 w-2/3" />
          <Skeleton className="aspect-[4/5] w-full rounded-2xl" />
        </div>
      ) : p.error ? (
        <ErrorState error={p.error} onRetry={p.onRetry} className="m-4" />
      ) : !active ? (
        <div className="px-4 py-12">
          <EmptyState
            icon={<InboxIcon />}
            title={p.status === 'pending' ? 'Inbox zero' : `No ${p.status} items`}
            description={
              p.status === 'pending'
                ? 'Nothing is waiting for review. New items from your agents will show up here.'
                : p.status === 'expired'
                  ? "Items that nobody reviewed before an expire rule's deadline show up here."
                  : 'Decisions you make in the inbox will be listed here.'
            }
            action={
              p.status === 'pending' ? (
                <Button asChild variant="outline">
                  <Link href="/integrations">Connect an agent</Link>
                </Button>
              ) : undefined
            }
          />
        </div>
      ) : (
        <>
          <div className="flex items-center justify-between px-4 pt-1 pb-2 text-xs text-muted-foreground">
            <span className="tabular-nums" data-testid="inbox-position">
              {p.activeIndex + 1} of {items.length}
              {p.hasNextPage ? '+' : ''}
            </span>
            <div className="flex items-center gap-1">
              <Button
                variant="ghost"
                size="icon"
                onClick={() => go(-1)}
                disabled={p.activeIndex === 0}
                aria-label="Previous item"
              >
                <ChevronLeft />
              </Button>
              <Button
                variant="ghost"
                size="icon"
                onClick={() => go(1)}
                disabled={p.activeIndex >= items.length - 1}
                aria-label="Next item"
              >
                <ChevronRight />
              </Button>
            </div>
          </div>

          <article
            className="mx-3 overflow-hidden rounded-2xl border bg-card shadow-sm"
            data-testid="inbox-preview"
            onTouchStart={(e) => {
              const t = e.touches[0]

              touch.current = { x: t.clientX, y: t.clientY }
            }}
            onTouchEnd={(e) => {
              const start = touch.current

              touch.current = null

              if (!start) {
                return
              }

              const t = e.changedTouches[0]
              const dx = t.clientX - start.x
              const dy = t.clientY - start.y

              // Horizontal swipe moves between items; vertical scroll is left alone.
              if (Math.abs(dx) > 70 && Math.abs(dx) > Math.abs(dy) * 1.6) {
                go(dx < 0 ? 1 : -1)
              }
            }}
          >
            <ItemPreview item={active} className="rounded-none border-0 border-b" compact scaled />
            <div className="grid gap-3 p-4">
              <div>
                <h2 className="text-lg leading-snug font-semibold tracking-tight break-words">
                  {active.title || 'Untitled'}
                </h2>
                <div className="mt-1.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
                  <StatusBadge status={active.status} />
                  <ExternalContentBadge item={active} />
                  <span className="inline-flex items-center gap-1">
                    <TypeIcon type={active.type} className="size-3.5" /> {TYPE_LABELS[active.type]}
                  </span>
                  <span>·</span>
                  <span className="truncate">{p.collectionName(active.collection_id)}</span>
                  {active.source && (
                    <>
                      <span>·</span>
                      <span>via {active.source}</span>
                    </>
                  )}
                  <span>·</span>
                  <span title={formatDateTime(active.created_at)}>{timeAgo(active.created_at)}</span>
                </div>
              </div>
              {active.feedback && (
                <div className="rounded-lg border-l-2 border-brand/70 bg-muted/50 px-3 py-2 text-sm">
                  <div className="mb-0.5 text-xs font-medium text-muted-foreground">Feedback</div>
                  <div className="whitespace-pre-wrap">{active.feedback}</div>
                </div>
              )}
              {Object.keys(active.metadata ?? {}).length > 0 && (
                <details className="group rounded-lg border text-sm">
                  <summary className="flex min-h-11 cursor-pointer items-center px-3 text-xs font-medium text-muted-foreground select-none">
                    Metadata
                    <ChevronRight className="ml-auto size-4 transition-transform group-open:rotate-90" />
                  </summary>
                  <dl className="grid gap-y-1.5 border-t px-3 py-2.5">
                    {Object.entries(active.metadata)
                      .slice(0, 20)
                      .map(([k, v]) => (
                        <div key={k} className="grid gap-0.5">
                          <dt className="font-mono text-[11px] text-muted-foreground">{k}</dt>
                          <dd className="font-mono text-xs break-words">
                            {typeof v === 'string' ? v : JSON.stringify(v)}
                          </dd>
                        </div>
                      ))}
                  </dl>
                </details>
              )}
              <Button asChild variant="outline" className="w-full">
                <Link href={`/items/${active.id}`}>
                  <ExternalLink /> Open details
                </Link>
              </Button>
            </div>
          </article>

          {items.length > 1 && (
            <section className="mt-6 px-3" aria-label="Queue">
              <h3 className="px-1 pb-2 text-xs font-medium tracking-wide text-muted-foreground uppercase">
                {p.status === 'pending' ? 'Up next' : 'More'}
              </h3>
              <div className="overflow-hidden rounded-2xl border bg-card" role="listbox" aria-label="Items">
                {items.map((it, i) => (
                  <QueueRow
                    key={it.id}
                    item={it}
                    active={i === p.activeIndex}
                    collectionName={p.collectionName(it.collection_id)}
                    time={timeAgo(p.status === 'pending' ? it.created_at : (it.decided_at ?? it.updated_at))}
                    showStatus={p.status !== 'pending'}
                    onPick={pick}
                  />
                ))}
              </div>
              {p.hasNextPage && (
                <Button variant="outline" className="mt-3 w-full" onClick={p.fetchNext} disabled={p.fetchingNext}>
                  {p.fetchingNext ? 'Loading…' : 'Load more'}
                </Button>
              )}
            </section>
          )}
          {/* Room for the sticky action bar. */}
          <div className={showBar ? 'h-24' : 'h-6'} />
        </>
      )}

      {showBar && active && (
        <div
          className="fixed inset-x-0 bottom-(--mobile-chrome-bottom) z-30 border-t bg-background/90 px-3 py-2.5 backdrop-blur-xl supports-[backdrop-filter]:bg-background/75"
          data-testid="inbox-actions"
        >
          <div className="mx-auto flex max-w-lg items-center gap-2">
            {canReject && (
              <Button
                variant="destructive"
                className="h-12 flex-1 rounded-xl text-[15px] pointer-coarse:h-12"
                onClick={p.reject}
                disabled={p.busy}
              >
                <X className="size-5" /> Reject
              </Button>
            )}
            {canApprove && (
              <Button
                className="h-12 flex-[1.4] rounded-xl bg-teal-700 text-[15px] text-white hover:bg-teal-700/90 pointer-coarse:h-12 dark:bg-teal-400 dark:text-teal-950 dark:hover:bg-teal-400/90"
                onClick={p.approve}
                disabled={p.busy}
              >
                <Check className="size-5" /> Approve
              </Button>
            )}
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  variant="outline"
                  className="size-12 shrink-0 rounded-xl pointer-coarse:size-12"
                  aria-label="More actions"
                  disabled={p.busy}
                >
                  <MoreHorizontal className="size-5" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" side="top" className="min-w-56">
                {canApprove && (
                  <DropdownMenuItem onSelect={p.edit}>
                    <Pencil /> Edit & approve
                  </DropdownMenuItem>
                )}
                {p.custom.length > 0 && (
                  <>
                    <DropdownMenuSeparator />
                    <DropdownMenuLabel>Collection actions</DropdownMenuLabel>
                    {p.custom.map((a) => (
                      <DropdownMenuItem key={a.name} onSelect={() => p.runAction(a)}>
                        <Play /> <span className="flex-1">{a.label}</span>
                      </DropdownMenuItem>
                    ))}
                  </>
                )}
                <DropdownMenuSeparator />
                <DropdownMenuItem asChild>
                  <Link href={`/items/${active.id}`}>
                    <ExternalLink /> Open details
                  </Link>
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>
      )}

      <Sheet open={filtersOpen} onOpenChange={setFiltersOpen}>
        <SheetContent side="bottom" className="rounded-t-2xl pb-[calc(1.25rem+env(safe-area-inset-bottom))]">
          <SheetHeader className="pb-0">
            <SheetTitle>Filter inbox</SheetTitle>
            <SheetDescription>Narrow the review queue by collection or type.</SheetDescription>
          </SheetHeader>
          <div className="grid gap-4 px-4">
            <div className="grid gap-2">
              <Label htmlFor="m-filter-collection">Collection</Label>
              <Select
                value={p.collectionId || 'all'}
                onValueChange={(v) => p.setParam('collection', v === 'all' ? undefined : v)}
              >
                <SelectTrigger id="m-filter-collection" className="w-full" aria-label="Collection filter">
                  <SelectValue placeholder="All collections" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All collections</SelectItem>
                  {(p.collections ?? []).map((c) => (
                    <SelectItem key={c.id} value={c.id}>
                      {c.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="grid gap-2">
              <Label htmlFor="m-filter-type">Type</Label>
              <Select value={p.type ?? 'all'} onValueChange={(v) => p.setParam('type', v === 'all' ? undefined : v)}>
                <SelectTrigger id="m-filter-type" className="w-full" aria-label="Type filter">
                  <SelectValue placeholder="All types" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All types</SelectItem>
                  {ITEM_TYPES.map((t) => (
                    <SelectItem key={t} value={t}>
                      {TYPE_LABELS[t]}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="flex gap-2 pt-1">
              {activeFilters > 0 && (
                <Button variant="outline" className="flex-1" onClick={p.clearFilters}>
                  Clear filters
                </Button>
              )}
              <Button className="flex-1" onClick={() => setFiltersOpen(false)}>
                Show results
              </Button>
            </div>
          </div>
        </SheetContent>
      </Sheet>
    </div>
  )
}
