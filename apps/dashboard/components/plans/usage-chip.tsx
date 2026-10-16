'use client'

import Link from 'next/link'

import { UNLIMITED, type WorkspaceUsage } from '@/lib/api'
import { useMode, useUsage } from '@/lib/queries'
import { meterTone, usageMeter } from '@/lib/usage'
import { cn } from '@/lib/utils'

type CountKey = 'watchers' | 'webhooks' | 'api_keys' | 'notification_channels'

/** Compact "1 / 3 watchers" plan usage indicator for page headers; links to Plan & usage. */
export function UsageChip({ metric, noun, className }: { metric: CountKey; noun: string; className?: string }) {
  const usage = useUsage()
  const { isLocal } = useMode()
  const data: WorkspaceUsage | undefined = usage.data

  if (!data) {
    return null
  }

  const used = data.usage[metric] ?? 0
  const max = isLocal || data.plan.id === 'unlocked' ? UNLIMITED : (data.plan.limits[metric] ?? UNLIMITED)
  const { level, pct } = usageMeter(used, max)
  const unlimited = level === 'unlimited'
  const tone = meterTone(level)

  return (
    <Link
      href="/settings/plan"
      data-testid={`usage-chip-${metric}`}
      data-level={unlimited ? 'ok' : level}
      title="Plan & usage"
      className={cn(
        'inline-flex h-6 items-center gap-1.5 rounded-md border bg-card px-2 text-[11px] text-muted-foreground transition-colors hover:bg-muted',
        (level === 'full' || level === 'warn') && [tone.border, tone.text],
        className,
      )}
    >
      <span className="tabular-nums">
        <span className="font-semibold text-foreground">{used.toLocaleString('en-US')}</span>
        {unlimited ? '' : ` / ${max.toLocaleString('en-US')}`} {noun}
      </span>
      {!unlimited && (
        <span className="hidden h-1.5 w-10 overflow-hidden rounded-full bg-muted sm:block" aria-hidden>
          <span
            className={cn('block h-full rounded-full', tone.fill)}
            style={{ width: `${Math.max(pct, used ? 6 : 0)}%` }}
          />
        </span>
      )}
      {unlimited && <span className="text-muted-foreground/70">· no limit</span>}
    </Link>
  )
}
