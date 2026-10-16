import type { Billing, BillingURL, RequestOptions } from '../types.js'
import { APIResource } from './base.js'

/** Stripe subscription of a cloud workspace (needs `admin`). */
export class BillingResource extends APIResource {
  /** Whether billing is enabled, the plan, Stripe's subscription status and the end of the trial or paid period. */
  get(options?: RequestOptions): Promise<Billing> {
    return this.http.request({ method: 'GET', path: '/v1/billing', options })
  }

  /**
   * Starts a Stripe Checkout for Pro and returns its URL (open it in a browser
   * to pay). The plan changes when Stripe's webhook arrives. `ConflictError`
   * (409) when billing is off or a subscription is already live.
   */
  checkout(options?: RequestOptions): Promise<BillingURL> {
    return this.http.request({ method: 'POST', path: '/v1/billing/checkout', options })
  }

  /**
   * Returns a Stripe customer portal URL (payment method, invoices,
   * cancellation). `ConflictError` (409) before the first checkout.
   */
  portal(options?: RequestOptions): Promise<BillingURL> {
    return this.http.request({ method: 'POST', path: '/v1/billing/portal', options })
  }
}
