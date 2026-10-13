'use client'

import {
  BellRing,
  Gauge,
  HardDrive,
  Infinity as InfinityIcon,
  KeyRound,
  Layers,
  ListChecks,
  Radar,
  Sparkles,
  Webhook,
} from 'lucide-react'

import { ErrorState } from '@/components/common/error-state'
import { PageContainer, PageHeader } from '@/components/common/page-header'
import { PlanBadge } from '@/components/plans/plan-badge'
import { EVERY_PLAN, PlanGrid } from '@/components/plans/plan-cards'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { UNLIMITED, type WorkspaceUsage } from '@/lib/api'
import { formatBytes, formatInterval } from '@/lib/format'
import { useMode, usePlans, useUsage } from '@/lib/queries'
import { cn } from '@/lib/utils'

import { SettingsNav } from './settings-nav'

type MeterKey = keyof WorkspaceUsage['usage'] & keyof WorkspaceUsage['plan']['limits']

const METERS: { key: MeterKey; label: string; Icon: typeof Layers; bytes?: boolean; hint?: string }[] = [
  { key: 'items_per_month', label: 'Items this month', Icon: Layers, hint: 'Resets on the 1st' },
  { key: 'webhooks', label: 'Webhooks', Icon: Webhook },
  { key: 'api_keys', label: 'API keys', Icon: KeyRound },
  { key: 'storage_bytes', label: 'Storage', Icon: HardDrive, bytes: true },
  { key: 'rules', label: 'Rules', Icon: ListChecks },
  { key: 'watchers', label: 'Watchers', Icon: Radar, hint: 'Active (not paused)' },
  { key: 'notification_channels', label: 'Notification channels', Icon: BellRing },
]

export function meterLevel(used: number, max: number): 'ok' | 'warn' | 'full' | 'unlimited' {
  if (max === UNLIMITED || max < 0) {
    return 'unlimited'
  }

  if (max === 0 || used >= max) {
    return 'full'
  }

  return used / max > 0.8 ? 'warn' : 'ok'
}

export function UsageMeter({
  label,
  used,
  max,
  bytes,
  hint,
  Icon,
}: {
  label: string
  used: number
  max: number
  bytes?: boolean
  hint?: string
  Icon?: typeof Layers
}) {
  const level = meterLevel(used, max)
  const fmt = (n: number) => (bytes ? formatBytes(n) : n.toLocaleString('en-US'))
  const pct = level === 'unlimited' ? 0 : max === 0 ? 100 : Math.min(100, (used / max) * 100)

  return (
    <div
      className="grid content-start gap-2.5 rounded-xl border bg-card p-3.5 sm:p-4"
      data-testid={`meter-${label}`}
      data-level={level}
    >
      <div className="flex min-h-5 flex-wrap items-center justify-between gap-x-2 gap-y-1 text-[13px] sm:text-sm">
        <span className="inline-flex items-center gap-2 font-medium">
          {Icon && <Icon className="size-4 shrink-0 text-muted-foreground" />}
          {label}
        </span>
        {level === 'full' && (
          <span className="text-xs font-medium text-rose-600 dark:text-rose-400">Limit reached</span>
        )}
        {level === 'warn' && (
          <span className="text-xs font-medium text-amber-600 dark:text-amber-400">Almost full</span>
        )}
      </div>
      <div className="flex items-baseline gap-1.5">
        <span className="text-xl font-semibold tracking-tight tabular-nums sm:text-2xl">{fmt(used)}</span>
        <span className="text-sm text-muted-foreground tabular-nums">
          /{' '}
          {level === 'unlimited' ? (
            <span aria-label="unlimited" title="Unlimited">
              ∞
            </span>
          ) : (
            fmt(max)
          )}
        </span>
      </div>
      <div
        role="progressbar"
        aria-label={label}
        aria-valuemin={0}
        aria-valuemax={level === 'unlimited' ? undefined : max}
        aria-valuenow={used}
        aria-valuetext={level === 'unlimited' ? `${fmt(used)} of unlimited` : `${fmt(used)} of ${fmt(max)}`}
        className="h-2 overflow-hidden rounded-full bg-muted"
      >
        {level === 'unlimited' ? (
          <div className="h-full w-full bg-[repeating-linear-gradient(-45deg,transparent_0_6px,color-mix(in_oklch,var(--foreground)_7%,transparent)_6px_12px)]" />
        ) : (
          <div
            className={cn(
              'h-full rounded-full transition-[width] duration-500',
              level === 'full' ? 'bg-rose-500' : level === 'warn' ? 'bg-amber-500' : 'bg-emerald-500',
            )}
            style={{ width: `${Math.max(pct, used > 0 ? 2 : 0)}%` }}
          />
        )}
      </div>
      {hint && <div className="text-xs text-muted-foreground">{hint}</div>}
    </div>
  )
}

function periodLabel(period?: string) {
  if (!period) {
    return ''
  }

  const [y, m] = period.split('-').map(Number)

  if (!y || !m) {
    return period
  }

  return new Date(Date.UTC(y, m - 1, 1)).toLocaleString('en-US', { month: 'long', year: 'numeric', timeZone: 'UTC' })
}

export function PlanUsagePage() {
  const { isLocal } = useMode()
  const usage = useUsage()
  const plans = usePlans({ enabled: !isLocal })
  const plan = usage.data?.plan
  const unlocked = isLocal || plan?.id === 'unlocked'

  return (
    <PageContainer className="max-w-5xl">
      <PageHeader
        title="Settings"
        description={isLocal ? 'Workspace, appearance and backups.' : 'Workspace, appearance, security and backups.'}
      />
      <SettingsNav className="mt-4" />

      <section className="mt-6 grid gap-4" aria-labelledby="plan-heading">
        <div className="flex flex-col gap-3 rounded-2xl border bg-card p-4 sm:flex-row sm:items-center sm:justify-between sm:p-5">
          <div className="flex items-start gap-3">
            <span
              className={cn(
                'flex size-10 shrink-0 items-center justify-center rounded-xl',
                unlocked ? 'bg-emerald-500/12 text-emerald-600 dark:text-emerald-400' : 'bg-brand-soft text-brand',
              )}
            >
              {unlocked ? <InfinityIcon className="size-5" /> : <Gauge className="size-5" />}
            </span>
            <div className="min-w-0">
              <h2
                id="plan-heading"
                className="flex flex-wrap items-center gap-2 text-base font-semibold tracking-tight"
              >
                {usage.isLoading ? (
                  <Skeleton className="h-5 w-32" />
                ) : unlocked ? (
                  <>
                    Unlocked <PlanBadge plan="unlocked" />
                  </>
                ) : (
                  <>
                    {plan?.name ?? 'Plan'} plan <PlanBadge plan={plan?.id} />
                  </>
                )}
              </h2>
              <p className="mt-0.5 text-sm text-muted-foreground">
                {unlocked
                  ? 'Unlocked — local install, no limits.'
                  : plan
                    ? `$${plan.price_usd_month}/mo · usage for ${periodLabel(usage.data?.period)}`
                    : "Your workspace plan and this month's usage."}
              </p>
            </div>
          </div>
          {!unlocked && plan && plan.id !== 'team' && (
            <Button asChild variant="outline" className="sm:self-center">
              <a href="#plans">
                <Sparkles /> Compare plans
              </a>
            </Button>
          )}
        </div>

        {usage.error ? (
          <ErrorState title="Could not load usage" error={usage.error} onRetry={() => usage.refetch()} />
        ) : (
          <div className="grid grid-cols-2 gap-3 lg:grid-cols-3" data-testid="usage-meters">
            {METERS.map((m) =>
              usage.isLoading || !usage.data ? (
                <Skeleton key={m.key} className="h-32 rounded-xl" />
              ) : (
                <UsageMeter
                  key={m.key}
                  label={m.label}
                  Icon={m.Icon}
                  bytes={m.bytes}
                  hint={
                    m.key === 'watchers' && usage.data.plan?.limits?.min_watcher_interval_seconds
                      ? `${unlocked ? 'Checks' : 'Active only · checks'} every ${formatInterval(usage.data.plan.limits.min_watcher_interval_seconds)} at most`
                      : unlocked
                        ? undefined
                        : m.hint
                  }
                  used={usage.data.usage?.[m.key] ?? 0}
                  max={unlocked ? UNLIMITED : (usage.data.plan?.limits?.[m.key] ?? UNLIMITED)}
                />
              ),
            )}
          </div>
        )}
        {!unlocked && plan && plan.limits.max_upload_bytes > 0 && (
          <p className="text-xs text-muted-foreground">
            Files up to {formatBytes(plan.limits.max_upload_bytes)} per upload on the {plan.name} plan.
          </p>
        )}
      </section>

      {!isLocal && (
        <section id="plans" className="mt-10 grid scroll-mt-6 gap-4" aria-labelledby="plans-heading">
          <div className="flex flex-col gap-1">
            <h2 id="plans-heading" className="text-lg font-semibold tracking-tight">
              Plans
            </h2>
            <p className="text-sm text-muted-foreground">
              {EVERY_PLAN}. Payments aren&apos;t live yet — plan changes are applied by the server operator.
            </p>
          </div>
          {plans.error ? (
            <ErrorState title="Could not load plans" error={plans.error} onRetry={() => plans.refetch()} />
          ) : (
            <PlanGrid
              plans={plans.data}
              loading={plans.isLoading}
              currentPlanId={plan?.id}
              renderCta={(p, current) =>
                current ? (
                  <Button variant="outline" disabled className="w-full">
                    Your current plan
                  </Button>
                ) : (p.price_usd_month ?? 0) < (plan?.price_usd_month ?? 0) ? (
                  <Button variant="ghost" disabled className="w-full">
                    Included in your plan
                  </Button>
                ) : (
                  <Button
                    disabled
                    className={cn(
                      'w-full',
                      p.id === 'team' && 'bg-white text-zinc-900 hover:bg-white/90 disabled:opacity-80',
                      p.highlight && 'bg-brand-gradient text-[#052817] disabled:opacity-90',
                    )}
                    title="Online payments are not available yet"
                  >
                    Upgrade — coming soon
                  </Button>
                )
              }
            />
          )}
        </section>
      )}
    </PageContainer>
  )
}
