import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'

import { CollectionSettingsSheet } from '@/components/collections/collection-settings'
import { PostsWorkspace } from '@/components/posts/posts-workspace'
import { SchedulePage } from '@/components/schedule/schedule-page'

import { channel, collection, post } from './fixtures'
import { nav } from './navigation'
import { type Call, mockApi, renderWithProviders } from './utils'

beforeEach(() => {
  nav.reset()
  localStorage.clear()
})

const hidePastOf = (calls: Call[]) => calls.map((c) => c.query.get('hide_past'))

function setupPosts(posts: (call: Call) => unknown, counts: Record<string, number> = { draft: 1, published: 2 }) {
  nav.set('/posts', 'collection=col_1')

  return mockApi({
    'GET /v1/collections': { data: [collection({ status_counts: counts })] },
    'GET /v1/channels': { data: [channel()] },
    'GET /v1/tags': { data: [] },
    'GET /v1/events': { data: [] },
    'GET /v1/posts': posts,
  })
}

const twoLists = (call: Call) => ({
  data:
    call.query.get('status') === 'published'
      ? [post({ id: 'pst_2', content: 'Went out', status: 'published' })]
      : call.query.get('status') === 'failed'
        ? []
        : call.query.get('status') === 'draft'
          ? [post({ id: 'pst_4', content: 'Still a draft', status: 'draft' })]
          : [post({ id: 'pst_1', content: 'Upcoming one', status: 'scheduled' })],
  next_cursor: null,
})

describe('upcoming and published posts in the posts list', () => {
  it('shows upcoming, drafts and published, each with its count, without a switch', async () => {
    const api = setupPosts(twoLists, { draft: 1, scheduled: 2, failed: 1, published: 5 })

    renderWithProviders(<PostsWorkspace />, { mode: 'local' })
    const upcoming = await screen.findByTestId('posts-upcoming')
    const drafts = await screen.findByTestId('posts-drafts')
    const published = await screen.findByTestId('posts-published')

    // Failed posts have their own section above (none here); drafts, with no time yet, are not upcoming.
    expect(within(upcoming).getByRole('heading', { name: 'Upcoming 2' })).toBeInTheDocument()
    expect(within(drafts).getByRole('heading', { name: 'Drafts 1' })).toBeInTheDocument()
    expect(within(drafts).getByText('Still a draft')).toBeInTheDocument()
    expect(within(upcoming).queryByText('Still a draft')).not.toBeInTheDocument()
    expect(upcoming.compareDocumentPosition(drafts) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(screen.queryByTestId('posts-failed')).not.toBeInTheDocument()
    expect(within(upcoming).getByText('Upcoming one')).toBeInTheDocument()
    expect(within(published).getByRole('heading', { name: 'Published 5' })).toBeInTheDocument()
    expect(within(published).getByText('Went out')).toBeInTheDocument()
    expect(screen.queryByRole('switch', { name: 'Show past posts' })).not.toBeInTheDocument()

    const calls = api.find('GET', '/v1/posts')

    expect(calls.map((c) => [c.query.get('hide_past'), c.query.get('status'), c.query.get('sort')]).toSorted()).toEqual(
      [
        [null, 'draft', 'created_desc'],
        [null, 'failed', 'created_desc'],
        [null, 'published', 'publish_desc'],
        [null, 'scheduled,publishing', 'created_desc'],
      ],
    )
  })

  it('failed posts get a section of their own above Upcoming', async () => {
    setupPosts(
      (call) => ({
        data:
          call.query.get('status') === 'failed'
            ? [post({ id: 'pst_3', content: 'Went wrong', status: 'failed' })]
            : twoLists(call).data,
        next_cursor: null,
      }),
      { scheduled: 1, failed: 1, published: 1 },
    )

    renderWithProviders(<PostsWorkspace />, { mode: 'local' })
    const failed = await screen.findByTestId('posts-failed')
    const upcoming = await screen.findByTestId('posts-upcoming')

    expect(within(failed).getByRole('heading', { name: 'Needs attention 1' })).toBeInTheDocument()
    expect(within(failed).getByText('Went wrong')).toBeInTheDocument()
    expect(within(upcoming).queryByText('Went wrong')).not.toBeInTheDocument()
    expect(failed.compareDocumentPosition(upcoming) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('Show archived alone adds no empty filter row above the list', async () => {
    const api = setupPosts(twoLists)

    nav.set('/posts', 'collection=col_1&archived_posts=1')
    renderWithProviders(<PostsWorkspace />, { mode: 'local' })
    await screen.findByTestId('posts-published')
    expect(screen.queryByTestId('post-filters')).not.toBeInTheDocument()
    expect(api.find('GET', '/v1/posts').every((c) => c.query.get('include_archived') === 'true')).toBe(true)
  })

  it('a status filter shows only its section', async () => {
    const api = setupPosts(twoLists)

    nav.set('/posts', 'collection=col_1&status=published')
    renderWithProviders(<PostsWorkspace />, { mode: 'local' })
    await screen.findByTestId('posts-published')
    expect(screen.queryByTestId('posts-upcoming')).not.toBeInTheDocument()
    expect(hidePastOf(api.find('GET', '/v1/posts'))).toEqual([null])
  })

  it('drafts alone: no empty Upcoming above them', async () => {
    setupPosts((call) => (call.query.get('status') === 'draft' ? twoLists(call) : { data: [], next_cursor: null }))
    renderWithProviders(<PostsWorkspace />, { mode: 'local' })
    expect(within(await screen.findByTestId('posts-drafts')).getByText('Still a draft')).toBeInTheDocument()
    expect(screen.queryByTestId('posts-upcoming')).not.toBeInTheDocument()
  })

  it('nothing coming up says so above the published posts', async () => {
    setupPosts((call) => (call.query.get('status') === 'published' ? twoLists(call) : { data: [], next_cursor: null }))
    renderWithProviders(<PostsWorkspace />, { mode: 'local' })
    expect(await screen.findByText('Nothing coming up: scheduled posts show here.')).toBeInTheDocument()
    expect(within(screen.getByTestId('posts-published')).getByText('Went out')).toBeInTheDocument()
  })

  it('a collection with no posts says No posts yet, and no published section', async () => {
    setupPosts(() => ({ data: [], next_cursor: null }), { draft: 0, published: 0 })
    renderWithProviders(<PostsWorkspace />, { mode: 'local' })
    expect(await screen.findByText('No posts yet')).toBeInTheDocument()
    expect(screen.queryByTestId('posts-published')).not.toBeInTheDocument()
  })
})

describe('past posts on the calendar', () => {
  it('always shows them', async () => {
    const api = mockApi({
      'GET /v1/posts': { data: [] },
      'GET /v1/collections': { data: [] },
      'GET /v1/events': { data: [] },
    })

    renderWithProviders(<SchedulePage />, { mode: 'local' })
    await screen.findByTestId('schedule-empty')

    expect(hidePastOf(api.find('GET', '/v1/posts'))).toEqual([null])
    expect(screen.queryByRole('switch', { name: 'Show past posts' })).not.toBeInTheDocument()
  })
})

describe('deleting a collection', () => {
  it('says its posts move to Default (by its current name) and its schedules go', async () => {
    const col = collection({ post_count: 3 })

    mockApi({
      'GET /v1/collections': { data: [collection({ id: 'col_def', name: 'Inbox', is_default: true }), col] },
      'GET /v1/schedules': { data: [] },
    })
    const user = userEvent.setup()

    renderWithProviders(<CollectionSettingsSheet collection={col} open onOpenChange={() => {}} />, { mode: 'local' })
    await user.click(await screen.findByRole('button', { name: /Danger zone/ }))
    expect(
      await screen.findByText('Permanently delete this collection and its schedules. Its posts move to Inbox.'),
    ).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Delete collection' }))
    const dialog = await screen.findByRole('dialog', { name: /Delete/ })

    expect(dialog).toHaveTextContent('The collection and its schedules are deleted')
    expect(dialog).toHaveTextContent('Its 3 posts move to Inbox unchanged')
    expect(dialog).toHaveTextContent("Drafts join Inbox's drafts, so one of its schedules may publish them.")
    expect(dialog).not.toHaveTextContent('items')
  })
})
