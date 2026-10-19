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
