'use client'

import { Check, FileText, Film, Music } from 'lucide-react'
import { memo, useEffect, useRef, useState } from 'react'

import { MEDIA_DRAG, setMediaDragImage } from '@/components/media/media-folders'
import { mediaThumbUrl, mediaUrl, type MediaLibraryItem } from '@/lib/api'
import { formatDuration, mediaKind } from '@/lib/media'
import { cn } from '@/lib/utils'

// The waveform bars: heights in %, each with a stable key.
const WAVE = [30, 55, 80, 45, 95, 65, 40, 75, 100, 60, 35, 70, 50, 85, 40, 25].map((h, i) => ({ id: `bar-${i}`, h }))

// One observer for every video tile: a tile's <video> mounts once it comes near the screen.
let observer: IntersectionObserver | undefined
const nearby = new Map<Element, () => void>()

function whenNear(el: Element, show: () => void) {
  if (typeof IntersectionObserver === 'undefined') {
    show()

    return () => undefined
  }

  observer ??= new IntersectionObserver(
    (entries) => {
      for (const e of entries) {
        if (e.isIntersecting) {
          nearby.get(e.target)?.()
          nearby.delete(e.target)
          observer?.unobserve(e.target)
        }
      }
    },
    { rootMargin: '400px' },
  )
  nearby.set(el, show)
  observer.observe(el)

  return () => {
    nearby.delete(el)
    observer?.unobserve(el)
  }
}

/**
 * A video's first frame, drawn by the browser from the metadata (the server keeps no poster images). Off-screen
 * tiles show a film icon instead, so a long library doesn't open a request per video.
 */
function VideoFrame({ id, className }: { id: string; className?: string }) {
  const [near, setNear] = useState(false)
  const box = useRef<HTMLSpanElement>(null)

  useEffect(() => {
    if (near || !box.current) {
      return
    }

    return whenNear(box.current, () => setNear(true))
  }, [near])

  if (!near) {
    return (
      <span
        ref={box}
        className={cn('flex size-full items-center justify-center bg-muted text-muted-foreground', className)}
      >
        <Film className="size-6" aria-hidden />
      </span>
    )
  }

  return (
    <video
      src={`${mediaUrl(id)}#t=0.1`}
      preload="metadata"
      muted
      playsInline
      tabIndex={-1}
      aria-hidden
      className={cn('pointer-events-none size-full bg-black object-cover', className)}
    />
  )
}

/** The picture (a thumbnail), the video's first frame, a waveform for audio, or an icon for older documents. */
function MediaPreview({ item, className }: { item: MediaLibraryItem; className?: string }) {
  const kind = mediaKind(item.mime_type)

  if (kind === 'image') {
    return (
      <img
        src={mediaThumbUrl(item.id, 320)}
        alt={item.alt_text || item.original_name}
        loading="lazy"
        decoding="async"
        // The tile is what drags (onto folders), never the picture as a file.
        draggable={false}
        className={cn('size-full object-cover', className)}
      />
    )
  }

  if (kind === 'video') {
    return <VideoFrame id={item.id} className={className} />
  }

  if (kind === 'audio') {
    // A quiet waveform (the same bars for every file: the server keeps no waveform) and the length when known.
    return (
      <span
        className={cn(
          'flex size-full flex-col items-center justify-center gap-2 bg-gradient-to-b from-muted to-muted/40 p-3 text-muted-foreground',
          className,
        )}
      >
        <span aria-hidden className="flex h-10 items-center gap-[3px]">
          {WAVE.map(({ id, h }) => (
            <span key={id} className="w-[3px] rounded-full bg-current opacity-70" style={{ height: `${h}%` }} />
          ))}
        </span>
        <span className="flex items-center gap-1 text-[11px] tabular-nums">
          <Music className="size-3" aria-hidden />
          {item.duration_ms ? formatDuration(item.duration_ms) : 'Audio'}
        </span>
      </span>
    )
  }

  // Documents uploaded before the library took media alone.
  const Icon = FileText

  return (
    <span
      className={cn('flex size-full flex-col items-center justify-center gap-1.5 p-2 text-muted-foreground', className)}
    >
      <Icon className="size-6" aria-hidden />
      <span className="line-clamp-2 text-center text-[11px] wrap-anywhere">{item.original_name}</span>
    </span>
  )
}

interface Tile {
  /** A click on the picture or the name: opens the file, or selects it while selecting (or with Shift). */
  activate: (item: MediaLibraryItem, shift: boolean) => void
  toggle: (item: MediaLibraryItem, shift: boolean) => void
  keyDown: (item: MediaLibraryItem, e: React.KeyboardEvent<HTMLButtonElement>) => void
  dragStart: (item: MediaLibraryItem, e: React.DragEvent) => void
  dragEnd: () => void
}

/**
 * One tile, memoised: it gets primitives and stable callbacks, so selecting a file re-renders that tile alone.
 * Whether the grid is selecting (the select boxes stay) comes from the grid's `data-selecting`, not a prop.
 */
const MediaTile = memo(function MediaTile({
  item,
  isSelected,
  isHighlighted,
  isMoving,
  selectable,
  draggable,
  caption,
  on,
}: {
  item: MediaLibraryItem
  isSelected: boolean
  isHighlighted: boolean
  isMoving: boolean
  selectable: boolean
  draggable: boolean
  caption?: React.ReactNode
  on: Tile
}) {
  const kind = mediaKind(item.mime_type)

  return (
    // oxlint-disable-next-line jsx-a11y/no-noninteractive-element-interactions -- dragging onto folders is a pointer shortcut; Move to folder is the keyboard way
    <li
      // Off-screen tiles skip layout and paint; the negative margin and padding keep the rings inside the box.
      className={cn(
        'group relative -m-1 min-w-0 p-1 [contain-intrinsic-size:auto_13rem] [content-visibility:auto]',
        isMoving && 'opacity-40',
      )}
      draggable={draggable}
      onDragStart={(e) => on.dragStart(item, e)}
      onDragEnd={on.dragEnd}
    >
      {/* The picture opens the file for a pointer; the name below is the tile's button (keyboard, screen
          readers), so the select box is the only control over the picture and nothing overlaps. */}
      {/* oxlint-disable-next-line jsx-a11y/click-events-have-key-events, jsx-a11y/no-static-element-interactions -- the name button below is the keyboard equivalent */}
      <div
        onClick={(e) => on.activate(item, e.shiftKey)}
        className={cn(
          'relative aspect-square cursor-pointer overflow-hidden rounded-xl border bg-muted transition group-has-[[data-tile]:focus-visible]:ring-3 group-has-[[data-tile]:focus-visible]:ring-ring/50',
          isSelected && 'border-brand ring-2 ring-brand/60',
          isHighlighted && !isSelected && 'ring-4 ring-brand/40',
        )}
      >
        <MediaPreview item={item} />
        {/* One row of badges: on narrow tiles the use count shortens instead of covering the length. */}
        <span className="absolute inset-x-1.5 bottom-1.5 flex items-end justify-between gap-1">
          {kind === 'video' && (
            <span className="inline-flex shrink-0 items-center gap-1 rounded-md bg-black/70 px-1.5 py-0.5 text-[11px] font-medium text-white tabular-nums">
              <Film className="size-3" aria-hidden />
              {item.duration_ms ? formatDuration(item.duration_ms) : 'Video'}
            </span>
          )}
          {item.uses > 0 && (
            <span
              className="ml-auto min-w-0 truncate rounded-md bg-black/70 px-1.5 py-0.5 text-[11px] font-medium text-white"
              title={`Used in ${item.uses} ${item.uses === 1 ? 'post' : 'posts'}`}
            >
              In {item.uses} {item.uses === 1 ? 'post' : 'posts'}
            </span>
          )}
        </span>
        {selectable && (
          <button
            type="button"
            aria-label={isSelected ? `Unselect ${item.original_name}` : `Select ${item.original_name}`}
            aria-pressed={isSelected}
            onClick={(e) => {
              e.stopPropagation()
              on.toggle(item, e.shiftKey)
            }}
            className={cn(
              'absolute top-2 right-2 flex size-5 items-center justify-center rounded-md border shadow-sm transition outline-none group-hover:opacity-100 focus-visible:opacity-100 focus-visible:ring-3 focus-visible:ring-ring/50 pointer-coarse:size-7 pointer-coarse:opacity-100 pointer-coarse:after:absolute pointer-coarse:after:-inset-1.5',
              isSelected
                ? 'border-brand bg-brand text-brand-foreground opacity-100'
                : 'border-white/80 bg-black/30 text-transparent opacity-0 group-data-[selecting]/grid:opacity-100',
            )}
          >
            <Check className="size-3.5" aria-hidden />
          </button>
        )}
      </div>
      <button
        type="button"
        data-tile
        data-testid="media-tile"
        aria-label={`${item.original_name}${isSelected ? ', selected' : ''}`}
        aria-pressed={selectable ? isSelected : undefined}
        title={item.original_name}
        onClick={(e) => on.activate(item, e.shiftKey)}
        onKeyDown={(e) => on.keyDown(item, e)}
        className="mt-1 block w-full truncate rounded-md px-1 py-0.5 text-center text-xs font-medium outline-none focus-visible:ring-2 focus-visible:ring-ring pointer-coarse:min-h-10"
      >
        {item.original_name}
      </button>
      {caption !== undefined && <p className="truncate px-1 text-center text-xs text-muted-foreground">{caption}</p>}
    </li>
  )
})

const NO_IDS = new Set<string>()

/**
 * Library tiles in a grid: the thumbnail and the name. Arrow keys move between tiles, Enter opens one, Space
 * selects it (when `onToggle` is given; Shift+click selects a range, Cmd/Ctrl+A all). The select box (top right)
 * shows on hover or focus, and the select boxes stay while anything is selected. With `dragIds`, tiles drag onto
 * folders: the selected ones when the tile is selected, else just it; they dim while they move.
 *
 * Tiles are memoised; the callbacks they get read the latest props from a ref, so they never change.
 */
export function MediaGrid({
  items,
  selected,
  onOpen,
  onToggle,
  onSelectAll,
  dragIds,
  caption,
  highlight = NO_IDS,
  className,
}: {
  items: MediaLibraryItem[]
  selected: Set<string>
  onOpen: (item: MediaLibraryItem) => void
  /** `range`: Shift was held (extend the selection to this tile). */
  onToggle?: (item: MediaLibraryItem, range?: boolean) => void
  /** Cmd/Ctrl+A in the grid. */
  onSelectAll?: () => void
  dragIds?: (item: MediaLibraryItem) => string[]
  /** A muted line under the name (a folder, "Deletes in 12 days"). */
  caption?: (item: MediaLibraryItem) => React.ReactNode
  /** Tiles to ring for a moment (just uploaded). */
  highlight?: Set<string>
  className?: string
}) {
  const grid = useRef<HTMLUListElement>(null)
  const [moving, setMoving] = useState<Set<string>>(NO_IDS)
  const latest = useRef({ items, selected, onOpen, onToggle, onSelectAll, dragIds })

  useEffect(() => {
    latest.current = { items, selected, onOpen, onToggle, onSelectAll, dragIds }
  })

  const [on] = useState<Tile>(() => ({
    activate: (item, shift) => {
      const now = latest.current

      if (now.onToggle && (now.selected.size > 0 || shift)) {
        now.onToggle(item, shift)
      } else {
        now.onOpen(item)
      }
    },
    toggle: (item, shift) => latest.current.onToggle?.(item, shift),
    keyDown: (item, e) => {
      const now = latest.current

      if (now.onSelectAll && (e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'a') {
        e.preventDefault()
        now.onSelectAll()

        return
      }

      if (e.key === ' ' && now.onToggle) {
        e.preventDefault()
        now.onToggle(item)

        return
      }

      // Arrow keys: the next tile in the row or the column.
      const tiles = Array.from(grid.current?.querySelectorAll<HTMLElement>('[data-tile]') ?? [])
      const from = tiles.indexOf(e.currentTarget)
      const cols = grid.current ? getComputedStyle(grid.current).gridTemplateColumns.split(' ').length : 1
      const to = { ArrowRight: from + 1, ArrowLeft: from - 1, ArrowDown: from + cols, ArrowUp: from - cols }[e.key]

      if (from >= 0 && to !== undefined && tiles[to]) {
        e.preventDefault()
        tiles[to].focus()
      }
    },
    dragStart: (item, e) => {
      const now = latest.current

      if (!now.dragIds) {
        return
      }

      const ids = now.dragIds(item)

      e.dataTransfer.setData(MEDIA_DRAG, JSON.stringify(ids))
      e.dataTransfer.effectAllowed = 'move'
      setMediaDragImage(
        e,
        ids.map((id) => now.items.find((m) => m.id === id)?.original_name ?? id),
      )
      setMoving(new Set(ids))
    },
    dragEnd: () => setMoving(NO_IDS),
  }))

  return (
    <ul
      ref={grid}
      className={cn('group/grid grid grid-cols-[repeat(auto-fill,minmax(min(100%,9rem),1fr))] gap-3', className)}
      aria-label="Media files"
      data-selecting={selected.size > 0 || undefined}
    >
      {items.map((item) => (
        <MediaTile
          key={item.id}
          item={item}
          isSelected={selected.has(item.id)}
          isHighlighted={highlight.has(item.id)}
          isMoving={moving.has(item.id)}
          selectable={!!onToggle}
          draggable={!!dragIds}
          caption={caption?.(item)}
          on={on}
        />
      ))}
    </ul>
  )
}
