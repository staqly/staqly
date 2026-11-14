'use client'

import { CreditCard, Lock } from 'lucide-react'
import Link from 'next/link'

import { Button } from '@/components/ui/button'
import { lockReason, type LockReason } from '@/lib/plans'
import { useBilling, useRole } from '@/lib/queries'
import { cn } from '@/lib/utils'

/**
 * What opens a locked workspace again: a new plan once the plan has ended (paying its old invoice opens nothing),
 * the payment while Stripe still holds the subscription.
 */
const LOCKED: Record<LockReason, { text: string; action: string; href: string }> = {
  payment_due: {
    text: 'The last payment did not go through. Update the payment method or pay the open invoice in Plan & usage and the workspace opens again, with everything in it.',
    action: 'Update payment',
    href: '/settings/plan',
  },
  payment_failed: {
    text: 'The payment for its plan failed, so the plan has ended. Choose a plan and the workspace opens again, with everything in it.',
    action: 'Choose a plan',
    href: '/plans',
  },
  ended: {
    text: 'Its plan has ended. Choose a plan and the workspace opens again, with everything in it.',
    action: 'Choose a plan',
    href: '/plans',
  },
}

/**
 * The only thing a locked workspace shows on its working pages (the shell's PlanGate): why, and the way back in for
 * those who manage it, with the export that stays theirs. `neverStarted`: it never had a plan.
 */
export function PlanLocked({ neverStarted }: { neverStarted: boolean }) {
  const { canManagePeople } = useRole()
  const billing = useBilling({ enabled: canManagePeople })
  // Why it is locked is billing's to say: unknown to people who don't manage the workspace, and until it loads.
  const reason = canManagePeople && billing.data && !neverStarted ? LOCKED[lockReason(billing.data)] : null
  const link = 'text-sm text-muted-foreground underline underline-offset-2 hover:text-foreground hover:no-underline'

  return (
    <div className="flex flex-1 items-center justify-center px-4 py-16" data-testid="plan-locked">
      <div className="grid max-w-md justify-items-center gap-4 text-center">
        <span className="flex size-12 items-center justify-center rounded-2xl bg-destructive/10 text-destructive">
          <Lock className="size-6" aria-hidden />
        </span>
        <h1 className="text-2xl font-semibold tracking-tight">
          {neverStarted ? 'This workspace has no plan yet' : 'This workspace is locked'}
        </h1>
        <p className="text-sm text-pretty text-muted-foreground">
          {neverStarted
            ? 'It opens once the account owner chooses a plan for it.'
            : reason
              ? reason.text
              : canManagePeople
                ? 'Its plan ended or a payment did not go through. Plan & usage says what opens it again.'
                : 'Its plan ended or a payment did not go through. An owner or admin can open it again in Plan & usage.'}
        </p>
        {canManagePeople && !neverStarted && (
          <>
            <Button asChild>
              <Link href={reason?.href ?? '/settings/plan'}>
                <CreditCard /> {reason?.action ?? 'Go to Plan & usage'}
              </Link>
            </Button>
            <div className="flex flex-wrap justify-center gap-x-4 gap-y-1">
              {reason && reason.href !== '/settings/plan' && (
                <Link href="/settings/plan" className={cn(link, 'pointer-coarse:py-3')}>
                  Plan &amp; usage
                </Link>
              )}
              <Link href="/settings/data" className={cn(link, 'pointer-coarse:py-3')}>
                Export your data
              </Link>
            </div>
          </>
        )}
      </div>
    </div>
  )
}
