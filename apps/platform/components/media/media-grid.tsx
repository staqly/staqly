'use client'

import { Check, FileText, Film, Music } from 'lucide-react'
import { useRef, useState } from 'react'

import { MEDIA_DRAG, setMediaDragImage } from '@/components/media/media-folders'
import { mediaThumbUrl, mediaUrl, type MediaLibraryItem } from '@/lib/api'
import { formatDuration, mediaKind } from '@/lib/media'
import { cn } from '@/lib/utils'

const WAVE = [30, 55, 80, 45, 95, 65, 40, 75, 100, 60, 35, 70, 50, 85, 40, 25]

/** The picture (a thumbnail), the video's first frame, a waveform for audio, or an icon for older documents. */
export function MediaPreview({ item, className }: { item: MediaLibraryItem; className?: string }) {
  const kind = mediaKind(item.mime_type)

  if (kind === 'image') {
    return (
      // eslint-disable-next-line @next/next/no-img-element
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
    // The browser draws the first frame from the metadata; the server keeps no poster images.
    return (
      <video
        src={`${mediaUrl(item.id)}#t=0.1`}
        preload="metadata"
        muted
        playsInline
        tabIndex={-1}
        aria-hidden
        className={cn('pointer-events-none size-full bg-black object-cover', className)}
      />
    )
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
          {WAVE.map((h, i) => (
            <span key={i} className="w-[3px] rounded-full bg-current opacity-70" style={{ height: `${h}%` }} />
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

/**
 * Library tiles in a grid: the thumbnail and the name. Arrow keys move between tiles, Enter opens one, Space
 * selects it (when `onToggle` is given; Shift+click selects a range, Cmd/Ctrl+A all). The select box (top right)
 * shows on
 * hover or focus, and the select boxes stay while anything is selected. With `dragIds`, tiles drag onto folders:
 * the selected ones when the tile is selected, else just it; they dim while they move.
 */
export function MediaGrid({
  items,
  selected,
  onOpen,
  onToggle,
  onSelectAll,
  dragIds,
  caption,
  highlight,
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
  const [moving, setMoving] = useState<Set<string>>(new Set())
  const selecting = selected.size > 0

  const move = (from: number, e: React.KeyboardEvent) => {
    const tiles = Array.from(grid.current?.querySelectorAll<HTMLElement>('[data-tile]') ?? [])
    const cols = grid.current ? getComputedStyle(grid.current).gridTemplateColumns.split(' ').length : 1
    const to = { ArrowRight: from + 1, ArrowLeft: from - 1, ArrowDown: from + cols, ArrowUp: from - cols }[e.key]

    if (to !== undefined && tiles[to]) {
      e.preventDefault()
      tiles[to].focus()
    }
  }

  return (
    <ul
      ref={grid}
      className={cn('grid grid-cols-[repeat(auto-fill,minmax(min(100%,9rem),1fr))] gap-3', className)}
      aria-label="Media files"
    >
      {items.map((item, i) => {
        const isSelected = selected.has(item.id)
        const kind = mediaKind(item.mime_type)

        return (
          // oxlint-disable-next-line jsx-a11y/no-noninteractive-element-interactions -- dragging onto folders is a pointer shortcut; Move to folder is the keyboard way
          <li
            key={item.id}
            className={cn('group relative min-w-0', moving.has(item.id) && 'opacity-40')}
            draggable={!!dragIds}
            onDragStart={(e) => {
              if (!dragIds) {
                return
              }

              const ids = dragIds(item)

              e.dataTransfer.setData(MEDIA_DRAG, JSON.stringify(ids))
              e.dataTransfer.effectAllowed = 'move'
              setMediaDragImage(
                e,
                ids.map((id) => items.find((m) => m.id === id)?.original_name ?? id),
              )
              setMoving(new Set(ids))
            }}
            onDragEnd={() => setMoving(new Set())}
          >
            {/* The picture opens the file for a pointer; the name below is the tile's button (keyboard, screen
                readers), so the select box is the only control over the picture and nothing overlaps. */}
            {/* oxlint-disable-next-line jsx-a11y/click-events-have-key-events, jsx-a11y/no-static-element-interactions -- the name button below is the keyboard equivalent */}
            <div
              onClick={(e) => (onToggle && (selecting || e.shiftKey) ? onToggle(item, e.shiftKey) : onOpen(item))}
              className={cn(
                'relative aspect-square cursor-pointer overflow-hidden rounded-xl border bg-muted transition group-has-[[data-tile]:focus-visible]:ring-3 group-has-[[data-tile]:focus-visible]:ring-ring/50',
                isSelected && 'border-brand ring-2 ring-brand/60',
                highlight?.has(item.id) && !isSelected && 'ring-4 ring-brand/40',
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
              {onToggle && (
                <button
                  type="button"
                  aria-label={isSelected ? `Unselect ${item.original_name}` : `Select ${item.original_name}`}
                  aria-pressed={isSelected}
                  onClick={(e) => {
                    e.stopPropagation()
                    onToggle(item, e.shiftKey)
                  }}
                  className={cn(
                    'absolute top-2 right-2 flex size-5 items-center justify-center rounded-md border shadow-sm transition outline-none group-hover:opacity-100 focus-visible:opacity-100 focus-visible:ring-3 focus-visible:ring-ring/50 pointer-coarse:size-7 pointer-coarse:opacity-100 pointer-coarse:after:absolute pointer-coarse:after:-inset-1.5',
                    isSelected
                      ? 'border-brand bg-brand text-brand-foreground opacity-100'
                      : cn('border-white/80 bg-black/30 text-transparent', selecting ? 'opacity-100' : 'opacity-0'),
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
              aria-pressed={onToggle ? isSelected : undefined}
              title={item.original_name}
              onClick={(e) => (onToggle && (selecting || e.shiftKey) ? onToggle(item, e.shiftKey) : onOpen(item))}
              onKeyDown={(e) => {
                if (onSelectAll && (e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'a') {
                  e.preventDefault()
                  onSelectAll()

                  return
                }

                if (e.key === ' ' && onToggle) {
                  e.preventDefault()
                  onToggle(item)

                  return
                }

                move(i, e)
              }}
              className="mt-1 block w-full truncate rounded-md px-1 py-0.5 text-center text-xs font-medium outline-none focus-visible:ring-2 focus-visible:ring-ring pointer-coarse:min-h-10"
            >
              {item.original_name}
            </button>
            {caption && <p className="truncate px-1 text-center text-xs text-muted-foreground">{caption(item)}</p>}
          </li>
        )
      })}
    </ul>
  )
}
