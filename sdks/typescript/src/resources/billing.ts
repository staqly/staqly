import type { Billing, BillingDetails, BillingInterval, BillingURL, PlanID, RequestOptions, XPosts } from '../types.js'
import { APIResource } from './base.js'

/** Stripe subscription of a cloud workspace (needs `admin`). */
export class BillingResource extends APIResource {
  /** Whether billing is enabled, the plan, Stripe's subscription status and the end of the trial or paid period. */
  get(options?: RequestOptions): Promise<Billing> {
    return this.http.request({ method: 'GET', path: '/v1/billing', options })
  }

  /**
   * The subscription period, cards on file (brand, last four digits) and the newest invoices, 24 per page, read
   * from Stripe. `starting_after`: the last invoice id of the previous page.
   */
  details(params: { starting_after?: string } = {}, options?: RequestOptions): Promise<BillingDetails> {
    return this.http.request({ method: 'GET', path: '/v1/billing/details', query: { ...params }, options })
  }

  /**
   * Pays for a plan (`starter`, `plus`, `pro`, `business`; default `pro`) billed monthly or yearly (`interval`
   * `month` or `year`, 20 % off or more; default `month`): returns a Stripe Checkout URL to open in a browser. A workspace
   * that already pays is switched to that plan instead (prorated) and gets the billing page URL. The plan changes
   * when Stripe's webhook arrives. `ConflictError` (409) when billing is off, the price is not set up, or it is the
   * plan and interval the workspace already has.
   */
  checkout(params: { plan?: PlanID; interval?: BillingInterval } = {}, options?: RequestOptions): Promise<BillingURL> {
    return this.http.request({ method: 'POST', path: '/v1/billing/checkout', body: { ...params }, options })
  }

  /** Posts to X this period: the plan's included ones, bought credits and what's left (`posts:read`). */
  xPosts(options?: RequestOptions): Promise<XPosts> {
    return this.http.request({ method: 'GET', path: '/v1/billing/x-posts', options })
  }

  /**
   * Buys a bundle of extra posts to X (`posts`: 200, 1000 or 4000): returns a Stripe Checkout URL to open in a
   * browser. The posts are added when Stripe confirms the payment and expire at the end of the billing period.
   */
  creditsCheckout(params: { posts: number; provider?: 'x' }, options?: RequestOptions): Promise<BillingURL> {
    return this.http.request({
      method: 'POST',
      path: '/v1/billing/credits/checkout',
      body: { provider: 'x', ...params },
      options,
    })
  }

  /**
   * Returns a Stripe customer portal URL (payment method, invoices,
   * cancellation). `ConflictError` (409) before the first checkout.
   */
  portal(options?: RequestOptions): Promise<BillingURL> {
    return this.http.request({ method: 'POST', path: '/v1/billing/portal', options })
  }
}
