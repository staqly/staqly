import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { TagList } from '@/components/items/item-tags'

import { item } from './fixtures'
import { renderWithProviders } from './utils'

describe('item tags', () => {
  it('lists tags as chips', () => {
    renderWithProviders(<TagList tags={['client-a', 'urgent']} />)
    expect(screen.getByTestId('item-tags')).toHaveTextContent('client-aurgent')
  })
})

describe('tags on a locked item', () => {
  it('no Edit tab after a decision; tags change from Properties without a new version', async () => {
    const { ItemDetail } = await import('@/components/items/item-detail')
    const { default: userEvent } = await import('@testing-library/user-event')
    const { mockApi } = await import('./utils')
    const { waitFor } = await import('@testing-library/react')
    const user = userEvent.setup()
    const itm = item({ status: 'approved', tags: ['client-a'] })
    const api = mockApi({
      'GET /v1/items/itm_1': itm,
      'GET /v1/items/itm_1/actions': { data: [] },
      'PATCH /v1/items/itm_1': { ...itm, tags: ['client-a', 'urgent'] },
    })

    renderWithProviders(<ItemDetail id="itm_1" />)
    expect(await screen.findByRole('tab', { name: 'Details' })).toBeInTheDocument()
    expect(screen.queryByRole('tab', { name: 'Edit' })).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Edit tags' }))
    // The text box has focus when the editor opens (not the chip's remove button), so typing goes straight in.
    await waitFor(() => expect(screen.getByRole('textbox', { name: 'Tags' })).toHaveFocus())
    await user.keyboard('urgent{Enter}')
    await waitFor(() => expect(api.find('PATCH', '/v1/items/itm_1')).toHaveLength(1))
    expect(api.find('PATCH', '/v1/items/itm_1')[0].body).toEqual({ tags: ['client-a', 'urgent'] })
  })
})
