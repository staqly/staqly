'use client'

import { ArrowLeft, CircleCheck, Clock, CreditCard, ExternalLink, ShieldCheck, type LucideIcon } from 'lucide-react'
import Link from 'next/link'
import { useRouter } from 'next/navigation'
import { useEffect, useRef } from 'react'

import { PublicShell } from '@/components/auth/public-shell'
import { Callout } from '@/components/common/callout'
import { ErrorState } from '@/components/common/error-state'
import { usePlanActions } from '@/components/plans/plans-page'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import type { PendingPayment } from '@/lib/api'
import { useMode, usePendingPayment, usePlans } from '@/lib/queries'
import { safeHttpUrl } from '@/lib/safe'
import { TONES, type Tone } from '@/lib/tones'
import { cn } from '@/lib/utils'

const PLANS = '/plans'

// Plan & usage reads the subscription from Stripe on this return and shows the new plan.
const PAID = '/settings/plan?checkout=switched'

type Reason = NonNullable<PendingPayment['reason']>

// What went wrong with a declined card, in the person's words.
const DECLINES: Partial<Record<Reason, string>> = {
  card_declined: 'Your bank declined the payment without giving a reason. It can tell you more.',
  insufficient_funds: "The card doesn't have enough funds for this payment.",
  expired_card: 'The card has expired.',
  incorrect_card_details: "Some of the card's details, such as the security code, don't match what your bank has.",
}

interface View {
  icon: LucideIcon
  tone: Tone
  title: string
  body: string
}

function view(reason: Reason | undefined, plan: string): View {
  switch (reason) {
    case 'authentication_required':
      return {
        icon: ShieldCheck,
        tone: 'warning',
        title: 'Confirm the payment with your bank',
        body: `To start ${plan}, your bank asks you to confirm the payment, as it often does for online card payments.`,
      }
    case 'processing':
      return {
        icon: Clock,
        tone: 'info',
        title: 'Your payment is being processed',
        body: `Your bank is still processing the payment. ${plan} starts as soon as it goes through.`,
      }
    case 'card_declined':
    case 'insufficient_funds':
    case 'expired_card':
    case 'incorrect_card_details':
      return { icon: CreditCard, tone: 'danger', title: 'Your card was declined', body: DECLINES[reason] ?? '' }
    default:
      return {
        icon: CreditCard,
        tone: 'danger',
        title: "The payment didn't go through",
        body: `The payment for ${plan} couldn't be taken from your card.`,
      }
  }
}

/**
 * After a plan switch that is charged at once (leaving the trial) wasn't paid: what happened, in plain words, and the
 * next step on Stripe's pages: confirm the payment with the bank on Stripe's invoice page, or change the card in the
 * billing portal. Payments never happen here; the server reads where the payment stands from Stripe, again whenever
 * the person comes back to this tab, and once it has gone through Plan & usage shows the new plan.
 */
export function PaymentFailedPage() {
  const { isLocal } = useMode()
  const router = useRouter()
  const pending = usePendingPayment({ enabled: !isLocal })
  const plans = usePlans({ enabled: !isLocal })
  const { portal } = usePlanActions()
  const { data, error, isLoading, isFetching, refetch } = pending
  // The change waited for its payment while this page was open: once nothing waits, it went through.
  const waited = useRef(false)

  useEffect(() => {
    if (data?.pending) {
      waited.current = true
    } else if (data && waited.current) {
      router.replace(PAID)
    }
  }, [data, router])

  const back = (
    <Button asChild variant="ghost" size="icon" aria-label="Back" className="group">
      <Link href={PLANS}>
        <ArrowLeft className="group-hover:animate-nudge-left motion-reduce:group-hover:animate-none" />
      </Link>
    </Button>
  )

  const planName = plans.data?.find((p) => p.id === data?.plan)?.name ?? 'the new plan'
  const payUrl = safeHttpUrl(data?.pay_url ?? '')
  const reason = data?.reason
  const v = view(reason, planName)
  const declined = !!reason && reason in DECLINES

  const toPlans = (
    <Button asChild variant="outline" className="w-full sm:w-auto">
      <Link href={PLANS}>Back to plans</Link>
    </Button>
  )

  const updateCard = (
    <Button
      variant={reason === 'authentication_required' && payUrl ? 'outline' : 'default'}
      className="w-full sm:w-auto"
      disabled={portal.isPending}
      onClick={() => portal.mutate()}
      data-testid="payment-update-card"
    >
      <CreditCard aria-hidden />
      Update your card
    </Button>
  )

  let content: React.ReactNode

  if (isLocal) {
    content = <p className="text-center text-sm text-muted-foreground">A local install has no plans and no payments.</p>
  } else if (error) {
    content = <ErrorState title="Could not check the payment" error={error} onRetry={() => void refetch()} />
  } else if (isLoading || !data) {
    content = <Skeleton className="h-72 rounded-2xl" />
  } else if (!data.pending) {
    content = (
      <div className="grid justify-items-center gap-4 text-center" data-testid="payment-none">
        <CircleCheck className={cn('size-10', TONES.success.text)} aria-hidden />
        <h1 className="text-2xl font-semibold tracking-tight sm:text-3xl">No payment is waiting</h1>
        <p className="text-sm text-muted-foreground">Nothing is waiting to be paid for your plan.</p>
        <div className="flex w-full flex-col gap-2 sm:w-auto sm:flex-row">
          <Button asChild className="w-full sm:w-auto">
            <Link href="/settings/plan">Plan & usage</Link>
          </Button>
          {toPlans}
        </div>
      </div>
    )
  } else {
    const Icon = v.icon

    content = (
      <div className="grid gap-6" data-testid={`payment-${reason ?? 'unknown'}`}>
        <div className="grid justify-items-center gap-3 text-center">
          <span className={cn('flex size-14 items-center justify-center rounded-full', TONES[v.tone].chip)}>
            <Icon className="size-7" aria-hidden />
          </span>
          <h1 className="text-2xl font-semibold tracking-tight sm:text-3xl">{v.title}</h1>
          <p className="text-sm text-muted-foreground">{v.body}</p>
        </div>
        <Callout tone="neutral" role="note">
          {reason === 'processing'
            ? 'Your free trial goes on until the payment goes through.'
            : "Nothing has changed yet: your free trial goes on as before, and you haven't been charged."}
        </Callout>
        {reason === 'authentication_required' ? (
          <Callout tone="info" role="note">
            {payUrl
              ? 'The payment opens on Stripe in a new tab. Once you have confirmed it there, come back to this tab: your new plan shows here by itself.'
              : 'Open billing to check your card, then choose the plan again; your bank will ask you to confirm the payment.'}
          </Callout>
        ) : reason === 'processing' ? null : (
          <Callout tone="info" role="note">
            {declined
              ? 'Update your card, or add another one, then choose the plan again.'
              : 'Check your card in billing, then choose the plan again.'}
            {payUrl && ' Or pay on Stripe with another card.'}
          </Callout>
        )}
        <div className="flex flex-col gap-2 sm:flex-row sm:flex-wrap sm:justify-center">
          {reason === 'authentication_required' && payUrl && (
            <Button asChild className="w-full sm:w-auto" data-testid="payment-confirm">
              <a href={payUrl} target="_blank" rel="noopener noreferrer">
                <ShieldCheck aria-hidden />
                Confirm the payment
                <ExternalLink aria-hidden />
              </a>
            </Button>
          )}
          {reason === 'processing' ? (
            <Button className="w-full sm:w-auto" disabled={isFetching} onClick={() => void refetch()}>
              Check again
            </Button>
          ) : (
            updateCard
          )}
          {reason !== 'authentication_required' && reason !== 'processing' && payUrl && (
            <Button asChild variant="outline" className="w-full sm:w-auto" data-testid="payment-pay-other">
              <a href={payUrl} target="_blank" rel="noopener noreferrer">
                Pay with another card
                <ExternalLink aria-hidden />
              </a>
            </Button>
          )}
          {toPlans}
        </div>
        {reason === 'authentication_required' && payUrl && (
          <p className="text-center text-sm text-muted-foreground">
            Confirmed it already?{' '}
            <button
              type="button"
              className="font-medium text-foreground underline underline-offset-4 pointer-coarse:-my-3 pointer-coarse:py-3"
              disabled={isFetching}
              onClick={() => void refetch()}
            >
              Check again
            </button>
          </p>
        )}
      </div>
    )
  }

  return (
    <PublicShell className="flex flex-col items-center pt-8 sm:pt-14" back={back}>
      <div className="w-full max-w-lg">{content}</div>
    </PublicShell>
  )
}
