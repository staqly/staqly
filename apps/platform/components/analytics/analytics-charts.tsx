'use client'

import { XCircle } from 'lucide-react'
import { useEffect, useRef, useState, type KeyboardEvent, type PointerEvent } from 'react'

import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { cn } from '@/lib/utils'

import {
  formatCount,
  formatDay,
  hourLabel,
  WEEKDAYS,
  WEEKDAYS_SHORT,
  type Analytics,
  type DayAnalytics,
  type TimeSlot,
  VIZ_COLORS,
} from './analytics-data'

// Hand-built SVG and tables, no chart library. Colours follow the data-viz palette: categorical slot 1 (blue) for
// published, the status "critical" red for failed (always with its label and icon), a one-hue blue ramp for the
// heatmap; each mode has its own steps (dark is selected, not flipped).
const HEAT_COLORS =
  '[--heat-1:#cde2fb] [--heat-2:#9ec5f4] [--heat-3:#5598e7] [--heat-4:#256abf] [--heat-5:#104281] dark:[--heat-1:#184f95] dark:[--heat-2:#1c5cab] dark:[--heat-3:#2a78d6] dark:[--heat-4:#5598e7] dark:[--heat-5:#9ec5f4]'

/** Clean axis ticks: 0 and three or four round steps up to at least max. */
export function niceTicks(max: number): number[] {
  if (max <= 0) {
    return [0, 1]
  }

  const raw = max / 4
  const mag = 10 ** Math.floor(Math.log10(raw))
  const step = ([1, 2, 5, 10].find((m) => m * mag >= raw) ?? 10) * mag
  const ticks: number[] = []

  for (let v = 0; v < max + step; v += step) {
    ticks.push(Math.round(v * 1e6) / 1e6)

    if (v >= max) {
      break
    }
  }

  return ticks
}

function useWidth<T extends HTMLElement>() {
  const ref = useRef<T>(null)
  const [width, setWidth] = useState(0)

  useEffect(() => {
    const el = ref.current

    if (!el) {
      return
    }

    const ro = new ResizeObserver(([entry]) => setWidth(Math.round(entry.contentRect.width)))

    ro.observe(el)
    setWidth(el.clientWidth)

    return () => ro.disconnect()
  }, [])

  return [ref, width] as const
}

function dayText(d: DayAnalytics) {
  return `${formatDay(d.date, { weekday: 'short', month: 'short', day: 'numeric' })}: ${formatCount(d.published)} published, ${formatCount(d.failed)} failed`
}

const PLOT_H = 168
const AXIS_H = 22
const LEFT = 36

/** Published and failed channels per day: stacked columns on one axis, a tooltip per day, a table twin. */
export function DailyChart({ data }: { data: Analytics }) {
  const days = data.daily
  const [box, measured] = useWidth<HTMLDivElement>()
  const width = measured || 640
  const [active, setActive] = useState<number | null>(null)
  const totalPublished = days.reduce((n, d) => n + d.published, 0)
  const totalFailed = days.reduce((n, d) => n + d.failed, 0)
  const busiest = days.reduce<DayAnalytics | null>(
    (b, d) => (!b || d.published + d.failed > b.published + b.failed ? d : b),
    null,
  )
  const ticks = niceTicks(Math.max(0, ...days.map((d) => d.published + d.failed)))
  const top = ticks.at(-1) ?? 1
  const plotW = Math.max(1, width - LEFT - 4)
  const band = plotW / Math.max(1, days.length)
  const barW = Math.max(2, Math.min(24, band - 2))
  const y = (v: number) => PLOT_H - (v / top) * PLOT_H
  const labelEvery = Math.max(1, Math.ceil(days.length / Math.max(2, Math.floor(plotW / 72))))
  const summary = `${formatCount(totalPublished)} published and ${formatCount(totalFailed)} failed over ${days.length} days${
    busiest && busiest.published + busiest.failed > 0 ? `; the busiest day was ${dayText(busiest)}` : ''
  }.`

  const pick = (e: PointerEvent<SVGSVGElement>) => {
    const rect = e.currentTarget.getBoundingClientRect()
    const i = Math.floor((e.clientX - rect.left - LEFT) / band)

    setActive(i >= 0 && i < days.length ? i : null)
  }

  const onKey = (e: KeyboardEvent<SVGSVGElement>) => {
    const last = days.length - 1
    const moves: Record<string, number> = {
      ArrowRight: Math.min(last, (active ?? -1) + 1),
      ArrowLeft: Math.max(0, (active ?? last + 1) - 1),
      Home: 0,
      End: last,
    }

    if (e.key in moves) {
      e.preventDefault()
      setActive(moves[e.key])
    } else if (e.key === 'Escape') {
      setActive(null)
    }
  }

  const tip = active === null ? null : days[active]
  const tipLeft = active === null ? 0 : Math.min(Math.max(LEFT + active * band + band / 2, 80), width - 80)

  return (
    <section
      aria-labelledby="daily-heading"
      className={cn('grid gap-3 rounded-xl border bg-card p-4 shadow-xs', VIZ_COLORS)}
    >
      <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
        <h2 id="daily-heading" className="text-base font-semibold">
          Published and failed per day
        </h2>
        <ul className="flex items-center gap-4 text-xs text-muted-foreground" aria-label="Legend">
          <li className="flex items-center gap-1.5">
            <span className="size-2.5 rounded-sm bg-(--viz-published)" aria-hidden />
            Published
          </li>
          <li className="flex items-center gap-1.5">
            <span className="size-2.5 rounded-sm bg-(--viz-failed)" aria-hidden />
            <XCircle className="size-3" aria-hidden />
            Failed
          </li>
        </ul>
      </div>
      <p className="text-xs text-muted-foreground">
        Channels per day, {formatDay(data.range.from)} to {formatDay(data.range.to)}. {summary}
      </p>
      <div ref={box} className="relative min-w-0">
        {/* oxlint-disable-next-line jsx-a11y/no-noninteractive-element-interactions -- arrow keys read each day as hover does; the table below holds every value */}
        <svg
          width={width}
          height={PLOT_H + AXIS_H + 8}
          role="img"
          tabIndex={0}
          aria-label={`Daily chart. ${summary} Use the arrow keys to read each day.`}
          className="block max-w-full overflow-visible rounded-md outline-none focus-visible:outline-2 focus-visible:outline-ring"
          onPointerMove={pick}
          onPointerLeave={() => setActive(null)}
          onKeyDown={onKey}
          onBlur={() => setActive(null)}
          data-testid="daily-chart"
        >
          <g transform="translate(0,4)">
            {ticks.map((t) => (
              <g key={t}>
                <line
                  x1={LEFT}
                  x2={width}
                  y1={y(t)}
                  y2={y(t)}
                  stroke={t === 0 ? 'var(--viz-axis)' : 'var(--viz-grid)'}
                />
                <text
                  x={LEFT - 6}
                  y={y(t)}
                  dy="0.32em"
                  textAnchor="end"
                  className="fill-muted-foreground text-[11px] tabular-nums"
                >
                  {formatCount(t)}
                </text>
              </g>
            ))}
            {days.map((d, i) => {
              const x = LEFT + i * band + (band - barW) / 2
              const pubH = (d.published / top) * PLOT_H
              const failH = (d.failed / top) * PLOT_H
              const gap = d.published && d.failed ? 2 : 0

              return (
                <g key={d.date} opacity={active === null || active === i ? 1 : 0.55}>
                  {d.published > 0 && (
                    <path d={column(x, PLOT_H, barW, pubH, d.failed === 0)} fill="var(--viz-published)" />
                  )}
                  {d.failed > 0 && (
                    <path
                      d={column(x, PLOT_H - pubH - gap, barW, Math.max(1, failH - gap), true)}
                      fill="var(--viz-failed)"
                    />
                  )}
                  {i % labelEvery === 0 && (
                    <text
                      x={LEFT + i * band + band / 2}
                      y={PLOT_H + 16}
                      textAnchor="middle"
                      className="fill-muted-foreground text-[11px] tabular-nums"
                    >
                      {formatDay(d.date)}
                    </text>
                  )}
                </g>
              )
            })}
          </g>
        </svg>
        {tip && (
          <div
            role="status"
            className="pointer-events-none absolute top-0 z-10 -translate-x-1/2 rounded-lg border bg-popover px-2.5 py-1.5 text-xs shadow-md"
            style={{ left: tipLeft }}
            data-testid="daily-tooltip"
          >
            <div className="font-medium">
              {formatDay(tip.date, { weekday: 'short', month: 'short', day: 'numeric' })}
            </div>
            <div className="flex items-center gap-1.5">
              <span className="h-0.5 w-3 rounded bg-(--viz-published)" aria-hidden />
              <strong className="tabular-nums">{formatCount(tip.published)}</strong>
              <span className="text-muted-foreground">published</span>
            </div>
            <div className="flex items-center gap-1.5">
              <span className="h-0.5 w-3 rounded bg-(--viz-failed)" aria-hidden />
              <strong className="tabular-nums">{formatCount(tip.failed)}</strong>
              <span className="text-muted-foreground">failed</span>
            </div>
          </div>
        )}
      </div>
      <details className="text-sm">
        <summary className="w-fit cursor-pointer text-muted-foreground hover:text-foreground">Show as a table</summary>
        <div
          className="mt-2 max-h-72 overflow-auto"
          tabIndex={0}
          role="region"
          aria-label="Published and failed per day"
        >
          <table className="w-full text-sm">
            <caption className="sr-only">Published and failed channels per day</caption>
            <thead>
              <tr className="border-b text-left text-muted-foreground">
                <th scope="col" className="py-1 font-medium">
                  Day
                </th>
                <th scope="col" className="py-1 text-right font-medium">
                  Published
                </th>
                <th scope="col" className="py-1 text-right font-medium">
                  Failed
                </th>
              </tr>
            </thead>
            <tbody>
              {days.map((d) => (
                <tr key={d.date} className="border-b last:border-0">
                  <th scope="row" className="py-1 text-left font-normal">
                    {formatDay(d.date, { weekday: 'short', month: 'short', day: 'numeric' })}
                  </th>
                  <td className="py-1 text-right tabular-nums">{formatCount(d.published)}</td>
                  <td className="py-1 text-right tabular-nums">{formatCount(d.failed)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </details>
    </section>
  )
}

/** A column from baseline yBottom up by h, its top corners rounded (4 px, or less on thin bars) when it ends there. */
function column(x: number, yBottom: number, w: number, h: number, roundTop: boolean): string {
  const r = roundTop ? Math.min(4, w / 2, h) : 0
  const top = yBottom - h

  return `M${x},${yBottom}V${top + r}Q${x},${top} ${x + r},${top}H${x + w - r}Q${x + w},${top} ${x + w},${top + r}V${yBottom}Z`
}

type HeatMode = 'published' | 'engagement'

/**
 * When posts go out (weekday by hour in the workspace's zone) and, where platforms report numbers, when they do
 * best. A real table: every cell has its row and column header and its number for screen readers.
 */
export function PostingTimes({ data }: { data: Analytics }) {
  const { cells, best, min_samples: minSamples } = data.posting_times
  const hasEngagement = cells.some((c) => c.samples > 0)
  const [mode, setMode] = useState<HeatMode>('published')
  const show: HeatMode = hasEngagement ? mode : 'published'
  const byKey = new Map(cells.map((c) => [c.weekday * 24 + c.hour, c]))
  const value = (c: TimeSlot | undefined) => (!c ? 0 : show === 'published' ? c.published : (c.avg_engagements ?? 0))
  const max = Math.max(0, ...cells.map((c) => value(c)))
  const busiest = cells.reduce<TimeSlot | null>((b, c) => (!b || c.published > b.published ? c : b), null)
  const hours = Array.from({ length: 24 }, (_, h) => h)

  return (
    <section
      aria-labelledby="times-heading"
      className={cn('grid gap-3 rounded-xl border bg-card p-4 shadow-xs', HEAT_COLORS)}
    >
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 id="times-heading" className="text-base font-semibold">
          Posting times
        </h2>
        {hasEngagement && (
          <ToggleGroup
            type="single"
            variant="outline"
            size="sm"
            value={show}
            aria-label="Show"
            onValueChange={(v) => v && setMode(v as HeatMode)}
          >
            <ToggleGroupItem value="published">Posts</ToggleGroupItem>
            <ToggleGroupItem value="engagement">Engagement</ToggleGroupItem>
          </ToggleGroup>
        )}
      </div>
      <p className="text-xs text-muted-foreground">
        {show === 'published'
          ? 'Channels published per weekday and hour, period total'
          : 'Average engagements per post with numbers, per weekday and hour'}{' '}
        ({data.range.timezone}).{' '}
        {busiest && busiest.published > 0
          ? `Most go out on ${WEEKDAYS[busiest.weekday]} at ${hourLabel(busiest.hour)}.`
          : 'Nothing went out in this range.'}
      </p>
      <div
        className="relative overflow-x-auto"
        tabIndex={0}
        role="region"
        aria-label="Posting times by weekday and hour"
      >
        <table
          className="w-full min-w-[34rem] table-fixed border-separate border-spacing-0.5 text-[11px]"
          data-testid="posting-heatmap"
        >
          <caption className="sr-only">
            {show === 'published' ? 'Channels published' : 'Average engagements'} by weekday (rows) and hour (columns)
          </caption>
          <thead>
            <tr>
              <th scope="col" className="relative w-9">
                <span className="sr-only">Weekday</span>
              </th>
              {hours.map((h) => (
                <th key={h} scope="col" className="relative text-left font-normal text-muted-foreground tabular-nums">
                  <span className={cn(h % 6 !== 0 && 'sr-only')}>{h % 6 === 0 ? h : hourLabel(h)}</span>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {WEEKDAYS.map((day, wd) => (
              <tr key={day}>
                <th scope="row" className="pr-1 text-left font-normal text-muted-foreground">
                  <abbr title={day} className="no-underline">
                    {WEEKDAYS_SHORT[wd]}
                  </abbr>
                </th>
                {hours.map((h) => {
                  const c = byKey.get(wd * 24 + h)
                  const v = value(c)
                  const step = v > 0 && max > 0 ? Math.min(5, Math.ceil((v / max) * 5)) : 0
                  const low = show === 'engagement' && !!c && c.samples > 0 && c.low_confidence
                  const text =
                    show === 'published'
                      ? `${formatCount(c?.published ?? 0)} published`
                      : c?.avg_engagements !== null && c?.avg_engagements !== undefined
                        ? `${c.avg_engagements} average engagements from ${c.samples} posts${low ? ', low confidence' : ''}`
                        : 'no numbers'

                  return (
                    <td
                      key={h}
                      title={`${day} ${hourLabel(h)}: ${text}`}
                      className={cn('relative h-5 rounded-[3px]', step === 0 && 'bg-muted')}
                      style={step ? { background: `var(--heat-${step})` } : undefined}
                    >
                      <span className="sr-only">{text}</span>
                      {low && (
                        <span className="absolute top-0.5 right-0.5 size-1 rounded-full bg-background" aria-hidden />
                      )}
                    </td>
                  )
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="flex flex-wrap items-center gap-x-6 gap-y-2 text-xs text-muted-foreground">
        <span className="flex items-center gap-1" aria-hidden>
          Fewer
          {[1, 2, 3, 4, 5].map((s) => (
            <span key={s} className="size-3 rounded-[3px]" style={{ background: `var(--heat-${s})` }} />
          ))}
          More
        </span>
        {show === 'engagement' && (
          <span className="flex items-center gap-1.5">
            <span className="relative size-3 rounded-[3px] bg-(--heat-3)" aria-hidden>
              <span className="absolute top-0.5 right-0.5 size-1 rounded-full bg-background" />
            </span>
            Fewer than {minSamples} posts with numbers: low confidence
          </span>
        )}
      </div>
      <BestTimes best={best} minSamples={minSamples} />
    </section>
  )
}

function BestTimes({ best, minSamples }: { best: TimeSlot[]; minSamples: number }) {
  if (best.length === 0) {
    return (
      <p className="text-sm text-muted-foreground" data-testid="best-times">
        Best times show up once posts in this range have numbers from their platforms (views, likes, replies). A
        published post&apos;s Statistics tab reads them; some platforms don&apos;t report numbers to staqly yet.
      </p>
    )
  }

  const confident = best.some((b) => !b.low_confidence)

  return (
    <div className="grid gap-1 text-sm" data-testid="best-times">
      <h3 className="font-medium">Best times by average engagement</h3>
      <ol className="grid gap-0.5">
        {best.map((b) => (
          <li key={`${b.weekday}-${b.hour}`}>
            {WEEKDAYS[b.weekday]} {hourLabel(b.hour)}: {b.avg_engagements} average engagements from {b.samples}{' '}
            {b.samples === 1 ? 'post' : 'posts'}
            {b.low_confidence && <span className="text-muted-foreground"> (low confidence)</span>}
          </li>
        ))}
      </ol>
      {!confident && (
        <p className="text-xs text-muted-foreground">
          Every slot has fewer than {minSamples} posts with numbers, so treat these as hints, not advice.
        </p>
      )}
    </div>
  )
}
