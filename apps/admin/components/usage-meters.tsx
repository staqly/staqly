import { formatBytes, formatDate, formatNumber } from '@/lib/format'
import { cn } from '@/lib/utils'

export type WorkspaceUsage = {
  plan: { limits: Record<string, number | boolean | undefined> }
  period: string
  usage: Record<string, number>
  plan_expires_at: string | null
  trial_ends_at: string | null
}

export function UsageMeters({ usage, trialEndsAt }: { usage: WorkspaceUsage; trialEndsAt?: string | null }) {
  const metrics = Object.entries(usage.usage ?? {})

  return (
    <div className="space-y-4">
      <dl className="grid gap-2 text-sm sm:grid-cols-3">
        <div>
          <dt className="text-muted-foreground">Period</dt>
          <dd>{usage.period || '—'}</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">Plan expires</dt>
          <dd>{formatDate(usage.plan_expires_at)}</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">Trial ends</dt>
          <dd>{formatDate(trialEndsAt ?? usage.trial_ends_at)}</dd>
        </div>
      </dl>
      {metrics.length === 0 && <p className="text-sm text-muted-foreground">No usage this period.</p>}
      <ul className="space-y-3">
        {metrics.map(([key, used]) => {
          const raw = usage.plan?.limits?.[key]
          // -1 is the API's "unlimited"; usage keys without a limit (seats) are never capped
          const limit = typeof raw === 'number' && raw >= 0 ? raw : null
          const fmt = key === 'storage_bytes' ? formatBytes : formatNumber
          const pct = limit && limit > 0 ? (used / limit) * 100 : 0

          return (
            <li key={key} className="space-y-1">
              <div className="flex justify-between gap-4 text-sm">
                <span className="capitalize">{key.replaceAll('_', ' ')}</span>
                <span className="tabular text-muted-foreground">
                  {fmt(used)} / {limit === null ? 'unlimited' : fmt(limit)}
                </span>
              </div>
              {limit !== null && (
                <div
                  role="progressbar"
                  aria-label={key.replaceAll('_', ' ')}
                  aria-valuemin={0}
                  aria-valuemax={limit}
                  aria-valuenow={Math.min(used, limit)}
                  className="h-2 overflow-hidden rounded-full bg-muted"
                >
                  <div
                    className={cn('h-full', pct >= 100 ? 'bg-red-500' : pct >= 80 ? 'bg-amber-500' : 'bg-primary')}
                    style={{ width: `${Math.min(pct, 100)}%` }}
                  />
                </div>
              )}
            </li>
          )
        })}
      </ul>
    </div>
  )
}
