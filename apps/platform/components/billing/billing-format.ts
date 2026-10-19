import { lazyDateFormat } from '@/lib/format'
import { money } from '@/lib/plans'

const dateFormat = lazyDateFormat({ day: 'numeric', month: 'short', year: 'numeric' })

/** Oct 28, 2026 (the viewer's locale and time zone); '' for no date. */
export function shortDate(value?: string | null) {
  return value ? dateFormat().format(new Date(value)) : ''
}

// ponytail: amounts are in cents; zero-decimal currencies (JPY, KRW) would show 100x too small. Staqly's plans are EUR.
/** A Stripe amount in cents: €29, €87.50. */
export function cents(amount: number, currency: string) {
  return money(amount / 100, currency || 'eur')
}

/** Whole days until `end` from `now`, rounded up (a trial ending in 6 days and 2 hours has 7 days left); 0 when past. */
export function daysLeft(end: string, now: number) {
  return Math.max(0, Math.ceil((new Date(end).getTime() - now) / 86_400_000))
}
