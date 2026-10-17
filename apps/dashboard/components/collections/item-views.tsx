'use client'

import { ArrowDown, ArrowUp, ArrowUpDown } from 'lucide-react'
import { memo, useMemo, useState } from 'react'

import { ExternalContentBadge } from '@/components/common/external-badge'
import { ArchivedBadge, DecisionState, ItemState, RunState } from '@/components/common/status-badge'
import { TYPE_LABELS, TypeIcon } from '@/components/common/type-icon'
import { ItemThumb } from '@/components/items/item-preview'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { useIsMobile } from '@/hooks/use-mobile'
import type { Item } from '@/lib/api'
import { formatDateTime, timeAgo } from '@/lib/format'
import { cn } from '@/lib/utils'

interface ViewProps {
  items: Item[]
  /** Keep this stable (same function between renders) so memoised cards and rows can skip rendering. */
  onOpen: (item: Item) => void
  /** Shown inside the table (under its header) when there are no items. */
  empty?: React.ReactNode
}

interface CardProps {
  item: Item
  onOpen: (item: Item) => void
}

type SortKey = 'title' | 'type' | 'decision' | 'run' | 'source' | 'created_at' | 'updated_at'

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
            <ItemState item={it} />
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
          {/* The keyboard path into the item; a mouse can click anywhere on the row. */}
          <button
            type="button"
            className="truncate rounded-sm text-left outline-none focus-visible:ring-2 focus-visible:ring-ring"
            onClick={(e) => {
              e.stopPropagation()
              onOpen(it)
            }}
          >
            {it.title || 'Untitled'}
          </button>
          <ExternalContentBadge item={it} compact />
          {it.archived_at && <ArchivedBadge />}
        </div>
      </TableCell>
      <TableCell>
        <span className="inline-flex items-center gap-1.5 text-muted-foreground">
          <TypeIcon type={it.type} className="size-3.5" /> {TYPE_LABELS[it.type]}
        </span>
      </TableCell>
      <TableCell>
        <DecisionState item={it} />
      </TableCell>
      <TableCell>
        <RunState item={it} />
      </TableCell>
      <TableCell className="hidden max-w-32 truncate text-muted-foreground lg:table-cell">{it.source || '—'}</TableCell>
      <TableCell className="hidden text-muted-foreground sm:table-cell" title={formatDateTime(it.created_at)}>
        {timeAgo(it.created_at)}
      </TableCell>
      <TableCell className="hidden text-muted-foreground xl:table-cell" title={formatDateTime(it.updated_at)}>
        {timeAgo(it.updated_at)}
      </TableCell>
    </TableRow>
  )
})

export function TableView({ items, onOpen, empty }: ViewProps) {
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
          className="inline-flex items-center gap-1 hover:text-foreground pointer-coarse:min-h-10"
          onClick={() => setSort((s) => (s?.key !== key ? { key, dir: 1 } : s.dir === 1 ? { key, dir: -1 } : null))}
        >
          {label}
          <Icon className={cn('size-3', !active && 'opacity-40')} />
        </button>
      </TableHead>
    )
  }

  if (isMobile) {
    if (rows.length === 0) {
      return empty
    }

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
            {/* The title takes what the fixed columns leave, never less than 12rem (the table scrolls instead);
                Source and Updated join only where there is room for them next to it. */}
            {header('title', 'Title', 'min-w-48')}
            {header('type', 'Type', 'w-24')}
            {header('decision', 'Decision', 'w-28')}
            {header('run', 'Run', 'w-32')}
            {header('source', 'Source', 'hidden w-32 lg:table-cell')}
            {header('created_at', 'Created', 'hidden w-28 sm:table-cell')}
            {header('updated_at', 'Updated', 'hidden w-28 xl:table-cell')}
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((it) => (
            <TableItemRow key={it.id} item={it} onOpen={onOpen} />
          ))}
          {rows.length === 0 && (
            <TableRow className="hover:bg-transparent">
              <TableCell colSpan={8} className="whitespace-normal">
                {empty}
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
    </div>
  )
}
