'use client'

import Link from 'next/link'

import { UNLIMITED, type WorkspaceUsage } from '@/lib/api'
import { useMode, useUsage } from '@/lib/queries'
import { cn } from '@/lib/utils'

type CountKey = 'rules' | 'watchers' | 'webhooks' | 'api_keys' | 'notification_channels'

/** Compact "3 / 5 rules" plan usage indicator for page headers; links to Plan & usage. */
export function UsageChip({ metric, noun, className }: { metric: CountKey; noun: string; className?: string }) {
  const usage = useUsage()
  const { isLocal } = useMode()
  const data: WorkspaceUsage | undefined = usage.data

  if (!data) {
    return null
  }

  const used = data.usage[metric] ?? 0
  const max = isLocal || data.plan.id === 'unlocked' ? UNLIMITED : (data.plan.limits[metric] ?? UNLIMITED)
  const unlimited = max === UNLIMITED || max < 0
  const pct = unlimited ? 0 : max === 0 ? 100 : Math.min(100, (used / max) * 100)
  const level = unlimited ? 'ok' : used >= max ? 'full' : pct > 80 ? 'warn' : 'ok'

  return (
    <Link
      href="/settings/plan"
      data-testid={`usage-chip-${metric}`}
      data-level={level}
      title="Plan & usage"
      className={cn(
        'inline-flex h-8 items-center gap-2 rounded-lg border bg-card px-2.5 text-xs text-muted-foreground transition-colors hover:bg-muted pointer-coarse:h-10',
        level === 'full' && 'border-rose-500/40 text-rose-700 dark:text-rose-300',
        level === 'warn' && 'border-amber-500/40 text-amber-700 dark:text-amber-300',
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
            className={cn(
              'block h-full rounded-full',
              level === 'full' ? 'bg-rose-500' : level === 'warn' ? 'bg-amber-500' : 'bg-emerald-500',
            )}
            style={{ width: `${Math.max(pct, used ? 6 : 0)}%` }}
          />
        </span>
      )}
      {unlimited && <span className="text-muted-foreground/70">· no limit</span>}
    </Link>
  )
}
