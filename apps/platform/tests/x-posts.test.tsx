import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { XPostsSection, xPostsLine, xPostsWarning } from '@/components/billing/x-posts'
import type { XPosts } from '@/lib/api'

import { mockApi, renderWithProviders } from './utils'

const BUNDLES = [
  { posts: 200, price: 10 },
  { posts: 1000, price: 40 },
  { posts: 4000, price: 150 },
]

function xposts(over: Partial<XPosts> = {}): XPosts {
  return {
    period: '2026-11',
    period_end: '2026-12-01T00:00:00.000000Z',
    unlimited: false,
    included: 250,
    used: 37,
    included_left: 213,
    credits_left: 0,
    credits_expire_at: null,
    left: 213,
    bundles: BUNDLES,
    currency: 'eur',
    link_post_weight: 14,
    ...over,
  }
}

afterEach(() => vi.unstubAllGlobals())

describe('X posts', () => {
  it('reads as included used, bought left and when they expire; warns at 80 % and when used up', () => {
    expect(xPostsLine(xposts())).toBe('37 of 250 included')
    expect(
      xPostsLine(
        xposts({
          used: 250,
          included_left: 0,
          credits_left: 200,
          credits_expire_at: '2026-11-30T00:00:00Z',
          left: 200,
        }),
      ),
    ).toBe('250 of 250 included · 200 extra left (expire Nov 30)')
    expect(xPostsWarning(xposts())).toBe('')
    expect(xPostsWarning(xposts({ used: 210, included_left: 40, left: 40 }))).toBe('Only 40 X posts left this period.')
    expect(xPostsWarning(xposts({ used: 250, included_left: 0, left: 0 }))).toBe(
      'Your X posts for this period are used up. Buy more or wait until Dec 1.',
    )
    expect(xPostsWarning(xposts({ unlimited: true }))).toBe('')
  })

  it('Plan & usage buys a bundle through Stripe Checkout', async () => {
    const assign = vi.fn<(url: string) => void>()

    vi.stubGlobal('location', { ...window.location, assign })
    const api = mockApi({
      'GET /v1/billing/x-posts': xposts({ used: 250, included_left: 0, left: 0 }),
      'POST /v1/billing/credits/checkout': { url: 'https://checkout.stripe.test/cs_1' },
    })
    const user = userEvent.setup()

    renderWithProviders(<XPostsSection canBuy />, { mode: 'cloud' })
    expect(await screen.findByTestId('x-posts-line')).toHaveTextContent('None bought this period')
    expect(screen.getByText(/used up/)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Buy more X posts' }))
    expect(await screen.findByRole('radio', { name: /1,000 X posts/ })).toHaveAttribute('aria-checked', 'true')
    await user.click(screen.getByRole('radio', { name: /4,000 X posts/ }))
    expect(screen.getByRole('radio', { name: /4,000 X posts/ })).toHaveTextContent('save 25%')
    await user.click(screen.getByRole('button', { name: 'Continue to payment' }))
    await waitFor(() => expect(api.find('POST', '/v1/billing/credits/checkout')).toHaveLength(1))
    expect(api.find('POST', '/v1/billing/credits/checkout')[0].body).toEqual({ provider: 'x', posts: 4000 })
    await waitFor(() => expect(assign).toHaveBeenCalledWith('https://checkout.stripe.test/cs_1'))
    // Still leaving for Stripe: no second checkout from another click.
    expect(screen.getByTestId('buy-x-posts-continue')).toBeDisabled()
  })

  it('only the account owner sees Buy more X posts', async () => {
    mockApi({ 'GET /v1/billing/x-posts': xposts() })
    renderWithProviders(<XPostsSection canBuy={false} />, { mode: 'cloud' })
    expect(await screen.findByTestId('x-posts-line')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Buy more X posts' })).not.toBeInTheDocument()
  })
})
