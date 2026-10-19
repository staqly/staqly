import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { WebhookFormSheet } from '@/components/webhooks/webhook-form-sheet'

import { collection } from './fixtures'
import { mockApi, renderWithProviders } from './utils'

describe('webhook form', () => {
  it('picks events from a grouped multi-select (post.* locks post events) and collections', async () => {
    const user = userEvent.setup()
    const api = mockApi({
      'GET /v1/collections': { data: [collection(), collection({ id: 'col_2', name: 'Blog' })] },
      'POST /v1/webhooks': { id: 'wh_1', url: 'https://n8n.test/h', events: ['post.*'], collection_ids: ['col_2'] },
    })

    renderWithProviders(<WebhookFormSheet open onOpenChange={() => {}} />)
    await user.type(screen.getByLabelText('Endpoint URL*'), 'https://n8n.test/h')
    // Timeout is entered in seconds and sent in ms.
    await user.clear(screen.getByLabelText('Timeout (seconds)*'))
    await user.type(screen.getByLabelText('Timeout (seconds)*'), '5')
    await user.click(screen.getByRole('button', { name: 'Events' }))
    expect(await screen.findByRole('option', { name: /schedule\.posts_requested/ })).toBeInTheDocument()
    expect(screen.getByRole('group', { name: 'Publishing' })).toBeInTheDocument()
    await user.click(screen.getByRole('option', { name: /post\.\*/ }))
    expect(screen.getByRole('option', { name: /post\.created/ })).toHaveAttribute('aria-disabled', 'true')
    expect(screen.getByRole('option', { name: /post\.created/ })).toHaveAttribute('data-checked', 'true')
    await user.keyboard('{Escape}')
    expect(screen.getByRole('button', { name: 'Events' })).toHaveTextContent(/^post\.\*$/)

    await user.click(screen.getByRole('button', { name: 'Collections' }))
    await user.click(await screen.findByRole('option', { name: /blog/i }))
    await user.keyboard('{Escape}')
    await user.click(screen.getByRole('button', { name: /create webhook/i }))
    await waitFor(() => expect(api.find('POST', '/v1/webhooks')).toHaveLength(1))
    expect(api.find('POST', '/v1/webhooks')[0].body).toMatchObject({
      events: ['post.*'],
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

    expect(events.textContent).toMatch(/post\.publish_requested|3/)
    await user.click(events)
    await user.click(await screen.findByRole('option', { name: /every event/i }))
    await user.keyboard('{Escape}')
    expect(events).toHaveTextContent('Every event (*)')
  })
})
