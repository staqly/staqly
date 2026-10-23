'use client'

import { useQueryClient } from '@tanstack/react-query'
import { BellRing, HardDrive, KeyRound, Layers, Share2, Webhook } from 'lucide-react'
import Link from 'next/link'
import { useEffect } from 'react'
import { toast } from 'sonner'

import { AddonsSection, bonusHint } from '@/components/billing/addons'
import { Invoices, PaymentMethods } from '@/components/billing/payments'
import { PlanSummary } from '@/components/billing/plan-summary'
import { XPostsSection } from '@/components/billing/x-posts'
import { ErrorState } from '@/components/common/error-state'
import { PageContainer } from '@/components/common/page-header'
import { usePlanActions } from '@/components/plans/plans-page'
import { SettingsHeader } from '@/components/settings/settings-header'
import { Skeleton } from '@/components/ui/skeleton'
import { api, POST_STATUSES, UNLIMITED, type WorkspaceUsage } from '@/lib/api'
import { formatBytes } from '@/lib/format'
import { qk, useAddons, useBilling, useBillingDetails, useMode, useRole, useUsage, useXPosts } from '@/lib/queries'
import { POST_STATUS_META } from '@/lib/status'
import { TONES } from '@/lib/tones'
import { meterTone, usageMeter } from '@/lib/usage'
import { cn } from '@/lib/utils'

import { SettingsNav } from './settings-nav'

type MeterKey = keyof WorkspaceUsage['usage'] & keyof WorkspaceUsage['plan']['limits']

const METERS: { key: MeterKey; label: string; Icon: typeof Layers; bytes?: boolean; hint?: string }[] = [
  { key: 'posts_per_month', label: 'Posts this month', Icon: Layers, hint: 'Created · resets on the 1st' },
  { key: 'channels', label: 'Channels', Icon: Share2 },
  { key: 'webhooks', label: 'Webhooks', Icon: Webhook },
  { key: 'api_keys', label: 'API keys', Icon: KeyRound },
  { key: 'storage_bytes', label: 'Storage', Icon: HardDrive, bytes: true },
  { key: 'alerts', label: 'Alerts', Icon: BellRing },
]

/**
 * Platforms whose posts count against an allowance of their own (their APIs charge per post): one compact chip each
 * in the full-width Posts per platform row under the tiles, so every tile keeps one height however many there are.
 */
const PLATFORM_POSTS: { key: MeterKey; provider: string; name: string }[] = [
  { key: 'x_posts_per_period', provider: 'x', name: 'X' },
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

const count = (n: number) => n.toLocaleString('en-US')

/** One platform's own allowance: a compact chip (name, used / limit, a tiny bar) in the Posts per platform row. */
function PlatformChip({
  provider,
  name,
  used,
  max,
  hint,
}: {
  provider: string
  name: string
  used: number
  max: number
  hint?: string
}) {
  const { level, pct } = usageMeter(used, max)
  const tone = meterTone(level)
  const status = level === 'full' ? 'Limit reached' : level === 'warn' ? 'Almost full' : ''

  return (
    <li
      className="inline-flex max-w-full flex-wrap items-center gap-x-2 gap-y-1 rounded-lg border bg-background px-2.5 py-1 text-xs"
      data-testid={`platform-posts-${provider}`}
      data-level={level}
      title={hint}
    >
      <span className="font-medium">{name}</span>
      <span className="text-muted-foreground tabular-nums">
        <span className={cn('font-medium', status ? tone.text : 'text-foreground')}>{count(used)}</span> / {count(max)}
      </span>
      <span
        role="progressbar"
        aria-label={`${name} posts`}
        aria-valuemin={0}
        aria-valuemax={max}
        aria-valuenow={used}
        aria-valuetext={`${count(used)} of ${count(max)}`}
        className="h-1.5 w-10 shrink-0 overflow-hidden rounded-full bg-muted"
      >
        <span
          className={cn('block h-full rounded-full transition-[width] duration-500', tone.fill)}
          style={{ width: `${Math.max(pct, used > 0 ? 4 : 0)}%` }}
        />
      </span>
      {status && <span className={cn('font-medium', tone.text)}>{status}</span>}
    </li>
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

/** A group of the page: a heading with a line under it, then its cards. */
function Group({
  id,
  title,
  description,
  children,
  testId,
}: {
  id: string
  title: string
  description?: React.ReactNode
  children: React.ReactNode
  testId?: string
}) {
  return (
    <section aria-labelledby={id} className="grid gap-3" data-testid={testId}>
      <div>
        <h2 id={id} className="text-base font-semibold tracking-tight">
          {title}
        </h2>
        {description && <p className="mt-0.5 text-sm text-muted-foreground">{description}</p>}
      </div>
      {children}
    </section>
  )
}

/** Back from Stripe (checkout, a plan switch, bought X posts): say so, and read the plan again once its event is in. */
function useCheckoutReturn() {
  const qc = useQueryClient()

  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    const result = params.get('checkout')
    const credits = params.get('credits')

    if (credits === 'success') {
      toast.success('Payment received', { description: 'Your extra X posts show here in a moment.' })
      setTimeout(() => void qc.invalidateQueries({ queryKey: qk.xPosts }), 3000)
    }

    if (result === 'success' || result === 'switched') {
      toast.success(result === 'success' ? 'Payment received' : 'Plan changed', {
        description: 'The new plan shows here in a moment.',
      })
      // The server reads the subscription from Stripe itself, so the plan shows (and a locked workspace opens)
      // even when Stripe's webhook is late or lost.
      setTimeout(() => {
        void api
          .syncBilling()
          .catch(() => undefined)
          .then(() => {
            void qc.invalidateQueries({ queryKey: qk.usage })
            void qc.invalidateQueries({ queryKey: qk.billing })
            void qc.invalidateQueries({ queryKey: qk.billingDetails })
          })
      }, 1500)
    }

    if (result || credits) {
      window.history.replaceState(null, '', window.location.pathname)
    }
  }, [qc])
}

/**
 * Settings, Plan & usage: the plan and where it stands (with the subscription's price, seats and next payment), the
 * usage against the plan's limits, add-ons and extra X posts, then the card on file and the invoices. Billing (the
 * subscription, add-ons, cards, invoices) is read and changed only by those who manage the workspace; everyone sees
 * the plan and the usage. Local installs are unlocked: no billing at all.
 */
export function PlanUsagePage() {
  const { isLocal } = useMode()
  const { canManagePeople, isOwner } = useRole()
  const manage = !isLocal && canManagePeople
  const usage = useUsage()
  const billing = useBilling({ enabled: manage })
  const details = useBillingDetails({ enabled: manage })
  const addons = useAddons({ enabled: manage })
  const xPosts = useXPosts({ enabled: !isLocal })
  const { portal } = usePlanActions()
  const { mutate: openPortal, isPending: opening } = portal
  const { refetch: refetchUsage } = usage
  const { refetch: refetchDetails } = details

  useCheckoutReturn()

  const u = usage.data
  const plan = u?.plan
  const unlocked = isLocal || plan?.id === 'unlocked'
  const b = billing.data
  const d = details.data
  const sub = d?.subscription ?? null
  const payOnline = !!b?.enabled
  const canBuy = isOwner && payOnline
  const portalOpen = isOwner && payOnline && !!b?.has_customer

  const summary = u ? (
    <PlanSummary
      usage={u}
      billing={b}
      details={d}
      detailsLoading={details.isLoading}
      canManage={manage}
      onManage={portalOpen ? () => openPortal() : undefined}
      managing={opening}
    />
  ) : usage.error ? null : (
    <Skeleton className="h-40 rounded-2xl" />
  )

  // The platforms with an allowance of their own this plan has (none when local or unlocked: nothing is metered).
  const metered =
    u && !isLocal && !unlocked ? PLATFORM_POSTS.filter((p) => (u.plan?.limits?.[p.key] ?? UNLIMITED) !== UNLIMITED) : []
  const platformPosts =
    u && metered.length > 0 ? (
      <div
        className="flex flex-wrap items-center gap-x-3 gap-y-2 rounded-xl border bg-card px-3.5 py-2.5 sm:px-4"
        data-testid="platform-posts"
      >
        <span id="platform-posts-heading" className="text-[13px] font-medium sm:text-sm">
          Posts per platform
        </span>
        <ul className="flex min-w-0 flex-wrap gap-2" aria-labelledby="platform-posts-heading">
          {metered.map((p) => (
            <PlatformChip
              key={p.key}
              provider={p.provider}
              name={p.name}
              used={u.usage?.[p.key] ?? 0}
              max={u.plan?.limits?.[p.key] ?? UNLIMITED}
              hint={bonusHint(plan, p.key) || undefined}
            />
          ))}
        </ul>
        <p className="text-xs text-muted-foreground sm:ml-auto">Included in the plan; bought posts come on top.</p>
      </div>
    ) : null

  const meters = usage.error ? (
    <ErrorState title="Could not load usage" error={usage.error} onRetry={() => refetchUsage()} />
  ) : (
    <div className="grid grid-cols-2 gap-3 lg:grid-cols-3" data-testid="usage-meters">
      {/* Local installs have no API keys: agents use the access token (Integrations, Agents). */}
      {METERS.filter((m) => !(isLocal && m.key === 'api_keys')).map((m) =>
        !u ? (
          <Skeleton key={m.key} className="h-32 rounded-xl" />
        ) : (
          <UsageMeter
            key={m.key}
            label={m.label}
            Icon={m.Icon}
            bytes={m.bytes}
            hint={unlocked ? undefined : bonusHint(plan, m.key) || m.hint}
            used={u.usage?.[m.key] ?? 0}
            max={unlocked ? UNLIMITED : (u.plan?.limits?.[m.key] ?? UNLIMITED)}
          />
        ),
      )}
    </div>
  )

  const usageGroup = (
    <Group
      id="usage-heading"
      title="Usage"
      testId="usage"
      description={
        unlocked
          ? `This month: ${periodLabel(u?.period)}.`
          : `Against the ${plan?.name ?? ''} plan's limits, for ${periodLabel(u?.period)}. Posts reset on the 1st (UTC).`
      }
    >
      {meters}
      {!usage.error && platformPosts}
      {u?.posts_by_status && <PeriodActivity usage={u} />}
      {!unlocked && plan && plan.limits.max_upload_bytes > 0 && (
        <p className="text-xs text-muted-foreground">
          Files up to {formatBytes(plan.limits.max_upload_bytes)} per upload on the {plan.name} plan.
        </p>
      )}
    </Group>
  )

  // Add-ons are sold on the subscription (for those who manage the workspace); extra X posts show to everyone.
  const hasAddons = manage && payOnline && !!addons.data?.length
  const hasXPosts = !isLocal && !!xPosts.data && !xPosts.data.unlimited
  const extras =
    !unlocked && (hasAddons || hasXPosts) ? (
      <Group
        id="addons-heading"
        title={hasAddons ? 'Add-ons and extra X posts' : 'Extra X posts'}
        description={
          canBuy
            ? 'On top of the plan, excl. VAT. Add-ons are charged with the subscription; X posts once.'
            : 'On top of the plan. The account owner buys them.'
        }
      >
        <XPostsSection canBuy={canBuy} />
        {hasAddons && <AddonsSection canBuy={canBuy} yearly={sub?.interval === 'year'} />}
      </Group>
    ) : null

  // The card on file and the invoices: Stripe's, once the workspace has a billing account.
  const payments =
    manage && payOnline && b?.has_customer ? (
      <Group id="payments-heading" title="Payments" testId="payments">
        {details.error ? (
          <ErrorState
            title="Could not load payments"
            error={details.error}
            onRetry={() => refetchDetails()}
            className="max-w-none"
          />
        ) : !d ? (
          <Skeleton className="h-40 rounded-xl" />
        ) : (
          <>
            <PaymentMethods details={d} onManage={portalOpen ? () => openPortal() : undefined} managing={opening} />
            <Invoices details={d} />
          </>
        )}
      </Group>
    ) : null

  return (
    <PageContainer>
      <SettingsHeader />
      <SettingsNav className="mt-4" />

      <div className="mt-6 grid gap-8">
        {summary}
        {usageGroup}
        {extras}
        {payments}
      </div>
    </PageContainer>
  )
}
