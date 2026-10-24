import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { PaymentFailedPage } from '@/components/plans/payment-failed-page'
import { PlansPage } from '@/components/plans/plans-page'
import type { PendingPayment } from '@/lib/api'
import { qk } from '@/lib/queries'

import { nav } from './navigation'
import { PLANS, usage } from './plans-fixtures'
import { makeClient, mockApi, renderWithProviders, sessionFixture } from './utils'

const PAY_URL = 'https://invoice.stripe.com/i/acct_1/test_in_1'

function setup(routes: Record<string, unknown>) {
  const client = makeClient()

  client.setQueryData(qk.session, {
    ...sessionFixture('cloud'),
    role: 'owner',
    accounts_enabled: true,
    account_owner: true,
  })

  const api = mockApi({
    'GET /v1/plans': { data: PLANS },
    'GET /v1/workspace/usage': {
      ...usage(PLANS[0]),
      trial_ends_at: new Date(Date.now() + 3 * 86_400_000).toISOString(),
    },
    'GET /v1/billing': {
      enabled: true,
      plan: 'starter',
      subscription_status: 'trialing',
      plan_expires_at: null,
      suspended: false,
      has_customer: true,
      trial: 'none',
    },
    'GET /v1/billing/details': {
      enabled: true,
      subscription: null,
      payment_methods: [],
      invoices: [],
      has_more_invoices: false,
    },
    'POST /v1/billing/portal': { url: 'https://billing.stripe.test/p_1' },
    ...routes,
  })

  return { client, api }
}

describe('a trial upgrade that was not paid', () => {
  const assign = vi.fn<(url: string) => void>()

  beforeEach(() => {
    nav.reset()
    vi.stubGlobal('location', { ...window.location, assign })
  })
  afterEach(() => assign.mockReset())

  it('sends the person from the plans to the payment page, with what Stripe said', async () => {
    const details = { reason: 'authentication_required', pay_url: PAY_URL, plan: 'pro' }
    const { client } = setup({
      'POST /v1/billing/checkout': {
        status: 402,
        body: { error: { code: 'payment_failed', message: 'your bank asks you to confirm the payment', details } },
      },
    })
    const user = userEvent.setup()

    renderWithProviders(<PlansPage />, { client })
    await user.click(within(await screen.findByTestId('plan-pro')).getByRole('button', { name: 'Upgrade to Pro' }))
    await waitFor(() => expect(nav.push).toHaveBeenCalledWith('/plans/payment-failed'))
    expect(client.getQueryData<PendingPayment>(qk.pendingPayment)).toEqual({ ...details, pending: true })
    expect(screen.queryByText(/confirm the payment/)).not.toBeInTheDocument() // no toast
  })

  it('the bank asks for a confirmation: confirm it on Stripe, then the new plan shows', async () => {
    let pending: PendingPayment = { pending: true, reason: 'authentication_required', pay_url: PAY_URL, plan: 'pro' }
    const { client, api } = setup({ 'GET /v1/billing/pending-payment': () => pending })
    const user = userEvent.setup()

    renderWithProviders(<PaymentFailedPage />, { client })
    expect(await screen.findByRole('heading', { name: 'Confirm the payment with your bank' })).toBeInTheDocument()
    expect(await screen.findByText(/To start Pro, your bank asks you to confirm the payment/)).toBeInTheDocument()
    expect(screen.getByText(/your free trial goes on as before/)).toBeInTheDocument()
    const confirm = screen.getByRole('link', { name: 'Confirm the payment' })

    expect(confirm).toHaveAttribute('href', PAY_URL)
    expect(confirm).toHaveAttribute('target', '_blank')
    expect(screen.getByRole('link', { name: 'Back to plans' })).toHaveAttribute('href', '/plans')

    // Paid on Stripe's page: back here, the server finds nothing waiting, and Plan & usage syncs the new plan.
    pending = { pending: false }
    await user.click(screen.getByRole('button', { name: 'Check again' }))
    await waitFor(() => expect(nav.replace.mock.calls[0]?.[0]).toBe('/settings/plan?checkout=switched'))
    expect(api.find('GET', '/v1/billing/pending-payment').length).toBeGreaterThanOrEqual(2)
  })

  it('the card was declined: the reason in plain words and the billing portal to change the card', async () => {
    const { client, api } = setup({
      'GET /v1/billing/pending-payment': {
        pending: true,
        reason: 'insufficient_funds',
        decline_code: 'insufficient_funds',
        pay_url: PAY_URL,
        plan: 'pro',
      },
    })
    const user = userEvent.setup()

    renderWithProviders(<PaymentFailedPage />, { client })
    expect(await screen.findByRole('heading', { name: 'Your card was declined' })).toBeInTheDocument()
    expect(screen.getByText("The card doesn't have enough funds for this payment.")).toBeInTheDocument()
    expect(screen.queryByText('insufficient_funds')).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Confirm the payment' })).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Pay with another card' })).toHaveAttribute('href', PAY_URL)

    await user.click(screen.getByRole('button', { name: 'Update your card' }))
    await waitFor(() => expect(assign).toHaveBeenCalledWith('https://billing.stripe.test/p_1'))
    expect(api.find('POST', '/v1/billing/portal')).toHaveLength(1)
    // Still busy while the browser leaves for Stripe: a second click can't open another portal session.
    expect(screen.getByRole('button', { name: 'Update your card' })).toBeDisabled()
    expect(nav.replace).not.toHaveBeenCalled()
  })

  it('opened with nothing waiting: says so and stays', async () => {
    const { client } = setup({ 'GET /v1/billing/pending-payment': { pending: false } })

    renderWithProviders(<PaymentFailedPage />, { client })
    expect(await screen.findByRole('heading', { name: 'No payment is waiting' })).toBeInTheDocument()
    expect(nav.replace).not.toHaveBeenCalled()
  })
})
