'use client'

import { Film, ImageIcon, Music, Type } from 'lucide-react'
import { useEffect, useRef, useState, type ReactNode } from 'react'

import { mediaThumbUrl } from '@/lib/api'
import { cn } from '@/lib/utils'

import { decode, peaks } from './audio'
import { clipStarts, length, soundLanes, type Clip, type Sound, type Timeline } from './doc'

export type Selection = { kind: 'clip' | 'caption' | 'sound'; id: string } | null

type Update = (fn: (t: Timeline) => void, coalesce?: string) => void

const ROW = 52
const LABEL = 92
const MIN = 0.5

/** Follows the pointer from a press until it is let go, in seconds along the timeline (`pps` pixels each). */
function dragSeconds(
  e: React.PointerEvent,
  pps: number,
  onMove: (ds: number) => void,
  onEnd?: (moved: boolean) => void,
) {
  e.preventDefault()
  e.stopPropagation()

  const x0 = e.clientX
  let moved = false

  const move = (ev: PointerEvent) => {
    if (!moved && Math.abs(ev.clientX - x0) < 3) {
      return
    }

    moved = true
    onMove(Math.round(((ev.clientX - x0) / pps) * 20) / 20)
  }
  const up = () => {
    window.removeEventListener('pointermove', move)
    window.removeEventListener('pointerup', up)
    onEnd?.(moved)
  }

  window.addEventListener('pointermove', move)
  window.addEventListener('pointerup', up)
}

/** The grab areas at a block's two ends. */
function Handles({
  onLeft,
  onRight,
}: {
  onLeft?: (e: React.PointerEvent) => void
  onRight: (e: React.PointerEvent) => void
}) {
  const cls =
    'absolute inset-y-0 z-10 w-2 cursor-ew-resize bg-white/0 transition-colors group-hover:bg-white/25 group-aria-selected:bg-white/40'

  return (
    <>
      {onLeft && <span aria-hidden className={cn(cls, 'left-0 rounded-l-md')} onPointerDown={onLeft} />}
      <span aria-hidden className={cn(cls, 'right-0 rounded-r-md')} onPointerDown={onRight} />
    </>
  )
}

/** A sound clip's waveform: the loudest sample in each pixel column of its part of the file. */
function Wave({ s, width }: { s: Sound; width: number }) {
  const ref = useRef<HTMLCanvasElement>(null)

  useEffect(() => {
    let live = true

    void decode(s.src).then((buf) => {
      const c = ref.current

      if (!live || !c || !buf) {
        return
      }

      const n = Math.max(1, Math.floor(width / 2))
      const p = peaks(buf, s.in, s.duration, n)
      const ctx = c.getContext('2d')!
      const h = c.height

      c.width = n * 2
      ctx.clearRect(0, 0, c.width, h)
      ctx.fillStyle = 'rgba(255,255,255,0.7)'

      for (let i = 0; i < n; i++) {
        const v = Math.max(1, p[i]! * h * 0.9)

        ctx.fillRect(i * 2, (h - v) / 2, 1.4, v)
      }
    })

    return () => {
      live = false
    }
  }, [s.src, s.in, s.duration, width])

  return (
    <canvas
      ref={ref}
      height={ROW - 18}
      className="pointer-events-none absolute inset-x-1 bottom-1 h-[calc(100%-18px)] w-[calc(100%-8px)] opacity-80"
    />
  )
}

function Row({
  label,
  icon,
  children,
  height = ROW,
}: {
  label: string
  icon: ReactNode
  children: ReactNode
  height?: number
}) {
  return (
    <div className="flex border-b border-border/60" style={{ height }}>
      <div
        className="sticky left-0 z-20 flex shrink-0 items-center gap-1.5 border-r bg-background px-2 text-xs text-muted-foreground [&_svg]:size-3.5"
        style={{ width: LABEL }}
      >
        {icon}
        {label}
      </div>
      <div className="relative flex-1">{children}</div>
    </div>
  )
}

/** The timeline under the preview: a ruler, the picture track, captions and sound lanes, and the playhead. */
export function Tracks({
  doc,
  time,
  pps,
  sel,
  onSelect,
  onSeek,
  update,
}: {
  doc: Timeline
  time: number
  pps: number
  sel: Selection
  onSelect: (s: Selection) => void
  onSeek: (t: number) => void
  update: Update
}) {
  const scroller = useRef<HTMLDivElement>(null)
  const video = doc.kind === 'video'
  const total = length(doc)
  const width = Math.max(total + 8, 20) * pps
  const starts = clipStarts(doc)
  const lanes = soundLanes(doc.sounds)
  // A clip being dragged to another place on the picture track: where it would go.
  const [moving, setMoving] = useState<{ id: string; dx: number; to: number } | null>(null)

  const seekFrom = (e: React.PointerEvent) => {
    const box = e.currentTarget.getBoundingClientRect()
    const at = (x: number) => Math.max(0, (x - box.left) / pps)

    onSeek(at(e.clientX))
    dragSeconds(e, pps, (ds) => onSeek(at(e.clientX) + ds))
  }

  // The playhead stays in view while it plays.
  useEffect(() => {
    const el = scroller.current

    if (!el) {
      return
    }

    const x = LABEL + time * pps

    if (x > el.scrollLeft + el.clientWidth - 40 || x < el.scrollLeft + LABEL) {
      el.scrollLeft = Math.max(0, x - LABEL - 40)
    }
  }, [time, pps])

  /** Where a clip moved from `from` to slot `to` would begin: the edge it lands on. */
  const dropX = (to: number, from: number) => (to < from ? starts[to]! : starts[to]! + doc.clips[to]!.duration)

  const step = total > 120 ? 10 : pps >= 60 ? 1 : pps >= 25 ? 2 : 5

  const clipBlock = (c: Clip, i: number) => {
    const left = starts[i]! * pps
    const w = c.duration * pps
    const selected = sel?.kind === 'clip' && sel.id === c.id
    const drag = moving?.id === c.id

    return (
      <div
        key={c.id}
        role="option"
        aria-selected={selected}
        aria-label={`${c.name}, ${c.duration.toFixed(1)} s`}
        tabIndex={0}
        onKeyDown={(e) => e.key === 'Enter' && onSelect({ kind: 'clip', id: c.id })}
        className={cn(
          'group absolute top-1.5 bottom-1.5 cursor-grab overflow-hidden rounded-md border border-white/10 bg-zinc-700 text-[11px] text-white shadow-sm',
          selected && 'ring-2 ring-brand',
          drag && 'z-30 cursor-grabbing opacity-90 shadow-lg',
        )}
        style={{ left, width: Math.max(8, w - 2), transform: drag ? `translateX(${moving.dx}px)` : undefined }}
        onPointerDown={(e) => {
          onSelect({ kind: 'clip', id: c.id })
          // Dragging a clip moves it along the track: it lands where its middle is.
          let to = i

          dragSeconds(
            e,
            pps,
            (ds) => {
              const mid = starts[i]! + c.duration / 2 + ds

              to = 0

              for (const [j, s] of starts.entries()) {
                if (j !== i && mid > s + doc.clips[j]!.duration / 2) {
                  to = j < i ? j + 1 : j
                }
              }

              setMoving({ id: c.id, dx: ds * pps, to })
            },
            () => {
              setMoving(null)

              if (to !== i) {
                update((t) => {
                  const [moved] = t.clips.splice(i, 1)

                  t.clips.splice(to, 0, moved!)
                })
              }
            },
          )
        }}
      >
        {c.kind === 'image' ? (
          <span
            aria-hidden
            className="absolute inset-0 bg-repeat-x opacity-70"
            style={{ backgroundImage: `url(${mediaThumbUrl(c.mediaId, 160)})`, backgroundSize: 'auto 100%' }}
          />
        ) : (
          <span aria-hidden className="absolute inset-0 bg-gradient-to-r from-indigo-900 to-indigo-700 opacity-90" />
        )}
        <span className="relative flex items-center gap-1 truncate px-2 pt-1 font-medium drop-shadow [&_svg]:size-3">
          {c.kind === 'video' ? <Film /> : <ImageIcon />}
          <span className="truncate">{c.name}</span>
        </span>
        <span className="absolute right-2 bottom-1 font-mono text-[10px] opacity-90 drop-shadow">
          {c.duration.toFixed(1)} s
        </span>
        <Handles
          onLeft={
            c.kind === 'video'
              ? (e) => {
                  const in0 = c.in
                  const d0 = c.duration

                  dragSeconds(e, pps, (ds) =>
                    update((t) => {
                      const k = t.clips.find((x) => x.id === c.id)!
                      const d = Math.max(-in0, Math.min(ds, d0 - MIN))

                      k.in = in0 + d
                      k.duration = d0 - d
                    }, `trim-${c.id}`),
                  )
                }
              : undefined
          }
          onRight={(e) => {
            const d0 = c.duration
            const max = c.kind === 'video' ? Math.max(MIN, (c.fileDuration ?? Infinity) - c.in) : 60

            dragSeconds(e, pps, (ds) =>
              update((t) => {
                t.clips.find((x) => x.id === c.id)!.duration = Math.max(MIN, Math.min(max, d0 + ds))
              }, `dur-${c.id}`),
            )
          }}
        />
      </div>
    )
  }

  return (
    <div ref={scroller} className="relative min-h-0 flex-1 overflow-auto bg-muted/30" data-testid="timeline-tracks">
      <div className="relative" style={{ width: width + LABEL }}>
        {/* The ruler: click or drag to move the playhead. */}
        <div className="sticky top-0 z-30 flex h-7 border-b bg-background">
          <div className="sticky left-0 z-20 shrink-0 border-r bg-background" style={{ width: LABEL }} />
          {/* oxlint-disable-next-line jsx-a11y/no-static-element-interactions -- the playhead also moves with the arrow keys */}
          <div className="relative flex-1 cursor-pointer" onPointerDown={seekFrom} data-testid="timeline-ruler">
            {Array.from({ length: Math.ceil(width / pps / step) }, (_, i) => i * step).map((s) => (
              <span
                key={s}
                className="absolute top-0 h-full border-l border-border pl-1 text-[10px] text-muted-foreground tabular-nums"
                style={{ left: s * pps }}
              >
                {s % 60 === 0 && s ? `${s / 60}:00` : `${s}s`}
              </span>
            ))}
          </div>
        </div>

        <div role="listbox" aria-label="Timeline">
          {video && (
            <Row label="Pictures" icon={<ImageIcon />}>
              {doc.clips.map(clipBlock)}
              {moving && (
                <span
                  aria-hidden
                  className="absolute top-1 bottom-1 z-20 w-0.5 rounded bg-brand"
                  style={{
                    left:
                      dropX(
                        moving.to,
                        doc.clips.findIndex((c) => c.id === moving.id),
                      ) * pps,
                  }}
                />
              )}
              {!doc.clips.length && (
                <span className="absolute inset-y-0 left-3 flex items-center text-xs text-muted-foreground">
                  Add pictures or videos from the left
                </span>
              )}
            </Row>
          )}
          {video && (
            <Row label="Text" icon={<Type />} height={36}>
              {doc.captions.map((c) => {
                const selected = sel?.kind === 'caption' && sel.id === c.id

                return (
                  <div
                    key={c.id}
                    role="option"
                    aria-selected={selected}
                    aria-label={`Caption: ${c.text}`}
                    tabIndex={0}
                    onKeyDown={(e) => e.key === 'Enter' && onSelect({ kind: 'caption', id: c.id })}
                    className={cn(
                      'group absolute top-1 bottom-1 cursor-grab truncate rounded-md bg-amber-500/90 px-2 text-[11px] leading-7 font-medium text-amber-950',
                      selected && 'ring-2 ring-brand',
                    )}
                    style={{ left: c.start * pps, width: Math.max(8, (c.end - c.start) * pps - 2) }}
                    onPointerDown={(e) => {
                      onSelect({ kind: 'caption', id: c.id })

                      const s0 = c.start
                      const e0 = c.end

                      dragSeconds(e, pps, (ds) =>
                        update((t) => {
                          const k = t.captions.find((x) => x.id === c.id)!
                          const d = Math.max(-s0, ds)

                          k.start = s0 + d
                          k.end = e0 + d
                        }, `move-${c.id}`),
                      )
                    }}
                  >
                    {c.text}
                    <Handles
                      onLeft={(e) => {
                        const s0 = c.start

                        dragSeconds(e, pps, (ds) =>
                          update((t) => {
                            const k = t.captions.find((x) => x.id === c.id)!

                            k.start = Math.max(0, Math.min(k.end - 0.3, s0 + ds))
                          }, `start-${c.id}`),
                        )
                      }}
                      onRight={(e) => {
                        const e0 = c.end

                        dragSeconds(e, pps, (ds) =>
                          update((t) => {
                            const k = t.captions.find((x) => x.id === c.id)!

                            k.end = Math.max(k.start + 0.3, e0 + ds)
                          }, `end-${c.id}`),
                        )
                      }}
                    />
                  </div>
                )
              })}
            </Row>
          )}
          {Array.from({ length: lanes.count }, (_, lane) => (
            <Row
              key={lane}
              label={lane ? `Sound ${lane + 1}` : video ? 'Sound' : 'Sound 1'}
              icon={<Music />}
              height={video ? ROW : ROW + 20}
            >
              {doc.sounds
                .filter((s) => lanes.lane.get(s.id) === lane)
                .map((s) => {
                  const selected = sel?.kind === 'sound' && sel.id === s.id
                  const w = s.duration * pps

                  return (
                    <div
                      key={s.id}
                      role="option"
                      aria-selected={selected}
                      aria-label={`${s.name}, ${s.duration.toFixed(1)} s`}
                      tabIndex={0}
                      onKeyDown={(e) => e.key === 'Enter' && onSelect({ kind: 'sound', id: s.id })}
                      className={cn(
                        'group absolute top-1.5 bottom-1.5 cursor-grab overflow-hidden rounded-md bg-emerald-700 text-[11px] text-white shadow-sm',
                        selected && 'ring-2 ring-brand',
                      )}
                      style={{ left: s.start * pps, width: Math.max(8, w - 2) }}
                      onPointerDown={(e) => {
                        onSelect({ kind: 'sound', id: s.id })

                        const s0 = s.start

                        dragSeconds(e, pps, (ds) =>
                          update(
                            (t) => void (t.sounds.find((x) => x.id === s.id)!.start = Math.max(0, s0 + ds)),
                            `move-${s.id}`,
                          ),
                        )
                      }}
                    >
                      <Wave s={s} width={w} />
                      {/* The fades, as ramps at both ends. */}
                      {s.fadeIn > 0 && (
                        <span
                          aria-hidden
                          className="absolute inset-y-0 left-0 bg-gradient-to-r from-black/50 to-transparent"
                          style={{ width: s.fadeIn * pps }}
                        />
                      )}
                      {s.fadeOut > 0 && (
                        <span
                          aria-hidden
                          className="absolute inset-y-0 right-0 bg-gradient-to-l from-black/50 to-transparent"
                          style={{ width: s.fadeOut * pps }}
                        />
                      )}
                      <span className="relative block truncate px-2 pt-0.5 font-medium drop-shadow">{s.name}</span>
                      <Handles
                        onLeft={(e) => {
                          const { start: st0, in: in0, duration: d0 } = s

                          dragSeconds(e, pps, (ds) =>
                            update((t) => {
                              const k = t.sounds.find((x) => x.id === s.id)!
                              const d = Math.max(-in0, -st0, Math.min(ds, d0 - MIN))

                              k.start = st0 + d
                              k.in = in0 + d
                              k.duration = d0 - d
                            }, `trim-${s.id}`),
                          )
                        }}
                        onRight={(e) => {
                          const d0 = s.duration

                          dragSeconds(e, pps, (ds) =>
                            update((t) => {
                              const k = t.sounds.find((x) => x.id === s.id)!

                              k.duration = Math.max(MIN, Math.min(k.fileDuration - k.in, d0 + ds))
                            }, `dur-${s.id}`),
                          )
                        }}
                      />
                    </div>
                  )
                })}
              {!doc.sounds.length && lane === 0 && (
                <span className="absolute inset-y-0 left-3 flex items-center text-xs text-muted-foreground">
                  Add music or sound from the left
                </span>
              )}
            </Row>
          ))}
        </div>

        {/* The playhead. */}
        <span
          aria-hidden
          className="pointer-events-none absolute top-0 bottom-0 z-40 w-0.5 bg-brand"
          style={{ left: LABEL + time * pps }}
        >
          <span className="absolute -top-0.5 -left-1.5 size-3.5 rotate-45 rounded-sm bg-brand" />
        </span>
      </div>
    </div>
  )
}
