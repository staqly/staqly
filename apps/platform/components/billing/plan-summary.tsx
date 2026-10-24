'use client'

import { ExternalLink, Gauge, Infinity as InfinityIcon, Sparkles } from 'lucide-react'
import Link from 'next/link'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import type { Billing, BillingDetails, WorkspaceUsage } from '@/lib/api'
import { pluralize } from '@/lib/format'
import { LIVE_SUBSCRIPTION, money } from '@/lib/plans'
import { TONES, type Tone } from '@/lib/tones'
import { cn } from '@/lib/utils'

import { cents, daysLeft, shortDate } from './billing-format'
import { Pill } from './payments'

type State = 'unlocked' | 'start' | 'suspended' | 'past_due' | 'trial' | 'canceling' | 'active'

/** Where the workspace's plan stands, from what this person may read (billing only for those who manage it). */
function planState(usage: WorkspaceUsage, billing?: Billing, details?: BillingDetails | null): State {
  const sub = details?.subscription

  if (usage.plan.id === 'unlocked') {
    return 'unlocked'
  }

  if (usage.suspended) {
    // With billing, a new workspace has no plan until its owner starts one (Starter: Stripe's trial, card required).
    return billing?.enabled && !billing.subscription_status ? 'start' : 'suspended'
  }

  if (billing?.subscription_status === 'past_due') {
    return 'past_due'
  }

  if (usage.trial_ends_at) {
    return 'trial'
  }

  if (sub && sub.status !== 'canceled' && (sub.cancel_at_period_end || sub.cancel_at)) {
    return 'canceling'
  }

  return 'active'
}

function Fact({
  label,
  value,
  hint,
  testId,
}: {
  label: string
  value: React.ReactNode
  hint?: string
  testId?: string
}) {
  return (
    <div className="grid min-w-0 content-start gap-0.5" data-testid={testId}>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="text-sm font-medium tabular-nums wrap-anywhere">{value}</dd>
      {hint && <dd className="text-xs text-muted-foreground">{hint}</dd>}
    </div>
  )
}

/**
 * The top of Plan & usage: the plan, where it stands (trial with the days left, active, payment failed, ending,
 * read-only), the price per user, the seats, the next payment, and the actions: change the plan (owners and admins)
 * and Stripe's billing portal (the account owner). People who don't manage the workspace see the plan only.
 */
export function PlanSummary({
  usage,
  billing,
  details,
  detailsLoading,
  canManage,
  onManage,
  managing,
}: {
  usage: WorkspaceUsage
  billing?: Billing
  details?: BillingDetails | null
  detailsLoading?: boolean
  /** Owners and admins: the plan picker. */
  canManage: boolean
  /** The account owner with a Stripe customer: opens the billing portal. */
  onManage?: () => void
  managing?: boolean
}) {
  // Read once: the days left don't need a clock (the page is reloaded more often than a day passes).
  const [now] = useState(() => Date.now())
  const plan = usage.plan
  const state = planState(usage, billing, details)
  const sub = details?.subscription ?? null
  const stripeTrial = billing?.subscription_status === 'trialing'
  const subscribed = LIVE_SUBSCRIPTION.includes(billing?.subscription_status ?? '')

  if (state === 'unlocked') {
    return (
      <section
        aria-labelledby="plan-heading"
        className="flex items-start gap-3 rounded-2xl border bg-card p-4 sm:p-5"
        data-testid="plan-summary"
      >
        <span className={cn('flex size-10 shrink-0 items-center justify-center rounded-xl', TONES.success.chip)}>
          <InfinityIcon className="size-5" />
        </span>
        <div className="min-w-0">
          <h2 id="plan-heading" className="text-base font-semibold tracking-tight">
            Community Edition
          </h2>
          <p className="mt-0.5 text-sm text-muted-foreground">Self-hosted, with no plan limits and no billing.</p>
        </div>
      </section>
    )
  }

  const days = usage.trial_ends_at ? daysLeft(usage.trial_ends_at, now) : 0
  const until = shortDate(usage.plan_expires_at)
  const ends = shortDate(sub?.cancel_at ?? sub?.current_period_end)
  const pills: Record<State, [string, Tone] | null> = {
    unlocked: null,
    start: ['No plan yet', 'warning'],
    suspended: ['Read-only', 'danger'],
    past_due: ['Payment failed', 'danger'],
    trial: [`Trial · ${pluralize(days, 'day')} left`, 'info'],
    canceling: [`Ends ${ends}`, 'warning'],
    active: ['Active', 'success'],
  }
  const pill = pills[state]
  const message: Record<State, string> = {
    unlocked: '',
    start:
      'Start your 7-day free trial to use this workspace: Starter is free for 7 days (card required, cancel anytime), or pick a paid plan. Until then you can read everything but not change it.',
    suspended:
      'Your plan has ended, so this workspace is read-only. You can still read and export everything; choose a plan to keep working.',
    past_due: `The last payment failed. Update the payment method: ${plan.name} stays active until ${until}.`,
    trial: stripeTrial
      ? `Your free trial ends on ${shortDate(usage.trial_ends_at)}. Then the card on file is charged; cancel before then to pay nothing.`
      : `Your free trial ends on ${shortDate(usage.trial_ends_at)}. Choose a plan to keep going afterwards; unpaid workspaces become read-only.`,
    canceling: `It will not renew: the workspace becomes read-only after ${ends}.`,
    active: '',
  }
  const loud = state === 'start' || state === 'suspended' || state === 'past_due'

  // Price per user: the subscription's (cents), else the plan's list price.
  const interval = sub?.interval || 'month'
  const price =
    sub && sub.amount > 0
      ? `${cents(sub.amount, sub.currency)} per user / ${interval}`
      : plan.price_month > 0
        ? `${money(plan.price_month, plan.currency)} per user / month`
        : ''
  const seats = billing?.seats ?? usage.usage.seats
  const quantity = Math.max(sub?.quantity ?? seats ?? 1, 1)
  // The next payment: when the trial ends or the period renews; none once the subscription is set to end.
  const nextAt = sub ? (sub.status === 'trialing' ? sub.trial_end : sub.current_period_end) : null
  let next: { value: string; hint: string } | null = null

  if (sub && sub.status !== 'canceled' && state !== 'canceling') {
    if (state === 'past_due') {
      next = { value: 'Due now', hint: 'Update the payment method to pay it' }
    } else if (nextAt && sub.amount > 0) {
      next = {
        value: `${cents(sub.amount * quantity, sub.currency)} on ${shortDate(nextAt)}`,
        hint: `${pluralize(quantity, 'user')} × ${cents(sub.amount, sub.currency)}, excl. VAT`,
      }
    }
  }

  const planLink = canManage && (
    <Button
      asChild
      variant={state === 'start' || state === 'suspended' || (state === 'trial' && !subscribed) ? 'default' : 'outline'}
    >
      <Link href="/plans">
        {state === 'start' ? (
          <>
            <Sparkles /> Start free trial
          </>
        ) : state === 'suspended' || (state === 'trial' && !subscribed) ? (
          <>
            <Sparkles /> Choose a plan
          </>
        ) : (
          'Change plan'
        )}
      </Link>
    </Button>
  )
  const manage = onManage && (
    <Button variant={state === 'past_due' ? 'default' : 'outline'} disabled={managing} onClick={onManage}>
      Manage billing <ExternalLink />
    </Button>
  )

  return (
    <section
      aria-labelledby="plan-heading"
      data-testid="plan-summary"
      data-state={state}
      className={cn(
        'grid gap-4 rounded-2xl border p-4 sm:p-5',
        loud
          ? 'border-destructive/40 bg-destructive/5'
          : state === 'trial'
            ? 'border-brand/30 bg-brand-soft/40'
            : 'bg-card',
      )}
    >
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex min-w-0 items-center gap-3">
          <span className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-brand-soft text-brand">
            <Gauge className="size-5" />
          </span>
          <div className="grid min-w-0 gap-1">
            <div className="flex flex-wrap items-center gap-2">
              <h2 id="plan-heading" className="text-base font-semibold tracking-tight">
                {plan.name} plan
              </h2>
              {pill && (
                <span data-testid="plan-status">
                  <Pill label={pill[0]} tone={pill[1]} />
                </span>
              )}
            </div>
            {message[state] && (
              <p className="text-sm text-muted-foreground" data-testid="plan-message" role={loud ? 'alert' : undefined}>
                {message[state]}
              </p>
            )}
          </div>
        </div>
        {(planLink || manage) && (
          <div className="flex flex-wrap gap-2 sm:shrink-0">
            {manage}
            {planLink}
          </div>
        )}
      </div>

      <dl className="grid grid-cols-2 gap-x-4 gap-y-3 border-t pt-4 sm:grid-cols-4" data-testid="plan-facts">
        {price && (
          <Fact label="Price" value={price} hint={interval === 'year' ? 'Billed yearly, excl. VAT' : 'Excl. VAT'} />
        )}
        <Fact label="Seats" value={pluralize(seats, 'user')} hint="Every member is billed" />
        {next && <Fact label="Next payment" value={next.value} hint={next.hint} testId="next-payment" />}
        {canManage && detailsLoading && (
          <div className="grid gap-1.5">
            <Skeleton className="h-3 w-20" />
            <Skeleton className="h-4 w-32" />
          </div>
        )}
        {state === 'canceling' && <Fact label="Ends on" value={ends} hint="It will not renew" />}
      </dl>
      {canManage && billing && !billing.enabled && (
        <p className="-mt-1 text-xs text-muted-foreground" role="note" data-testid="billing-off">
          Online payments are not enabled on this server, so there are no cards or invoices here. Its operator changes
          plans.
        </p>
      )}
      {!canManage && (
        <p className="-mt-1 text-xs text-muted-foreground">Owners and admins of this workspace change the plan.</p>
      )}
    </section>
  )
}
