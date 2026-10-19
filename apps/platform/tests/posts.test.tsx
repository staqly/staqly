import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { PostSheet } from '@/components/posts/post-sheet'
import { PostsWorkspace } from '@/components/posts/posts-workspace'

import { channel, collection, post, target } from './fixtures'
import { nav } from './navigation'
import { mockApi, renderWithProviders } from './utils'

beforeEach(() => nav.reset())

describe('posts workspace', () => {
  it('lists the collections on the left and the open collection posts with their state', async () => {
    nav.set('/posts', 'collection=col_1')
    mockApi({
      'GET /v1/collections': { data: [collection(), collection({ id: 'col_2', name: 'Blog' })] },
      'GET /v1/collections/col_1': collection(),
      'GET /v1/channels': { data: [channel()] },
      'GET /v1/tags': { data: [] },
      'GET /v1/events': { data: [] },
      'GET /v1/posts': {
        data: [
          post({ id: 'pst_1', status: 'scheduled', publish_at: '2026-02-01T09:00:00Z' }),
          post({ id: 'pst_2', content: 'Second draft', status: 'draft', targets: [] }),
        ],
        next_cursor: null,
      },
    })
    renderWithProviders(<PostsWorkspace />, { mode: 'local' })

    expect(await screen.findByRole('heading', { level: 1, name: 'Instagram Avatar' })).toBeInTheDocument()
    const rows = await screen.findAllByTestId('post-row')

    expect(rows).toHaveLength(2)
    expect(within(rows[0]!).getByText('Scheduled')).toBeInTheDocument()
    expect(within(rows[1]!).getByText('Draft')).toBeInTheDocument()
    expect(within(rows[1]!).getByText('Second draft')).toBeInTheDocument()
    expect(screen.getByTestId('collections-list')).toHaveTextContent('Blog')
    expect(screen.getByTestId('new-post')).toBeEnabled()
  })

  it('filters by status through the URL', async () => {
    nav.set('/posts', 'collection=col_1')
    const api = mockApi({
      'GET /v1/collections': { data: [collection()] },
      'GET /v1/collections/col_1': collection(),
      'GET /v1/channels': { data: [] },
      'GET /v1/tags': { data: [] },
      'GET /v1/events': { data: [] },
      'GET /v1/posts': { data: [post()], next_cursor: null },
    })
    const user = userEvent.setup()

    renderWithProviders(<PostsWorkspace />, { mode: 'local' })
    await screen.findAllByTestId('post-row')
    await user.click(screen.getByRole('combobox', { name: 'Status filter' }))
    await user.click(await screen.findByRole('option', { name: 'Failed' }))
    // The filter lives in the URL (the page re-reads it); the list request carries it.
    await waitFor(() =>
      expect(nav.replace).toHaveBeenCalledWith(expect.stringContaining('status=failed'), expect.anything()),
    )
    expect(api.find('GET', '/v1/posts').length).toBeGreaterThan(0)
  })
})

// ProseMirror does not run in jsdom: the post sheet's editor is a plain textarea here.
vi.mock('@/components/posts/post-editor', () => ({
  PostEditor: (p: { id: string; value: string; editable?: boolean; testId?: string }) => (
    <textarea id={p.id} value={p.value} readOnly={p.editable === false} data-testid={p.testId} onChange={() => {}} />
  ),
}))

const channels = {
  data: [channel(), channel({ id: 'ch_2', name: 'Brand on Bluesky', provider: 'bluesky' })],
}

describe('post sheet', () => {
  it('is the composer: each channel shows its state, link and error; Retry and Publish now', async () => {
    const failed = post({
      id: 'pst_1',
      status: 'failed',
      targets: [
        target({ status: 'published', external_url: 'https://mastodon.social/@brand/1' }),
        target({
          channel_id: 'ch_2',
          channel_name: 'Brand on Bluesky',
          provider: 'bluesky',
          status: 'failed',
          error: 'Rate limited',
        }),
      ],
    })
    const api = mockApi({
      'GET /v1/posts/pst_1': failed,
      'GET /v1/collections': { data: [collection()] },
      'GET /v1/channels': channels,
      'GET /v1/providers': { data: [] },
      'POST /v1/posts/pst_1/targets/ch_2/retry': { ...failed, status: 'publishing' },
      'POST /v1/posts/pst_1/publish': { ...failed, status: 'publishing' },
      'GET /v1/events': { data: [] },
    })
    const user = userEvent.setup()

    renderWithProviders(<PostSheet id="pst_1" onClose={() => {}} />, { mode: 'local' })
    // How each channel went sits under the text (the post sheet has no preview column).
    const list = await screen.findByRole('list', { name: 'Channels' })
    const rows = await within(list).findAllByTestId('post-target')

    expect(rows).toHaveLength(2)
    expect(within(rows[0]!).getByRole('link')).toHaveAttribute('href', 'https://mastodon.social/@brand/1')
    expect(within(rows[1]!).getByText('Rate limited')).toBeInTheDocument()
    expect(screen.getByTestId('post-content')).toHaveValue(failed.content)
    // Publish now first: once the post is publishing, More no longer offers it.
    await user.click(screen.getByRole('button', { name: 'More actions' }))
    await user.click(screen.getByRole('menuitem', { name: 'Publish now' }))
    await waitFor(() => expect(api.find('POST', '/v1/posts/pst_1/publish')).toHaveLength(1))
    await user.click(within(rows[1]!).getByRole('button', { name: 'Retry' }))
    await waitFor(() => expect(api.find('POST', '/v1/posts/pst_1/targets/ch_2/retry')).toHaveLength(1))
  })

  it('opens on the channel it was clicked for', async () => {
    mockApi({
      'GET /v1/posts/pst_1': post({
        id: 'pst_1',
        status: 'scheduled',
        publish_at: '2099-01-05T09:00:00Z',
        targets: [target(), target({ channel_id: 'ch_2', channel_name: 'Brand on Bluesky', provider: 'bluesky' })],
      }),
      'GET /v1/collections': { data: [collection()] },
      'GET /v1/channels': channels,
      'GET /v1/providers': { data: [] },
      'GET /v1/events': { data: [] },
    })

    renderWithProviders(<PostSheet id="pst_1" channel="ch_2" onClose={() => {}} />, { mode: 'local' })
    expect(await screen.findByRole('tab', { name: 'Text for Brand on Bluesky' })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    expect(screen.getByRole('button', { name: 'Update' })).toBeInTheDocument()
  })

  it('a published post is read only and can be duplicated and deleted', async () => {
    const api = mockApi({
      'GET /v1/posts/pst_1': post({ id: 'pst_1', status: 'published', targets: [target({ status: 'published' })] }),
      'GET /v1/collections': { data: [collection()] },
      'GET /v1/channels': channels,
      'GET /v1/providers': { data: [] },
      'POST /v1/posts/pst_1/duplicate': post({ id: 'pst_2' }),
      'DELETE /v1/posts/pst_1': { status: 204 },
      'GET /v1/events': { data: [] },
    })
    const user = userEvent.setup()

    renderWithProviders(<PostSheet id="pst_1" onClose={() => {}} />, { mode: 'local' })
    expect(await screen.findByText(/can no longer be edited/)).toBeInTheDocument()
    expect(screen.getByTestId('post-content')).toHaveAttribute('readonly')
    expect(screen.queryByTestId('publish-now')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'More actions' }))
    expect(screen.queryByRole('menuitem', { name: 'Publish now' })).not.toBeInTheDocument()
    await user.click(screen.getByRole('menuitem', { name: 'Duplicate' }))
    await waitFor(() => expect(api.find('POST', '/v1/posts/pst_1/duplicate')).toHaveLength(1))
    await user.click(screen.getByRole('button', { name: 'More actions' }))
    await user.click(screen.getByRole('menuitem', { name: 'Delete' }))
    await user.click(await screen.findByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(api.find('DELETE', '/v1/posts/pst_1')).toHaveLength(1))
  })
})
