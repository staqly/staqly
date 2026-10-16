'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import {
  BellRing,
  CheckCheck,
  Gauge,
  HardDrive,
  Infinity as InfinityIcon,
  KeyRound,
  Layers,
  Radar,
  Sparkles,
  Webhook,
} from 'lucide-react'
import { useEffect, useState } from 'react'
import { toast } from 'sonner'

import { ErrorState } from '@/components/common/error-state'
import { PageContainer } from '@/components/common/page-header'
import { PlanBadge } from '@/components/plans/plan-badge'
import { EVERY_PLAN, PlanGrid } from '@/components/plans/plan-cards'
import { SettingsHeader } from '@/components/settings/settings-header'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { api, UNLIMITED, type Billing, type WorkspaceUsage } from '@/lib/api'
import { formatBytes, formatInterval } from '@/lib/format'
import { toastApiError } from '@/lib/notify'
import { qk, useBilling, useMode, usePlans, useUsage } from '@/lib/queries'
import { TONES } from '@/lib/tones'
import { meterTone, usageMeter } from '@/lib/usage'
import { cn } from '@/lib/utils'

import { SettingsNav } from './settings-nav'

type MeterKey = keyof WorkspaceUsage['usage'] & keyof WorkspaceUsage['plan']['limits']

const METERS: { key: MeterKey; label: string; Icon: typeof Layers; bytes?: boolean; hint?: string }[] = [
  {
    key: 'decisions_per_month',
    label: 'Approvals this month',
    Icon: CheckCheck,
    hint: 'Approve and reject · resets on the 1st',
  },
  { key: 'items_per_month', label: 'Items this month', Icon: Layers },
  { key: 'webhooks', label: 'Webhooks', Icon: Webhook },
  { key: 'api_keys', label: 'API keys', Icon: KeyRound },
  { key: 'storage_bytes', label: 'Storage', Icon: HardDrive, bytes: true },
  { key: 'watchers', label: 'Watchers', Icon: Radar, hint: 'Active (not paused)' },
  { key: 'notification_channels', label: 'Notification channels', Icon: BellRing },
]

function UsageMeter({
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
  const { level, pct } = usageMeter(used, max)
  const tone = meterTone(level)
  const fmt = (n: number) => (bytes ? formatBytes(n) : n.toLocaleString('en-US'))

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
        {level === 'full' && <span className={cn('text-xs font-medium', tone.text)}>Limit reached</span>}
        {level === 'warn' && <span className={cn('text-xs font-medium', tone.text)}>Almost full</span>}
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
            className={cn('h-full rounded-full transition-[width] duration-500', tone.fill)}
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

/** Trial / paid period end, and the read-only state once it ran out unpaid. */
function PlanPeriod({
  usage,
  billing,
  switching,
  onSwitchToFree,
  onUpgrade,
  onManage,
}: {
  usage: WorkspaceUsage
  billing?: Billing
  switching: boolean
  onSwitchToFree: () => void
  onUpgrade?: () => void
  onManage?: () => void
}) {
  const [now] = useState(() => Date.now())

  if (!usage.plan_expires_at && !usage.suspended) {
    return null
  }

  const subscribed = ['active', 'trialing', 'past_due'].includes(billing?.subscription_status ?? '')

  const ends = usage.plan_expires_at ? new Date(usage.plan_expires_at) : undefined
  const days = ends ? Math.max(0, Math.ceil((ends.getTime() - now) / 86_400_000)) : 0
  const date = ends?.toLocaleDateString(undefined, { day: 'numeric', month: 'long', year: 'numeric' })

  return (
    <div
      role={usage.suspended ? 'alert' : 'status'}
      data-testid="plan-period"
      className={cn(
        'flex flex-col gap-3 rounded-2xl border p-4 sm:flex-row sm:items-center sm:justify-between sm:p-5',
        usage.suspended ? 'border-destructive/40 bg-destructive/5' : 'border-brand/30 bg-brand-soft/40',
      )}
    >
      <div className="text-sm">
        <p className="font-semibold">
          {usage.suspended
            ? 'Your Pro plan has ended — this workspace is read-only'
            : subscribed
              ? billing?.subscription_status === 'past_due'
                ? `Payment failed — Pro stays active until ${date}`
                : `Pro: paid through ${date}`
              : `Pro trial: ${days} ${days === 1 ? 'day' : 'days'} left`}
        </p>
        <p className="mt-0.5 text-muted-foreground">
          {usage.suspended
            ? 'You can still read and export everything. Upgrade, or switch to Free to keep working within its limits.'
            : subscribed
              ? 'Change the payment method, download invoices or cancel in the billing portal.'
              : `Everything is unlimited until ${date}. Afterwards upgrade, or switch to Free; unpaid workspaces become read-only.`}
        </p>
      </div>
      <div className="flex flex-wrap gap-2 sm:self-center">
        {subscribed && onManage ? (
          <Button variant="outline" onClick={onManage}>
            Manage billing
          </Button>
        ) : (
          <>
            {onUpgrade && (
              <Button variant={usage.suspended ? 'default' : 'outline'} onClick={onUpgrade}>
                Upgrade to Pro
              </Button>
            )}
            <Button
              variant={usage.suspended && !onUpgrade ? 'default' : 'outline'}
              disabled={switching}
              onClick={onSwitchToFree}
            >
              Switch to Free
            </Button>
          </>
        )}
      </div>
    </div>
  )
}

export function PlanUsagePage() {
  const { isLocal } = useMode()
  const usage = useUsage()
  const plans = usePlans({ enabled: !isLocal })
  const plan = usage.data?.plan
  const unlocked = isLocal || plan?.id === 'unlocked'
  const qc = useQueryClient()
  const toFree = useMutation({
    mutationFn: () => api.switchToFree(),
    onSuccess: (u) => {
      qc.setQueryData(qk.usage, u)
      toast.success('Switched to the Free plan')
    },
    onError: (e) => toastApiError(e),
  })
  const billing = useBilling({ enabled: !isLocal })
  const canPay = !!billing.data?.enabled
  const currentPrice = plan?.price_usd_month ?? 0
  const openStripe = useMutation({
    mutationFn: (to: 'checkout' | 'portal') => (to === 'checkout' ? api.billingCheckout() : api.billingPortal()),
    onSuccess: ({ url }) => window.location.assign(url),
    onError: (e) => toastApiError(e),
  })

  // Back from Stripe Checkout: the plan changes when Stripe's webhook arrives.
  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    const result = params.get('checkout')

    if (!result) {
      return
    }

    if (result === 'success') {
      toast.success('Payment received', { description: 'Pro is active in a moment.' })
      setTimeout(() => {
        void qc.invalidateQueries({ queryKey: qk.usage })
        void qc.invalidateQueries({ queryKey: qk.billing })
      }, 3000)
    }

    window.history.replaceState(null, '', window.location.pathname)
  }, [qc])

  return (
    <PageContainer>
      <SettingsHeader />
      <SettingsNav className="mt-4" />

      <section className="mt-6 grid gap-4" aria-labelledby="plan-heading">
        {!unlocked && usage.data && (
          <PlanPeriod
            usage={usage.data}
            billing={billing.data}
            switching={toFree.isPending}
            onSwitchToFree={() => toFree.mutate()}
            onUpgrade={canPay ? () => openStripe.mutate('checkout') : undefined}
            onManage={billing.data?.has_customer ? () => openStripe.mutate('portal') : undefined}
          />
        )}
        <div className="flex flex-col gap-3 rounded-2xl border bg-card p-4 sm:flex-row sm:items-center sm:justify-between sm:p-5">
          <div className="flex items-start gap-3">
            <span
              className={cn(
                'flex size-10 shrink-0 items-center justify-center rounded-xl',
                unlocked ? TONES.success.chip : 'bg-brand-soft text-brand',
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
          {!unlocked && plan && plan.price_usd_month === 0 && (
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
            {/* Local installs are keyless: API keys show only once someone made one (for a remote agent). */}
            {METERS.filter((m) => !(isLocal && m.key === 'api_keys' && !(usage.data?.usage.api_keys ?? 0))).map((m) =>
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
              {EVERY_PLAN}.{' '}
              {canPay
                ? 'Pay by card through Stripe; cancel any time in the billing portal.'
                : 'Online payments are not enabled on this instance — plan changes are applied by the server operator.'}
            </p>
          </div>
          {plans.error ? (
            <ErrorState title="Could not load plans" error={plans.error} onRetry={() => plans.refetch()} />
          ) : (
            <PlanGrid
              plans={plans.data}
              loading={plans.isLoading}
              currentPlanId={plan?.id}
              renderCta={(p, current) => {
                // A plain condition: `??` inside the nested ternary makes the React Compiler skip this page.
                const cheaper = (p.price_usd_month ?? 0) < currentPrice

                return current ? (
                  <Button variant="outline" disabled className="w-full">
                    Your current plan
                  </Button>
                ) : cheaper ? (
                  <Button
                    variant="outline"
                    className="w-full"
                    disabled={toFree.isPending}
                    onClick={() => toFree.mutate()}
                  >
                    Switch to {p.name}
                  </Button>
                ) : canPay ? (
                  <Button
                    className={cn('w-full', p.highlight && 'bg-brand-gradient text-[#052817]')}
                    disabled={openStripe.isPending}
                    onClick={() => openStripe.mutate('checkout')}
                  >
                    Upgrade to {p.name}
                  </Button>
                ) : (
                  <Button
                    disabled
                    className={cn('w-full', p.highlight && 'bg-brand-gradient text-[#052817] disabled:opacity-90')}
                    title="Online payments are not enabled on this instance"
                  >
                    Upgrade — ask the operator
                  </Button>
                )
              }}
            />
          )}
        </section>
      )}
    </PageContainer>
  )
}
