import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { WebhookFormSheet } from '@/components/webhooks/webhook-form-sheet'

import { collection, REGENERATE } from './fixtures'
import { mockApi, renderWithProviders } from './utils'

describe('webhook form', () => {
  it('picks events from a grouped multi-select (item.* locks item events), action events included, and collections', async () => {
    const user = userEvent.setup()
    const api = mockApi({
      'GET /v1/collections': {
        data: [collection({ actions: [REGENERATE] }), collection({ id: 'col_2', name: 'Blog' })],
      },
      'POST /v1/webhooks': { id: 'wh_1', url: 'https://n8n.test/h', events: ['item.*'], collection_ids: ['col_2'] },
    })

    renderWithProviders(<WebhookFormSheet open onOpenChange={() => {}} />)
    await user.type(screen.getByLabelText('Endpoint URL*'), 'https://n8n.test/h')
    // Timeout is entered in seconds and sent in ms.
    await user.clear(screen.getByLabelText('Timeout (seconds)*'))
    await user.type(screen.getByLabelText('Timeout (seconds)*'), '5')
    await user.click(screen.getByRole('button', { name: 'Events' }))
    expect(await screen.findByRole('option', { name: /item\.regenerate_requested/ })).toBeInTheDocument()
    expect(screen.getByRole('group', { name: 'Decisions' })).toBeInTheDocument()
    await user.click(screen.getByRole('option', { name: /item\.approved/ }))
    await user.click(screen.getByRole('option', { name: /item\.rejected/ }))
    await user.click(screen.getByRole('option', { name: /item\.\*/ }))
    expect(screen.getByRole('option', { name: /item\.created/ })).toHaveAttribute('aria-disabled', 'true')
    expect(screen.getByRole('option', { name: /item\.created/ })).toHaveAttribute('data-checked', 'true')
    await user.keyboard('{Escape}')
    expect(screen.getByRole('button', { name: 'Events' })).toHaveTextContent(/^item\.\*$/)

    await user.click(screen.getByRole('button', { name: 'Collections' }))
    await user.click(await screen.findByRole('option', { name: /blog/i }))
    await user.keyboard('{Escape}')
    await user.click(screen.getByRole('button', { name: /create webhook/i }))
    await waitFor(() => expect(api.find('POST', '/v1/webhooks')).toHaveLength(1))
    expect(api.find('POST', '/v1/webhooks')[0].body).toMatchObject({
      events: ['item.*'],
      collection_ids: ['col_2'],
      timeout_ms: 5000,
      active: true,
    })
  })

  it('sums up a wildcard pick: * reads Every event, covered events are not counted', async () => {
    const user = userEvent.setup()

    mockApi({ 'GET /v1/collections': { data: [collection()] } })
    renderWithProviders(<WebhookFormSheet open onOpenChange={() => {}} />)
    const events = screen.getByRole('button', { name: 'Events' })

    expect(events).toHaveTextContent('item.approved')
    await user.click(events)
    await user.click(await screen.findByRole('option', { name: /every event/i }))
    await user.keyboard('{Escape}')
    expect(events).toHaveTextContent('Every event (*)')
  })
})
