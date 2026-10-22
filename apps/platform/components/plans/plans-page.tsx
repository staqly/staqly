'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, Clock } from 'lucide-react'
import Link from 'next/link'
import { useRouter, useSearchParams } from 'next/navigation'
import { useRef, useState } from 'react'
import { flushSync } from 'react-dom'

import { PublicShell } from '@/components/auth/public-shell'
import { daysLeft, shortDate } from '@/components/billing/billing-format'
import { ErrorState } from '@/components/common/error-state'
import { EVERY_PLAN, IntervalToggle, PlanGrid } from '@/components/plans/plan-cards'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { api, type BillingInterval, type Plan } from '@/lib/api'
import { pluralize } from '@/lib/format'
import { toastApiError } from '@/lib/notify'
import { LIVE_SUBSCRIPTION } from '@/lib/plans'
import { qk, useBilling, useBillingDetails, useMode, usePlans, useRole, useUsage } from '@/lib/queries'
import { cn } from '@/lib/utils'

/** Pay for a plan (Stripe Checkout, or a switch of the live subscription) and open the billing portal. */
export function usePlanActions() {
  const qc = useQueryClient()
  const checkout = useMutation({
    mutationFn: ({
      plan,
      interval,
      welcome,
      promotionCode,
    }: {
      plan: string
      interval: BillingInterval
      welcome?: boolean
      promotionCode?: string
    }) => api.billingCheckout(plan, interval, welcome ? 'welcome' : undefined, promotionCode),
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

/**
 * A promotion code for the plan buttons: a small link under the billing period that opens a short modal, then the
 * applied code with Change and Remove. A modal, not a drawer: one field.
 */
function PromotionCode({ code, onChange }: { code: string | undefined; onChange: (code: string) => void }) {
  const [open, setOpen] = useState(false)
  const [draft, setDraft] = useState('')
  const [error, setError] = useState('')
  // The link, or Change once a code is applied: focus comes back here when the modal closes or the code is removed.
  const trigger = useRef<HTMLButtonElement>(null)

  const show = () => {
    setDraft(code ?? '')
    setError('')
    setOpen(true)
  }

  const remove = () => {
    flushSync(() => onChange(''))
    trigger.current?.focus()
  }

  const triggerClass = 'h-auto p-0 text-sm font-normal pointer-coarse:-my-3 pointer-coarse:h-auto pointer-coarse:py-3'

  return (
    <div className="mt-4 flex flex-wrap items-center justify-center gap-x-3 gap-y-1 text-sm text-muted-foreground">
      {code ? (
        <>
          <p role="status" data-testid="promotion-code-applied">
            Code <span className="font-mono text-foreground uppercase">{code}</span> applied
          </p>
          <Button ref={trigger} type="button" variant="link" className={triggerClass} onClick={show}>
            Change
          </Button>
          <Button
            type="button"
            variant="link"
            className={cn(triggerClass, 'text-muted-foreground hover:text-foreground')}
            onClick={remove}
          >
            Remove
          </Button>
        </>
      ) : (
        <Button ref={trigger} type="button" variant="link" className={triggerClass} onClick={show}>
          Have a promotion code?
        </Button>
      )}
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent
          onCloseAutoFocus={(e) => {
            // The link turns into Change when a code is applied: focus whichever is there now.
            e.preventDefault()
            trigger.current?.focus({ preventScroll: true })
          }}
        >
          <DialogHeader>
            <DialogTitle>Add a promotion code</DialogTitle>
            <DialogDescription>
              Stripe checks the code when you choose a plan; one made for other plans is refused with the reason.
            </DialogDescription>
          </DialogHeader>
          <form
            noValidate
            className="grid gap-4"
            onSubmit={(e) => {
              e.preventDefault()

              if (!draft.trim()) {
                setError('Type the code, such as SPRING20.')

                return
              }

              onChange(draft.trim())
              setOpen(false)
            }}
          >
            <Field data-invalid={!!error}>
              <FieldLabel htmlFor="promotion-code" required>
                Promotion code
              </FieldLabel>
              <Input
                id="promotion-code"
                className="font-mono uppercase"
                placeholder="SPRING20"
                autoComplete="off"
                maxLength={100}
                value={draft}
                aria-required
                aria-invalid={!!error}
                onChange={(e) => {
                  setDraft(e.target.value)

                  if (error && e.target.value.trim()) {
                    setError('')
                  }
                }}
                data-testid="promotion-code"
              />
              <FieldError>{error}</FieldError>
            </Field>
            <DialogFooter>
              <Button type="button" variant="ghost" onClick={() => setOpen(false)}>
                Cancel
              </Button>
              <Button type="submit">Apply</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  )
}

/** Full-screen plan picker: Starter, Plus, Pro and Business, billed monthly or yearly. */
export function PlansPage() {
  const { isLocal } = useMode()
  const { isOwner, accountsEnabled, canManagePeople } = useRole()
  // The account owner pays; a workspace without an account is paid for by its admins.
  const payer = accountsEnabled ? isOwner : canManagePeople
  const usage = useUsage()
  const router = useRouter()
  const params = useSearchParams()
  // Opened from the welcome's last step: Stripe comes back to the end of the welcome.
  const fromWelcome = params.get('from') === 'welcome'
  // The billing period picked on the pricing page (staqly.ai → sign-up → here).
  const askedInterval = params.get('interval')
  const [now] = useState(Date.now)
  const trialEnds = usage.data?.trial_ends_at
  const trialDays = trialEnds ? daysLeft(trialEnds, now) : 0
  const plans = usePlans({ enabled: !isLocal })
  const billing = useBilling({ enabled: !isLocal })
  const subscribed = LIVE_SUBSCRIPTION.includes(billing.data?.subscription_status ?? '')
  const details = useBillingDetails({ enabled: !isLocal && subscribed })
  // The paid period is known once the details are in: no guess meanwhile, so a yearly plan never reads as
  // monthly (and its own button never offers a switch to what it already is).
  const paidInterval: BillingInterval | undefined =
    subscribed && details.data ? (details.data.subscription?.interval === 'year' ? 'year' : 'month') : undefined
  const periodUnknown = subscribed && !details.data
  const [chosen, setChosen] = useState<BillingInterval | null>(
    askedInterval === 'year' || askedInterval === 'month' ? askedInterval : null,
  )
  const interval = chosen ?? paidInterval ?? 'month'
  const { checkout } = usePlanActions()
  const plan = usage.data?.plan
  const canPay = !!billing.data?.enabled
  // The trial runs on a plan without being paid for: every plan is a choice then, none is "current".
  const currentId = subscribed ? plan?.id : undefined
  // Starter's 7-day trial (card required) goes to each workspace once, one workspace of an account at a time.
  const trial = billing.data?.trial
  const trialHere = canPay && !subscribed && trial === 'available'
  // Leaving Starter's trial for another plan ends the trial and charges the new plan at once.
  const trialing = billing.data?.subscription_status === 'trialing'
  // A promotion code applied here: a plan switch never opens Stripe Checkout, whose own code box covers new plans.
  const [code, setCode] = useState('')
  const promotionCode = code.trim() || undefined

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

    if (current && periodUnknown) {
      return (
        <Button variant="outline" disabled className="w-full">
          Your current plan
        </Button>
      )
    }

    if (current && interval === paidInterval) {
      return promotionCode && canPay && payer ? (
        <Button
          variant="outline"
          className="w-full"
          disabled={checkout.isPending}
          onClick={() => checkout.mutate({ plan: p.id, interval, promotionCode })}
        >
          Apply the code to this plan
        </Button>
      ) : (
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
        onClick={() => checkout.mutate({ plan: p.id, interval, welcome: fromWelcome, promotionCode })}
      >
        {label}
      </Button>
    )

    if (subscribed) {
      return trialing ? (
        <div className="grid gap-1.5">
          {button}
          <p className="text-center text-xs text-muted-foreground" data-testid={`plan-charge-${p.id}`}>
            Your trial ends now and {p.name} is charged today; it starts once the payment goes through.
          </p>
        </div>
      ) : (
        button
      )
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
      back={
        <Button asChild variant="ghost" size="icon" aria-label="Back" className="group">
          <Link
            href={BACK}
            onClick={(e) => {
              // Back to where the plans were opened from (the sidebar, Plan & usage); Plan & usage on a fresh tab.
              if (window.history.length > 1) {
                e.preventDefault()
                router.back()
              }
            }}
          >
            <ArrowLeft className="group-hover:animate-nudge-left motion-reduce:group-hover:animate-none" />
          </Link>
        </Button>
      }
    >
      <div className="max-w-xl text-center">
        {trialEnds && (
          <p
            className="mb-4 inline-flex items-center gap-1.5 rounded-full border border-brand/30 bg-brand-soft/60 px-3 py-1 text-sm font-medium"
            data-testid="plans-trial-ends"
          >
            <Clock className="size-4 text-brand" aria-hidden />
            {trialDays === 0
              ? 'Your free trial ends today'
              : `Your free trial ends on ${shortDate(trialEnds)}, in ${pluralize(trialDays, 'day')}`}
          </p>
        )}
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
      {!isLocal && canPay && payer && <PromotionCode code={promotionCode} onChange={setCode} />}
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
