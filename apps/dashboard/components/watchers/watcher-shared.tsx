'use client'

import { Bot, Globe, Rss } from 'lucide-react'

import { NativeSelect } from '@/components/common/native-select'
import type { Watcher, WatcherKind } from '@/lib/api'
import { useMode, usePlans, useUsage } from '@/lib/queries'
import { TONES, type Tone } from '@/lib/tones'
import { cn } from '@/lib/utils'
import { INTERVAL_OPTIONS, intervalOption, planForInterval } from '@/lib/watchers'

function sourceIcon(kind: WatcherKind | string) {
  return kind === 'url_diff'
    ? { Icon: Globe, cls: 'bg-emerald-500/12 text-emerald-700 dark:text-emerald-300' }
    : kind === 'rss'
      ? { Icon: Rss, cls: 'bg-amber-500/12 text-amber-700 dark:text-amber-300' }
      : { Icon: Bot, cls: 'bg-muted text-muted-foreground' }
}

export function SourceIcon({ kind, className }: { kind: string; className?: string }) {
  const { Icon, cls } = sourceIcon(kind)

  return (
    <span className={cn('flex size-9 shrink-0 items-center justify-center rounded-lg', cls, className)} aria-hidden>
      <Icon className="size-[18px]" />
    </span>
  )
}

export const KIND_LABELS: Record<string, string> = { rss: 'Feed', url_diff: 'Page changes' }

const STATUS: Record<Watcher['status'], { label: string; tone: Tone }> = {
  active: { label: 'Active', tone: 'success' },
  degraded: { label: 'Degraded', tone: 'warning' },
  paused: { label: 'Paused', tone: 'neutral' },
}

export function WatcherStatusBadge({ status }: { status: Watcher['status'] }) {
  const m = STATUS[status] ?? STATUS.paused

  return (
    <span
      data-testid="watcher-status"
      data-status={status}
      className={cn(
        'inline-flex h-5 shrink-0 items-center gap-1.5 rounded-md px-1.5 text-[11px] font-medium whitespace-nowrap ring-1 ring-inset',
        TONES[m.tone].badge,
      )}
    >
      <span className={cn('size-1.5 rounded-full', TONES[m.tone].dot)} />
      {m.label}
    </span>
  )
}

/** Plan's fastest allowed interval (seconds). Local installs: every minute. */
export function useMinInterval() {
  const usage = useUsage()
  const { isLocal } = useMode()
  const limit = usage.data?.plan.limits.min_watcher_interval_seconds

  return { minSeconds: isLocal ? Math.min(limit ?? 60, 60) : (limit ?? 900) }
}

/** Interval picker; options faster than the plan allows are disabled with the plan that unlocks them. */
export function IntervalSelect({
  id,
  value,
  onChange,
  className,
  invalid,
}: {
  id?: string
  value: number
  onChange: (seconds: number) => void
  className?: string
  invalid?: boolean
}) {
  const { isLocal } = useMode()
  const plans = usePlans({ enabled: !isLocal })
  const { minSeconds } = useMinInterval()
  const options = INTERVAL_OPTIONS.some((o) => o.seconds === value)
    ? INTERVAL_OPTIONS
    : [...INTERVAL_OPTIONS, intervalOption(value)].toSorted((a, b) => a.seconds - b.seconds)
  const locked = options.filter((o) => o.seconds < minSeconds)
  const unlock = locked.length ? planForInterval(locked[locked.length - 1].seconds, plans.data) : undefined

  return (
    <div className={cn('grid gap-1.5', className)}>
      <NativeSelect
        id={id}
        value={String(value)}
        onChange={(e) => onChange(Number(e.target.value))}
        aria-label={id ? undefined : 'Check interval'}
        aria-invalid={invalid || undefined}
      >
        {options.map((o) => {
          const disabled = o.seconds < minSeconds
          const plan = disabled ? planForInterval(o.seconds, plans.data) : undefined

          return (
            <option key={o.seconds} value={o.seconds} disabled={disabled}>
              {o.label}
              {disabled ? ` — ${plan ? `${plan.name} plan` : 'upgrade'}` : ''}
            </option>
          )
        })}
      </NativeSelect>
      {locked.length > 0 && (
        <p className="text-xs text-muted-foreground" data-testid="interval-hint">
          Faster checks need {unlock ? `the ${unlock.name} plan` : 'a bigger plan'}.
        </p>
      )}
    </div>
  )
}
