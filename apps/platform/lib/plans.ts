/** Subscription statuses that pay (or are about to): a plan change switches them instead of a new checkout. */
export const LIVE_SUBSCRIPTION = ['active', 'trialing', 'past_due', 'incomplete']

/** €12, €15: whole amounts without cents (others with two decimals), in the plans' currency (euros). */
export function money(n: number, currency = 'eur') {
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: currency.toUpperCase(),
    minimumFractionDigits: Number.isInteger(n) ? 0 : 2,
    maximumFractionDigits: 2,
  }).format(n)
}

/**
 * Why a locked workspace is locked, which decides what opens it: `payment_due` (Stripe still holds the subscription
 * and waits for its invoice: paying it restores the workspace), `payment_failed` (the retries ran out and the plan
 * ended) or `ended`. An ended plan comes back only with a new one; paying its old invoice opens nothing.
 */
export type LockReason = 'payment_due' | 'payment_failed' | 'ended'

export function lockReason(billing: {
  subscription_status: string | null
  ended_for?: 'payment_failed' | null
}): LockReason {
  if (billing.subscription_status === 'past_due' || billing.subscription_status === 'unpaid') {
    return 'payment_due'
  }

  return billing.ended_for === 'payment_failed' ? 'payment_failed' : 'ended'
}
