import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { teamTotal } from '@/components/plans/plan-cards'
import { PlansPage } from '@/components/plans/plans-page'
import { qk } from '@/lib/queries'

import { nav } from './navigation'
import { PLANS, usage } from './plans-fixtures'
import { makeClient, mockApi, renderWithProviders, sessionFixture } from './utils'

const SUB = {
  status: 'active',
  current_period_start: '2026-10-01T00:00:00Z',
  current_period_end: '2026-11-01T00:00:00Z',
  cancel_at_period_end: false,
  cancel_at: null,
  canceled_at: null,
  trial_end: null,
  amount: 2900,
  currency: 'usd',
  interval: 'month',
}

function render(paying: boolean, trial = paying ? 'none' : 'available') {
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
      ...usage(paying ? PLANS[1] : PLANS[0]),
      trial_ends_at: paying ? null : new Date(Date.now() + 3 * 86_400_000 - 60_000).toISOString(),
    },
    'GET /v1/billing': {
      enabled: true,
      plan: paying ? 'plus' : 'starter',
      subscription_status: paying ? 'active' : null,
      plan_expires_at: '2026-11-04T00:00:00Z',
      suspended: false,
      has_customer: paying,
      trial,
    },
    'GET /v1/billing/details': {
      enabled: true,
      subscription: paying ? SUB : null,
      payment_methods: [],
      invoices: [],
      has_more_invoices: false,
    },
    'POST /v1/billing/checkout': { url: 'https://checkout.stripe.test/cs_1' },
  })

  renderWithProviders(<PlansPage />, { client })

  return api
}

describe('plan picker', () => {
  const assign = vi.fn<(url: string) => void>()

  vi.stubGlobal('location', { ...window.location, assign })
  afterEach(() => assign.mockReset())

  it('in the trial: four plans to choose, monthly or yearly in whole euros a month', async () => {
    const api = render(false)
    const user = userEvent.setup()

    const plus = await screen.findByTestId('plan-plus')

    expect(screen.getByTestId('plans-trial-ends')).toHaveTextContent(/Your free trial ends on .+, in 3 days/)
    expect(screen.getByRole('link', { name: 'Back' })).toBeInTheDocument()
    expect(within(plus).getByText('€29')).toBeInTheDocument()
    expect(screen.queryByText('Your current plan')).not.toBeInTheDocument() // nothing is paid for yet
    await user.click(screen.getByRole('radio', { name: /yearly/i }))
    expect(within(plus).getByText('€23')).toBeInTheDocument()
    expect(within(plus).getByText('€276 per user, billed yearly, excl. VAT')).toBeInTheDocument()
    expect(within(screen.getByTestId('plan-starter')).getByText('€12')).toBeInTheDocument()
    await user.click(within(plus).getByRole('button', { name: 'Choose Plus' }))
    await waitFor(() => expect(api.find('POST', '/v1/billing/checkout')).toHaveLength(1))
    expect(api.find('POST', '/v1/billing/checkout')[0].body).toEqual({ plan: 'plus', interval: 'year' })
    await waitFor(() => expect(assign).toHaveBeenCalledWith('https://checkout.stripe.test/cs_1'))
  })

  it('paying for Plus monthly: the current card, upgrades and downgrades switch the subscription', async () => {
    const api = render(true)
    const user = userEvent.setup()

    const plus = await screen.findByTestId('plan-plus')

    await waitFor(() => expect(within(plus).getByRole('button', { name: 'Your current plan' })).toBeDisabled())
    expect(within(screen.getByTestId('plan-starter')).getByRole('button', { name: 'Switch to Starter' })).toBeEnabled()
    await user.click(within(screen.getByTestId('plan-pro')).getByRole('button', { name: 'Upgrade to Pro' }))
    await waitFor(() =>
      expect(api.find('POST', '/v1/billing/checkout')[0]?.body).toEqual({ plan: 'pro', interval: 'month' }),
    )
    // The same plan billed yearly is a switch too.
    await user.click(screen.getByRole('radio', { name: /yearly/i }))
    expect(within(plus).getByRole('button', { name: 'Switch to yearly' })).toBeEnabled()
  })

  it('a promotion code goes with a switch, and can be applied to the current plan', async () => {
    const api = render(true)
    const user = userEvent.setup()

    const plus = await screen.findByTestId('plan-plus')

    await waitFor(() => expect(within(plus).getByRole('button', { name: 'Your current plan' })).toBeDisabled())
    await user.type(screen.getByLabelText('Promotion code'), 'BIZ-5UUS-DUQR')
    await user.click(within(screen.getByTestId('plan-business')).getByRole('button', { name: 'Upgrade to Business' }))
    await waitFor(() =>
      expect(api.find('POST', '/v1/billing/checkout')[0]?.body).toEqual({
        plan: 'business',
        interval: 'month',
        promotion_code: 'BIZ-5UUS-DUQR',
      }),
    )
    await user.click(within(plus).getByRole('button', { name: 'Apply the code to this plan' }))
    await waitFor(() =>
      expect(api.find('POST', '/v1/billing/checkout')[1]?.body).toEqual({
        plan: 'plus',
        interval: 'month',
        promotion_code: 'BIZ-5UUS-DUQR',
      }),
    )
  })
})

describe('from the pricing page', () => {
  it('opens on the billing period picked there', async () => {
    nav.set('/plans', 'plan=pro&interval=year')
    render(false)

    const plus = await screen.findByTestId('plan-plus')

    expect(screen.getByRole('radio', { name: /yearly/i })).toHaveAttribute('aria-checked', 'true')
    expect(within(plus).getByText('€23')).toBeInTheDocument()
    nav.reset()
  })
})

describe('per user pricing', () => {
  it('a team pays the seat price times its members', () => {
    const plus = PLANS.find((p) => p.id === 'plus')!

    expect(teamTotal(plus, 'month', 3)).toBe('3 users × €29 = €87/month')
    expect(teamTotal(plus, 'year', 2)).toBe('2 users × €276 = €552/year')
  })

  it('says before checkout whether Starter starts a free trial or is charged today', async () => {
    render(false)

    const starter = await screen.findByTestId('plan-starter')

    expect(within(starter).getByRole('button', { name: 'Start 7-day free trial' })).toBeInTheDocument()
    expect(within(starter).getByTestId('plan-charge-starter')).toHaveTextContent(/Free for 7 days, card required/)
    expect(within(screen.getByTestId('plan-plus')).getByTestId('plan-charge-plus')).toHaveTextContent('Charged today.')
  })

  it('a second workspace while another is in its trial: Starter is charged today, and says why', async () => {
    render(false, 'other_workspace')

    const starter = await screen.findByTestId('plan-starter')

    expect(within(starter).getByRole('button', { name: 'Choose Starter' })).toBeInTheDocument()
    expect(within(starter).getByTestId('plan-charge-starter')).toHaveTextContent(
      'Charged today: another workspace of yours is in its free trial.',
    )
  })
})
