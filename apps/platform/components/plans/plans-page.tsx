'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { X } from 'lucide-react'
import Link from 'next/link'
import { useState } from 'react'

import { PublicShell } from '@/components/auth/public-shell'
import { ErrorState } from '@/components/common/error-state'
import { EVERY_PLAN, IntervalToggle, PlanGrid } from '@/components/plans/plan-cards'
import { Button } from '@/components/ui/button'
import { api, type BillingInterval, type Plan } from '@/lib/api'
import { toastApiError } from '@/lib/notify'
import { LIVE_SUBSCRIPTION } from '@/lib/plans'
import { qk, useBilling, useBillingDetails, useMode, usePlans, useRole, useUsage } from '@/lib/queries'
import { cn } from '@/lib/utils'

/** Pay for a plan (Stripe Checkout, or a switch of the live subscription) and open the billing portal. */
export function usePlanActions() {
  const qc = useQueryClient()
  const checkout = useMutation({
    mutationFn: ({ plan, interval }: { plan: string; interval: BillingInterval }) =>
      api.billingCheckout(plan, interval),
    onSuccess: ({ url }) => {
      void qc.invalidateQueries({ queryKey: qk.billing })
      window.location.assign(url)
    },
    onError: (e) => toastApiError(e),
  })
  const portal = useMutation({
    mutationFn: () => api.billingPortal(),
    onSuccess: ({ url }) => window.location.assign(url),
    onError: (e) => toastApiError(e),
  })

  return { checkout, portal }
}

const BACK = '/settings/plan'

/** Full-screen plan picker: Starter, Plus, Pro and Business, billed monthly or yearly. */
export function PlansPage() {
  const { isLocal } = useMode()
  const { accountOwner, accountsEnabled, canManagePeople } = useRole()
  // The account owner pays; a workspace without an account is paid for by its admins.
  const payer = accountsEnabled ? accountOwner : canManagePeople
  const usage = useUsage()
  const plans = usePlans({ enabled: !isLocal })
  const billing = useBilling({ enabled: !isLocal })
  const subscribed = LIVE_SUBSCRIPTION.includes(billing.data?.subscription_status ?? '')
  const details = useBillingDetails({ enabled: !isLocal && subscribed })
  const paidInterval: BillingInterval | undefined = subscribed
    ? details.data?.subscription?.interval === 'year'
      ? 'year'
      : 'month'
    : undefined
  const [chosen, setChosen] = useState<BillingInterval | null>(null)
  const interval = chosen ?? paidInterval ?? 'month'
  const { checkout } = usePlanActions()
  const plan = usage.data?.plan
  const canPay = !!billing.data?.enabled
  // The trial runs on a plan without being paid for: every plan is a choice then, none is "current".
  const currentId = subscribed ? plan?.id : undefined
  // Starter's 7-day trial (card required) goes to each workspace once, one workspace of an account at a time.
  const trial = billing.data?.trial
  const trialHere = canPay && !subscribed && trial === 'available'

  const cta = (p: Plan, current: boolean) => {
    const withTrial = trialHere && p.id === 'starter'
    const label =
      !subscribed || !plan
        ? withTrial
          ? 'Start 7-day free trial'
          : `Choose ${p.name}`
        : p.price_month > plan.price_month
          ? `Upgrade to ${p.name}`
          : p.price_month < plan.price_month
            ? `Switch to ${p.name}`
            : `Switch to ${interval === 'year' ? 'yearly' : 'monthly'}`

    if (current && interval === paidInterval) {
      return (
        <Button variant="outline" disabled className="w-full">
          Your current plan
        </Button>
      )
    }

    if (!canPay || !payer) {
      return (
        <Button
          disabled
          className={cn('w-full', p.highlight && 'bg-brand-gradient text-[#052817] disabled:opacity-90')}
          title={canPay ? 'Only the account owner pays for plans' : 'Online payments are not enabled on this instance'}
        >
          {canPay ? `${label}: ask the account owner` : `${label}: ask the operator`}
        </Button>
      )
    }

    const button = (
      <Button
        variant={p.highlight ? 'default' : 'outline'}
        className={cn('w-full', p.highlight && 'bg-brand-gradient text-[#052817]')}
        disabled={checkout.isPending}
        onClick={() => checkout.mutate({ plan: p.id, interval })}
      >
        {label}
      </Button>
    )

    if (subscribed) {
      return button
    }

    // Before the first subscription: say what happens at Stripe, so nobody is charged by surprise.
    return (
      <div className="grid gap-1.5">
        {button}
        <p className="text-center text-xs text-muted-foreground" data-testid={`plan-charge-${p.id}`}>
          {withTrial
            ? 'Free for 7 days, card required. Charged when the trial ends; cancel before to pay nothing.'
            : p.id === 'starter' && trial === 'other_workspace'
              ? 'Charged today: another workspace of yours is in its free trial.'
              : p.id === 'starter' && trial === 'used'
                ? 'Charged today: this workspace already had its free trial.'
                : 'Charged today.'}
        </p>
      </div>
    )
  }

  return (
    <PublicShell
      className="flex flex-col items-center pt-8 sm:pt-14"
      nav={
        <Button asChild variant="ghost" size="icon" aria-label="Close">
          <Link href={BACK}>
            <X />
          </Link>
        </Button>
      }
    >
      <div className="max-w-xl text-center">
        <h1 className="text-3xl font-semibold tracking-tight sm:text-4xl">Choose your plan</h1>
        <p className="mt-3 text-sm text-muted-foreground">
          {EVERY_PLAN}. Prices are per user: everyone in the workspace counts, whatever their role.{' '}
          {canPay
            ? subscribed
              ? 'A switch takes effect at once; Stripe charges or credits the difference.'
              : trialHere
                ? 'Pay by card through Stripe. Starter starts with a 7-day free trial (card required); Plus, Pro and Business are charged today. Cancel any time.'
                : 'Pay by card through Stripe; the plan you choose is charged today. Cancel any time.'
            : 'Online payments are not enabled on this instance, so plan changes are applied by the server operator.'}
        </p>
      </div>
      {!isLocal && <IntervalToggle value={interval} onChange={setChosen} className="mt-8" />}
      <div className="mt-8 w-full max-w-6xl">
        {isLocal ? (
          <p className="text-center text-sm text-muted-foreground">A local install has no plans and no limits.</p>
        ) : plans.error ? (
          <ErrorState title="Could not load plans" error={plans.error} onRetry={() => plans.refetch()} />
        ) : (
          <PlanGrid
            plans={plans.data}
            loading={plans.isLoading}
            interval={interval}
            currentPlanId={currentId}
            renderCta={cta}
            seats={billing.data?.seats}
          />
        )}
      </div>
      <Link href={BACK} className="mt-8 text-sm text-muted-foreground underline-offset-4 hover:underline">
        Back to Plan & usage
      </Link>
    </PublicShell>
  )
}
