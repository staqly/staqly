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

  it('a post opened without ?collection= shows its own collection behind it, and links keep the view', async () => {
    nav.set('/posts/pst_9')
    const api = mockApi({
      'GET /v1/collections': { data: [collection(), collection({ id: 'col_2', name: 'Blog' })] },
      'GET /v1/posts/pst_9': post({ id: 'pst_9', collection_id: 'col_2' }),
      'GET /v1/channels': { data: [] },
      'GET /v1/tags': { data: [] },
      'GET /v1/events': { data: [] },
      'GET /v1/posts': { data: [post({ id: 'pst_9', collection_id: 'col_2' })], next_cursor: null },
    })

    renderWithProviders(<PostsWorkspace />, { mode: 'local' })
    expect(await screen.findByRole('heading', { level: 1, name: 'Blog' })).toBeInTheDocument()
    expect(await screen.findByTestId('post-link')).toHaveAttribute('href', '/posts/pst_9?collection=col_2')
    // Only the post's collection was listed, and no collection was fetched on its own.
    expect(api.find('GET', '/v1/posts').map((c) => c.query.get('collection_id'))).toEqual(['col_2'])
    expect(api.calls.filter((c) => c.path.startsWith('/v1/collections/'))).toHaveLength(0)
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
    // The filters sit behind the Filter button.
    await user.click(screen.getByTestId('filter-button'))
    await user.click(screen.getByRole('combobox', { name: 'Status filter' }))
    await user.click(await screen.findByRole('option', { name: 'Failed' }))
    // The filter lives in the URL (the page re-reads it); the list request carries it.
    await waitFor(() =>
      expect(nav.replace).toHaveBeenCalledWith(expect.stringContaining('status=failed'), expect.anything()),
    )
    expect(api.find('GET', '/v1/posts').length).toBeGreaterThan(0)
  })

  it('with one collection, no collections pane or strip: New collection sits in the header', async () => {
    nav.set('/posts')
    mockApi({
      'GET /v1/collections': { data: [collection()] },
      'GET /v1/channels': { data: [] },
      'GET /v1/tags': { data: [] },
      'GET /v1/events': { data: [] },
      'GET /v1/posts': { data: [], next_cursor: null },
    })

    renderWithProviders(<PostsWorkspace />, { mode: 'local' })
    expect(await screen.findByRole('heading', { level: 1, name: 'Instagram Avatar' })).toBeInTheDocument()
    expect(screen.queryByTestId('collections-pane')).not.toBeInTheDocument()
    expect(screen.queryByTestId('collections-strip')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /New collection/ })).toBeInTheDocument()
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
      // The footer's Publish now saves the edits first, then publishes.
      'PATCH /v1/posts/pst_1': failed,
      'POST /v1/posts/pst_1/schedule': { ...failed, status: 'scheduled' },
      'GET /v1/events': { data: [] },
      'GET /v1/posts/pst_1/comments': { data: [] },
    })
    const user = userEvent.setup()

    renderWithProviders(<PostSheet id="pst_1" onClose={() => {}} />, { mode: 'local' })
    // How each channel went is in the Delivery tab.
    await user.click(await screen.findByRole('tab', { name: /Delivery/ }))
    const list = await screen.findByRole('list', { name: 'Delivery per channel' })
    const rows = await within(list).findAllByTestId('post-target')

    expect(rows).toHaveLength(2)
    expect(within(rows[0]!).getByRole('link')).toHaveAttribute('href', 'https://mastodon.social/@brand/1')
    expect(within(rows[1]!).getByText('Rate limited')).toBeInTheDocument()
    expect(screen.getByTestId('post-content')).toHaveValue(failed.content)
    // Publish now lives in the footer (the header row only has the post's own actions).
    await user.click(screen.getByTestId('publish-now'))
    // It saves, then schedules for a few seconds from now (the Undo window).
    await waitFor(() => expect(api.find('POST', '/v1/posts/pst_1/schedule')).toHaveLength(1))
    await user.click(within(rows[1]!).getByRole('button', { name: 'Retry' }))
    await waitFor(() => expect(api.find('POST', '/v1/posts/pst_1/targets/ch_2/retry')).toHaveLength(1))
  })

  it('opens on the channel it was clicked for', async () => {
    mockApi({
      'GET /v1/posts/pst_1': post({
        id: 'pst_1',
        status: 'scheduled',
        publish_at: '2099-01-05T09:00:00Z',
        targets: [
          target(),
          target({ channel_id: 'ch_2', channel_name: 'Brand on Bluesky', provider: 'bluesky', status: 'failed' }),
        ],
      }),
      'GET /v1/collections': { data: [collection()] },
      'GET /v1/channels': channels,
      'GET /v1/providers': { data: [] },
      'GET /v1/events': { data: [] },
    })

    renderWithProviders(<PostSheet id="pst_1" channel="ch_2" onClose={() => {}} />, { mode: 'local' })
    const tab = await screen.findByRole('tab', { name: 'Text for Brand on Bluesky' })

    expect(tab).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('heading', { name: 'Text for Brand on Bluesky' })).toBeInTheDocument()
    // The delivery dot sits at the top left, clear of the platform badge at the bottom right.
    expect(within(tab).getByTestId('tab-channel-status')).toHaveClass('-top-0.5', '-left-0.5', 'bg-destructive')
    expect(
      within(screen.getByRole('tab', { name: 'Text for Brand on Mastodon' })).queryByTestId('tab-channel-status'),
    ).toBeNull()
    // A scheduled post: the main action saves it (keeping its time), Publish now is the other one.
    expect(screen.getByTestId('save-post')).toHaveTextContent('Save changes')
    expect(screen.getByTestId('publish-now')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Update' })).not.toBeInTheDocument()
  })

  it('a published post is read only, can be duplicated and cannot be deleted', async () => {
    const api = mockApi({
      'GET /v1/posts/pst_1': post({ id: 'pst_1', status: 'published', targets: [target({ status: 'published' })] }),
      'GET /v1/collections': { data: [collection()] },
      'GET /v1/channels': channels,
      'GET /v1/providers': { data: [] },
      'POST /v1/posts/pst_1/duplicate': post({ id: 'pst_2' }),
      'GET /v1/events': { data: [] },
      'GET /v1/posts/pst_1/comments': { data: [] },
    })
    const user = userEvent.setup()

    renderWithProviders(<PostSheet id="pst_1" onClose={() => {}} />, { mode: 'local' })
    expect(await screen.findByText(/can no longer be edited/)).toBeInTheDocument()
    expect(screen.getByTestId('post-content')).toHaveAttribute('readonly')
    expect(screen.queryByTestId('publish-now')).not.toBeInTheDocument()
    const actions = screen.getByTestId('post-actions-row')

    expect(within(actions).queryByRole('button', { name: 'Publish now' })).not.toBeInTheDocument()
    // It went out: history stays, so there is no Delete.
    expect(within(actions).queryByRole('button', { name: 'Delete' })).not.toBeInTheDocument()
    await user.click(within(actions).getByRole('button', { name: 'Duplicate' }))
    await waitFor(() => expect(api.find('POST', '/v1/posts/pst_1/duplicate')).toHaveLength(1))
  })

  it('a post published on some of its channels has no Delete either; a failed one keeps it', async () => {
    const partial = post({
      id: 'pst_1',
      status: 'failed',
      targets: [target({ status: 'published' }), target({ channel_id: 'ch_2', status: 'failed' })],
    })
    const api = mockApi({
      'GET /v1/posts/pst_1': partial,
      'GET /v1/posts/pst_2': post({ id: 'pst_2', status: 'failed', targets: [target({ status: 'failed' })] }),
      'DELETE /v1/posts/pst_2': { status: 204 },
      'GET /v1/collections': { data: [collection()] },
      'GET /v1/channels': channels,
      'GET /v1/providers': { data: [] },
      'GET /v1/events': { data: [] },
      'GET /v1/posts/pst_1/comments': { data: [] },
      'GET /v1/posts/pst_2/comments': { data: [] },
    })
    const user = userEvent.setup()
    const view = renderWithProviders(<PostSheet id="pst_1" onClose={() => {}} />, { mode: 'local' })
    const actions = await screen.findByTestId('post-actions-row')

    expect(within(actions).getByRole('button', { name: 'Duplicate' })).toBeInTheDocument()
    expect(within(actions).queryByRole('button', { name: 'Delete' })).not.toBeInTheDocument()

    view.unmount()
    renderWithProviders(<PostSheet id="pst_2" onClose={() => {}} />, { mode: 'local' })
    await user.click(within(await screen.findByTestId('post-actions-row')).getByRole('button', { name: 'Delete' }))
    await user.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(api.find('DELETE', '/v1/posts/pst_2')).toHaveLength(1))
  })
})
