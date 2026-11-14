'use client'

import Link from 'next/link'

import { UNLIMITED, type WorkspaceUsage } from '@/lib/api'
import { useMode, useUsage } from '@/lib/queries'
import { meterTone, usageMeter } from '@/lib/usage'
import { cn } from '@/lib/utils'

type CountKey = 'channels' | 'webhooks' | 'api_keys' | 'alerts'

/** Compact "1 / 3 channels" plan usage indicator for page headers; links to Plan & usage. `noun` is plural. */
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
        // touch: a 44 px tall hit area around the 24 px chip
        "relative inline-flex h-6 items-center gap-1.5 rounded-md border bg-card px-2 text-[11px] text-muted-foreground transition-colors after:absolute after:inset-x-0 after:-inset-y-2.5 hover:bg-muted pointer-coarse:after:content-['']",
        (level === 'full' || level === 'warn') && [tone.border, tone.text],
        className,
      )}
    >
      <span className="tabular-nums">
        <span className="font-semibold text-foreground">{used.toLocaleString('en-US')}</span>
        {unlimited ? '' : ` / ${max.toLocaleString('en-US')}`} {unlimited && used === 1 ? noun.replace(/s$/, '') : noun}
      </span>
      {!unlimited && (
        <span className="hidden h-1.5 w-10 overflow-hidden rounded-full bg-muted sm:block" aria-hidden>
          <span
            className={cn('block h-full rounded-full', tone.fill)}
            style={{ width: `${Math.max(pct, used ? 6 : 0)}%` }}
          />
        </span>
      )}
      {unlimited && <span>· no limit</span>}
    </Link>
  )
}
