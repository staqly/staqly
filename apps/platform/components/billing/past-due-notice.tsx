'use client'

import Link from 'next/link'

import { Callout } from '@/components/common/callout'
import { useRole } from '@/lib/queries'

/**
 * The strip at the top of every page while a payment is failing (the shell's usePastDue, which loads it then). One line that stays in view on a wide
 * screen, exactly `--shell-notice` high (the shell sets it; full-height pages take it off their height); on a phone
 * it wraps and scrolls with the page.
 */
export function PastDueNotice() {
  const { isOwner } = useRole()
  const text = isOwner
    ? 'The last payment for this workspace failed. Update the payment method to keep it open.'
    : 'The last payment for this workspace failed. Its owner can update the payment method.'

  return (
    <div className="z-20 border-b bg-background md:sticky md:top-0" data-testid="past-due-notice">
      <Callout tone="warning" role="status" className="rounded-none border-0 px-4 sm:px-6 lg:px-8">
        <div className="md:flex md:items-center md:gap-3">
          <span className="md:min-w-0 md:truncate" title={text}>
            {text}
          </span>{' '}
          <Link
            href="/settings/plan"
            className="shrink-0 font-medium whitespace-nowrap underline underline-offset-2 hover:no-underline pointer-coarse:-my-3 pointer-coarse:py-3"
          >
            Plan &amp; usage
          </Link>
        </div>
      </Callout>
    </div>
  )
}
