'use client'

import {
  Check,
  ChevronLeft,
  ChevronRight,
  ExternalLink,
  Inbox as InboxIcon,
  MoreHorizontal,
  Pencil,
  SlidersHorizontal,
  X,
} from 'lucide-react'
import Link from 'next/link'
import { memo, useRef, useState } from 'react'

import { EmptyState, ErrorState } from '@/components/common/error-state'
import { ExternalContentBadge } from '@/components/common/external-badge'
import { ItemState } from '@/components/common/status-badge'
import { TYPE_LABELS, TypeIcon } from '@/components/common/type-icon'
import { FeedbackNote } from '@/components/items/feedback-note'
import { ActionMenuItem } from '@/components/items/item-actions'
import { ItemPreview, ItemThumb } from '@/components/items/item-preview'
import { MetadataCard } from '@/components/items/metadata-card'
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
import { statusMeta } from '@/lib/status'
import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

import { STEPS, type Step } from './steps'

/** Inbox steps; the selected one is tinted with its status tone (lib/tones.ts). */
/** The inbox steps as phone tabs, in the order items move. */
const STEP_TABS = (['review', 'approved', 'processing', 'done', 'failed', 'rejected', 'archived'] as const).map(
  (id) => ({
    value: id as string,
    label: STEPS[id].label,
    dot: statusMeta(STEPS[id].status).dot,
    on: TONES[statusMeta(STEPS[id].status).tone].on,
  }),
)

interface Props {
  items: Item[]
  active?: Item
  activeIndex: number
  setActiveId: (id: string) => void
  move: (delta: number) => void
  /** 'pending' is the review queue; anything else lists items. */
  status: string
  step: Step
  collectionId: string
  type?: ItemQuery['type']
  setParam: (key: string, value: string | undefined) => void
  clearFilters: () => void
  /** Tags or source narrow the list (set from a link; the phone sheet clears them with the rest). */
  narrowedBy: number
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
        {showStatus && <ItemState item={item} />}
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
  const activeFilters = (p.collectionId ? 1 : 0) + (p.type ? 1 : 0) + p.narrowedBy
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
          value={p.step.id}
          onValueChange={(v) => v && p.setParam('step', v === 'review' ? undefined : v)}
          aria-label="Steps"
          className="w-full overflow-x-auto"
        >
          {STEP_TABS.map((t) => (
            <ToggleGroupItem key={t.value} value={t.value} className={cn('shrink-0 gap-1.5', t.on)}>
              <span aria-hidden className={cn('size-1.5 rounded-full', t.dot)} />
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
            title={activeFilters ? 'No items match these filters' : p.step.empty.title}
            description={activeFilters ? 'Try another step, or clear the filters.' : p.step.empty.description}
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
                  <ItemState item={active} />
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
              <MetadataCard metadata={active.metadata} />
              <FeedbackNote item={active} />
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
                  {p.fetchingNext ? 'Loading...' : 'Load more'}
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
                variant="reject"
                className="h-12 flex-1 rounded-xl text-[15px] pointer-coarse:h-12"
                onClick={p.reject}
                disabled={p.busy}
              >
                <X className="size-5" /> Reject
              </Button>
            )}
            {canApprove && (
              <Button
                variant="approve"
                className="h-12 flex-[1.4] rounded-xl text-[15px] pointer-coarse:h-12"
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
                    <Pencil /> Edit
                  </DropdownMenuItem>
                )}
                {p.custom.length > 0 && (
                  <>
                    <DropdownMenuSeparator />
                    <DropdownMenuLabel>Collection actions</DropdownMenuLabel>
                    {p.custom.map((a) => (
                      <ActionMenuItem key={a.name} action={a} onRun={p.runAction} />
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
