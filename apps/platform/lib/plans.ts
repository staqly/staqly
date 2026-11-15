import type { PromotionCode } from '@/lib/api'

/** Subscription statuses that pay (or are about to): a plan change switches them instead of a new checkout. */
export const LIVE_SUBSCRIPTION = ['active', 'trialing', 'past_due', 'incomplete']

/** €12, CHF 15, $15: whole amounts without cents (others with two decimals), in the plans' currency. */
export function money(n: number, currency = 'eur') {
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: currency.toUpperCase(),
    minimumFractionDigits: Number.isInteger(n) ? 0 : 2,
    maximumFractionDigits: 2,
  }).format(n)
}

/** "20% off" or "€10 off", then how long: "for 3 months", "on the first payment". */
export function promotionSummary(
  promo: Pick<PromotionCode, 'percent_off' | 'amount_off' | 'currency' | 'duration' | 'duration_in_months'>,
) {
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
