import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { Inbox } from '@/components/inbox/inbox'
import type { Item } from '@/lib/api'

import { APPROVE, REGENERATE, REJECT, collection, item } from './fixtures'
import { nav } from './navigation'
import { mockApi, renderWithProviders } from './utils'

const ITEMS: Item[] = [
  item({ id: 'itm_1', title: 'Avatar one', feedback: 'Earlier note' }),
  item({ id: 'itm_2', title: 'Avatar two', version: 3, metadata: {} }),
  item({ id: 'itm_3', title: 'Caption three', type: 'text', media: null, content: 'Hello', metadata: {} }),
]

function setup(extra: Record<string, unknown> = {}) {
  return mockApi({
    'GET /v1/items': { data: ITEMS },
    'GET /v1/collections': { data: [collection({ status_counts: { pending: 3 }, item_count: 3 })] },
    'GET /v1/items/*': (c: { path: string }) =>
      c.path.endsWith('/actions') ? { data: [APPROVE, REJECT, REGENERATE] } : ITEMS.find((i) => c.path.endsWith(i.id)),
    'GET /v1/events': { data: [] },
    'POST /v1/items/itm_1/actions/approve': item({ id: 'itm_1', title: 'Avatar one', status: 'approved', version: 2 }),
    'POST /v1/items/itm_1/actions/reject': item({ id: 'itm_1', title: 'Avatar one', status: 'rejected', version: 2 }),
    ...extra,
  })
}

/** Phone viewport: useIsMobile() reads (max-width: 767px). */
function phone() {
  vi.spyOn(window, 'matchMedia').mockImplementation(
    (query: string) =>
      ({
        matches: query.includes('max-width'),
        media: query,
        onchange: null,
        addEventListener: () => {},
        removeEventListener: () => {},
        addListener: () => {},
        removeListener: () => {},
        dispatchEvent: () => false,
      }) as MediaQueryList,
  )
}

const card = () => screen.getByTestId('inbox-preview')

async function ready() {
  const bar = await screen.findByTestId('inbox-actions')

  await within(bar).findByRole('button', { name: /^approve/i })

  return bar
}

describe('mobile inbox', () => {
  beforeEach(() => {
    nav.set('/inbox')
    phone()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('shows one card with its position, feedback and metadata, and the queue', async () => {
    setup()
    renderWithProviders(<Inbox />, { mode: 'local' })
    await ready()

    expect(screen.getByTestId('inbox-mobile')).toBeInTheDocument()
    expect(screen.getByTestId('inbox-count-label')).toHaveTextContent('3 to review')
    expect(screen.getByTestId('inbox-position')).toHaveTextContent('1 of 3')

    const preview = screen.getByTestId('inbox-preview')

    expect(within(preview).getByRole('heading', { name: 'Avatar one' })).toBeInTheDocument()
    expect(within(preview).getByText('Earlier note')).toBeInTheDocument()
    expect(within(preview).getByTestId('metadata-body')).toHaveTextContent('"prompt": "studio portrait"')
    expect(within(preview).getByRole('link', { name: /open details/i })).toHaveAttribute('href', '/items/itm_1')
    expect(screen.getByRole('heading', { name: 'Up next' })).toBeInTheDocument()
    expect(within(screen.getByRole('listbox', { name: 'Items' })).getAllByRole('option')).toHaveLength(3)
    expect(screen.getByRole('button', { name: 'Previous item' })).toBeDisabled()
  })

  it('moves with the arrows, by swiping and by picking from the queue', async () => {
    setup()
    const user = userEvent.setup()

    renderWithProviders(<Inbox />, { mode: 'local' })
    await ready()

    await user.click(screen.getByRole('button', { name: 'Next item' }))
    expect(await screen.findByRole('heading', { name: 'Avatar two' })).toBeInTheDocument()
    expect(screen.getByTestId('inbox-position')).toHaveTextContent('2 of 3')

    // A horizontal swipe to the left goes forward; a mostly vertical one is a scroll and does nothing.
    fireEvent.touchStart(card(), { touches: [{ clientX: 300, clientY: 100 }] })
    fireEvent.touchEnd(card(), { changedTouches: [{ clientX: 100, clientY: 110 }] })
    expect(await screen.findByRole('heading', { name: 'Caption three' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Next item' })).toBeDisabled()

    fireEvent.touchStart(card(), { touches: [{ clientX: 100, clientY: 100 }] })
    fireEvent.touchEnd(card(), { changedTouches: [{ clientX: 200, clientY: 400 }] })
    expect(screen.getByTestId('inbox-position')).toHaveTextContent('3 of 3')

    fireEvent.touchStart(card(), { touches: [{ clientX: 100, clientY: 100 }] })
    fireEvent.touchEnd(card(), { changedTouches: [{ clientX: 300, clientY: 100 }] })
    expect(await screen.findByRole('heading', { name: 'Avatar two' })).toBeInTheDocument()

    await user.click(screen.getByRole('option', { name: /avatar one/i }))
    expect(await screen.findByRole('heading', { name: 'Avatar one', level: 2 })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: /avatar one/i })).toHaveAttribute('aria-selected', 'true')
  })

  it('approves from the sticky bar and advances', async () => {
    const api = setup()
    const user = userEvent.setup()

    renderWithProviders(<Inbox />, { mode: 'local' })

    const bar = await ready()

    await user.click(within(bar).getByRole('button', { name: /^approve/i }))
    await waitFor(() => expect(api.find('POST', '/v1/items/itm_1/actions/approve')).toHaveLength(1))
    expect(api.find('POST', '/v1/items/itm_1/actions/approve')[0].body).toEqual({ expected_version: 1 })
    expect(await screen.findByRole('heading', { name: 'Avatar two' })).toBeInTheDocument()
  })

  it('asks for feedback before rejecting from the sticky bar', async () => {
    const api = setup()
    const user = userEvent.setup()

    renderWithProviders(<Inbox />, { mode: 'local' })

    const bar = await ready()

    await user.click(within(bar).getByRole('button', { name: /^reject/i }))

    const dialog = await screen.findByRole('dialog')

    await user.type(within(dialog).getByRole('textbox'), 'Too dark{Enter}')
    await waitFor(() => expect(api.find('POST', '/v1/items/itm_1/actions/reject')).toHaveLength(1))
    expect(api.find('POST', '/v1/items/itm_1/actions/reject')[0].body).toEqual({
      feedback: 'Too dark',
      expected_version: 1,
    })
  })

  it('offers edit, custom actions and details in the more menu', async () => {
    setup()
    const user = userEvent.setup()

    renderWithProviders(<Inbox />, { mode: 'local' })

    const bar = await ready()

    await user.click(within(bar).getByRole('button', { name: 'More actions' }))
    expect(await screen.findByRole('menuitem', { name: /^edit$/i })).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: /open details/i })).toHaveAttribute('href', '/items/itm_1')
    await user.click(screen.getByRole('menuitem', { name: /regenerate/i }))
    expect(await screen.findByRole('dialog', { name: 'Regenerate' })).toBeInTheDocument()
  })

  it('switches status with the segmented control', async () => {
    setup()
    const user = userEvent.setup()

    renderWithProviders(<Inbox />, { mode: 'local' })
    await ready()
    await user.click(screen.getByRole('radio', { name: /rejected/i }))
    expect(nav.replace).toHaveBeenCalledWith('/inbox?step=rejected', { scroll: false })
  })

  it('filters by collection in the sheet and clears filters', async () => {
    nav.set('/inbox', 'type=image')
    setup()
    const user = userEvent.setup()

    renderWithProviders(<Inbox />, { mode: 'local' })
    await ready()

    // One active filter (type) is counted on the button.
    const filters = screen.getByRole('button', { name: 'Filters' })

    expect(filters).toHaveTextContent('1')
    await user.click(filters)

    const sheet = await screen.findByRole('dialog', { name: 'Filter inbox' })

    await user.click(within(sheet).getByRole('combobox', { name: 'Collection filter' }))
    await user.click(await screen.findByRole('option', { name: 'Instagram Avatar' }))
    expect(nav.replace).toHaveBeenLastCalledWith('/inbox?type=image&collection=col_1', { scroll: false })

    await user.click(
      within(screen.getByRole('dialog', { name: 'Filter inbox' })).getByRole('button', { name: 'Clear filters' }),
    )
    expect(nav.replace).toHaveBeenLastCalledWith('/inbox', { scroll: false })
  })

  it('shows inbox zero and per-status empty states', async () => {
    setup({ 'GET /v1/items': { data: [] } })
    renderWithProviders(<Inbox />, { mode: 'local' })
    expect(await screen.findByText('Inbox zero')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Connect an agent' })).toHaveAttribute('href', '/integrations')
    expect(screen.queryByTestId('inbox-actions')).not.toBeInTheDocument()
  })

  it('explains an empty step', async () => {
    nav.set('/inbox', 'step=failed')
    setup({ 'GET /v1/items': { data: [] } })
    renderWithProviders(<Inbox />, { mode: 'local' })
    expect(await screen.findByText('No failures')).toBeInTheDocument()
    expect(screen.getByText(/ready to retry/)).toBeInTheDocument()
  })

  it('shows a load error with a retry', async () => {
    setup({ 'GET /v1/items': { status: 500, body: { error: { code: 'internal', message: 'boom' } } } })
    renderWithProviders(<Inbox />, { mode: 'local' })
    expect(await screen.findByText('Could not load data')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Try again' })).toBeInTheDocument()
  })

  it('loads more of the queue', async () => {
    const api = setup({
      'GET /v1/items': (c: { query: URLSearchParams }) =>
        c.query.get('cursor') === 'cur_2'
          ? { data: [item({ id: 'itm_4', title: 'Fourth', metadata: {} })] }
          : { data: ITEMS, next_cursor: 'cur_2' },
    })
    const user = userEvent.setup()

    renderWithProviders(<Inbox />, { mode: 'local' })
    await ready()
    expect(screen.getByTestId('inbox-position')).toHaveTextContent('1 of 3+')
    await user.click(screen.getByRole('button', { name: 'Load more' }))
    expect(await screen.findByRole('option', { name: /fourth/i })).toBeInTheDocument()
    expect(api.find('GET', '/v1/items').some((c) => c.query.get('cursor') === 'cur_2')).toBe(true)
  })
})
