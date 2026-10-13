'use client'

import { ArrowDown, ArrowUp, ArrowUpDown, Check, X } from 'lucide-react'
import { memo, useMemo, useState, useSyncExternalStore } from 'react'

import { ExternalContentBadge } from '@/components/common/external-badge'
import { StatusBadge } from '@/components/common/status-badge'
import { TYPE_LABELS, TypeIcon } from '@/components/common/type-icon'
import { FeedbackDialog, REJECT_ACTION, useRunAction } from '@/components/items/item-actions'
import {
  ExternalMediaGate,
  ItemExcerpt,
  ItemThumb,
  isExternalMedia,
  itemMediaSrc,
  itemThumbSrc,
} from '@/components/items/item-preview'
import { Button } from '@/components/ui/button'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { useIsMobile } from '@/hooks/use-mobile'
import type { Item } from '@/lib/api'
import { formatDateTime, timeAgo } from '@/lib/format'
import { cn } from '@/lib/utils'

interface ViewProps {
  items: Item[]
  /** Keep this stable (same function between renders) so memoised cards and rows can skip rendering. */
  onOpen: (item: Item) => void
}

interface CardProps {
  item: Item
  onOpen: (item: Item) => void
}

function metaPreview(item: Item, n = 2): [string, string][] {
  return Object.entries(item.metadata ?? {})
    .filter(([, v]) => v !== null && (typeof v !== 'object' || Array.isArray(v)))
    .slice(0, n)
    .map(([k, v]) => [k, typeof v === 'string' ? v : JSON.stringify(v)])
}

/** Approve / reject buttons for pending items, used on gallery cards. */
function QuickReview({ item }: { item: Item }) {
  const run = useRunAction()
  // null until first opened: mounting a closed dialog in each of thousands of cards is not free.
  const [rejecting, setRejecting] = useState<boolean | null>(null)

  if (item.status !== 'pending') {
    return null
  }

  return (
    // Event boundary: clicks on these buttons — and inside the portalled reject dialog, whose events bubble
    // through the React tree — must not also open the card. The buttons themselves are keyboard accessible.
    // oxlint-disable-next-line jsx-a11y/click-events-have-key-events, jsx-a11y/no-static-element-interactions
    <div className="flex gap-1" onClick={(e) => e.stopPropagation()}>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            size="icon-sm"
            className="bg-teal-700 text-white shadow-sm hover:bg-teal-700/90 dark:bg-teal-400 dark:text-teal-950 dark:hover:bg-teal-400/90"
            aria-label={`Approve ${item.title}`}
            disabled={run.isPending}
            onClick={() => run.mutate({ item, action: 'approve', body: { expected_version: item.version } })}
          >
            <Check />
          </Button>
        </TooltipTrigger>
        <TooltipContent>Approve</TooltipContent>
      </Tooltip>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            size="icon-sm"
            variant="secondary"
            className="text-rose-600 shadow-sm"
            aria-label={`Reject ${item.title}`}
            disabled={run.isPending}
            onClick={() => setRejecting(true)}
          >
            <X />
          </Button>
        </TooltipTrigger>
        <TooltipContent>Reject with feedback</TooltipContent>
      </Tooltip>
      {rejecting !== null && (
        <FeedbackDialog
          open={rejecting}
          onOpenChange={setRejecting}
          action={REJECT_ACTION}
          itemTitle={item.title}
          pending={run.isPending}
          onSubmit={(feedback) =>
            run.mutate(
              { item, action: 'reject', body: { feedback, expected_version: item.version } },
              { onSettled: () => setRejecting(false) },
            )
          }
        />
      )}
    </div>
  )
}

// Card width per column count (see COLUMN_QUERIES): lets the browser pick the thumbnail from srcSet.
const GALLERY_SIZES = '(min-width: 1536px) 20vw, (min-width: 1024px) 25vw, (min-width: 640px) 34vw, 50vw'

/** Gallery media at its natural aspect ratio (masonry-ish columns). */
function GalleryMedia({ item }: { item: Item }) {
  const src = itemMediaSrc(item)
  const [broken, setBroken] = useState(false)
  const [loadExternal, setLoadExternal] = useState(false)

  if (!src || broken) {
    return <ItemThumb item={item} className="aspect-[4/3] w-full rounded-none border-0" />
  }

  if (item.type === 'video' || item.media?.mime_type?.startsWith('video/')) {
    return <ItemThumb item={item} className="aspect-video w-full rounded-none border-0" />
  }

  // External images (item.url) would tell their host who is looking: click to load, like the preview.
  if (isExternalMedia(item) && !loadExternal) {
    return <ExternalMediaGate src={src} kind="image" onLoad={() => setLoadExternal(true)} />
  }

  const thumb = itemThumbSrc(item, 640)

  return (
    // oxlint-disable-next-line nextjs/no-img-element -- authenticated media served via the API proxy
    <img
      src={thumb ?? src}
      srcSet={thumb ? `${itemThumbSrc(item, 320)} 320w, ${thumb} 640w, ${itemThumbSrc(item, 1280)} 1280w` : undefined}
      sizes={thumb ? GALLERY_SIZES : undefined}
      alt=""
      loading="lazy"
      decoding="async"
      onError={() => setBroken(true)}
      className="block max-h-[520px] min-h-28 w-full bg-muted object-cover"
    />
  )
}

const COLUMN_QUERIES: [string, number][] = [
  ['(min-width: 1536px)', 5],
  ['(min-width: 1024px)', 4],
  ['(min-width: 640px)', 3],
]

function subscribeColumns(cb: () => void) {
  const mqls = COLUMN_QUERIES.map(([q]) => window.matchMedia(q))

  mqls.forEach((m) => m.addEventListener('change', cb))

  return () => mqls.forEach((m) => m.removeEventListener('change', cb))
}

/** Number of gallery columns for the current viewport (2 on phones … 5 on wide screens). */
function useGalleryColumns() {
  return useSyncExternalStore(
    subscribeColumns,
    () => COLUMN_QUERIES.find(([q]) => window.matchMedia(q).matches)?.[1] ?? 2,
    () => 4,
  )
}

/** One gallery card. Memoised: loading another page or opening an item must not re-render every card. */
const GalleryCard = memo(function GalleryCard({ item: it, onOpen }: CardProps) {
  const visual = !!itemMediaSrc(it) && (it.type === 'image' || it.type === 'video')

  return (
    <div
      role="button"
      tabIndex={0}
      data-testid="gallery-card"
      onClick={() => onOpen(it)}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault()
          onOpen(it)
        }
      }}
      className="group relative flex cursor-pointer flex-col overflow-hidden rounded-xl border bg-card text-left shadow-xs transition-all [contain-intrinsic-size:auto_280px] [content-visibility:auto] outline-none hover:border-foreground/20 hover:shadow-md focus-visible:ring-3 focus-visible:ring-ring/50"
    >
      <div className="relative overflow-hidden border-b bg-muted/40">
        {visual ? (
          <GalleryMedia item={it} />
        ) : (
          <div className="flex min-h-28 flex-col gap-2 p-3 pt-10">
            <TypeIcon type={it.type} className="size-4 text-muted-foreground" />
            <ItemExcerpt item={it} className="line-clamp-[9] overflow-hidden" length={360} />
          </div>
        )}
        <div className="absolute top-2 left-2 flex flex-wrap gap-1">
          <StatusBadge status={it.status} className="bg-background/90 backdrop-blur" />
          <ExternalContentBadge item={it} compact className="bg-background/90 backdrop-blur" />
        </div>
        <div className="absolute right-2 bottom-2 opacity-0 transition-opacity group-focus-within:opacity-100 group-hover:opacity-100 pointer-coarse:opacity-100">
          <QuickReview item={it} />
        </div>
      </div>
      <div className="grid gap-1 p-2.5">
        <div className="truncate text-sm font-medium" title={it.title}>
          {it.title || 'Untitled'}
        </div>
        <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
          <TypeIcon type={it.type} className="size-3" />
          <span>{TYPE_LABELS[it.type]}</span>
          <span>·</span>
          <span className="truncate">{timeAgo(it.created_at)}</span>
        </div>
        {metaPreview(it).map(([k, v]) => (
          <div key={k} className="flex gap-1 truncate font-mono text-[10.5px] text-muted-foreground max-sm:hidden">
            <span className="opacity-70">{k}:</span>
            <span className="truncate text-foreground/80">{v}</span>
          </div>
        ))}
      </div>
    </div>
  )
})

export function GalleryView({ items, onOpen }: ViewProps) {
  const n = useGalleryColumns()
  // Masonry-ish: deal items round-robin into columns so reading order stays left → right.
  const columns = Array.from({ length: n }, (_, c) => items.filter((_item, i) => i % n === c))

  return (
    <div className="flex items-start gap-3" data-testid="gallery-view">
      {columns.map((col, ci) => (
        // oxlint-disable-next-line react/no-array-index-key -- a column is identified by its position
        <div key={ci} className="flex min-w-0 flex-1 flex-col gap-3">
          {col.map((it) => (
            <GalleryCard key={it.id} item={it} onOpen={onOpen} />
          ))}
        </div>
      ))}
    </div>
  )
}

type SortKey = 'title' | 'type' | 'status' | 'source' | 'created_at' | 'updated_at'

/** Phone layout of a table row. */
const TableCard = memo(function TableCard({ item: it, onOpen }: CardProps) {
  return (
    <li>
      <button
        type="button"
        onClick={() => onOpen(it)}
        className="flex w-full items-center gap-3 rounded-xl border bg-card p-3 text-left shadow-xs active:bg-muted/60"
      >
        <ItemThumb item={it} className="size-12" />
        <span className="grid min-w-0 flex-1 gap-1">
          <span className="truncate text-sm font-medium">{it.title || 'Untitled'}</span>
          <span className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
            <StatusBadge status={it.status} />
            <ExternalContentBadge item={it} />
            <span className="inline-flex items-center gap-1">
              <TypeIcon type={it.type} className="size-3" /> {TYPE_LABELS[it.type]}
            </span>
            {it.source && <span className="truncate">via {it.source}</span>}
            <span title={formatDateTime(it.created_at)}>{timeAgo(it.created_at)}</span>
          </span>
        </span>
      </button>
    </li>
  )
})

const TableItemRow = memo(function TableItemRow({ item: it, onOpen }: CardProps) {
  return (
    <TableRow className="cursor-pointer" onClick={() => onOpen(it)}>
      <TableCell className="py-1.5">
        <ItemThumb item={it} className="size-8" />
      </TableCell>
      <TableCell className="max-w-0 font-medium">
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate">{it.title || 'Untitled'}</span>
          <ExternalContentBadge item={it} compact />
        </div>
      </TableCell>
      <TableCell>
        <span className="inline-flex items-center gap-1.5 text-muted-foreground">
          <TypeIcon type={it.type} className="size-3.5" /> {TYPE_LABELS[it.type]}
        </span>
      </TableCell>
      <TableCell>
        <StatusBadge status={it.status} />
      </TableCell>
      <TableCell className="hidden truncate text-muted-foreground md:table-cell">{it.source || '—'}</TableCell>
      <TableCell className="hidden text-muted-foreground sm:table-cell" title={formatDateTime(it.created_at)}>
        {timeAgo(it.created_at)}
      </TableCell>
      <TableCell className="hidden text-muted-foreground lg:table-cell" title={formatDateTime(it.updated_at)}>
        {timeAgo(it.updated_at)}
      </TableCell>
    </TableRow>
  )
})

export function TableView({ items, onOpen }: ViewProps) {
  const isMobile = useIsMobile()
  const [sort, setSort] = useState<{ key: SortKey; dir: 1 | -1 } | null>(null)
  const rows = useMemo(() => {
    if (!sort) {
      return items
    }

    return items.toSorted((a, b) => {
      const av = String(a[sort.key] ?? '').toLowerCase()
      const bv = String(b[sort.key] ?? '').toLowerCase()

      return av < bv ? -sort.dir : av > bv ? sort.dir : 0
    })
  }, [items, sort])

  function header(key: SortKey, label: string, className?: string) {
    const active = sort?.key === key
    const Icon = !active ? ArrowUpDown : sort!.dir === 1 ? ArrowUp : ArrowDown

    return (
      <TableHead className={className} aria-sort={active ? (sort!.dir === 1 ? 'ascending' : 'descending') : 'none'}>
        <button
          type="button"
          className="inline-flex items-center gap-1 hover:text-foreground"
          onClick={() => setSort((s) => (s?.key !== key ? { key, dir: 1 } : s.dir === 1 ? { key, dir: -1 } : null))}
        >
          {label}
          <Icon className={cn('size-3', !active && 'opacity-40')} />
        </button>
      </TableHead>
    )
  }

  if (isMobile) {
    return (
      <ul className="grid gap-2" data-testid="table-view">
        {rows.map((it) => (
          <TableCard key={it.id} item={it} onOpen={onOpen} />
        ))}
      </ul>
    )
  }

  return (
    <div className="scrollbar-thin overflow-x-auto rounded-xl border bg-card" data-testid="table-view">
      <Table>
        <TableHeader>
          <TableRow className="hover:bg-transparent">
            <TableHead className="w-12" />
            {header('title', 'Title')}
            {header('type', 'Type', 'w-24')}
            {header('status', 'Status', 'w-28')}
            {header('source', 'Source', 'hidden w-32 md:table-cell')}
            {header('created_at', 'Created', 'hidden w-36 sm:table-cell')}
            {header('updated_at', 'Updated', 'hidden w-36 lg:table-cell')}
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((it) => (
            <TableItemRow key={it.id} item={it} onOpen={onOpen} />
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

const ListRow = memo(function ListRow({ item: it, onOpen }: CardProps) {
  return (
    <li className="[contain-intrinsic-size:auto_48px] [content-visibility:auto]">
      <button
        type="button"
        onClick={() => onOpen(it)}
        className="flex min-h-12 w-full items-center gap-3 px-3 py-2 text-left hover:bg-muted/50 pointer-coarse:min-h-14"
      >
        <ItemThumb item={it} className="size-7 pointer-coarse:size-9" />
        <span className="min-w-0 flex-1 truncate text-sm">{it.title || 'Untitled'}</span>
        <ExternalContentBadge item={it} compact />
        {it.feedback && (
          <span className="hidden max-w-60 truncate text-xs text-muted-foreground italic lg:inline">
            “{it.feedback}”
          </span>
        )}
        <span className="hidden text-xs text-muted-foreground sm:inline">{it.source}</span>
        <StatusBadge status={it.status} />
        <span
          className="hidden w-24 shrink-0 text-right text-xs text-muted-foreground sm:inline"
          title={formatDateTime(it.created_at)}
        >
          {timeAgo(it.created_at)}
        </span>
      </button>
    </li>
  )
})

export function ListView({ items, onOpen }: ViewProps) {
  return (
    <ul className="divide-y overflow-hidden rounded-xl border bg-card" data-testid="list-view">
      {items.map((it) => (
        <ListRow key={it.id} item={it} onOpen={onOpen} />
      ))}
    </ul>
  )
}
