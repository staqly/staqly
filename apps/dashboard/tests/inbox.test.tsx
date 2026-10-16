import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'

import { Inbox } from '@/components/inbox/inbox'
import { undoDelay } from '@/components/items/item-actions'
import type { Item } from '@/lib/api'

import { APPROVE, REGENERATE, REJECT, collection, item } from './fixtures'
import { nav } from './navigation'
import { findActiveRow, getActiveRow, mockApi, queryRow, renderWithProviders } from './utils'

const ITEMS: Item[] = [
  item({ id: 'itm_1', title: 'Avatar one' }),
  item({ id: 'itm_2', title: 'Avatar two', version: 3 }),
  item({ id: 'itm_3', title: 'Caption three', type: 'text', media: null, content: 'Hello' }),
]

function setup(extra: Record<string, unknown> = {}) {
  return mockApi({
    'GET /v1/items': { data: ITEMS },
    'GET /v1/collections': { data: [collection({ status_counts: { pending: 3 }, item_count: 3 })] },
    'GET /v1/items/*': (c: { path: string }) =>
      c.path.endsWith('/actions') ? { data: [APPROVE, REJECT, REGENERATE] } : ITEMS.find((i) => c.path.endsWith(i.id)),
    'GET /v1/events': { data: [] },
    'POST /v1/items/itm_1/actions/approve': item({ id: 'itm_1', title: 'Avatar one', status: 'approved', version: 2 }),
    'POST /v1/items/itm_2/actions/approve': item({ id: 'itm_2', title: 'Avatar two', status: 'approved', version: 4 }),
    'PATCH /v1/items/itm_1': item({ id: 'itm_1', title: 'Avatar final', version: 2 }),
    'POST /v1/items/itm_1/actions/reject': item({ id: 'itm_1', title: 'Avatar one', status: 'rejected', version: 2 }),
    ...extra,
  })
}

const checkbox = (name: RegExp) => screen.getByRole('checkbox', { name })

async function ready() {
  // The active row (the first one to start with) carries Reject and Approve.
  const preview = await findActiveRow()

  await within(preview).findByRole('button', { name: /^approve/i })

  return preview
}

describe('approval inbox', () => {
  beforeEach(() => nav.set('/inbox'))

  it('lists pending items oldest first and previews the first one', async () => {
    const api = setup()

    renderWithProviders(<Inbox />, { mode: 'local' })
    const preview = await ready()

    expect(getActiveRow(/Avatar one/i)).toBeInTheDocument()
    const q = api.find('GET', '/v1/items')[0].query

    expect(q.get('decision')).toBe('pending')
    expect(q.get('sort')).toBe('created_asc')
    expect(screen.getByTestId('inbox-count-label')).toHaveTextContent('3 to review')
    // No preview pane: a click on the row opens the whole item in the drawer.
    expect(screen.queryByTestId('inbox-preview')).not.toBeInTheDocument()
    await userEvent.setup().click(within(preview).getByText('Avatar one'))
    expect(await screen.findByRole('dialog')).toBeInTheDocument()
  })

  it('approves via the button and advances to the next item', async () => {
    const api = setup()
    const user = userEvent.setup()

    renderWithProviders(<Inbox />, { mode: 'local' })
    const preview = await ready()

    await user.click(within(preview).getByRole('button', { name: /^approve/i }))
    await waitFor(() => expect(api.find('POST', '/v1/items/itm_1/actions/approve')).toHaveLength(1))
    expect(api.find('POST', '/v1/items/itm_1/actions/approve')[0].body).toEqual({ expected_version: 1 })
    expect(await findActiveRow(/Avatar two/i)).toBeInTheDocument()
    expect(queryRow(/avatar one/i)).not.toBeInTheDocument()
  })

  it('holds an approval for Undo: nothing is sent and the item comes back', async () => {
    const api = setup()
    const user = userEvent.setup()

    undoDelay.ms = 60_000

    try {
      renderWithProviders(<Inbox />, { mode: 'local' })
      const preview = await ready()

      await user.click(within(preview).getByRole('button', { name: /^approve/i }))
      expect(await findActiveRow(/Avatar two/i)).toBeInTheDocument()
      await user.click(await screen.findByRole('button', { name: 'Undo' }))
      expect(await findActiveRow(/Avatar one/i)).toBeInTheDocument()
      expect(api.find('POST', '/v1/items/itm_1/actions/approve')).toHaveLength(0)
    } finally {
      undoDelay.ms = 0
    }
  })

  it('supports j/k navigation and the Shift+A shortcut', async () => {
    const api = setup()

    renderWithProviders(<Inbox />, { mode: 'local' })
    await ready()
    fireEvent.keyDown(window, { key: 'j' })
    expect(await findActiveRow(/Avatar two/i)).toBeInTheDocument()
    fireEvent.keyDown(window, { key: 'k' })
    expect(await findActiveRow(/Avatar one/i)).toBeInTheDocument()
    fireEvent.keyDown(window, { key: 'j' })
    await findActiveRow(/Avatar two/i)
    await within(getActiveRow()).findByRole('button', { name: /^approve/i })
    // Caps Lock on: Shift+A arrives as 'a' with shiftKey
    fireEvent.keyDown(window, { key: 'a', shiftKey: true })
    await waitFor(() => expect(api.find('POST', '/v1/items/itm_2/actions/approve')).toHaveLength(1))
    expect(api.find('POST', '/v1/items/itm_2/actions/approve')[0].body).toEqual({ expected_version: 3 })
  })

  it('r opens a feedback box that requires text and Enter submits it', async () => {
    const api = setup()
    const user = userEvent.setup()

    renderWithProviders(<Inbox />, { mode: 'local' })
    await ready()
    fireEvent.keyDown(window, { key: 'R', shiftKey: true })
    const dialog = await screen.findByRole('dialog')
    const submit = within(dialog).getByRole('button', { name: 'Reject' })

    const box = within(dialog).getByLabelText('Feedback*')

    await user.click(submit)
    expect(within(dialog).getByRole('alert')).toHaveTextContent('Feedback is required for this action')
    expect(box).toHaveAttribute('aria-invalid', 'true')
    await user.type(box, '{Enter}')
    expect(api.find('POST', '/v1/items/itm_1/actions/reject')).toHaveLength(0)
    await user.type(box, 'Face too small{Enter}')
    await waitFor(() => expect(api.find('POST', '/v1/items/itm_1/actions/reject')).toHaveLength(1))
    expect(api.find('POST', '/v1/items/itm_1/actions/reject')[0].body).toEqual({
      feedback: 'Face too small',
      expected_version: 1,
    })
  })

  it('Shift+E edits the item and leaves it pending', async () => {
    const api = setup()
    const user = userEvent.setup()

    renderWithProviders(<Inbox />, { mode: 'local' })
    await ready()
    fireEvent.keyDown(window, { key: 'E', shiftKey: true })
    const dialog = await screen.findByRole('dialog')
    const title = within(dialog).getByLabelText('Title*')

    await user.clear(title)
    await user.type(title, 'Avatar final')
    await user.click(within(dialog).getByRole('button', { name: 'Save changes' }))
    await waitFor(() => expect(api.find('PATCH', '/v1/items/itm_1')).toHaveLength(1))
    expect(api.find('PATCH', '/v1/items/itm_1')[0].body).toEqual({ title: 'Avatar final', expected_version: 1 })
    expect(api.find('POST', '/v1/items/itm_1/actions/approve')).toHaveLength(0)
  })

  it('x selects items and shift+A bulk approves them', async () => {
    const api = setup({
      'POST /v1/items/bulk-actions': {
        data: [
          { id: 'itm_1', ok: true, item: item({ id: 'itm_1', status: 'approved' }) },
          { id: 'itm_2', ok: false, error: { code: 'conflict', message: 'already decided' } },
        ],
      },
    })

    renderWithProviders(<Inbox />, { mode: 'local' })
    await ready()
    fireEvent.keyDown(window, { key: 'x' })
    fireEvent.keyDown(window, { key: 'j' })
    await findActiveRow(/Avatar two/i)
    fireEvent.keyDown(window, { key: 'x' })
    expect(await screen.findByTestId('bulk-bar')).toBeInTheDocument()
    expect(screen.getByTestId('inbox-count-label')).toHaveTextContent('2 of 3 selected')
    fireEvent.keyDown(window, { key: 'A', shiftKey: true })

    // Several items are confirmed first: a bulk approval has no Undo.
    const confirm = await screen.findByRole('alertdialog', { name: 'Approve 2 items?' })

    expect(api.find('POST', '/v1/items/bulk-actions')).toHaveLength(0)
    fireEvent.click(within(confirm).getByRole('button', { name: 'Approve 2' }))
    await waitFor(() => expect(api.find('POST', '/v1/items/bulk-actions')).toHaveLength(1))
    expect(api.find('POST', '/v1/items/bulk-actions')[0].body).toEqual({ ids: ['itm_1', 'itm_2'], action: 'approve' })
    expect(await screen.findByText(/1 item skipped \(1 already decided\)/)).toBeInTheDocument()
  })

  it('selects all from the list header and ranges with shift-click', async () => {
    setup()
    const user = userEvent.setup()

    renderWithProviders(<Inbox />, { mode: 'local' })
    await ready()
    await user.click(checkbox(/^select all$/i))
    expect(screen.getByTestId('inbox-count-label')).toHaveTextContent('3 of 3 selected')
    await user.click(checkbox(/^deselect all$/i))
    expect(screen.getByTestId('inbox-count-label')).toHaveTextContent('3 to review')

    await user.click(checkbox(/select avatar one/i))
    await user.keyboard('{Shift>}')
    await user.click(checkbox(/select caption three/i))
    await user.keyboard('{/Shift}')
    expect(checkbox(/select avatar two/i)).toBeChecked()
    expect(screen.getByTestId('inbox-count-label')).toHaveTextContent('3 of 3 selected')
    expect(checkbox(/^deselect all$/i)).toBeChecked()

    await user.click(checkbox(/select avatar two/i))
    expect(checkbox(/^select all$/i)).toHaveAttribute('data-state', 'indeterminate')
  })

  it('decides only with Shift; Shift+R rejects the selection', async () => {
    const api = setup({ 'POST /v1/items/bulk-actions': { data: [] } })

    renderWithProviders(<Inbox />, { mode: 'local' })
    await ready()
    fireEvent.keyDown(window, { key: 'a' })
    fireEvent.keyDown(window, { key: 'r' })
    await new Promise((r) => setTimeout(r, 50))
    expect(api.calls.filter((c) => c.method === 'POST')).toHaveLength(0)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

    // Select the second item, go back to the first: Shift+R rejects the selection, not the active item.
    fireEvent.keyDown(window, { key: 'j' })
    fireEvent.keyDown(window, { key: 'x' })
    fireEvent.keyDown(window, { key: 'k' })
    fireEvent.keyDown(window, { key: 'R', shiftKey: true })
    const dialog = await screen.findByRole('dialog')

    await userEvent.setup().type(within(dialog).getByLabelText('Feedback*'), 'Too dark{Enter}')
    await waitFor(() => expect(api.find('POST', '/v1/items/bulk-actions')).toHaveLength(1))
    expect(api.find('POST', '/v1/items/bulk-actions')[0].body).toEqual({
      ids: ['itm_2'],
      action: 'reject',
      feedback: 'Too dark',
    })
  })

  it('deletes the selection after a confirmation', async () => {
    const api = setup({ 'DELETE /v1/items/itm_1': null, 'DELETE /v1/items/itm_2': null })
    const user = userEvent.setup()

    renderWithProviders(<Inbox />, { mode: 'local' })
    await ready()
    await user.click(checkbox(/select avatar one/i))
    await user.click(checkbox(/select avatar two/i))
    await user.click(within(screen.getByTestId('bulk-bar')).getByRole('button', { name: /delete/i }))
    const dialog = await screen.findByRole('alertdialog')

    expect(within(dialog).getByText('Delete 2 items?')).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(queryRow(/avatar two/i)).not.toBeInTheDocument())
    expect(api.find('DELETE', '/v1/items/itm_1')).toHaveLength(1)
    expect(api.find('DELETE', '/v1/items/itm_2')).toHaveLength(1)
  })

  it('handles 409 conflicts gracefully', async () => {
    setup({
      'POST /v1/items/itm_1/actions/approve': {
        status: 409,
        body: {
          error: { code: 'conflict', message: 'cannot approve an item that is approved (allowed from: pending)' },
        },
      },
    })
    const user = userEvent.setup()

    renderWithProviders(<Inbox />, { mode: 'local' })
    const preview = await ready()

    await user.click(within(preview).getByRole('button', { name: /^approve/i }))
    expect(await screen.findByText(/Already decided/)).toBeInTheDocument()
  })

  it('shows the shortcut legend', async () => {
    setup()
    renderWithProviders(<Inbox />, { mode: 'local' })
    await ready()
    fireEvent.keyDown(window, { key: '?' })
    const dialog = await screen.findByRole('dialog')

    expect(within(dialog).getByText('Approve (selected, or this item)')).toBeInTheDocument()
    expect(within(dialog).getByText('Reject with feedback')).toBeInTheDocument()
  })
})

describe('empty inbox', () => {
  it('sends a test approval to the Default collection', async () => {
    const user = userEvent.setup()
    const api = mockApi({
      'GET /v1/items': { data: [] },
      'GET /v1/collections': { data: [collection({ id: 'col_def', name: 'Default', is_default: true })] },
      'GET /v1/events': { data: [] },
      'POST /v1/collections/col_def/items': item({ id: 'itm_test', title: 'Test approval' }),
    })

    renderWithProviders(<Inbox />, { mode: 'local' })
    await user.click(await screen.findByRole('button', { name: 'Send a test approval' }))

    await waitFor(() => expect(api.find('POST', '/v1/collections/col_def/items')).toHaveLength(1))
    expect(api.find('POST', '/v1/collections/col_def/items')[0].body).toMatchObject({ title: 'Test approval' })
  })

  it('decided items take collection actions in bulk, never approve or reject', async () => {
    nav.set('/inbox', 'step=approved')
    const publish = {
      name: 'publish',
      label: 'Publish',
      event: 'item.publish_requested',
      requires_feedback: false,
      set_status: 'completed' as const,
      allowed_statuses: ['approved' as const],
    }
    const api = setup({
      'GET /v1/items': { data: [item({ id: 'itm_9', title: 'Done', status: 'approved' })] },
      'GET /v1/collections': { data: [collection({ actions: [REGENERATE, publish] })] },
      'POST /v1/items/bulk-actions': {
        data: [{ id: 'itm_9', ok: true, item: item({ id: 'itm_9', status: 'completed' }) }],
      },
    })
    const user = userEvent.setup()

    renderWithProviders(<Inbox />, { mode: 'local' })
    await findActiveRow(/Done/i)
    expect(screen.getByTestId('inbox-count-label')).toHaveTextContent('1 approved')
    // Each row offers what can be done with its item now: Publish, not Regenerate (not allowed from approved).
    const row = document.querySelector<HTMLElement>('[data-item-id="itm_9"]')!

    expect(within(row).getByRole('button', { name: 'Publish' })).toBeInTheDocument()
    expect(within(row).queryByRole('button', { name: /regenerate|approve|reject/i })).not.toBeInTheDocument()
    await user.click(checkbox(/^select all$/i))
    const bar = screen.getByTestId('bulk-bar')

    // Regenerate isn't allowed from approved; approve/reject never show here.
    expect(within(bar).queryByRole('button', { name: /approve|reject|regenerate/i })).not.toBeInTheDocument()
    fireEvent.keyDown(window, { key: 'A', shiftKey: true })
    await user.click(within(bar).getByRole('button', { name: 'Publish' }))
    await waitFor(() => expect(api.find('POST', '/v1/items/bulk-actions')).toHaveLength(1))
    expect(api.find('POST', '/v1/items/bulk-actions')[0].body).toEqual({ ids: ['itm_9'], action: 'publish' })

    // The legend has no decisions here.
    fireEvent.keyDown(window, { key: '?' })
    const legend = await screen.findByRole('dialog')

    expect(within(legend).getByText('Toggle selection')).toBeInTheDocument()
    expect(within(legend).queryByText(/approve|reject/i)).not.toBeInTheDocument()
  })

  it('processing rows show how long they waited and report the result by hand', async () => {
    nav.set('/inbox', 'step=processing')
    const api = setup({
      'GET /v1/items': { data: [item({ id: 'itm_p', title: 'Waiting job', status: 'processing' })] },
      'POST /v1/items/itm_p/result': item({ id: 'itm_p', title: 'Waiting job', status: 'completed' }),
    })
    const user = userEvent.setup()

    renderWithProviders(<Inbox />, { mode: 'local' })
    const row = (await screen.findByText('Waiting job')).closest<HTMLElement>('[data-item-id]')!

    expect(within(row).getByTestId('processing-wait')).toBeInTheDocument()
    await user.click(within(row).getByRole('button', { name: 'Report result' }))
    await user.click(await screen.findByRole('menuitem', { name: /completed/i }))
    await waitFor(() => expect(api.find('POST', '/v1/items/itm_p/result')).toHaveLength(1))
    expect(api.find('POST', '/v1/items/itm_p/result')[0].body).toEqual({ status: 'completed' })
  })
})
