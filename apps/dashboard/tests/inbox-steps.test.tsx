import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'

import { Inbox } from '@/components/inbox/inbox'
import type { Item } from '@/lib/api'

import { APPROVE, REGENERATE, REJECT, collection, item } from './fixtures'
import { nav } from './navigation'
import { mockApi, renderWithProviders } from './utils'

const PENDING: Item[] = [item({ id: 'itm_1', title: 'Avatar one' }), item({ id: 'itm_2', title: 'Avatar two' })]
const ARCHIVED: Item[] = [
  item({ id: 'itm_9', title: 'Old post', status: 'archived', archived_at: '2026-01-01T00:00:00Z' }),
]
const FLOW = {
  since: '2026-10-13T00:00:00Z',
  sources: [{ source: 'claude-code', items: 2, last_created_at: '2026-10-14T00:00:00Z' }],
  states: [],
  events: {},
}

function setup(extra: Record<string, unknown> = {}) {
  return mockApi({
    'GET /v1/items': (c: { query: URLSearchParams }) => ({
      data: c.query.get('decision') === 'archived' ? ARCHIVED : PENDING,
    }),
    'GET /v1/collections': {
      data: [collection({ actions: [REGENERATE], status_counts: { pending: 2 }, item_count: 3 })],
    },
    'GET /v1/items/*': { data: [APPROVE, REJECT] },
    'GET /v1/events': { data: [] },
    'GET /v1/flow': FLOW,
    'GET /v1/tags': { data: [] },
    ...extra,
  })
}

describe('inbox steps and bulk', () => {
  beforeEach(() => nav.set('/inbox'))

  it('Filter puts the source in the URL', async () => {
    setup()
    const user = userEvent.setup()

    renderWithProviders(<Inbox />, { mode: 'local' })
    await screen.findByText('Avatar one')
    await user.click(screen.getByRole('button', { name: 'Filter' }))
    await user.click(await screen.findByRole('combobox', { name: 'Source filter' }))
    await user.click(await screen.findByRole('option', { name: 'claude-code' }))
    expect(nav.replace).toHaveBeenCalledWith('/inbox?source=claude-code', { scroll: false })
  })

  it('old ?decision= links open the matching step; ?step= only takes known steps', async () => {
    nav.set('/inbox', 'decision=archived')
    const api = setup()

    renderWithProviders(<Inbox />, { mode: 'local' })
    expect(await screen.findByText('Old post')).toBeInTheDocument()
    expect(api.find('GET', '/v1/items')[0].query.get('decision')).toBe('archived')
  })

  it('Archived: Unarchive and Delete the selection', async () => {
    nav.set('/inbox', 'step=archived')
    const api = setup({
      'POST /v1/items/itm_9/unarchive': item({ id: 'itm_9', title: 'Old post', status: 'approved' }),
      'DELETE /v1/items/itm_9': null,
    })
    const user = userEvent.setup()

    renderWithProviders(<Inbox />, { mode: 'local' })
    await user.click(await screen.findByRole('checkbox', { name: 'Select Old post' }))
    const bar = screen.getByTestId('bulk-bar')

    await user.click(within(bar).getByRole('button', { name: /unarchive/i }))
    await waitFor(() => expect(api.find('POST', '/v1/items/itm_9/unarchive')).toHaveLength(1))
  })

  it('Delete in the bar asks first', async () => {
    const api = setup({ 'DELETE /v1/items/itm_1': null })
    const user = userEvent.setup()

    renderWithProviders(<Inbox />, { mode: 'local' })
    await user.click(await screen.findByRole('checkbox', { name: 'Select Avatar one' }))
    await user.click(within(screen.getByTestId('bulk-bar')).getByRole('button', { name: /delete/i }))
    const dialog = await screen.findByRole('alertdialog')

    expect(api.find('DELETE', '/v1/items/itm_1')).toHaveLength(0)
    await user.click(within(dialog).getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(api.find('DELETE', '/v1/items/itm_1')).toHaveLength(1))
  })

  it('Reject can ask the agent to regenerate; the next reject starts empty', async () => {
    const api = setup({
      'POST /v1/items/itm_1/actions/reject': item({ id: 'itm_1', title: 'Avatar one', status: 'processing' }),
    })
    const user = userEvent.setup()

    renderWithProviders(<Inbox />, { mode: 'local' })
    const rowOne = (await screen.findByText('Avatar one')).closest('li')!

    await user.click(within(rowOne).getByRole('button', { name: /reject/i }))
    let dialog = await screen.findByRole('dialog')

    await user.type(within(dialog).getByLabelText('Feedback*'), 'Too dark')
    await user.click(within(dialog).getByRole('checkbox'))
    await user.click(within(dialog).getByRole('button', { name: 'Reject' }))
    await waitFor(() => expect(api.find('POST', '/v1/items/itm_1/actions/reject')).toHaveLength(1))
    expect(api.find('POST', '/v1/items/itm_1/actions/reject')[0].body).toMatchObject({
      feedback: 'Too dark',
      follow_up: 'regenerate',
    })

    const rowTwo = (await screen.findByText('Avatar two')).closest('li')!

    await user.click(within(rowTwo).getByRole('button', { name: /reject/i }))
    dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByLabelText('Feedback*')).toHaveValue('')
    expect(within(dialog).getByRole('checkbox')).not.toBeChecked()
  })
})
