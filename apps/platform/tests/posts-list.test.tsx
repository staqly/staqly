import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'

import { PostsWorkspace } from '@/components/posts/posts-workspace'

import { channel, collection, post, target } from './fixtures'
import { nav } from './navigation'
import { mockApi, renderWithProviders } from './utils'

beforeEach(() => nav.reset())

const three = [
  post({ id: 'pst_1', content: 'First post', status: 'scheduled', publish_at: '2026-02-01T09:00:00Z' }),
  post({ id: 'pst_2', content: 'Second post', open_comments: 2 }),
  post({ id: 'pst_3', content: 'Third post' }),
]

function setup(extra: Record<string, unknown> = {}) {
  nav.set('/posts', 'collection=col_1')

  return mockApi({
    'GET /v1/collections': { data: [collection()] },
    'GET /v1/collections/col_1': collection(),
    'GET /v1/channels': { data: [channel()] },
    'GET /v1/tags': { data: [] },
    'GET /v1/events': { data: [] },
    'GET /v1/posts': { data: three, next_cursor: null },
    ...extra,
  })
}

describe('posts list', () => {
  it('selects posts (Shift+click for a range) and deletes them from the bar after a confirmation', async () => {
    const api = setup({ 'POST /v1/posts/delete': { deleted: ['pst_1', 'pst_2', 'pst_3'], missing: [], kept: [] } })
    const user = userEvent.setup()

    renderWithProviders(<PostsWorkspace />, { mode: 'local' })
    await screen.findAllByTestId('post-row')
    expect(screen.queryByRole('toolbar', { name: 'Selected posts' })).not.toBeInTheDocument()

    await user.click(screen.getByRole('checkbox', { name: 'Select First post' }))
    await user.keyboard('{Shift>}')
    await user.click(screen.getByRole('checkbox', { name: 'Select Third post' }))
    await user.keyboard('{/Shift}')
    const bar = screen.getByRole('toolbar', { name: 'Selected posts' })

    expect(bar).toHaveTextContent('3 selected')
    expect(screen.getByRole('checkbox', { name: 'Select Second post' })).toHaveAttribute('aria-checked', 'true')

    await user.click(within(bar).getByRole('button', { name: 'Delete' }))
    const confirm = await screen.findByRole('alertdialog')

    expect(confirm).toHaveTextContent('Delete 3 posts?')
    await user.click(within(confirm).getByRole('button', { name: 'Delete 3 posts' }))
    await waitFor(() => expect(api.find('POST', '/v1/posts/delete')).toHaveLength(1))
    expect(api.find('POST', '/v1/posts/delete')[0]!.body).toEqual({ ids: ['pst_1', 'pst_2', 'pst_3'] })
    await waitFor(() => expect(screen.queryByRole('toolbar', { name: 'Selected posts' })).not.toBeInTheDocument())
  })

  it('posts that went out have no row Delete, and the bar deletes only the others', async () => {
    const out = [
      post({ id: 'pst_1', content: 'Out everywhere', status: 'published', targets: [target({ status: 'published' })] }),
      post({
        id: 'pst_2',
        content: 'Out on one channel',
        status: 'failed',
        targets: [target({ status: 'published' }), target({ channel_id: 'ch_2', status: 'failed' })],
      }),
      post({ id: 'pst_3', content: 'Never out' }),
    ]
    const api = setup({
      'GET /v1/posts': { data: out, next_cursor: null },
      'POST /v1/posts/delete': { deleted: ['pst_3'], missing: [], kept: [] },
    })
    const user = userEvent.setup()

    renderWithProviders(<PostsWorkspace />, { mode: 'local' })
    const rows = await screen.findAllByTestId('post-row')

    expect(within(rows[0]!).queryByTestId('post-row-delete')).not.toBeInTheDocument()
    expect(within(rows[1]!).queryByTestId('post-row-delete')).not.toBeInTheDocument()
    expect(within(rows[2]!).getByTestId('post-row-delete')).toBeInTheDocument()

    // Only what went out is selected: Delete says why it does nothing.
    await user.click(screen.getByRole('checkbox', { name: 'Select Out everywhere' }))
    const bar = screen.getByRole('toolbar', { name: 'Selected posts' })
    const del = within(bar).getByRole('button', { name: 'Delete' })

    expect(del).toHaveAttribute('aria-disabled', 'true')
    await user.click(del)
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()

    await user.click(screen.getByRole('checkbox', { name: 'Select Out on one channel' }))
    await user.click(screen.getByRole('checkbox', { name: 'Select Never out' }))
    await user.click(within(bar).getByRole('button', { name: 'Delete' }))
    const confirm = await screen.findByRole('alertdialog')

    expect(confirm).toHaveTextContent('Delete 1 post?')
    expect(confirm).toHaveTextContent('2 posts that went out stay as history.')
    await user.click(within(confirm).getByRole('button', { name: 'Delete 1 post' }))
    await waitFor(() => expect(api.find('POST', '/v1/posts/delete')).toHaveLength(1))
    expect(api.find('POST', '/v1/posts/delete')[0]!.body).toEqual({ ids: ['pst_3'] })
  })

  it('Escape clears the selection; Cancel schedule only takes scheduled posts off', async () => {
    const api = setup({ 'POST /v1/posts/pst_1/cancel': { ...three[0], status: 'draft' } })
    const user = userEvent.setup()

    renderWithProviders(<PostsWorkspace />, { mode: 'local' })
    await screen.findAllByTestId('post-row')
    await user.click(screen.getByRole('checkbox', { name: 'Select Second post' }))
    expect(within(screen.getByRole('toolbar')).getByRole('button', { name: 'Cancel schedule' })).toBeDisabled()
    await user.keyboard('{Escape}')
    expect(screen.queryByRole('toolbar', { name: 'Selected posts' })).not.toBeInTheDocument()

    await user.click(screen.getByRole('checkbox', { name: 'Select First post' }))
    await user.click(screen.getByRole('checkbox', { name: 'Select Second post' }))
    await user.click(within(screen.getByRole('toolbar')).getByRole('button', { name: 'Cancel schedule' }))
    await waitFor(() => expect(api.find('POST', '/v1/posts/pst_1/cancel')).toHaveLength(1))
    expect(api.find('POST', '/v1/posts/pst_2/cancel')).toHaveLength(0)
  })

  it('each row has its actions: delete one after a confirmation, the menu offers Cancel schedule', async () => {
    const api = setup({ 'DELETE /v1/posts/pst_3': { status: 204 } })
    const user = userEvent.setup()

    renderWithProviders(<PostsWorkspace />, { mode: 'local' })
    const rows = await screen.findAllByTestId('post-row')

    expect(within(rows[1]!).getByRole('button', { name: '2 open comments' })).toBeInTheDocument()
    expect(within(rows[0]!).getByText(/by Content agent/)).toBeInTheDocument()

    await user.click(within(rows[0]!).getByRole('button', { name: /More actions for First post/ }))
    expect(await screen.findByRole('menuitem', { name: 'Cancel schedule' })).toBeInTheDocument()
    await user.keyboard('{Escape}')

    // The menu mounts on first use; from the keyboard it opens on its first item and closes back to its button.
    within(rows[1]!)
      .getByRole('button', { name: /More actions for Second post/ })
      .focus()
    await user.keyboard('{Enter}')
    await waitFor(() => expect(screen.getAllByRole('menuitem')[0]).toHaveFocus())
    await user.keyboard('{Escape}')
    await waitFor(() =>
      expect(within(rows[1]!).getByRole('button', { name: /More actions for Second post/ })).toHaveFocus(),
    )

    await user.click(within(rows[2]!).getByRole('button', { name: 'Delete' }))
    const confirm = await screen.findByRole('alertdialog')

    await user.click(within(confirm).getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(api.find('DELETE', '/v1/posts/pst_3')).toHaveLength(1))
  })
})
