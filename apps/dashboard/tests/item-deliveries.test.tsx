import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { ItemDeliveries } from '@/components/items/item-deliveries'

import { item } from './fixtures'
import { mockApi, renderWithProviders } from './utils'

describe('what happened next', () => {
  it('lists the webhook deliveries of this item', async () => {
    const api = mockApi({
      'GET /v1/webhook-deliveries': {
        data: [
          {
            id: 'dlv_1',
            webhook_id: 'wh_1',
            event_id: 'evt_1',
            event_type: 'item.approved',
            status: 'succeeded',
            attempts: 1,
            last_status_code: 200,
            last_error: '',
            created_at: '2026-01-01T00:00:00Z',
          },
        ],
      },
      'GET /v1/webhooks': { data: [{ id: 'wh_1', url: 'https://n8n.test/hook' }] },
    })

    renderWithProviders(<ItemDeliveries item={item({ status: 'approved' })} />)
    expect(await screen.findByText('item.approved')).toBeInTheDocument()
    expect(await screen.findByText('→ https://n8n.test/hook')).toBeInTheDocument()
    expect(screen.getByText('Delivered · 200')).toBeInTheDocument()
    expect(api.find('GET', '/v1/webhook-deliveries')[0].query.get('item_id')).toBe('itm_1')
  })

  it('says when no webhook was notified', async () => {
    mockApi({ 'GET /v1/webhook-deliveries': { data: [] }, 'GET /v1/webhooks': { data: [] } })
    renderWithProviders(<ItemDeliveries item={item({ status: 'approved' })} />)
    expect(await screen.findByText(/No webhook was notified about this item/)).toBeInTheDocument()
  })
})
