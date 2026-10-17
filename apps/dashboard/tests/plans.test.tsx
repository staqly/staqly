import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { AddItemDialog } from '@/components/collections/add-item-dialog'
import { PlanUsagePage } from '@/components/settings/plan-usage-page'
import { WebhookFormSheet } from '@/components/webhooks/webhook-form-sheet'
import { describePlanLimit } from '@/lib/notify'
import { usageMeter } from '@/lib/usage'

import { nav } from './navigation'
import { PLANS, UNLOCKED, usage } from './plans-fixtures'
import { mockApi, renderWithProviders } from './utils'

const meterLevel = (used: number, max: number | null | undefined) => usageMeter(used, max).level

const LIMIT_402 = {
  status: 402,
  body: {
    error: {
      code: 'plan_limit_exceeded',
      message: 'The Free plan allows 3 webhooks; upgrade the workspace plan to continue.',
      details: { limit: 'webhooks', max: 3, used: 3, plan: 'free' },
    },
  },
}

describe('plan & usage page', () => {
  it('cloud: shows the current plan, usage meters with warning levels and the plan cards', async () => {
    mockApi({
      'GET /v1/workspace/usage': usage(PLANS[0], {
        decisions_per_month: 43,
        items_per_month: 1234,
        webhooks: 3,
        api_keys: 1,
        storage_bytes: 200 << 20,
      }),
      'GET /v1/plans': { data: PLANS },
    })
    renderWithProviders(<PlanUsagePage />, { mode: 'cloud' })

    expect(await screen.findByRole('heading', { name: /free plan/i })).toBeInTheDocument()
    const approvals = await screen.findByTestId('meter-Approvals this month')

    expect(approvals).toHaveAttribute('data-level', 'warn') // 86% → amber
    expect(within(approvals).getByText('43')).toBeInTheDocument()
    expect(within(approvals).getByText(/50/)).toBeInTheDocument()
    expect(within(approvals).getByText(/almost full/i)).toBeInTheDocument()
    // Items and collections are unlimited on every plan.
    expect(screen.getByTestId('meter-Items this month')).toHaveAttribute('data-level', 'unlimited')

    const hooks = screen.getByTestId('meter-Webhooks')

    expect(hooks).toHaveAttribute('data-level', 'full') // 3/3 → red
    expect(within(hooks).getByText(/limit reached/i)).toBeInTheDocument()
    expect(within(hooks).getByRole('progressbar')).toHaveAttribute('aria-valuetext', '3 of 3')

    expect(screen.getByTestId('meter-API keys')).toHaveAttribute('data-level', 'full') // 1/1: Free has one key
    expect(within(screen.getByTestId('meter-Storage')).getByText('200 MB')).toBeInTheDocument()

    // This period's items by status, and the webhook delivery backlog (fixture: 2 pending, 1 approved, 1 failed delivery).
    const period = screen.getByTestId('period-items')

    expect(within(period).getByText('3')).toBeInTheDocument()
    expect(within(period).getByRole('link', { name: /2\s*pending/i })).toHaveAttribute('href', '/inbox?step=review')
    expect(within(screen.getByTestId('webhook-deliveries')).getByText('Failed').nextSibling).toHaveTextContent('1')

    // Plan cards with the current plan marked; without Stripe, upgrades go through the operator.
    expect(await screen.findByTestId('plan-free')).toHaveAttribute('data-current', 'true')
    expect(within(screen.getByTestId('plan-free')).getByText('Current plan')).toBeInTheDocument()
    const upgrades = screen.getAllByRole('button', { name: /upgrade — ask the operator/i })

    expect(upgrades).toHaveLength(1)
    upgrades.forEach((b) => expect(b).toBeDisabled())
  })

  it('with billing enabled, Upgrade starts a Stripe Checkout', async () => {
    const assign = vi.fn<(url: string) => void>()

    vi.stubGlobal('location', { ...window.location, assign, search: '', pathname: '/settings/plan' })
    const { calls } = mockApi({
      'GET /v1/workspace/usage': usage(PLANS[0], {}),
      'GET /v1/plans': { data: PLANS },
      'GET /v1/billing': {
        enabled: true,
        plan: 'free',
        subscription_status: null,
        plan_expires_at: null,
        suspended: false,
        has_customer: false,
      },
      'POST /v1/billing/checkout': { url: 'https://checkout.stripe.com/c/pay/cs_test_a1b2c3' },
    })

    renderWithProviders(<PlanUsagePage />, { mode: 'cloud' })

    await userEvent.click(await screen.findByRole('button', { name: 'Upgrade to Pro' }))
    await waitFor(() => expect(assign).toHaveBeenCalledWith('https://checkout.stripe.com/c/pay/cs_test_a1b2c3'))
    expect(calls.some((c) => c.method === 'POST' && c.path === '/v1/billing/checkout')).toBe(true)
    vi.unstubAllGlobals()
  })

  it('shows ∞ for unlimited limits', async () => {
    mockApi({
      'GET /v1/workspace/usage': usage(PLANS[1], { items_per_month: 4321, webhooks: 12, api_keys: 7 }),
      'GET /v1/plans': { data: PLANS },
    })
    renderWithProviders(<PlanUsagePage />, { mode: 'cloud' })
    const items = await screen.findByTestId('meter-Items this month')

    expect(items).toHaveAttribute('data-level', 'unlimited')
    expect(within(items).getByText('4,321')).toBeInTheDocument()
    expect(within(items).getByLabelText('unlimited')).toHaveTextContent('∞')
    // Storage is still limited on Pro.
    expect(screen.getByTestId('meter-Storage')).toHaveAttribute('data-level', 'ok')
  })

  it('cloud: Pro trial shows the days left', async () => {
    const ends = new Date(Date.now() + 3 * 86_400_000 - 60_000).toISOString()

    mockApi({
      'GET /v1/workspace/usage': { ...usage(PLANS[1]), plan_expires_at: ends, suspended: false },
      'GET /v1/plans': { data: PLANS },
    })
    renderWithProviders(<PlanUsagePage />, { mode: 'cloud' })
    expect(await screen.findByTestId('plan-period')).toHaveTextContent('Pro trial: 3 days left')
  })

  it('cloud: a suspended workspace is read-only and can switch to Free', async () => {
    const user = userEvent.setup()
    const suspended = { ...usage(PLANS[1]), plan_expires_at: '2026-10-01T00:00:00Z', suspended: true }
    const api = mockApi({
      'GET /v1/workspace/usage': suspended,
      'GET /v1/plans': { data: PLANS },
      'POST /v1/workspace/plan': usage(PLANS[0]),
    })

    renderWithProviders(<PlanUsagePage />, { mode: 'cloud' })
    const banner = await screen.findByRole('alert')

    expect(banner).toHaveTextContent(/read-only/i)
    await user.click(within(banner).getByRole('button', { name: 'Switch to Free' }))
    await waitFor(() => expect(api.find('POST', '/v1/workspace/plan')).toHaveLength(1))
    expect(api.find('POST', '/v1/workspace/plan')[0].body).toEqual({ plan: 'free' })
    await waitFor(() => expect(screen.queryByTestId('plan-period')).not.toBeInTheDocument())
  })

  it('local: unlocked, no limits and no plan cards', async () => {
    const api = mockApi({
      'GET /v1/workspace/usage': usage(UNLOCKED, { items_per_month: 7, webhooks: 1 }),
    })

    renderWithProviders(<PlanUsagePage />, { mode: 'local' })
    expect(await screen.findByText(/unlocked — local install, no limits/i)).toBeInTheDocument()
    const items = await screen.findByTestId('meter-Items this month')

    expect(within(items).getByText('7')).toBeInTheDocument()
    expect(within(items).getByText('∞')).toBeInTheDocument()

    // Keyless local install without keys: no API keys meter.
    expect(screen.queryByTestId('meter-API keys')).not.toBeInTheDocument()

    for (const m of ['Webhooks', 'Storage']) {
      expect(screen.getByTestId(`meter-${m}`)).toHaveAttribute('data-level', 'unlimited')
    }

    expect(screen.queryByTestId('plan-grid')).not.toBeInTheDocument()
    expect(api.find('GET', '/v1/plans')).toHaveLength(0)
  })

  it("shows the watcher meter with the plan's watcher interval; no rules meter", async () => {
    mockApi({
      'GET /v1/workspace/usage': usage(PLANS[0], { watchers: 2 }),
      'GET /v1/plans': { data: PLANS },
    })
    renderWithProviders(<PlanUsagePage />, { mode: 'cloud' })
    const watchers = await screen.findByTestId('meter-Watchers')

    expect(screen.queryByTestId('meter-Rules')).not.toBeInTheDocument()

    expect(watchers).toHaveAttribute('data-level', 'ok')
    expect(within(watchers).getByRole('progressbar')).toHaveAttribute('aria-valuetext', '2 of 3')
    expect(watchers).toHaveTextContent('Active only · checks every 15 min at most')
  })

  it('local: the watcher meter is unlimited', async () => {
    mockApi({ 'GET /v1/workspace/usage': usage(UNLOCKED, { watchers: 9 }) })
    renderWithProviders(<PlanUsagePage />, { mode: 'local' })
    expect(await screen.findByTestId('meter-Watchers')).toHaveAttribute('data-level', 'unlimited')
    expect(within(screen.getByTestId('meter-Watchers')).getByText('9')).toBeInTheDocument()
  })

  it('meter levels: amber above 80%, red at 100%', () => {
    expect(meterLevel(80, 100)).toBe('ok')
    expect(meterLevel(81, 100)).toBe('warn')
    expect(meterLevel(100, 100)).toBe('full')
    expect(meterLevel(5, -1)).toBe('unlimited')
    // 0 means none allowed (full), a missing limit (older servers) is unlimited.
    expect(usageMeter(0, 0)).toEqual({ level: 'full', pct: 100 })
    expect(meterLevel(3, undefined)).toBe('unlimited')
    expect(meterLevel(3, null)).toBe('unlimited')
  })
})

describe('402 plan_limit_exceeded', () => {
  it('creating a webhook over the limit shows a friendly message with a View plans action', async () => {
    mockApi({
      'GET /v1/collections': { data: [] },
      'POST /v1/webhooks': LIMIT_402,
    })
    const user = userEvent.setup()

    renderWithProviders(<WebhookFormSheet open onOpenChange={() => {}} webhook={null} />, { mode: 'cloud' })
    await user.type(await screen.findByLabelText('Endpoint URL*'), 'https://example.com/hook')
    await user.click(screen.getByRole('button', { name: /create webhook/i }))

    expect(await screen.findByText('Plan limit reached')).toBeInTheDocument()
    expect(screen.getByText('Your Free plan includes 3 webhooks (3 used).')).toBeInTheDocument()
    const view = screen.getByRole('button', { name: 'View plans' })
    // Without a registered app router the action falls back to a full navigation; register one like <Providers> does.
    const { setNavigator } = await import('@/lib/notify')

    setNavigator((href) => nav.push(href))
    fireEvent.click(view)
    await waitFor(() => expect(nav.push).toHaveBeenCalledWith('/settings/plan'))
    setNavigator(null)
  })

  it('creating an item over a plan limit explains the limit', async () => {
    mockApi({
      'POST /v1/collections/col_1/items': {
        status: 402,
        body: {
          error: {
            code: 'plan_limit_exceeded',
            message: 'The Free plan allows 1.0 GB of storage; upgrade the workspace plan to continue.',
            details: { limit: 'storage_bytes', max: 1 << 30, used: 1 << 30, plan: 'free' },
          },
        },
      },
    })
    const user = userEvent.setup()

    renderWithProviders(<AddItemDialog collectionId="col_1" open onOpenChange={() => {}} />, { mode: 'cloud' })
    await user.click(await screen.findByRole('tab', { name: /text/i }))
    await user.type(screen.getByLabelText('Title*'), 'Hello')
    await user.type(screen.getByLabelText('Text*'), 'hello')
    await user.click(screen.getByRole('button', { name: /add item/i }))
    expect(await screen.findByText('Plan limit reached')).toBeInTheDocument()
    expect(screen.getByText('Your Free plan includes 1 GB of storage (1 GB used).')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'View plans' })).toBeInTheDocument()
  })

  it('describes byte limits in human units', () => {
    expect(describePlanLimit({ limit: 'max_upload_bytes', max: 25 << 20, plan: 'free' }, 'x')).toBe(
      'Your Free plan accepts files up to 25 MB.',
    )
    expect(describePlanLimit({ limit: 'storage_bytes', max: 1 << 30, used: 1 << 30, plan: 'free' }, 'x')).toBe(
      'Your Free plan includes 1 GB of storage (1 GB used).',
    )
    expect(describePlanLimit({ limit: 'decisions_per_month', max: 50, used: 50, plan: 'free' }, 'x')).toBe(
      'Your Free plan includes 50 approvals per month (50 used).',
    )
    expect(describePlanLimit(undefined, 'fallback')).toBe('fallback')
  })
})
