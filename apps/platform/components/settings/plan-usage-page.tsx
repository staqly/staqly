'use client'

import {
  Send,
  BellRing,
  Gauge,
  HardDrive,
  Infinity as InfinityIcon,
  KeyRound,
  Layers,
  Share2,
  Sparkles,
  Webhook,
} from 'lucide-react'
import Link from 'next/link'
import { useState } from 'react'

import { bonusHint } from '@/components/billing/addons'
import { ErrorState } from '@/components/common/error-state'
import { PageContainer } from '@/components/common/page-header'
import { PlanBadge } from '@/components/plans/plan-badge'
import { money } from '@/components/plans/plan-cards'
import { usePlanActions } from '@/components/plans/plans-page'
import { SettingsHeader } from '@/components/settings/settings-header'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { POST_STATUSES, UNLIMITED, type Billing, type WorkspaceUsage } from '@/lib/api'
import { formatBytes } from '@/lib/format'
import { LIVE_SUBSCRIPTION } from '@/lib/plans'
import { useBilling, useMode, useUsage } from '@/lib/queries'
import { POST_STATUS_META } from '@/lib/status'
import { TONES } from '@/lib/tones'
import { meterTone, usageMeter } from '@/lib/usage'
import { cn } from '@/lib/utils'

import { SettingsNav } from './settings-nav'

type MeterKey = keyof WorkspaceUsage['usage'] & keyof WorkspaceUsage['plan']['limits']

export const METERS: { key: MeterKey; label: string; Icon: typeof Layers; bytes?: boolean; hint?: string }[] = [
  { key: 'posts_per_month', label: 'Posts this month', Icon: Layers, hint: 'Created · resets on the 1st' },
  { key: 'channels', label: 'Channels', Icon: Share2 },
  { key: 'webhooks', label: 'Webhooks', Icon: Webhook },
  { key: 'api_keys', label: 'API keys', Icon: KeyRound },
  { key: 'storage_bytes', label: 'Storage', Icon: HardDrive, bytes: true },
  { key: 'alerts', label: 'Alerts', Icon: BellRing },
  {
    key: 'x_posts_per_period',
    label: 'X posts this month',
    Icon: Send,
    hint: 'Included in the plan; bought posts come on top',
  },
]

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

/** Shown even at 0; the others only when a post of this period has them. */
const MAIN_STATUSES = new Set(['scheduled', 'published', 'failed'])

/** Posts created this period by current status, and the webhook delivery backlog. */
function PeriodActivity({ usage }: { usage: WorkspaceUsage }) {
  const counts = usage.posts_by_status
  const created = Object.values(counts).reduce((a, b) => a + b, 0)
  const statuses = POST_STATUSES.filter((s) => MAIN_STATUSES.has(s) || (counts[s] ?? 0) > 0)
  const d = usage.webhook_deliveries
  const rows = [
    { label: 'Queued', value: d.queued, hint: 'Waiting for the first attempt' },
    { label: 'Retrying', value: d.retrying, hint: 'Failed at least once, will try again' },
    { label: 'Failed', value: d.failed, hint: 'Gave up after every retry; redeliver it from Webhooks' },
  ]

  return (
    <div className="grid gap-3 lg:grid-cols-3">
      <div
        className="grid content-start gap-3 rounded-xl border bg-card p-3.5 sm:p-4 lg:col-span-2"
        data-testid="period-posts"
      >
        <div className="flex items-center justify-between gap-2 text-[13px] sm:text-sm">
          <span className="font-medium">Posts created this period</span>
          <span className="text-muted-foreground">
            <span className="font-semibold text-foreground tabular-nums">{created.toLocaleString('en-US')}</span>{' '}
            created
          </span>
        </div>
        <div className="grid grid-cols-3 gap-2 sm:grid-cols-6">
          {statuses.map((s) => {
            const meta = POST_STATUS_META[s]

            return (
              <Link
                key={s}
                href={`/posts?status=${s}`}
                title={`${meta.description}: open in Posts`}
                className="grid justify-items-center gap-1.5 rounded-lg p-2 hover:bg-muted"
              >
                <span className={cn('text-xl font-semibold tabular-nums sm:text-2xl', TONES[meta.tone].text)}>
                  {(counts[s] ?? 0).toLocaleString('en-US')}
                </span>
                <span className={cn('rounded-md px-1.5 py-0.5 text-[11px] font-medium ring-1 ring-inset', meta.badge)}>
                  {meta.label}
                </span>
              </Link>
            )
          })}
        </div>
        <p className="text-xs text-muted-foreground">
          By current status. Counts every post created since the 1st (UTC).
        </p>
      </div>
      <div className="grid content-start gap-2 rounded-xl border bg-card p-3.5 sm:p-4" data-testid="webhook-deliveries">
        <Link
          href="/webhooks"
          className="inline-flex w-fit items-center gap-2 text-[13px] font-medium hover:underline sm:text-sm pointer-coarse:-my-3 pointer-coarse:py-3"
        >
          <Webhook className="size-4 shrink-0 text-muted-foreground" /> Webhook delivery
        </Link>
        <dl className="divide-y text-sm">
          {rows.map((r) => (
            <div key={r.label} className="flex items-center justify-between py-1.5" title={r.hint}>
              <dt className="text-muted-foreground">{r.label}</dt>
              <dd
                className={cn('font-semibold tabular-nums', r.label === 'Failed' && r.value > 0 && TONES.danger.text)}
              >
                {r.value.toLocaleString('en-US')}
              </dd>
            </div>
          ))}
        </dl>
      </div>
    </div>
  )
}

export function periodLabel(period?: string) {
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
function PlanPeriod({ usage, billing, onManage }: { usage: WorkspaceUsage; billing?: Billing; onManage?: () => void }) {
  const [now] = useState(() => Date.now())

  if (!usage.plan_expires_at && !usage.suspended) {
    return null
  }

  const subscribed = LIVE_SUBSCRIPTION.includes(billing?.subscription_status ?? '')
  const trialing = billing?.subscription_status === 'trialing'
  // With billing, a new workspace has no plan until its owner starts one (Starter: Stripe's trial, card required).
  const neverStarted = usage.suspended && !!billing?.enabled && !billing.subscription_status
  const name = usage.plan.name
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
          {neverStarted
            ? 'Start your 7-day free trial to use this workspace'
            : usage.suspended
              ? 'Your plan has ended, so this workspace is read-only'
              : trialing
                ? `${name} trial: ${days} ${days === 1 ? 'day' : 'days'} left`
                : subscribed
                  ? billing?.subscription_status === 'past_due'
                    ? `Payment failed: ${name} stays active until ${date}`
                    : `${name}: paid through ${date}`
                  : `${name} trial: ${days} ${days === 1 ? 'day' : 'days'} left`}
        </p>
        <p className="mt-0.5 text-muted-foreground">
          {neverStarted
            ? 'Starter is free for 7 days (card required, cancel anytime), or pick a paid plan. Until then you can read everything but not change it.'
            : usage.suspended
              ? 'You can still read and export everything. Choose a plan to keep working.'
              : trialing
                ? `Then ${money(usage.plan.price_month, usage.plan.currency)} per user / month on the card on file, excl. VAT. Cancel anytime in Manage billing.`
                : subscribed
                  ? 'Change the payment method, download invoices or cancel in the billing portal.'
                  : `Your free ${name} trial runs until ${date}. Choose a plan to keep going afterwards; unpaid workspaces become read-only.`}
        </p>
      </div>
      <div className="flex flex-wrap gap-2 sm:self-center">
        {subscribed && onManage ? (
          <Button variant="outline" onClick={onManage}>
            Manage billing
          </Button>
        ) : (
          <Button asChild variant={usage.suspended ? 'default' : 'outline'}>
            <Link href="/plans">
              <Sparkles /> {neverStarted ? 'Start free trial' : 'Choose a plan'}
            </Link>
          </Button>
        )}
      </div>
    </div>
  )
}

export function PlanUsagePage() {
  const { isLocal } = useMode()
  const usage = useUsage()
  const plan = usage.data?.plan
  const unlocked = isLocal || plan?.id === 'unlocked'
  const { portal } = usePlanActions()
  const billing = useBilling({ enabled: !isLocal })

  return (
    <PageContainer>
      <SettingsHeader />
      <SettingsNav className="mt-4" />

      <section className="mt-6 grid gap-4" aria-labelledby="plan-heading">
        {!unlocked && usage.data && (
          <PlanPeriod
            usage={usage.data}
            billing={billing.data}
            onManage={billing.data?.has_customer ? () => portal.mutate() : undefined}
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
                  // No badge: it would only say Unlocked a second time (also to screen readers).
                  'Unlocked'
                ) : (
                  <>
                    {plan?.name ?? 'Plan'} plan{' '}
                    {/* The badge repeats the plan's name: decoration for screen readers. */}
                    <span aria-hidden>
                      <PlanBadge plan={plan?.id} />
                    </span>
                  </>
                )}
              </h2>
              <p className="mt-0.5 text-sm text-muted-foreground">
                {unlocked
                  ? 'Unlocked — local install, no limits.'
                  : plan
                    ? `${money(plan.price_month, plan.currency)} per user / month · usage for ${periodLabel(usage.data?.period)}`
                    : "Your workspace plan and this month's usage."}
              </p>
            </div>
          </div>
          {!unlocked && plan && (
            <Button asChild variant="outline" className="sm:self-center">
              <Link href="/plans">Change plan</Link>
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
                  hint={unlocked ? undefined : bonusHint(plan, m.key) || m.hint}
                  used={usage.data.usage?.[m.key] ?? 0}
                  max={unlocked ? UNLIMITED : (usage.data.plan?.limits?.[m.key] ?? UNLIMITED)}
                />
              ),
            )}
          </div>
        )}
        {usage.data?.posts_by_status && <PeriodActivity usage={usage.data} />}
        {!unlocked && plan && plan.limits.max_upload_bytes > 0 && (
          <p className="text-xs text-muted-foreground">
            Files up to {formatBytes(plan.limits.max_upload_bytes)} per upload on the {plan.name} plan.
          </p>
        )}
      </section>
    </PageContainer>
  )
}
