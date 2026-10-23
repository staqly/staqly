import { cleanup, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'

import { BillingRedirect } from '@/components/settings/billing-redirect'
import { PlanUsagePage } from '@/components/settings/plan-usage-page'
import { AppShell } from '@/components/shell/app-shell'
import { qk } from '@/lib/queries'

import { nav } from './navigation'
import { PLANS, UNLOCKED, usage } from './plans-fixtures'
import { makeClient, mockApi, renderWithProviders, sessionFixture } from './utils'

const OFF = {
  enabled: false,
  subscription: null,
  payment_methods: [],
  invoices: [],
  has_more_invoices: false,
  managed_payments: false,
}

const BILLING_PRO = {
  enabled: true,
  plan: 'pro',
  subscription_status: 'active',
  plan_expires_at: '2026-11-18T10:00:00Z',
  trial_ends_at: null,
  suspended: false,
  has_customer: true,
  seats: 3,
  trial: 'none',
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

const SUBSCRIPTION = {
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
}

const DETAILS = {
  enabled: true,
  subscription: SUBSCRIPTION,
  payment_methods: [{ brand: 'visa', last4: '4242', exp_month: 4, exp_year: 2030, default: true }],
  invoices: [invoice(2)],
  has_more_invoices: true,
  managed_payments: false,
}

const XPOSTS = {
  period: '2026-10',
  period_end: '2026-11-01T00:00:00Z',
  unlimited: false,
  included: 450,
  used: 40,
  included_left: 410,
  credits_left: 0,
  credits_expire_at: null,
  left: 410,
  bundles: [{ posts: 200, price: 10 }],
  currency: 'eur',
  link_post_weight: 14,
}

const inDays = (days: number) => new Date(Date.now() + days * 86_400_000 - 60_000).toISOString()

/** Settings, Plan & usage as an owner (cloud) unless a session says otherwise. */
function renderPlan(session: Record<string, unknown> = { role: 'owner', accounts_enabled: true }) {
  const client = makeClient()

  client.setQueryData(qk.session, { ...sessionFixture('cloud'), ...session })

  return renderWithProviders(<PlanUsagePage />, { client })
}

describe('plan & usage', () => {
  beforeEach(() => nav.set('/settings/plan'))

  it('one tab for plan, usage and billing: no Billing tab', async () => {
    mockApi({
      'GET /v1/billing': BILLING_PRO,
      'GET /v1/billing/details': DETAILS,
      'GET /v1/billing/addons': { data: [] },
      'GET /v1/billing/x-posts': XPOSTS,
      'GET /v1/workspace/usage': usage(PLANS[1]),
    })
    renderPlan()
    const tabs = await screen.findByRole('navigation', { name: 'Settings sections' })

    expect(within(tabs).getByRole('link', { name: 'Plan & usage' })).toHaveAttribute('aria-current', 'page')
    expect(within(tabs).queryByRole('link', { name: 'Billing' })).not.toBeInTheDocument()
  })

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
      'GET /v1/billing/addons': { data: [] },
      'GET /v1/billing/x-posts': XPOSTS,
      'GET /v1/workspace/usage': usage(PLANS[0], { posts_per_month: 12 }),
    })
    renderPlan()

    expect(await screen.findByTestId('billing-off')).toHaveTextContent(/not enabled on this server/)
    expect(screen.getByRole('heading', { level: 2, name: 'Starter plan' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /change plan/i })).toHaveAttribute('href', '/plans')
    expect(screen.getByTestId('usage-meters')).toBeInTheDocument()
    expect(screen.queryByTestId('payments')).not.toBeInTheDocument()
  })

  it('a paying workspace: status, price per user, seats, the next payment, the card, invoices and older pages', async () => {
    const api = mockApi({
      'GET /v1/billing': BILLING_PRO,
      'GET /v1/billing/details': (call: { query: URLSearchParams }) =>
        call.query.get('starting_after') === 'in_2'
          ? { ...DETAILS, subscription: null, payment_methods: [], invoices: [invoice(1)], has_more_invoices: false }
          : DETAILS,
      'GET /v1/billing/addons': { data: [] },
      'GET /v1/billing/x-posts': XPOSTS,
      'GET /v1/workspace/usage': usage(PLANS[1], { posts_per_month: 40 }),
    })
    const user = userEvent.setup()

    renderPlan()
    const summary = await screen.findByTestId('plan-summary')

    await waitFor(() => expect(within(summary).getByTestId('plan-status')).toHaveTextContent('Active'))
    expect(within(summary).getByTestId('plan-facts')).toHaveTextContent('$30 per user / month')
    expect(within(summary).getByTestId('plan-facts')).toHaveTextContent('3 users')
    // The day in this machine's time zone (10:00 UTC is the 16th at UTC+14, the 14th west of UTC-10).
    const due = new Date(SUBSCRIPTION.current_period_end).toLocaleDateString('en-US', {
      month: 'short',
      day: 'numeric',
      year: 'numeric',
    })

    expect(within(summary).getByTestId('next-payment')).toHaveTextContent(`$90 on ${due}`)
    expect(within(summary).getByTestId('next-payment')).toHaveTextContent('3 users × $30, excl. VAT')
    expect(within(summary).getByRole('button', { name: /manage billing/i })).toBeInTheDocument()
    expect(within(summary).getByRole('link', { name: 'Change plan' })).toHaveAttribute('href', '/plans')
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

  it('platforms with an allowance of their own are lines inside Posts this month, not tiles of their own', async () => {
    mockApi({
      'GET /v1/billing': BILLING_PRO,
      'GET /v1/billing/details': DETAILS,
      'GET /v1/billing/addons': { data: [] },
      'GET /v1/billing/x-posts': XPOSTS,
      'GET /v1/workspace/usage': usage(PLANS[1], { posts_per_month: 40, x_posts_per_period: 12 }),
    })
    renderPlan()

    const posts = await screen.findByTestId('meter-Posts this month')
    const x = within(posts).getByTestId('platform-posts-x')

    expect(x).toHaveTextContent('X')
    expect(x).toHaveTextContent('12 / 450')
    expect(within(posts).getByRole('progressbar', { name: 'X posts' })).toHaveAttribute('aria-valuenow', '12')
    expect(screen.queryByTestId('meter-X posts this month')).not.toBeInTheDocument()
  })

  it("Stripe's 7-day trial shows 7 days left: it counts to the trial's end, not the plan's end with its grace", async () => {
    const trialEnd = inDays(7)

    mockApi({
      'GET /v1/billing': {
        ...BILLING_PRO,
        plan: 'starter',
        subscription_status: 'trialing',
        plan_expires_at: inDays(10),
        trial_ends_at: trialEnd,
      },
      'GET /v1/billing/details': {
        ...DETAILS,
        subscription: {
          ...SUBSCRIPTION,
          status: 'trialing',
          trial_end: trialEnd,
          current_period_end: trialEnd,
          amount: 1500,
        },
        invoices: [],
        has_more_invoices: false,
      },
      'GET /v1/billing/addons': { data: [] },
      'GET /v1/billing/x-posts': XPOSTS,
      'GET /v1/workspace/usage': { ...usage(PLANS[0]), plan_expires_at: inDays(10), trial_ends_at: trialEnd },
    })
    renderPlan()
    const summary = await screen.findByTestId('plan-summary')

    expect(within(summary).getByTestId('plan-status')).toHaveTextContent('Trial · 7 days left')
    expect(within(summary).getByTestId('plan-message')).toHaveTextContent('the card on file is charged')
    await waitFor(() => expect(within(summary).getByTestId('next-payment')).toHaveTextContent('$45 on'))
    expect(await screen.findByText('No invoices yet')).toBeInTheDocument()
    expect(screen.getByText(/not charged until it ends/)).toBeInTheDocument()
  })

  it('the 7-day trial of a server without billing shows 7 days left and Choose a plan', async () => {
    const end = inDays(7)

    mockApi({
      'GET /v1/billing': {
        ...BILLING_PRO,
        enabled: false,
        plan: 'starter',
        subscription_status: null,
        has_customer: false,
        plan_expires_at: end,
        trial_ends_at: end,
      },
      'GET /v1/billing/details': OFF,
      'GET /v1/billing/addons': { data: [] },
      'GET /v1/billing/x-posts': XPOSTS,
      'GET /v1/workspace/usage': { ...usage(PLANS[0]), plan_expires_at: end, trial_ends_at: end },
    })
    renderPlan()
    const summary = await screen.findByTestId('plan-summary')

    expect(within(summary).getByTestId('plan-status')).toHaveTextContent('Trial · 7 days left')
    expect(within(summary).getByRole('link', { name: /choose a plan/i })).toHaveAttribute('href', '/plans')
  })

  it('Managed Payments: no invoices here says Link sends them', async () => {
    mockApi({
      'GET /v1/billing': BILLING_PRO,
      'GET /v1/billing/details': { ...DETAILS, invoices: [], has_more_invoices: false, managed_payments: true },
      'GET /v1/billing/addons': { data: [] },
      'GET /v1/billing/x-posts': XPOSTS,
      'GET /v1/workspace/usage': usage(PLANS[1]),
    })
    renderPlan()

    expect(await screen.findByText('No invoices yet')).toBeInTheDocument()
    expect(screen.getByText(/emails from Link/)).toBeInTheDocument()
  })

  it('people who do not manage the workspace see the plan and usage, no billing', async () => {
    const api = mockApi({
      'GET /v1/billing/x-posts': XPOSTS,
      'GET /v1/workspace/usage': usage(PLANS[1], { posts_per_month: 40 }),
    })

    renderPlan({ role: 'editor', accounts_enabled: true })
    expect(await screen.findByRole('heading', { level: 2, name: 'Plus plan' })).toBeInTheDocument()
    expect(screen.getByText('Owners and admins of this workspace change the plan.')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /change plan/i })).not.toBeInTheDocument()
    expect(screen.queryByTestId('payments')).not.toBeInTheDocument()
    expect(await screen.findByTestId('x-posts')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Buy more X posts' })).not.toBeInTheDocument()
    expect(api.find('GET', '/v1/billing')).toHaveLength(0)
    expect(api.find('GET', '/v1/billing/details')).toHaveLength(0)
    expect(api.find('GET', '/v1/billing/addons')).toHaveLength(0)
  })

  it('local: unlocked, no billing at all', async () => {
    const api = mockApi({ 'GET /v1/workspace/usage': usage(UNLOCKED) })

    renderWithProviders(<PlanUsagePage />, { mode: 'local' })
    expect(await screen.findByRole('heading', { level: 2, name: 'Community Edition' })).toBeInTheDocument()
    expect(screen.queryByTestId('plan-facts')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /manage billing/i })).not.toBeInTheDocument()
    expect(api.calls.filter((c) => c.path.startsWith('/v1/billing'))).toHaveLength(0)
  })

  it('back from Stripe Checkout: a toast, and the query leaves the address', async () => {
    window.history.replaceState(null, '', '/settings/plan?checkout=success')
    mockApi({
      'GET /v1/billing': BILLING_PRO,
      'GET /v1/billing/details': DETAILS,
      'GET /v1/billing/addons': { data: [] },
      'GET /v1/billing/x-posts': XPOSTS,
      'GET /v1/workspace/usage': usage(PLANS[1]),
    })
    renderPlan()

    expect(await screen.findByText('Payment received')).toBeInTheDocument()
    expect(window.location.search).toBe('')
  })

  it('/settings/billing forwards to Plan & usage with its query (old links, Stripe return URLs)', async () => {
    window.history.replaceState(null, '', '/settings/billing?checkout=switched')
    renderWithProviders(<BillingRedirect />)
    await waitFor(() => expect(nav.replace.mock.calls[0]?.[0]).toBe('/settings/plan?checkout=switched'))
    window.history.replaceState(null, '', '/')
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
    'GET /v1/workspace/usage': {
      ...usage(plan),
      plan_expires_at: trial ? '2026-10-27T10:00:00Z' : null,
      trial_ends_at: trial ? inDays(3) : null,
    },
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

  it('in the trial: the sidebar counts the days down and leads to the plans', async () => {
    shell()

    const banner = await screen.findByTestId('trial-banner')

    expect(banner).toHaveTextContent('3 days left in your trial')
    expect(within(banner).getByRole('link', { name: /Upgrade/ })).toHaveAttribute('href', '/plans')
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
