import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { CollectionsIndex } from '@/components/collections/collections-index'
import { CreateCollectionDialog } from '@/components/collections/create-collection-dialog'

import { collection } from './fixtures'
import { nav } from './navigation'
import { mockApi, renderWithProviders } from './utils'

describe('create collection dialog', () => {
  it('validates, sends the form and navigates to the new collection', async () => {
    const api = mockApi({
      'POST /v1/collections': { status: 201, body: collection({ id: 'col_new', name: 'Research' }) },
      'GET /v1/collections': { data: [] },
    })
    const user = userEvent.setup()

    renderWithProviders(<CreateCollectionDialog open onOpenChange={() => {}} />)

    await user.click(screen.getByRole('button', { name: /create collection/i }))
    expect(await screen.findByText('Name is required')).toBeInTheDocument()
    expect(api.find('POST', '/v1/collections')).toHaveLength(0)

    await user.type(screen.getByLabelText('Name'), 'Research')
    await user.type(screen.getByLabelText('Description'), 'Links and notes')
    await user.click(screen.getByRole('radio', { name: 'Table' }))
    await user.click(screen.getByRole('button', { name: /create collection/i }))

    await waitFor(() => expect(nav.push).toHaveBeenCalledWith('/collections/col_new'))
    expect(api.find('POST', '/v1/collections')[0].body).toEqual({
      name: 'Research',
      description: 'Links and notes',
      default_view: 'table',
    })
    expect(api.find('POST', '/v1/collections')[0].headers['X-CSRF-Token']).toBe('csrf-test')
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
    await user.type(screen.getByLabelText('Name'), 'Dup')
    await user.click(screen.getByRole('button', { name: /create collection/i }))
    expect(await screen.findByText('already exists')).toBeInTheDocument()
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
    const cards = await screen.findAllByTestId('collection-card')

    expect(cards).toHaveLength(2)
    expect(cards[0]).toHaveAttribute('href', '/collections/col_1')
    expect(cards[0].querySelector('img')).toHaveAttribute('src', '/api/v1/media/med_c')
    expect(cards[1].querySelector('img')).toHaveAttribute('src', 'https://x.test/c.png')
    expect(screen.getByText('2 to review')).toBeInTheDocument()
    expect(screen.getByText('5 items')).toBeInTheDocument()
  })
})
