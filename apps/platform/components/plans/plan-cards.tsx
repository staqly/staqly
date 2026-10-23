import { Check, Sparkles } from 'lucide-react'

import { SkeletonList } from '@/components/common/skeleton-list'
import { UNLIMITED, type BillingInterval, type Plan, type PlanLimits, type PromotionCode } from '@/lib/api'
import { formatBytes } from '@/lib/format'
import { money } from '@/lib/plans'
import { cn } from '@/lib/utils'

export { money }

export const EVERY_PLAN = 'Signed webhooks, full history and MCP on every plan'

export const TAGLINES: Record<string, string> = {
  starter: 'For one creator and a few accounts.',
  plus: 'For a creator or a small team posting every day.',
  pro: 'For teams and agents running many channels.',
  business: 'For agencies and brands: no limits on channels.',
}

/** The price per month shown on a card: billed monthly, or the yearly price spread over twelve months. */
export function monthly(plan: Plan, interval: BillingInterval) {
  return interval === 'year' ? Math.round((plan.price_year / 12) * 100) / 100 : plan.price_month
}

/**
 * A plan's price per user in an interval after a promotion code, or undefined when the code doesn't apply to it. A
 * percent comes off each price; an amount comes off the whole invoice, so each of the seats gets its share.
 */
export function discounted(plan: Plan, interval: BillingInterval, promo?: PromotionCode, seats = 1) {
  if (!promo?.plans.includes(plan.id)) {
    return undefined
  }

  const each = interval === 'year' ? plan.price_year : plan.price_month

  if (promo.percent_off) {
    return Math.round(each * (100 - promo.percent_off)) / 100
  }

  if (promo.amount_off && promo.currency?.toLowerCase() === plan.currency.toLowerCase()) {
    return Math.max(0, Math.round(each * 100 - promo.amount_off / Math.max(seats, 1)) / 100)
  }

  return undefined
}

/** "20% off" or "€10 off", then how long: "for 3 months", "on the first payment". */
export function promotionSummary(promo: PromotionCode) {
  const off = promo.percent_off
    ? `${promo.percent_off}% off`
    : promo.amount_off
      ? `${money(promo.amount_off / 100, promo.currency)} off`
      : 'Discount'
  const time =
    promo.duration === 'once'
      ? ' the first payment'
      : promo.duration === 'repeating' && promo.duration_in_months
        ? ` for ${promo.duration_in_months === 1 ? '1 month' : `${promo.duration_in_months} months`}`
        : ''

  return off + time
}

/** What a team pays: "3 users × €29 = €87/month" (yearly: "3 users × €276 = €828/year"). */
export function teamTotal(plan: Plan, interval: BillingInterval, seats: number, promo?: PromotionCode) {
  const each = discounted(plan, interval, promo, seats) ?? (interval === 'year' ? plan.price_year : plan.price_month)
  const users = seats === 1 ? '1 user' : `${seats} users`

  return `${users} × ${money(each, plan.currency)} = ${money(each * seats, plan.currency)}/${interval === 'year' ? 'year' : 'month'}`
}

/** Monthly / Yearly switch above the plan cards. */
export function IntervalToggle({
  value,
  onChange,
  className,
}: {
  value: BillingInterval
  onChange: (v: BillingInterval) => void
  className?: string
}) {
  return (
    <div
      role="radiogroup"
      aria-label="Billing period"
      className={cn('inline-flex rounded-full border bg-muted/50 p-1 text-sm', className)}
    >
      {(
        [
          ['month', 'Monthly'],
          ['year', 'Yearly'],
        ] as const
      ).map(([v, label]) => (
        <button
          key={v}
          type="button"
          role="radio"
          aria-checked={value === v}
          onClick={() => onChange(v)}
          className={cn(
            'inline-flex h-8 items-center gap-1.5 rounded-full px-4 font-medium outline-none focus-visible:ring-3 focus-visible:ring-ring/50 pointer-coarse:h-10',
            value === v ? 'bg-foreground text-background shadow-sm' : 'text-muted-foreground hover:text-foreground',
          )}
        >
          {label}
          {v === 'year' && (
            <span className="rounded-full bg-brand-gradient px-1.5 py-0.5 text-[11px] font-semibold text-[#052817]">
              Save 20%
            </span>
          )}
        </button>
      ))}
    </div>
  )
}

function count(n: number, noun: string) {
  return n === UNLIMITED ? `Unlimited ${noun}` : `${n.toLocaleString('en-US')} ${noun}`
}

/** Feature lines derived from limits, used when the server does not send any. */
function limitFeatures(l: PlanLimits): string[] {
  return [
    l.posts_per_month === UNLIMITED ? 'Unlimited posts' : `${l.posts_per_month.toLocaleString('en-US')} posts / month`,
    count(l.channels, 'channels'),
    count(l.webhooks, 'webhooks'),
    count(l.api_keys, 'API keys'),
    l.storage_bytes === UNLIMITED ? 'Unlimited storage' : `${formatBytes(l.storage_bytes)} storage`,
  ]
}

function PlanCard({
  plan,
  interval,
  current,
  cta,
  seats,
  promotion,
}: {
  plan: Plan
  interval: BillingInterval
  current?: boolean
  /** The workspace's members: shows what the team pays (3 users × $29 = $87/month). */
  seats?: number
  /** A checked promotion code: the cards it applies to show the old price struck through and the new one. */
  promotion?: PromotionCode
  /** Call-to-action rendered at the bottom of the price block. */
  cta?: React.ReactNode
}) {
  const highlight = plan.highlight
  const features = plan.features?.length ? plan.features : limitFeatures(plan.limits)
  const after = discounted(plan, interval, promotion, seats)
  const perMonth = (n: number) => (interval === 'year' ? Math.round((n / 12) * 100) / 100 : n)

  const body = (
    <div className="relative flex h-full flex-col rounded-[calc(var(--radius)*1.8)] bg-card p-5 text-card-foreground sm:p-6">
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-base font-semibold tracking-tight">{plan.name}</h3>
        {current ? (
          <span className="rounded-full bg-foreground px-2 py-0.5 text-[11px] font-medium text-background">
            Current plan
          </span>
        ) : highlight ? (
          <span className="inline-flex items-center gap-1 rounded-full bg-brand-gradient px-2 py-0.5 text-[11px] font-semibold text-[#052817] shadow-sm shadow-emerald-500/30">
            <Sparkles className="size-3" /> Most popular
          </span>
        ) : null}
      </div>
      <p className="mt-1 text-sm text-muted-foreground">{TAGLINES[plan.id] ?? ''}</p>
      <div className="mt-5 flex flex-wrap items-baseline gap-x-2 gap-y-1">
        {after !== undefined && (
          <s
            className="text-xl font-medium text-muted-foreground tabular-nums decoration-destructive decoration-2"
            data-testid={`plan-old-price-${plan.id}`}
          >
            <span className="sr-only">Was </span>
            {money(monthly(plan, interval), plan.currency)}
          </s>
        )}
        <span className="flex items-baseline gap-1">
          <span
            className={cn('text-4xl font-semibold tracking-tight tabular-nums', after !== undefined && 'text-brand')}
            data-testid={`plan-price-${plan.id}`}
          >
            {after !== undefined && <span className="sr-only">Now </span>}
            {money(after !== undefined ? perMonth(after) : monthly(plan, interval), plan.currency)}
          </span>
          <span className="text-sm whitespace-nowrap text-muted-foreground">/ user / mo</span>
        </span>
      </div>
      <p className="mt-1 text-xs text-muted-foreground">
        {interval === 'year'
          ? `${money(after ?? plan.price_year, plan.currency)} per user, billed yearly`
          : 'Billed monthly'}
        , excl. VAT
      </p>
      {after !== undefined && promotion && (
        <p className="mt-1 text-xs font-medium text-brand" data-testid={`plan-discount-${plan.id}`}>
          {promotionSummary(promotion)} with {promotion.code}
        </p>
      )}
      {seats !== undefined && seats > 1 && (
        <p className="mt-1 text-xs font-medium" data-testid={`plan-total-${plan.id}`}>
          {teamTotal(plan, interval, seats, promotion)}
        </p>
      )}
      {cta && <div className="mt-5 grid">{cta}</div>}
      <div className="my-5 h-px bg-border" />
      <ul className="grid gap-2.5 text-sm" aria-label={`${plan.name} features`}>
        {[...features, EVERY_PLAN.replace(' on every plan', '')].map((f) => (
          <li key={f} className="flex items-start gap-2.5">
            <span
              className={cn(
                'mt-0.5 flex size-4 shrink-0 items-center justify-center rounded-full',
                highlight ? 'bg-brand text-brand-foreground' : 'bg-brand-soft text-brand',
              )}
            >
              <Check className="size-3" strokeWidth={3} />
            </span>
            <span>{f}</span>
          </li>
        ))}
      </ul>
    </div>
  )

  return (
    <div
      data-testid={`plan-${plan.id}`}
      data-current={current || undefined}
      className={cn(
        'relative rounded-[calc(var(--radius)*1.8+1px)] p-px',
        highlight
          ? 'bg-gradient-to-b from-[#7cf29c] via-[#22c774]/60 to-[#0b7a4b]/25 shadow-xl shadow-emerald-500/10'
          : 'bg-border',
        current && !highlight && 'bg-foreground/60 dark:bg-foreground/50',
      )}
    >
      {body}
    </div>
  )
}

export function PlanGrid({
  plans,
  loading,
  interval = 'month',
  currentPlanId,
  renderCta,
  className,
  seats,
  promotion,
}: {
  plans?: Plan[]
  loading?: boolean
  interval?: BillingInterval
  currentPlanId?: string
  seats?: number
  promotion?: PromotionCode
  renderCta?: (plan: Plan, current: boolean) => React.ReactNode
  className?: string
}) {
  const grid = 'grid items-stretch gap-4 sm:grid-cols-2 xl:grid-cols-4'

  if (loading) {
    return (
      <div className={cn(grid, className)}>
        <SkeletonList count={4} className="h-96 rounded-2xl" />
      </div>
    )
  }

  // "Most popular" helps pick a first plan; a workspace that has one is switching, not choosing.
  const shown = currentPlanId ? plans?.map((p) => ({ ...p, highlight: false })) : plans

  return (
    <div className={cn(grid, className)} data-testid="plan-grid">
      {(shown ?? []).map((p) => (
        <PlanCard
          key={p.id}
          plan={p}
          interval={interval}
          current={p.id === currentPlanId}
          cta={renderCta?.(p, p.id === currentPlanId)}
          seats={seats}
          promotion={promotion}
        />
      ))}
    </div>
  )
}
