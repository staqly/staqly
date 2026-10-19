import { cleanup, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { BillingPage } from '@/components/settings/billing-page'
import { AppShell } from '@/components/shell/app-shell'
import { qk } from '@/lib/queries'

import { nav } from './navigation'
import { PLANS, usage } from './plans-fixtures'
import { makeClient, mockApi, renderWithProviders, sessionFixture } from './utils'

const OFF = { enabled: false, subscription: null, payment_methods: [], invoices: [], has_more_invoices: false }

const BILLING_PRO = {
  enabled: true,
  plan: 'pro',
  subscription_status: 'active',
  plan_expires_at: '2026-11-18T10:00:00Z',
  suspended: false,
  has_customer: true,
}

function invoice(n: number) {
  return {
    id: `in_${n}`,
    number: `A-000${n}`,
    created: '2026-10-15T10:00:00Z',
    period_start: null,
    period_end: null,
    total: 3000,
    amount_paid: 3000,
    amount_due: 0,
    currency: 'usd',
    status: 'paid',
    hosted_invoice_url: `https://invoice.stripe.test/${n}`,
    invoice_pdf: `https://invoice.stripe.test/${n}.pdf`,
  }
}

const DETAILS = {
  enabled: true,
  subscription: {
    status: 'active',
    current_period_start: '2026-10-15T10:00:00Z',
    current_period_end: '2026-11-15T10:00:00Z',
    cancel_at_period_end: false,
    cancel_at: null,
    canceled_at: null,
    trial_end: null,
    amount: 3000,
    quantity: 3,
    currency: 'usd',
    interval: 'month',
  },
  payment_methods: [{ brand: 'visa', last4: '4242', exp_month: 4, exp_year: 2030, default: true }],
  invoices: [invoice(2)],
  has_more_invoices: true,
}

describe('billing page', () => {
  it('without online payments: the plan, the usage and a note, no cards or invoices', async () => {
    mockApi({
      'GET /v1/billing': {
        ...BILLING_PRO,
        enabled: false,
        plan: 'starter',
        subscription_status: null,
        has_customer: false,
      },
      'GET /v1/billing/details': OFF,
      'GET /v1/workspace/usage': usage(PLANS[0], { posts_per_month: 12 }),
    })
    renderWithProviders(<BillingPage />, { mode: 'cloud' })

    expect(await screen.findByTestId('billing-off')).toHaveTextContent(/not enabled on this instance/)
    expect(await screen.findByTestId('billing-plan')).toHaveTextContent('Starter')
    expect(screen.getByRole('link', { name: /choose a plan/i })).toHaveAttribute('href', '/plans')
    expect(screen.queryByText('Invoices')).not.toBeInTheDocument()
  })

  it('a paid workspace: status, renewal, the card, invoices with links and older pages', async () => {
    const api = mockApi({
      'GET /v1/billing': BILLING_PRO,
      'GET /v1/billing/details': (call: { query: URLSearchParams }) =>
        call.query.get('starting_after') === 'in_2'
          ? { ...DETAILS, subscription: null, payment_methods: [], invoices: [invoice(1)], has_more_invoices: false }
          : DETAILS,
      'GET /v1/workspace/usage': usage(PLANS[1], { posts_per_month: 40 }),
    })
    const user = userEvent.setup()

    renderWithProviders(<BillingPage />, { mode: 'cloud' })

    const plan = await screen.findByTestId('billing-plan')

    await waitFor(() => expect(plan).toHaveTextContent('Active'))
    expect(plan).toHaveTextContent('$30.00 per user / month · 3 users = $90.00 / month')
    expect(screen.getByTestId('billing-renewal')).toHaveTextContent('Renews on Nov 15, 2026')
    expect(screen.getByTestId('payment-methods')).toHaveTextContent('4242')
    expect(screen.getByTestId('payment-methods')).toHaveTextContent('Expires 04/2030')

    const rows = screen.getAllByTestId('invoice-row')

    expect(rows).toHaveLength(1)
    expect(within(rows[0]).getByRole('link', { name: /view/i })).toHaveAttribute(
      'href',
      'https://invoice.stripe.test/2',
    )
    await user.click(screen.getByRole('button', { name: 'Show older invoices' }))
    await waitFor(() => expect(screen.getAllByTestId('invoice-row')).toHaveLength(2))
    expect(screen.queryByRole('button', { name: 'Show older invoices' })).not.toBeInTheDocument()
    expect(api.find('GET', '/v1/billing/details').some((c) => c.query.get('starting_after') === 'in_2')).toBe(true)
  })
})

// In the free Starter trial (an end date, no subscription) unless a billing state is given.
function shell(
  plan = PLANS[0],
  billing: unknown = { ...BILLING_PRO, plan: 'starter', subscription_status: null },
  onboarded = true,
) {
  const client = makeClient()
  const trial = (billing as { subscription_status: string | null }).subscription_status === null

  client.setQueryData(qk.session, { ...sessionFixture('cloud'), role: 'owner', accounts_enabled: true, onboarded })
  mockApi({
    'GET /v1/collections': { data: [] },
    'GET /v1/workspace/usage': { ...usage(plan), plan_expires_at: trial ? '2026-10-27T10:00:00Z' : null },
    'GET /v1/billing': billing,
    'GET /v1/plans': { data: PLANS },
  })
  nav.set('/schedule')
  renderWithProviders(
    <AppShell>
      <div>page</div>
    </AppShell>,
    { client },
  )
}

describe('user menu', () => {
  it('Getting started comes first while the welcome is unfinished, and is gone once it is done', async () => {
    shell(PLANS[0], undefined, false)
    const user = userEvent.setup()

    await user.click((await screen.findAllByTestId('user-menu-trigger'))[0])
    let menu = await screen.findByRole('menu')
    const items = within(menu).getAllByRole('menuitem')
    const first = items.findIndex((i) => i.textContent?.includes('Getting started'))

    expect(first).toBeGreaterThanOrEqual(0)
    expect(items.findIndex((i) => i.textContent?.includes('Choose a plan'))).toBeGreaterThan(first)
    expect(items.findIndex((i) => i.textContent?.includes('Billing'))).toBeGreaterThan(first)
    await user.keyboard('{Escape}')
    cleanup()

    shell(PLANS[0], undefined, true)
    await user.click((await screen.findAllByTestId('user-menu-trigger'))[0])
    menu = await screen.findByRole('menu')
    expect(within(menu).queryByTestId('menu-getting-started')).not.toBeInTheDocument()
  })

  it('in the trial: a highlighted Choose a plan and Billing', async () => {
    shell()
    const user = userEvent.setup()

    await user.click((await screen.findAllByTestId('user-menu-trigger'))[0])
    const menu = await screen.findByRole('menu')

    expect(await within(menu).findByTestId('menu-upgrade')).toHaveTextContent('Choose a plan')
    expect(within(menu).getByRole('menuitem', { name: 'Billing' })).toBeInTheDocument()
  })

  it('paying for Plus: Upgrade to the next plan, Pro', async () => {
    shell(PLANS[1], { ...BILLING_PRO, plan: 'plus' })
    const user = userEvent.setup()

    await user.click((await screen.findAllByTestId('user-menu-trigger'))[0])
    const menu = await screen.findByRole('menu')

    expect(await within(menu).findByTestId('menu-upgrade')).toHaveTextContent('Upgrade to Pro')
  })

  it('on the top plan (Business): Billing, no upgrade', async () => {
    shell(PLANS[3], { ...BILLING_PRO, plan: 'business' })
    const user = userEvent.setup()

    await user.click((await screen.findAllByTestId('user-menu-trigger'))[0])
    const menu = await screen.findByRole('menu')

    expect(within(menu).getByRole('menuitem', { name: 'Billing' })).toBeInTheDocument()
    await waitFor(() => expect(within(menu).queryByTestId('menu-upgrade')).not.toBeInTheDocument())
  })
})
