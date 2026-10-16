import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { ItemDetail } from '@/components/items/item-detail'

import { collection, item } from './fixtures'
import { mockApi, renderWithProviders } from './utils'

describe('unarchive from the item menu', () => {
  it('an archived item offers Unarchive', async () => {
    const archived = item({ title: 'Old', status: 'archived', archived_at: '2026-01-01T00:00:00Z' })
    const api = mockApi({
      'GET /v1/items/itm_1': archived,
      'GET /v1/items/itm_1/actions': { data: [] },
      'GET /v1/items/itm_1/events': { data: [] },
      'GET /v1/items/itm_1/versions': { data: [] },
      'GET /v1/collections/col_1': collection(),
      'POST /v1/items/itm_1/unarchive': item({ title: 'Old', status: 'approved' }),
    })
    const user = userEvent.setup()

    renderWithProviders(<ItemDetail id="itm_1" />, { mode: 'local' })
    await user.click(await screen.findByRole('button', { name: 'More actions' }))
    await user.click(await screen.findByRole('menuitem', { name: /unarchive/i }))
    await waitFor(() => expect(api.find('POST', '/v1/items/itm_1/unarchive')).toHaveLength(1))
    expect(await screen.findByText('“Old” is back from the archive')).toBeInTheDocument()
  })
})
