import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { CollectionPage } from '@/components/collections/collection-page'
import { CollectionSettingsSheet } from '@/components/collections/collection-settings'

import { collection, item } from './fixtures'
import { nav } from './navigation'
import { mockApi, renderWithProviders, type Call } from './utils'

const ITEMS = [
  item(),
  item({ id: 'itm_2', title: 'Caption', type: 'text', media: null, content: 'Hello', status: 'approved' }),
]

function routes(over: Record<string, unknown> = {}) {
  return {
    'GET /v1/collections/col_1': collection(),
    'GET /v1/collections': { data: [collection()] },
    'GET /v1/items': { data: ITEMS },
    'GET /v1/events': { data: [] },
    ...over,
  }
}

const itemQueries = (calls: Call[]) => calls.filter((c) => c.method === 'GET' && c.path === '/v1/items')

describe('collection page', () => {
  it('shows the header and items, queries by collection and opens an item in the URL', async () => {
    nav.set('/collections/col_1')
    const api = mockApi(routes())
    const user = userEvent.setup()

    renderWithProviders(<CollectionPage id="col_1" />, { mode: 'cloud' })
    expect(await screen.findByRole('heading', { name: 'Instagram Avatar' })).toBeInTheDocument()
    expect(screen.getByText('Avatars')).toBeInTheDocument()
    expect(screen.getByText('2 items')).toBeInTheDocument()
    expect(await screen.findByText('Caption')).toBeInTheDocument()

    const q = itemQueries(api.calls)[0].query

    expect(q.get('collection_id')).toBe('col_1')
    expect(q.get('sort')).toBe('created_desc')
    expect(q.get('limit')).toBe('48')
    expect(q.has('status')).toBe(false)

    await user.click(screen.getByText('Caption'))
    expect(nav.replace).toHaveBeenCalledWith('/collections/col_1?item=itm_2', { scroll: false })
  })

  it('opens an item of the table view from the keyboard', async () => {
    nav.set('/collections/col_1')
    mockApi(routes({ 'GET /v1/collections/col_1': collection({ default_view: 'table' }) }))
    const user = userEvent.setup()

    renderWithProviders(<CollectionPage id="col_1" />, { mode: 'cloud' })
    const title = await within(await screen.findByTestId('table-view')).findByRole('button', { name: 'Caption' })

    title.focus()
    await user.keyboard('{Enter}')
    expect(nav.replace).toHaveBeenCalledTimes(1)
    expect(nav.replace).toHaveBeenCalledWith('/collections/col_1?item=itm_2', { scroll: false })
  })

  it('says the collection was not found on 404, without a retry', async () => {
    nav.set('/collections/col_x')
    mockApi(
      routes({
        'GET /v1/collections/col_x': {
          status: 404,
          body: { error: { code: 'not_found', message: 'collection not found' } },
        },
      }),
    )

    renderWithProviders(<CollectionPage id="col_x" />, { mode: 'cloud' })
    expect(await screen.findByText('Collection not found')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Try again' })).not.toBeInTheDocument()
  })

  it('offers a retry when loading fails for another reason', async () => {
    nav.set('/collections/col_1')
    let fail = true
    const api = mockApi(
      routes({
        'GET /v1/collections/col_1': () =>
          fail ? { status: 500, body: { error: { code: 'internal', message: 'boom' } } } : collection(),
      }),
    )
    const user = userEvent.setup()

    renderWithProviders(<CollectionPage id="col_1" />, { mode: 'cloud' })
    expect(await screen.findByText('Could not load collection')).toBeInTheDocument()
    fail = false
    await user.click(screen.getByRole('button', { name: 'Try again' }))
    expect(await screen.findByRole('heading', { name: 'Instagram Avatar' })).toBeInTheDocument()
    expect(api.find('GET', '/v1/collections/col_1')).toHaveLength(2)
  })

  it('filters from the URL and clears them from the empty state', async () => {
    nav.set('/collections/col_1', 'decision=rejected&type=image')
    const api = mockApi(routes({ 'GET /v1/items': { data: [] } }))
    const user = userEvent.setup()

    renderWithProviders(<CollectionPage id="col_1" />, { mode: 'cloud' })
    expect(await screen.findByText('No items match these filters')).toBeInTheDocument()
    // The empty state sits inside the table, under its column headers.
    expect(screen.getByRole('columnheader', { name: /title/i })).toBeInTheDocument()
    expect(screen.getByRole('cell')).toHaveTextContent('No items match these filters')

    const q = itemQueries(api.calls)[0].query

    expect(q.get('decision')).toBe('rejected')
    expect(q.get('type')).toBe('image')

    await user.click(screen.getByRole('button', { name: 'Clear filters' }))
    expect(nav.replace).toHaveBeenLastCalledWith('/collections/col_1', { scroll: false })
  })

  it('filters by decision, run and tag', async () => {
    nav.set('/collections/col_1')
    mockApi(routes())
    const user = userEvent.setup()

    renderWithProviders(<CollectionPage id="col_1" />, { mode: 'cloud' })
    await screen.findByText('Caption')
    await user.click(screen.getByRole('button', { name: 'Decision filter' }))
    // "All but archived" is the default; picking Rejected narrows to it.
    await user.click(await screen.findByRole('option', { name: 'Rejected' }))
    expect(nav.replace).toHaveBeenCalledWith('/collections/col_1?decision=rejected', { scroll: false })
  })

  it('mirrors the search box into the URL and ranks by relevance', async () => {
    nav.set('/collections/col_1')
    const api = mockApi(routes())
    const user = userEvent.setup()

    renderWithProviders(<CollectionPage id="col_1" />, { mode: 'cloud' })
    await screen.findByText('Caption')
    await user.type(screen.getByRole('textbox', { name: 'Search items' }), 'cap')
    await waitFor(() => expect(nav.replace).toHaveBeenCalledWith('/collections/col_1?q=cap', { scroll: false }))
    await waitFor(() => expect(itemQueries(api.calls).some((c) => c.query.get('q') === 'cap')).toBe(true))

    const searched = itemQueries(api.calls).find((c) => c.query.get('q') === 'cap')!

    expect(searched.query.get('sort')).toBe('relevance')
  })

  it('loads the next page with the cursor', async () => {
    nav.set('/collections/col_1')
    const api = mockApi(
      routes({
        'GET /v1/items': (c: Call) =>
          c.query.get('cursor') === 'cur_2'
            ? { data: [item({ id: 'itm_3', title: 'Third' })] }
            : { data: ITEMS, next_cursor: 'cur_2' },
      }),
    )
    const user = userEvent.setup()

    renderWithProviders(<CollectionPage id="col_1" />, { mode: 'cloud' })
    await user.click(await screen.findByRole('button', { name: 'Load more' }))
    expect(await screen.findByText('Third')).toBeInTheDocument()
    expect(itemQueries(api.calls).at(-1)!.query.get('cursor')).toBe('cur_2')
    expect(screen.queryByRole('button', { name: 'Load more' })).not.toBeInTheDocument()
  })

  it('opens the add item dialog from an empty collection', async () => {
    nav.set('/collections/col_1')
    mockApi(routes({ 'GET /v1/items': { data: [] } }))
    const user = userEvent.setup()

    renderWithProviders(<CollectionPage id="col_1" />, { mode: 'cloud' })
    expect(await screen.findByText('This collection is empty')).toBeInTheDocument()

    // One "Add item" in the header, one in the empty state.
    const add = screen.getAllByRole('button', { name: /add\s*item/i })

    expect(add).toHaveLength(2)
    await user.click(add[1])
    expect(await screen.findByRole('dialog', { name: 'Add item' })).toBeInTheDocument()
  })

  it('makes archived collections read-only', async () => {
    nav.set('/collections/col_1')
    mockApi(
      routes({
        'GET /v1/collections/col_1': collection({ archived: true, archived_at: '2026-02-01T00:00:00Z' }),
        'GET /v1/items': { data: [] },
      }),
    )

    renderWithProviders(<CollectionPage id="col_1" />, { mode: 'cloud' })
    expect(await screen.findByText('Archived')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^add\s*item$/i })).toBeDisabled()
    expect(screen.getByRole('button', { name: /^add\s*item$/i })).toHaveAttribute(
      'title',
      'Archived collections are read-only',
    )
    expect(await screen.findByText('This collection is empty')).toBeInTheDocument()
    // The empty state has no second "Add item" button for an archived collection.
    expect(screen.getAllByRole('button', { name: /add\s*item/i })).toHaveLength(1)
  })
})

function renderSettings(c = collection(), over: Record<string, unknown> = {}) {
  const api = mockApi({ 'GET /v1/collections': { data: [c] }, ...over })

  renderWithProviders(<CollectionSettingsSheet collection={c} open onOpenChange={() => {}} />, { mode: 'cloud' })

  return api
}

describe('collection settings', () => {
  it('offers neither archive nor delete for the Default collection', async () => {
    renderSettings(collection({ name: 'Default', is_default: true }))

    expect(await screen.findByText(/cannot be archived or deleted/i)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /archive collection/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /delete collection/i })).not.toBeInTheDocument()
  })

  it('saves name, description and colour only once the form is dirty', async () => {
    const api = renderSettings(collection(), {
      'PATCH /v1/collections/col_1': collection({ name: 'Avatars 2', description: 'New', color: '#3b82f6' }),
    })
    const user = userEvent.setup()
    const save = screen.getByRole('button', { name: 'Save changes' })

    expect(save).toBeDisabled()

    const name = screen.getByRole('textbox', { name: 'Name' })

    await user.clear(name)
    await user.type(name, '  Avatars 2 ')
    await user.clear(screen.getByLabelText('Description'))
    await user.type(screen.getByLabelText('Description'), 'New')
    await user.click(screen.getByRole('radio', { name: 'Blue' }))
    await user.click(save)

    await waitFor(() => expect(api.find('PATCH', '/v1/collections/col_1')).toHaveLength(1))
    expect(api.find('PATCH', '/v1/collections/col_1')[0].body).toEqual({
      name: 'Avatars 2',
      description: 'New',
      color: '#3b82f6',
    })
    expect(await screen.findByText(/was updated/)).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('button', { name: 'Save changes' })).toBeDisabled())
  })

  it('requires a name and shows server field errors', async () => {
    const api = renderSettings(collection(), {
      'PATCH /v1/collections/col_1': {
        status: 422,
        body: {
          error: { code: 'validation_failed', message: 'validation failed', fields: { name: 'already exists' } },
        },
      },
    })
    const user = userEvent.setup()
    const name = screen.getByRole('textbox', { name: 'Name' })

    expect(screen.getByText('Name', { selector: 'label' })).toHaveTextContent('Name*')
    expect(name).toHaveAttribute('aria-required', 'true')
    await user.clear(name)
    await user.click(screen.getByRole('button', { name: 'Save changes' }))
    expect(await screen.findByText('Name is required')).toHaveAttribute('data-slot', 'field-error')
    expect(name).toHaveAttribute('aria-invalid', 'true')
    expect(name.closest('[data-slot=field]')).toHaveAttribute('data-invalid', 'true')
    expect(api.find('PATCH', '/v1/collections/col_1')).toHaveLength(0)

    await user.type(name, 'Dup')
    expect(screen.queryByText('Name is required')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Save changes' }))
    expect(await screen.findByText('Already exists')).toHaveAttribute('data-slot', 'field-error')
    expect(name).toHaveAttribute('aria-invalid', 'true')
  })

  it('archives an active collection', async () => {
    const api = renderSettings(collection(), {
      'PATCH /v1/collections/col_1': collection({ archived: true }),
    })
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: 'Archive collection' }))
    await waitFor(() => expect(api.find('PATCH', '/v1/collections/col_1')).toHaveLength(1))
    expect(api.find('PATCH', '/v1/collections/col_1')[0].body).toEqual({ archived: true })
    expect(await screen.findByText(/was archived/)).toBeInTheDocument()
  })

  it('restores an archived collection', async () => {
    const api = renderSettings(collection({ archived: true }), {
      'PATCH /v1/collections/col_1': collection({ archived: false }),
    })
    const user = userEvent.setup()

    expect(screen.getByRole('heading', { name: 'Archived' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Unarchive collection' }))
    await waitFor(() => expect(api.find('PATCH', '/v1/collections/col_1')).toHaveLength(1))
    expect(api.find('PATCH', '/v1/collections/col_1')[0].body).toEqual({ archived: false })
    expect(await screen.findByText(/was restored/)).toBeInTheDocument()
  })

  it('deletes only after the name is typed, then leaves for the collection list', async () => {
    const api = renderSettings(collection(), { 'DELETE /v1/collections/col_1': { status: 204 } })
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: 'Delete collection' }))

    const dialog = await screen.findByRole('dialog', { name: /delete “instagram avatar”\?/i })
    const confirm = within(dialog).getByRole('button', { name: 'Delete permanently' })

    const box = within(dialog).getByRole('textbox', { name: /to confirm/i })

    expect(within(dialog).getByText(/its 2 items/)).toBeInTheDocument()
    expect(within(dialog).getByText(/to confirm/, { selector: 'label' })).toHaveTextContent(/confirm\*$/)
    expect(box).toHaveAttribute('aria-required', 'true')
    await user.type(box, 'Instagram')
    await user.click(confirm)
    expect(within(dialog).getByRole('alert')).toHaveTextContent('Type “Instagram Avatar” exactly to confirm')
    expect(box).toHaveAttribute('aria-invalid', 'true')
    expect(api.find('DELETE', '/v1/collections/col_1')).toHaveLength(0)
    await user.type(box, ' Avatar')
    expect(within(dialog).queryByRole('alert')).not.toBeInTheDocument()
    expect(box).toHaveAttribute('aria-invalid', 'false')
    await user.click(confirm)

    await waitFor(() => expect(nav.push).toHaveBeenCalledWith('/collections'))

    const del = api.find('DELETE', '/v1/collections/col_1')

    expect(del).toHaveLength(1)
    expect(del[0].query.get('confirm')).toBe('Instagram Avatar')
    expect(await screen.findByText('“Instagram Avatar” was deleted')).toBeInTheDocument()
  })

  it('keeps the dialog open and reports a failed delete', async () => {
    renderSettings(collection(), {
      'DELETE /v1/collections/col_1': {
        status: 409,
        body: { error: { code: 'conflict', message: 'confirmation does not match' } },
      },
    })
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: 'Delete collection' }))

    const dialog = await screen.findByRole('dialog', { name: /delete/i })

    await user.type(within(dialog).getByRole('textbox', { name: /to confirm/i }), 'Instagram Avatar')
    await user.click(within(dialog).getByRole('button', { name: 'Delete permanently' }))
    expect(await screen.findByText('confirmation does not match')).toBeInTheDocument()
    expect(nav.push).not.toHaveBeenCalled()
    expect(screen.getByRole('dialog', { name: /delete/i })).toBeInTheDocument()
  })
})
