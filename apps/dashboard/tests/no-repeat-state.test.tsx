import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { ApiKeysManager } from '@/components/integrations/api-keys'
import { buildItemPatch, ItemEditForm } from '@/components/items/item-edit-form'
import { WebhooksPage } from '@/components/webhooks/webhooks-page'
import type { Webhook } from '@/lib/api'

import { collection, item } from './fixtures'
import { nav } from './navigation'
import { mockApi, renderWithProviders } from './utils'

// A state that is already set can't be set again: the UI offers only the opposite
// transition and never sends the current state twice (the API answers 409).

/** Leaves every request matching method + path pending, so a mutation stays in flight. */
function stall(api: ReturnType<typeof mockApi>, method: string, path: string) {
  const reply = api.fetchMock.getMockImplementation()!

  api.fetchMock.mockImplementation((input, init) => {
    const url = new URL(String(input), 'http://localhost')

    if ((init?.method ?? 'GET').toUpperCase() === method && url.pathname.replace(/^\/api/, '') === path) {
      api.calls.push({ method, path, query: url.searchParams, body: JSON.parse(String(init?.body)), headers: {} })

      return new Promise<Response>(() => {})
    }

    return reply(input, init)
  })
}

describe('no re-applying the current state', () => {
  it('item edit form: nothing to save until a value differs; status is not editable here', async () => {
    const user = userEvent.setup()

    mockApi({ 'GET /v1/collections': { data: [collection()] } })
    const itm = item({ id: 'itm_1', status: 'pending', type: 'text', content: 'x' })

    renderWithProviders(<ItemEditForm item={itm} />)
    const save = screen.getByRole('button', { name: /save changes/i })

    expect(save).toBeDisabled()
    expect(screen.queryByRole('combobox', { name: 'Status' })).not.toBeInTheDocument()
    await user.type(screen.getByLabelText('Title*'), '!')
    expect(save).toBeEnabled()
  })

  it('locks the content of an approved item and says a fix resubmits a rejected one', () => {
    mockApi({ 'GET /v1/collections': { data: [collection()] } })
    const { unmount } = renderWithProviders(
      <ItemEditForm item={item({ status: 'approved', type: 'text', content: 'x' })} />,
    )

    expect(screen.getByTestId('content-locked')).toBeInTheDocument()
    expect(screen.getByLabelText('Title*')).toHaveAttribute('readonly')
    unmount()
    renderWithProviders(<ItemEditForm item={item({ status: 'rejected', type: 'text', content: 'x' })} />)
    expect(screen.getByTestId('content-resubmits')).toHaveTextContent('back to Pending')
    expect(screen.getByLabelText('Title*')).not.toHaveAttribute('readonly')
  })

  it('item patch never carries a status (actions change it)', () => {
    const itm = item({ status: 'archived', type: 'text', content: 'x' })
    const base = {
      title: itm.title,
      type: itm.type,
      content: itm.content,
      url: itm.url,
      metadata: JSON.stringify(itm.metadata ?? {}),
      source: itm.source,
      external_ref: itm.external_ref,
      collection_id: itm.collection_id,
      tags: [],
    }

    expect(buildItemPatch(itm, { ...base, status: 'pending' })).toEqual({})
  })

  it('webhook switch is locked while its change is in flight', async () => {
    const wh: Webhook = {
      id: 'wh_1',
      url: 'https://example.com/hook',
      description: 'Receiver',
      secret: 'whsec_****r2xa',
      events: ['item.approved'],
      collection_ids: [],
      active: true,
      timeout_ms: 10000,
      max_attempts: 8,
      created_at: '2026-10-13T12:00:00Z',
      updated_at: '2026-10-13T12:00:00Z',
    }
    const api = mockApi({
      'GET /v1/webhooks': { data: [wh] },
      'GET /v1/webhook-deliveries': { data: [] },
      'GET /v1/collections': { data: [collection()] },
    })

    stall(api, 'PATCH', '/v1/webhooks/wh_1')
    const user = userEvent.setup()

    renderWithProviders(<WebhooksPage />, { mode: 'cloud' })
    const sw = await screen.findByRole('switch', { name: 'Pause webhook' })

    await user.click(sw)
    await waitFor(() => expect(sw).toBeDisabled())
    await user.click(sw)
    expect(api.find('PATCH', '/v1/webhooks/wh_1')).toHaveLength(1)
    expect(api.find('PATCH', '/v1/webhooks/wh_1')[0].body).toEqual({ active: false })
  })

  it('a revoked API key offers no Revoke (or Rotate)', async () => {
    const key = {
      id: 'key_1',
      name: 'Active key',
      prefix: 'stk_abcd',
      scopes: ['items:read'],
      created_at: '2026-01-01T00:00:00Z',
      last_used_at: null,
      expires_at: null,
      revoked_at: null,
    }

    mockApi({
      'GET /v1/api-keys': {
        data: [key, { ...key, id: 'key_2', name: 'Old key', prefix: 'stk_old1', revoked_at: '2026-02-01T00:00:00Z' }],
        available_scopes: ['items:read'],
      },
    })
    nav.set('/integrations/api-keys')
    renderWithProviders(<ApiKeysManager />, { mode: 'cloud' })

    const rows = await screen.findAllByTestId('api-key-row')
    const active = rows.find((r) => within(r).queryByText('Active key'))!
    const revoked = rows.find((r) => within(r).queryByText('Old key'))!

    expect(within(active).getByRole('button', { name: 'Options for Active key' })).toBeInTheDocument()
    expect(within(revoked).getByText('Revoked')).toBeInTheDocument()
    expect(within(revoked).queryByRole('button', { name: /options for/i })).not.toBeInTheDocument()
  })
})
