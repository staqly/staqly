import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { WebhooksPage } from '@/components/webhooks/webhooks-page'
import type { Delivery, Webhook } from '@/lib/api'

import { collection } from './fixtures'
import { mockApi, renderWithProviders } from './utils'

function webhook(over: Partial<Webhook> = {}): Webhook {
  return {
    id: 'wh_1',
    url: 'https://example.com/hook',
    description: 'Receiver',
    secret: 'whsec_****r2xa',
    events: ['item.approved', 'item.rejected'],
    collection_ids: [],
    active: true,
    timeout_ms: 10000,
    max_attempts: 8,
    created_at: '2026-10-13T12:00:00Z',
    updated_at: '2026-10-13T12:00:00Z',
    ...over,
  }
}

function delivery(over: Partial<Delivery> = {}): Delivery {
  return {
    id: 'dlv_1',
    webhook_id: 'wh_1',
    event_id: 'evt_1',
    event_type: 'item.approved',
    status: 'succeeded',
    attempts: 1,
    next_attempt_at: null,
    last_status_code: 200,
    last_error: '',
    last_response: '',
    last_attempt_at: '2026-10-13T12:00:00Z',
    delivered_at: '2026-10-13T12:00:00Z',
    created_at: '2026-10-13T12:00:00Z',
    ...over,
  }
}

function routes(over: Record<string, unknown> = {}) {
  return {
    'GET /v1/webhooks': { data: [webhook()] },
    'GET /v1/webhook-deliveries': { data: [] },
    'GET /v1/collections': { data: [collection()] },
    ...over,
  }
}

describe('webhooks page', () => {
  it('shows an empty state that opens the create form', async () => {
    mockApi(routes({ 'GET /v1/webhooks': { data: [] } }))
    const user = userEvent.setup()

    renderWithProviders(<WebhooksPage />, { mode: 'cloud' })
    expect(await screen.findByText('No webhooks')).toBeInTheDocument()

    const buttons = screen.getAllByRole('button', { name: /new webhook/i })

    await user.click(buttons[buttons.length - 1])
    expect(await screen.findByRole('dialog')).toBeInTheDocument()
    expect(screen.getByLabelText('Endpoint URL*')).toBeInTheDocument()
  })

  it('renders cards with events and collection scope, and the deliveries of the first webhook', async () => {
    const api = mockApi(
      routes({
        'GET /v1/webhooks': {
          data: [
            webhook(),
            webhook({ id: 'wh_2', url: 'https://b.test/x', description: '', collection_ids: ['col_1'] }),
          ],
        },
        'GET /v1/webhook-deliveries': {
          data: [
            delivery(),
            delivery({
              id: 'dlv_2',
              status: 'failed',
              attempts: 8,
              last_status_code: 500,
              last_error: 'receiver returned 500',
            }),
            delivery({
              id: 'dlv_3',
              status: 'pending',
              last_status_code: null,
              next_attempt_at: '2026-10-13T12:05:00Z',
            }),
          ],
        },
      }),
    )

    renderWithProviders(<WebhooksPage />, { mode: 'cloud' })

    const cards = await screen.findAllByTestId('webhook-card')

    expect(cards).toHaveLength(2)
    expect(within(cards[0]).getByText('item.approved')).toBeInTheDocument()
    expect(within(cards[0]).getByText('All collections')).toBeInTheDocument()
    expect(within(cards[0]).getByText(/Timeout 10s · up to 8 attempts/)).toBeInTheDocument()
    expect(within(cards[1]).getByText('No description')).toBeInTheDocument()
    expect(within(cards[1]).getByText('in Instagram Avatar')).toBeInTheDocument()

    const table = await screen.findByTestId('deliveries-table')

    expect(within(table).getByText('receiver returned 500')).toBeInTheDocument()
    expect(within(table).getByText('500')).toBeInTheDocument()
    // Pending deliveries cannot be retried; finished ones can.
    expect(within(table).getByRole('button', { name: 'Retry delivery dlv_1' })).toBeInTheDocument()
    expect(within(table).getByRole('button', { name: 'Retry delivery dlv_2' })).toBeInTheDocument()
    expect(within(table).queryByRole('button', { name: 'Retry delivery dlv_3' })).not.toBeInTheDocument()

    // A click on the row opens the whole delivery: full error, response code.
    await userEvent.setup().click(within(table).getByText('receiver returned 500'))
    const details = await screen.findByRole('dialog')

    expect(within(details).getByText('dlv_2')).toBeInTheDocument()
    expect(within(details).getByText('receiver returned 500')).toBeInTheDocument()

    const q = api.find('GET', '/v1/webhook-deliveries')[0].query

    expect(q.get('webhook_id')).toBe('wh_1')
    expect(q.get('limit')).toBe('50')
  })

  it('switches the deliveries list to the selected webhook', async () => {
    const api = mockApi(
      routes({ 'GET /v1/webhooks': { data: [webhook(), webhook({ id: 'wh_2', url: 'https://b.test/x' })] } }),
    )
    const user = userEvent.setup()

    renderWithProviders(<WebhooksPage />, { mode: 'cloud' })
    await user.click(await screen.findByText('https://b.test/x'))
    await waitFor(() =>
      expect(api.find('GET', '/v1/webhook-deliveries').some((c) => c.query.get('webhook_id') === 'wh_2')).toBe(true),
    )
    expect(await screen.findByText(/no deliveries yet/i)).toBeInTheDocument()
  })

  it('selects a webhook by clicking anywhere on its card, but not on its controls', async () => {
    const api = mockApi(
      routes({ 'GET /v1/webhooks': { data: [webhook(), webhook({ id: 'wh_2', url: 'https://b.test/x' })] } }),
    )
    const user = userEvent.setup()

    renderWithProviders(<WebhooksPage />, { mode: 'cloud' })
    const cards = await screen.findAllByTestId('webhook-card')
    const selectedWh2 = () =>
      api.find('GET', '/v1/webhook-deliveries').some((c) => c.query.get('webhook_id') === 'wh_2')

    await user.click(within(cards[1]).getByRole('button', { name: 'Webhook options' }))
    await user.keyboard('{Escape}')
    expect(selectedWh2()).toBe(false)
    await user.click(within(cards[1]).getByText(/Timeout 10s/))
    await waitFor(() => expect(selectedWh2()).toBe(true))
  })

  it('re-queues a failed delivery', async () => {
    const api = mockApi(
      routes({
        'GET /v1/webhook-deliveries': { data: [delivery({ id: 'dlv_2', status: 'failed', last_status_code: 500 })] },
        'POST /v1/webhook-deliveries/dlv_2/retry': delivery({ id: 'dlv_2', status: 'pending' }),
      }),
    )
    const user = userEvent.setup()

    renderWithProviders(<WebhooksPage />, { mode: 'cloud' })
    await user.click(await screen.findByRole('button', { name: 'Retry delivery dlv_2' }))
    expect(await screen.findByText('Delivery re-queued')).toBeInTheDocument()
    expect(api.find('POST', '/v1/webhook-deliveries/dlv_2/retry')).toHaveLength(1)
  })

  it('pauses a webhook with the switch', async () => {
    const api = mockApi(routes({ 'PATCH /v1/webhooks/wh_1': webhook({ active: false }) }))
    const user = userEvent.setup()

    renderWithProviders(<WebhooksPage />, { mode: 'cloud' })
    await user.click(await screen.findByRole('switch', { name: 'Pause webhook' }))
    await waitFor(() => expect(api.find('PATCH', '/v1/webhooks/wh_1')).toHaveLength(1))
    expect(api.find('PATCH', '/v1/webhooks/wh_1')[0].body).toEqual({ active: false })
    expect(await screen.findByText('Webhook paused')).toBeInTheDocument()
  })

  it('sends a test event', async () => {
    const api = mockApi(routes({ 'POST /v1/webhooks/wh_1/test': delivery({ status: 'pending' }) }))
    const user = userEvent.setup()

    renderWithProviders(<WebhooksPage />, { mode: 'cloud' })
    await user.click(await screen.findByRole('button', { name: /send test/i }))
    expect(await screen.findByText('Test event queued')).toBeInTheDocument()
    expect(api.find('POST', '/v1/webhooks/wh_1/test')).toHaveLength(1)
  })

  it('deletes only after confirmation', async () => {
    const api = mockApi(routes({ 'DELETE /v1/webhooks/wh_1': { status: 204 } }))
    const user = userEvent.setup()

    renderWithProviders(<WebhooksPage />, { mode: 'cloud' })
    await user.click(await screen.findByRole('button', { name: 'Webhook options' }))
    await user.click(await screen.findByRole('menuitem', { name: /delete/i }))

    let dialog = await screen.findByRole('alertdialog', { name: 'Delete this webhook?' })

    expect(within(dialog).getByText(/https:\/\/example.com\/hook will stop receiving events/)).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument())
    expect(api.find('DELETE', '/v1/webhooks/wh_1')).toHaveLength(0)

    await user.click(screen.getByRole('button', { name: 'Webhook options' }))
    await user.click(await screen.findByRole('menuitem', { name: /delete/i }))
    dialog = await screen.findByRole('alertdialog', { name: 'Delete this webhook?' })
    await user.click(within(dialog).getByRole('button', { name: 'Delete webhook' }))
    await waitFor(() => expect(api.find('DELETE', '/v1/webhooks/wh_1')).toHaveLength(1))
    expect(await screen.findByText('Webhook deleted')).toBeInTheDocument()
  })

  it('opens a blank form from the empty state after the edited webhook was deleted', async () => {
    let hooks = [webhook()]

    mockApi(
      routes({
        'GET /v1/webhooks': () => ({ data: hooks }),
        'DELETE /v1/webhooks/wh_1': () => {
          hooks = []

          return { status: 204 }
        },
      }),
    )
    const user = userEvent.setup()

    renderWithProviders(<WebhooksPage />, { mode: 'cloud' })
    await user.click(await screen.findByRole('button', { name: 'Webhook options' }))
    await user.click(await screen.findByRole('menuitem', { name: /edit/i }))
    expect(await screen.findByRole('dialog', { name: 'Edit webhook' })).toBeInTheDocument()
    await user.keyboard('{Escape}')
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())

    await user.click(screen.getByRole('button', { name: 'Webhook options' }))
    await user.click(await screen.findByRole('menuitem', { name: /delete/i }))
    await user.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Delete webhook' }))
    expect(await screen.findByText('No webhooks')).toBeInTheDocument()

    const buttons = screen.getAllByRole('button', { name: /new webhook/i })

    await user.click(buttons[buttons.length - 1])
    expect(await screen.findByRole('dialog', { name: 'New webhook' })).toBeInTheDocument()
  })

  it('shows a load error with a retry', async () => {
    mockApi(routes({ 'GET /v1/webhooks': { status: 500, body: { error: { code: 'internal', message: 'boom' } } } }))

    renderWithProviders(<WebhooksPage />, { mode: 'cloud' })
    expect(await screen.findByText('Could not load data')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Try again' })).toBeInTheDocument()
  })
})
