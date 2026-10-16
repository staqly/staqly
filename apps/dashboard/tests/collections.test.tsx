import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it } from 'vitest'

import { CollectionsIndex } from '@/components/collections/collections-index'
import { CreateCollectionDialog } from '@/components/collections/create-collection-dialog'

import { collection } from './fixtures'
import { nav } from './navigation'
import { mockApi, renderWithProviders } from './utils'

/** Kept mounted like in the app shell, opened and closed by its parent. */
function Reopenable() {
  const [open, setOpen] = useState(false)

  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>
        Open
      </button>
      <CreateCollectionDialog open={open} onOpenChange={setOpen} />
    </>
  )
}

describe('create collection dialog', () => {
  it('validates, sends the form and navigates to the new collection', async () => {
    const api = mockApi({
      'POST /v1/collections': { status: 201, body: collection({ id: 'col_new', name: 'Research' }) },
      'GET /v1/collections': { data: [] },
    })
    const user = userEvent.setup()

    renderWithProviders(<CreateCollectionDialog open onOpenChange={() => {}} />)

    // Required fields carry a red asterisk; optional ones don't.
    const nameLabel = screen.getByText('Name', { selector: 'label' })

    expect(nameLabel.querySelector('[data-slot="required-mark"]')).toHaveTextContent('*')
    expect(nameLabel.querySelector('[data-slot="required-mark"]')).toHaveAttribute('aria-hidden', 'true')
    expect(nameLabel.querySelector('[data-slot="required-mark"]')).toHaveClass('text-destructive')
    expect(
      screen.getByText('Description', { selector: 'label' }).querySelector('[data-slot="required-mark"]'),
    ).toBeNull()
    // The accessible name stays "Name"; the requirement is announced through aria-required.
    expect(screen.getByRole('textbox', { name: 'Name' })).toHaveAttribute('aria-required', 'true')
    expect(screen.getByRole('textbox', { name: 'Name' })).not.toHaveAttribute('aria-invalid', 'true')

    await user.click(screen.getByRole('button', { name: /create collection/i }))
    expect(await screen.findByText('Name is required')).toBeInTheDocument()
    expect(api.find('POST', '/v1/collections')).toHaveLength(0)
    // The failed submit turns the field red: aria-invalid on the input, data-invalid on its Field.
    expect(screen.getByRole('textbox', { name: 'Name' })).toHaveAttribute('aria-invalid', 'true')
    expect(nameLabel.closest('[data-slot="field"]')).toHaveAttribute('data-invalid', 'true')

    await user.type(screen.getByLabelText('Name*'), 'Research')
    await user.type(screen.getByLabelText('Description'), 'Links and notes')
    await user.click(screen.getByRole('radio', { name: 'Blue' }))
    await user.click(screen.getByRole('button', { name: /create collection/i }))

    await waitFor(() => expect(nav.push).toHaveBeenCalledWith('/collections/col_new'))
    expect(api.find('POST', '/v1/collections')[0].body).toEqual({
      name: 'Research',
      description: 'Links and notes',
      color: '#3b82f6',
      default_view: 'table',
    })
    expect(api.find('POST', '/v1/collections')[0].headers['X-CSRF-Token']).toBe('csrf-test')
  })

  it('starts empty again after Cancel', async () => {
    mockApi({ 'GET /v1/collections': { data: [] } })
    const user = userEvent.setup()

    renderWithProviders(<Reopenable />)
    await user.click(screen.getByRole('button', { name: 'Open' }))
    await user.type(await screen.findByLabelText('Name*'), 'Draft name')
    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    await user.click(screen.getByRole('button', { name: 'Open' }))
    expect(await screen.findByLabelText('Name*')).toHaveValue('')
  })

  it('shows server validation errors', async () => {
    mockApi({
      'POST /v1/collections': {
        status: 422,
        body: {
          error: { code: 'validation_failed', message: 'validation failed', fields: { name: 'already exists' } },
        },
      },
    })
    const user = userEvent.setup()

    renderWithProviders(<CreateCollectionDialog open onOpenChange={() => {}} />)
    await user.type(screen.getByLabelText('Name*'), 'Dup')
    await user.click(screen.getByRole('button', { name: /create collection/i }))
    expect(await screen.findByText('Already exists')).toHaveAttribute('data-slot', 'field-error')
    expect(screen.getByRole('textbox', { name: 'Name' })).toHaveAttribute('aria-invalid', 'true')
  })
})

describe('collections index', () => {
  it('renders cards with cover image and counts', async () => {
    mockApi({
      'GET /v1/collections': {
        data: [
          collection({ cover: { media_id: 'med_c' }, status_counts: { pending: 2, approved: 3 }, item_count: 5 }),
          collection({
            id: 'col_2',
            name: 'Links',
            cover: { url: 'https://x.test/c.png' },
            status_counts: {},
            item_count: 0,
          }),
        ],
      },
    })
    nav.set('/collections')
    renderWithProviders(<CollectionsIndex />)
    const table = await screen.findByRole('table')
    const rows = within(table).getAllByTestId('collection-row')

    expect(rows).toHaveLength(2)
    expect(within(rows[0]).getByRole('link')).toHaveAttribute('href', '/collections/col_1')
    expect(within(rows[0]).getByText('5')).toBeInTheDocument()
    expect(within(rows[1]).getByText('0')).toBeInTheDocument()
  })
})
