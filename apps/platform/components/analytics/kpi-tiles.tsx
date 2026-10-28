import { ArrowDownRight, ArrowUpRight, Minus } from 'lucide-react'

import { formatDateTime } from '@/lib/format'
import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

import { formatCount, formatDelay, formatRate, type Analytics } from './analytics-data'

interface Delta {
  /** The change against the previous period, already formatted with its sign ("+12", "-3.5 pts"). */
  text: string
  direction: 'up' | 'down' | 'flat'
  /** Whether going up is good (published) or bad (failures, delay). */
  upIsGood: boolean
}

function countDelta(now: number, before: number, upIsGood: boolean): Delta {
  const d = now - before

  return {
    text: d === 0 ? 'no change' : `${d > 0 ? '+' : ''}${formatCount(d)}`,
    direction: d > 0 ? 'up' : d < 0 ? 'down' : 'flat',
    upIsGood,
  }
}

/**
 * The headline numbers. Each says what kind of figure it is (a period total, an average, right now) and compares
 * with the previous period of the same length only when that period had something to compare with.
 */
export function KpiTiles({ data }: { data: Analytics }) {
  const { totals: t, previous: p, queue } = data
  const prevFinished = p.targets_published + p.targets_failed
  const prevLabel = `vs previous ${data.range.days} days`
  const nextAt = queue.next_publish_at ? formatDateTime(queue.next_publish_at) : null

  let rateDelta: Delta | null = null

  if (t.success_rate !== null && p.success_rate !== null) {
    const pts = Math.round((t.success_rate - p.success_rate) * 1000) / 10

    rateDelta = {
      text: pts === 0 ? 'no change' : `${pts > 0 ? '+' : ''}${pts} pts`,
      direction: pts > 0 ? 'up' : pts < 0 ? 'down' : 'flat',
      upIsGood: true,
    }
  }

  let delayDelta: Delta | null = null

  if (t.delay_avg_seconds !== null && p.delay_avg_seconds !== null) {
    const d = t.delay_avg_seconds - p.delay_avg_seconds

    delayDelta = {
      text: Math.abs(d) < 1 ? 'no change' : `${d > 0 ? '+' : '-'}${formatDelay(Math.abs(d))}`,
      direction: Math.abs(d) < 1 ? 'flat' : d > 0 ? 'up' : 'down',
      upIsGood: false,
    }
  }

  return (
    <section aria-labelledby="kpi-heading">
      <h2 id="kpi-heading" className="sr-only">
        Summary
      </h2>
      <ul className="grid grid-cols-2 gap-3 md:grid-cols-3 lg:grid-cols-5" data-testid="kpi-tiles">
        <Tile
          label="Posts published"
          kind="Period total"
          value={formatCount(t.posts_published)}
          detail={`${formatCount(t.targets_published)} on channels`}
          delta={
            prevFinished > 0 || p.posts_published > 0 ? countDelta(t.posts_published, p.posts_published, true) : null
          }
          prevLabel={prevLabel}
        />
        <Tile
          label="Success rate"
          kind="Period average"
          value={formatRate(t.success_rate)}
          detail={t.success_rate === null ? 'Nothing finished yet' : 'of channels that finished'}
          delta={rateDelta}
          prevLabel={prevLabel}
        />
        <Tile
          label="Failures"
          kind="Period total"
          value={formatCount(t.targets_failed)}
          detail={t.retried ? `${formatCount(t.retried)} needed a retry` : 'No retries needed'}
          delta={prevFinished > 0 ? countDelta(t.targets_failed, p.targets_failed, false) : null}
          prevLabel={prevLabel}
        />
        <Tile
          label="Upcoming"
          kind="Right now"
          value={formatCount(queue.scheduled)}
          detail={nextAt ? `Next ${nextAt}` : `${formatCount(queue.drafts)} drafts waiting`}
          delta={null}
          prevLabel={prevLabel}
        />
        <Tile
          label="Average delay"
          kind="Period average"
          value={formatDelay(t.delay_avg_seconds)}
          detail={
            t.delay_p90_seconds === null
              ? 'Nothing scheduled went out'
              : `9 in 10 within ${formatDelay(t.delay_p90_seconds)}`
          }
          delta={delayDelta}
          prevLabel={prevLabel}
        />
      </ul>
    </section>
  )
}

function Tile({
  label,
  kind,
  value,
  detail,
  delta,
  prevLabel,
}: {
  label: string
  kind: string
  value: string
  detail: string
  delta: Delta | null
  prevLabel: string
}) {
  const good = delta && delta.direction !== 'flat' && (delta.direction === 'up') === delta.upIsGood
  const Icon = !delta || delta.direction === 'flat' ? Minus : delta.direction === 'up' ? ArrowUpRight : ArrowDownRight

  return (
    <li className="grid content-start gap-1 rounded-xl border bg-card p-4 shadow-xs" data-testid="kpi-tile">
      <div className="grid">
        <span className="text-sm font-medium">{label}</span>
        <span className="text-xs text-muted-foreground">{kind}</span>
      </div>
      <span className="text-2xl font-semibold tracking-tight">{value}</span>
      <span className="text-xs text-muted-foreground wrap-anywhere">{detail}</span>
      {delta && (
        <span
          className={cn(
            'flex items-center gap-1 text-xs',
            delta.direction === 'flat' ? 'text-muted-foreground' : good ? TONES.success.text : TONES.danger.text,
          )}
          data-testid="kpi-delta"
        >
          <Icon className="size-3.5 shrink-0" aria-hidden />
          <span>
            {delta.text} <span className="text-muted-foreground">{prevLabel}</span>
          </span>
        </span>
      )}
    </li>
  )
}
