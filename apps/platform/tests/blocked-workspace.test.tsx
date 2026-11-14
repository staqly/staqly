import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'

import { PlanUsagePage } from '@/components/settings/plan-usage-page'
import { AppShell } from '@/components/shell/app-shell'
import type { Addon, Billing, BillingDetails, XPosts } from '@/lib/api'
import { lockReason } from '@/lib/plans'
import { qk } from '@/lib/queries'
import { shortDay } from '@/lib/x-posts'

import { nav } from './navigation'
import { PLANS, usage } from './plans-fixtures'
import { makeClient, mockApi, renderWithProviders, sessionFixture } from './utils'

const BILLING: Billing = {
  enabled: true,
  plan: 'plus',
  subscription_status: 'canceled',
  plan_expires_at: '2026-11-01T10:00:00Z',
  trial_ends_at: null,
  suspended: true,
  has_customer: true,
  seats: 1,
  trial: 'used',
  trial_review: null,
  trial_revoked: null,
  ended_for: null,
}

const SUBSCRIPTION: NonNullable<BillingDetails['subscription']> = {
  status: 'canceled',
  current_period_start: '2026-10-01T10:00:00Z',
  current_period_end: '2026-11-01T10:00:00Z',
  cancel_at_period_end: false,
  cancel_at: null,
  canceled_at: '2026-11-12T10:00:00Z',
  trial_end: null,
  amount: 2900,
  quantity: 1,
  currency: 'eur',
  interval: 'month',
  past_due_for: '',
  discount: null,
}

const OPEN_INVOICE: BillingDetails['invoices'][number] = {
  id: 'in_open',
  number: 'A-0029',
  created: '2026-11-01T10:00:00Z',
  period_start: null,
  period_end: null,
  total: 973,
  amount_paid: 0,
  amount_due: 973,
  currency: 'eur',
  status: 'open',
  hosted_invoice_url: 'https://invoice.stripe.test/open',
  invoice_pdf: 'https://invoice.stripe.test/open.pdf',
  amount_refunded: 0,
  disputed: false,
}

const XPOSTS: XPosts = {
  period: '2026-11',
  period_end: '2026-12-01T00:00:00Z',
  unlimited: false,
  included: 100,
  used: 0,
  included_left: 100,
  credits_left: 0,
  credits_expire_at: null,
  left: 100,
  bundles: [{ posts: 200, price: 10 }],
  currency: 'eur',
  link_post_weight: 14,
}

const PACK: Addon = {
  id: 'channels_pack',
  name: 'Channels pack',
  description: '10 more channels',
  features: [],
  price_month: 9,
  price_year: 86,
  currency: 'eur',
  active: false,
}

/** What a workspace with this billing state answers; `suspended` is the lock. */
function routes(billing: Record<string, unknown>, suspended: boolean) {
  const sub = { ...SUBSCRIPTION, status: billing.subscription_status }

  return {
    'GET /v1/billing': { ...BILLING, suspended, ...billing },
    'GET /v1/billing/details': {
      enabled: true,
      subscription: sub,
      payment_methods: [],
      invoices: [OPEN_INVOICE],
      has_more_invoices: false,
      managed_payments: false,
    },
    'GET /v1/billing/addons': { data: [PACK] },
    'GET /v1/billing/x-posts': XPOSTS,
    'GET /v1/workspace/usage': { ...usage(PLANS[1]), suspended },
  }
}

function client(role = 'owner') {
  const c = makeClient()

  c.setQueryData(qk.session, { ...sessionFixture('cloud'), role, accounts_enabled: true })

  return c
}

function renderPlan(billing: Record<string, unknown> = {}) {
  nav.set('/settings/plan')
  const api = mockApi(routes(billing, true))

  renderWithProviders(<PlanUsagePage />, { client: client() })

  return api
}

let api: ReturnType<typeof mockApi>

function renderShell(path: string, billing: Record<string, unknown>, suspended: boolean, role = 'owner') {
  nav.set(path)
  api = mockApi(routes(billing, suspended))

  return renderWithProviders(
    <AppShell>
      <p>the page</p>
    </AppShell>,
    { client: client(role) },
  )
}

describe('a locked workspace', () => {
  beforeEach(() => localStorage.clear())

  it('reads the reason off billing: a live subscription is paid, an ended one needs a new plan', () => {
    expect(lockReason({ subscription_status: 'past_due' })).toBe('payment_due')
    expect(lockReason({ subscription_status: 'unpaid', ended_for: null })).toBe('payment_due')
    expect(lockReason({ subscription_status: 'canceled', ended_for: 'payment_failed' })).toBe('payment_failed')
    expect(lockReason({ subscription_status: 'canceled', ended_for: null })).toBe('ended')
    // A server without the field.
    expect(lockReason({ subscription_status: 'canceled' })).toBe('ended')
  })

  it('9a: Plan & usage says locked like every other page, and offers the export that still works', async () => {
    renderPlan()

    const message = await screen.findByTestId('plan-message')

    await waitFor(() => expect(message).toHaveTextContent(/^Your plan has ended, so this workspace is locked\./))
    expect(message).not.toHaveTextContent(/read-only|read and export everything/)
    expect(screen.getByTestId('plan-status')).toHaveTextContent('Locked')
    expect(within(message).getByRole('link', { name: 'export your data' })).toHaveAttribute('href', '/settings/data')
  })

  it('9a: the locked screen leads to the export, and Data & privacy stays open to who manages the workspace', async () => {
    const { unmount } = renderShell('/posts', {}, true)
    const locked = await screen.findByTestId('plan-locked')

    expect(within(locked).getByRole('link', { name: 'Export your data' })).toHaveAttribute('href', '/settings/data')
    unmount()

    renderShell('/settings/data', {}, true)
    expect(await screen.findByText('the page')).toBeInTheDocument()
    expect(screen.queryByTestId('plan-locked')).not.toBeInTheDocument()
  })

  it('9a: an editor sees the lock on Data & privacy too, with no actions', async () => {
    renderShell('/settings/data', {}, true, 'editor')

    const locked = await screen.findByTestId('plan-locked')

    expect(locked).toHaveTextContent('An owner or admin can open it again in Plan & usage.')
    expect(within(locked).queryByRole('link')).not.toBeInTheDocument()
  })

  it('9b: an ended plan is opened by choosing a plan, never by paying its old invoice', async () => {
    const { unmount } = renderShell('/posts', {}, true)
    const locked = await screen.findByTestId('plan-locked')

    expect(await within(locked).findByRole('link', { name: 'Choose a plan' })).toHaveAttribute('href', '/plans')
    expect(locked).toHaveTextContent('Its plan has ended. Choose a plan and the workspace opens again')
    expect(locked).not.toHaveTextContent(/pay/i)
    unmount()

    renderPlan()

    const row = await screen.findByTestId('invoice-row')

    expect(row).toHaveTextContent('Unpaid')
    expect(row).not.toHaveTextContent('Due')
    expect(within(row).getByRole('link', { name: /View/ })).toHaveAttribute('href', 'https://invoice.stripe.test/open')
    expect(within(row).queryByRole('link', { name: /Pay/ })).not.toBeInTheDocument()
    expect(screen.getByTestId('invoices-plan-ended')).toHaveTextContent('does not open the workspace')
    expect(within(screen.getByTestId('plan-summary')).getByRole('link', { name: 'Choose a plan' })).toHaveAttribute(
      'href',
      '/plans',
    )
  })

  it('9b: a subscription Stripe still holds is opened by paying: the invoice stays Due / Pay', async () => {
    const { unmount } = renderShell('/posts', { subscription_status: 'past_due' }, true)
    const locked = await screen.findByTestId('plan-locked')

    expect(await within(locked).findByRole('link', { name: 'Update payment' })).toHaveAttribute(
      'href',
      '/settings/plan',
    )
    expect(locked).toHaveTextContent('Update the payment method or pay the open invoice in Plan & usage')
    unmount()

    renderPlan({ subscription_status: 'past_due' })

    const row = await screen.findByTestId('invoice-row')

    expect(row).toHaveTextContent('Due')
    expect(within(row).getByRole('link', { name: /Pay/ })).toBeInTheDocument()
    expect(screen.queryByTestId('invoices-plan-ended')).not.toBeInTheDocument()
    await waitFor(() =>
      expect(screen.getByTestId('plan-message')).toHaveTextContent(
        'The last payment failed, so this workspace is locked. Update the payment method or pay the open invoice',
      ),
    )
    expect(within(screen.getByTestId('plan-summary')).getByRole('link', { name: 'Change plan' })).toBeInTheDocument()
  })

  it('9c: add-ons and extra X posts cannot be bought while locked, and say why', async () => {
    const calls = renderPlan()
    const user = userEvent.setup()
    const add = await screen.findByRole('button', { name: /Add/ })
    const buy = await screen.findByRole('button', { name: 'Buy more X posts' })

    expect(add).toHaveAttribute('aria-disabled', 'true')
    expect(buy).toHaveAttribute('aria-disabled', 'true')

    await user.click(add)
    await user.click(buy)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    expect(calls.find('POST', '/v1/billing/addons/channels_pack')).toHaveLength(0)

    add.focus() // moved there from another control
    buy.focus()
    expect(await screen.findByRole('tooltip')).toHaveTextContent(
      'Not while the workspace is locked. Choose a plan first.',
    )
  })

  it('9d: says the payment failed when the API names it, on the locked screen and on Plan & usage', async () => {
    const { unmount } = renderShell('/posts', { ended_for: 'payment_failed' }, true)
    const locked = await screen.findByTestId('plan-locked')

    await waitFor(() =>
      expect(locked).toHaveTextContent('The payment for its plan failed, so the plan has ended. Choose a plan'),
    )
    unmount()

    renderPlan({ ended_for: 'payment_failed' })
    await waitFor(() =>
      expect(screen.getByTestId('plan-message')).toHaveTextContent(
        'The payment for your plan failed, so the plan ended and this workspace is locked. Choose a plan',
      ),
    )
  })
})

describe('a payment that failed while Stripe still retries', () => {
  beforeEach(() => localStorage.clear())

  it('10: owners read it on every page, with the way to Plan & usage', async () => {
    renderShell('/posts', { subscription_status: 'past_due' }, false)

    const notice = await screen.findByTestId('past-due-notice')

    expect(notice).toHaveTextContent(
      'The last payment for this workspace failed. Update the payment method to keep it open.',
    )
    expect(within(notice).getByRole('link', { name: 'Plan & usage' })).toHaveAttribute('href', '/settings/plan')
    expect(screen.getByText('the page')).toBeInTheDocument()
  })

  it('10: admins are told who can fix it; other roles, a paid workspace and Plan & usage itself show nothing', async () => {
    const admin = renderShell('/posts', { subscription_status: 'past_due' }, false, 'admin')

    expect(await screen.findByTestId('past-due-notice')).toHaveTextContent('Its owner can update the payment method.')
    admin.unmount()

    for (const [path, status, role] of [
      ['/posts', 'past_due', 'editor'],
      ['/posts', 'active', 'owner'],
      ['/settings/plan', 'past_due', 'owner'],
    ] as const) {
      const view = renderShell(path, { subscription_status: status }, false, role)

      expect(await screen.findByText('the page')).toBeInTheDocument()
      // Owners: billing has answered, so the notice would be there by now. Editors never ask for it.
      await waitFor(() => expect(api.find('GET', '/v1/billing')).toHaveLength(role === 'owner' ? 1 : 0))
      await new Promise((r) => setTimeout(r, 20))
      expect(screen.queryByTestId('past-due-notice')).not.toBeInTheDocument()
      view.unmount()
    }
  })
})

describe('a locked workspace whose payment Stripe still retries', () => {
  beforeEach(() => localStorage.clear())

  it('shows the locked screen alone, not the past-due strip over it', async () => {
    renderShell('/posts', { subscription_status: 'past_due' }, true)

    expect(await screen.findByTestId('plan-locked')).toHaveTextContent('The last payment did not go through.')
    await waitFor(() => expect(api.find('GET', '/v1/billing').length).toBeGreaterThan(0))
    expect(screen.queryByTestId('past-due-notice')).not.toBeInTheDocument()
    expect(screen.queryByText('the page')).not.toBeInTheDocument()
  })
})

describe('short days', () => {
  it('carry the year when it is not this one', () => {
    const now = Date.parse('2026-11-13T12:00:00Z')

    expect(shortDay('2026-11-30T00:00:00Z', now)).toBe('Nov 30')
    expect(shortDay('2027-11-12T00:00:00Z', now)).toBe('Nov 12, 2027')
    expect(shortDay(null, now)).toBe('')
  })
})

describe('a payment that failed mid-period, or still waits', () => {
  it('a failed seat or add-on payment names what failed and promises no date', async () => {
    nav.set('/settings/plan')
    const r = routes({ subscription_status: 'past_due' }, false)

    mockApi({
      ...r,
      'GET /v1/billing/details': {
        ...r['GET /v1/billing/details'],
        subscription: { ...r['GET /v1/billing/details'].subscription, past_due_for: 'extra' },
      },
      'GET /v1/billing/pending-payment': { pending: false },
    })
    renderWithProviders(<PlanUsagePage />, { client: client() })

    await waitFor(() =>
      expect(screen.getByTestId('plan-message')).toHaveTextContent('The payment for an extra seat or an add-on failed'),
    )
    expect(screen.getByTestId('plan-message')).not.toHaveTextContent('stays active until')
    expect(screen.queryByTestId('pending-payment')).not.toBeInTheDocument()
  })

  it('an upgrade or add-on waiting for its payment is said on Plan & usage, with the way to what to do', async () => {
    nav.set('/settings/plan')
    mockApi({
      ...routes({ subscription_status: 'active' }, false),
      'GET /v1/billing/pending-payment': { pending: true, reason: 'card_declined', addon: 'channels_pack' },
    })
    renderWithProviders(<PlanUsagePage />, { client: client() })
    const notice = await screen.findByTestId('pending-payment')

    expect(notice).toHaveTextContent('An add-on is waiting for its payment')
    expect(within(notice).getByRole('link', { name: 'See what to do' })).toHaveAttribute(
      'href',
      '/plans/payment-failed',
    )
  })
})
