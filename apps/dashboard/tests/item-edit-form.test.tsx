import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { ItemEditForm } from '@/components/items/item-edit-form'

import { item } from './fixtures'
import { mockApi, renderWithProviders } from './utils'

describe('item edit form', () => {
  it('shows an uploaded image as its file, with an optional URL and a description', () => {
    mockApi({ 'GET /v1/collections': { data: [] } })
    renderWithProviders(
      <ItemEditForm
        item={item({
          type: 'image',
          url: '',
          media: { id: 'med_1', mime_type: 'image/png', size_bytes: 70, original_name: 'dot.png', url: '' },
        })}
      />,
    )

    expect(screen.getByText('dot.png')).toBeInTheDocument()
    expect(screen.queryByLabelText(/^URL/)).not.toBeInTheDocument()
    expect(screen.getByLabelText('Description')).not.toHaveAttribute('aria-required', 'true')
  })

  it('saves a second change after the first one', async () => {
    const user = userEvent.setup()
    let version = 1
    let title = 'Draft'
    const api = mockApi({
      'GET /v1/collections': { data: [] },
      'PATCH /v1/items/itm_1': (c: { body: { title?: string } }) => {
        version += 1
        title = c.body.title ?? title

        return item({ id: 'itm_1', title, version, type: 'text', content: 'x' })
      },
    })

    renderWithProviders(<ItemEditForm item={item({ id: 'itm_1', title, version, type: 'text', content: 'x' })} />)
    const input = screen.getByLabelText(/^Title/)

    await user.clear(input)
    await user.type(input, 'First')
    await user.click(screen.getByRole('button', { name: /save changes/i }))
    await waitFor(() => expect(api.find('PATCH', '/v1/items/itm_1')).toHaveLength(1))

    await user.clear(input)
    await user.type(input, 'Second')
    await user.click(screen.getByRole('button', { name: /save changes/i }))
    await waitFor(() => expect(api.find('PATCH', '/v1/items/itm_1')).toHaveLength(2))
    expect(api.find('PATCH', '/v1/items/itm_1')[1].body).toMatchObject({ title: 'Second', expected_version: 2 })
  })
})
