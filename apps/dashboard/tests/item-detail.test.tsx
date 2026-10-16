import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { ItemDetail } from '@/components/items/item-detail'

import { APPROVE, REJECT, collection, item } from './fixtures'
import { mockApi, renderWithProviders } from './utils'

const EVENTS = {
  data: [
    {
      id: 'evt_2',
      seq: 2,
      type: 'item.rejected',
      collection_id: 'col_1',
      item_id: 'itm_1',
      actor: { type: 'session', id: 's1', name: 'Dashboard' },
      data: { feedback: 'Make the face larger', previous_status: 'pending', item: { status: 'rejected' } },
      created_at: '2026-01-02T10:00:00Z',
    },
    {
      id: 'evt_1',
      seq: 1,
      type: 'item.created',
      collection_id: 'col_1',
      item_id: 'itm_1',
      actor: { type: 'api_key', id: 'key_1', name: 'n8n production' },
      data: {},
      created_at: '2026-01-01T10:00:00Z',
    },
  ],
}

function routes(over: Record<string, unknown> = {}) {
  return {
    'GET /v1/items/itm_1': item({ version: 5, title: 'Avatar v1' }),
    'GET /v1/items/itm_1/actions': { data: [APPROVE, REJECT] },
    'GET /v1/items/itm_1/events': EVENTS,
    'GET /v1/items/itm_1/versions': {
      data: [
        {
          version: 4,
          snapshot: item({ title: 'Older title', version: 4 }),
          actor: 'api_key:key_1',
          created_at: '2026-01-01T11:00:00Z',
        },
      ],
    },
    'GET /v1/collections/col_1': collection(),
    'GET /v1/collections': { data: [collection()] },
    'GET /v1/events': { data: [] },
    ...over,
  }
}

describe('item detail', () => {
  it('renders preview, properties and the event history with actor and feedback', async () => {
    mockApi(routes())
    const user = userEvent.setup()

    renderWithProviders(<ItemDetail id="itm_1" />, { mode: 'cloud' })
    expect(await screen.findByRole('heading', { name: 'Avatar v1' })).toBeInTheDocument()
    expect(screen.getByTestId('preview-image')).toBeInTheDocument()
    expect(await screen.findByRole('button', { name: /^approve$/i })).toBeInTheDocument()

    await user.click(screen.getByRole('tab', { name: 'History' }))
    const timeline = await screen.findByTestId('event-timeline')

    expect(within(timeline).getAllByText('Rejected').length).toBeGreaterThan(0)
    expect(within(timeline).getByText('Make the face larger')).toBeInTheDocument()
    expect(within(timeline).getByText('n8n production')).toBeInTheDocument()
    expect(within(timeline).getByText('Created')).toBeInTheDocument()

    await user.click(screen.getByRole('tab', { name: 'Versions' }))
    expect(await screen.findByText('Older title')).toBeInTheDocument()
  })

  it('saves edits with expected_version', async () => {
    let current = item({ version: 5, title: 'Avatar v1' })
    const api = mockApi(
      routes({
        'GET /v1/items/itm_1': () => current,
        'PATCH /v1/items/itm_1': () => {
          current = item({ version: 6, title: 'Renamed', metadata: { prompt: 'studio portrait', seed: 7 } })

          return current
        },
      }),
    )
    const user = userEvent.setup()

    renderWithProviders(<ItemDetail id="itm_1" />, { mode: 'cloud' })
    await screen.findByRole('heading', { name: 'Avatar v1' })
    await user.click(screen.getByRole('tab', { name: 'Edit' }))
    const title = await screen.findByLabelText('Title*')

    await user.clear(title)
    await user.type(title, 'Renamed')
    const meta = screen.getByLabelText('Metadata')

    await user.clear(meta)
    await user.click(meta)
    await user.paste('{"prompt":"studio portrait","seed":7}')
    await user.click(screen.getByRole('button', { name: /save changes/i }))
    await waitFor(() => expect(api.find('PATCH', '/v1/items/itm_1')).toHaveLength(1))
    expect(api.find('PATCH', '/v1/items/itm_1')[0].body).toEqual({
      title: 'Renamed',
      metadata: { prompt: 'studio portrait', seed: 7 },
      expected_version: 5,
    })
    expect(await screen.findByRole('heading', { name: 'Renamed' })).toBeInTheDocument()
  })

  it('rejects invalid metadata JSON client-side', async () => {
    const api = mockApi(routes())
    const user = userEvent.setup()

    renderWithProviders(<ItemDetail id="itm_1" />, { mode: 'cloud' })
    await screen.findByRole('heading', { name: 'Avatar v1' })
    await user.click(screen.getByRole('tab', { name: 'Edit' }))
    const meta = await screen.findByLabelText('Metadata')

    await user.clear(meta)
    await user.type(meta, 'not json')
    await user.click(screen.getByRole('button', { name: /save changes/i }))
    expect(await screen.findByText('Metadata must be a JSON object')).toBeInTheDocument()
    expect(api.find('PATCH', '/v1/items/itm_1')).toHaveLength(0)
  })

  it('shows a conflict message on 409', async () => {
    mockApi(
      routes({
        'PATCH /v1/items/itm_1': {
          status: 409,
          body: { error: { code: 'conflict', message: 'item was modified (version 6, expected 5)' } },
        },
      }),
    )
    const user = userEvent.setup()

    renderWithProviders(<ItemDetail id="itm_1" />, { mode: 'cloud' })
    await screen.findByRole('heading', { name: 'Avatar v1' })
    await user.click(screen.getByRole('tab', { name: 'Edit' }))
    await user.type(await screen.findByLabelText('Title*'), ' x')
    await user.click(screen.getByRole('button', { name: /save changes/i }))
    expect(await screen.findByText('Someone else changed this item')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /reload latest/i })).toBeInTheDocument()
  })
})
