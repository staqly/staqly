import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { AddonsSection, bonusHint } from '@/components/billing/addons'
import type { Addon, Plan } from '@/lib/api'

import { mockApi, renderWithProviders } from './utils'

const limits = {
  posts_per_month: -1,
  channels: 25,
  alerts: 10,
  webhooks: 10,
  api_keys: 10,
  storage_bytes: 50 * 2 ** 30,
  max_upload_bytes: 100 * 2 ** 20,
  data_export: true,
  x_posts_per_period: -1,
}

function plan(over: Partial<Plan> = {}): Plan {
  return {
    id: 'plus',
    name: 'Plus',
    price_month: 29,
    price_year: 276,
    currency: 'eur',
    highlight: true,
    features: [],
    limits,
    base_limits: { ...limits, channels: 15, x_posts_per_period: 450 },
    limit_bonuses: [
      { limit: 'channels', add: 10, unlimited: false, from: 'Channels pack' },
      { limit: 'x_posts_per_period', add: 0, unlimited: true, from: 'X unlimited' },
    ],
    ...over,
  }
}

const PACK: Addon = {
  id: 'channels_pack',
  name: 'Channels pack',
  description: '10 more channels',
  features: ['10 more channels'],
  price_month: 9,
  price_year: 86,
  currency: 'eur',
  active: false,
}

describe('add-ons and entitlements', () => {
  it('says where a raised limit comes from, and nothing when no add-on raised it', () => {
    expect(bonusHint(plan(), 'channels')).toBe('15 + 10 from Channels pack')
    expect(bonusHint(plan(), 'x_posts_per_period')).toBe('Unlimited from X unlimited')
    expect(bonusHint(plan(), 'webhooks')).toBe('')
    expect(bonusHint(plan({ base_limits: undefined, limit_bonuses: undefined }), 'channels')).toBe('')
  })

  it('adds an add-on after a confirmation that says it is charged now, and removes an active one', async () => {
    const api = mockApi({
      'GET /v1/billing/addons': { data: [PACK, { ...PACK, id: 'ai_pack', name: 'AI pack', active: true }] },
      'POST /v1/billing/addons/channels_pack': { status: 204 },
      'DELETE /v1/billing/addons/ai_pack': { status: 204 },
    })
    const user = userEvent.setup()

    renderWithProviders(<AddonsSection canBuy yearly={false} />, { mode: 'cloud' })
    const list = await screen.findByTestId('addons')

    expect(within(list).getAllByText('€9 per month · excl. VAT')).toHaveLength(2)
    await user.click(within(list).getByRole('button', { name: /add/i }))
    const confirm = await screen.findByRole('alertdialog')

    expect(confirm).toHaveTextContent('charged now')
    await user.click(within(confirm).getByRole('button', { name: 'Add' }))
    await waitFor(() => expect(api.find('POST', '/v1/billing/addons/channels_pack')).toHaveLength(1))

    await user.click(within(list).getByRole('button', { name: 'Remove' }))
    await user.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Remove' }))
    await waitFor(() => expect(api.find('DELETE', '/v1/billing/addons/ai_pack')).toHaveLength(1))
  })

  it('shows prices only to people who cannot buy, and nothing when none are on sale', async () => {
    mockApi({ 'GET /v1/billing/addons': { data: [PACK] } })
    renderWithProviders(<AddonsSection canBuy={false} yearly />, { mode: 'cloud' })
    const list = await screen.findByTestId('addons')

    expect(within(list).getByText('€86 per year · excl. VAT')).toBeInTheDocument()
    expect(within(list).queryByRole('button')).not.toBeInTheDocument()
  })
})
