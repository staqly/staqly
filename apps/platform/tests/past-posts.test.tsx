import { screen, waitFor } from '@testing-library/react'
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

function setupPosts(posts: (call: Call) => unknown, counts = { draft: 1, published: 2 }) {
  nav.set('/posts', 'collection=col_1')

  return mockApi({
    'GET /v1/collections': { data: [collection({ status_counts: counts })] },
    'GET /v1/channels': { data: [channel()] },
    'GET /v1/tags': { data: [] },
    'GET /v1/events': { data: [] },
    'GET /v1/posts': posts,
  })
}

describe('past posts in the posts list', () => {
  it('are hidden by default; the switch brings them back and is remembered in this browser', async () => {
    const api = setupPosts((call) => ({
      data:
        call.query.get('hide_past') === 'true'
          ? [post({ id: 'pst_1', content: 'Upcoming one', status: 'scheduled' })]
          : [
              post({ id: 'pst_1', content: 'Upcoming one', status: 'scheduled' }),
              post({ id: 'pst_2', content: 'Went out', status: 'published' }),
            ],
      next_cursor: null,
    }))
    const user = userEvent.setup()

    renderWithProviders(<PostsWorkspace />, { mode: 'local' })
    expect(await screen.findAllByTestId('post-row')).toHaveLength(1)
    expect(hidePastOf(api.find('GET', '/v1/posts'))).toEqual(['true'])

    const toggle = screen.getByRole('switch', { name: 'Show past posts' })

    expect(toggle).toHaveAttribute('aria-checked', 'false')
    await user.click(toggle)
    await waitFor(() => expect(screen.getAllByTestId('post-row')).toHaveLength(2))
    expect(hidePastOf(api.find('GET', '/v1/posts')).at(-1)).toBeNull()
    expect(localStorage.getItem('staqly.posts.showPast')).toBe('1')
  })

  it('a status filter for published posts asks for them even while the switch is off', async () => {
    const api = setupPosts(() => ({ data: [post({ status: 'published' })], next_cursor: null }))

    nav.set('/posts', 'collection=col_1&status=published')
    renderWithProviders(<PostsWorkspace />, { mode: 'local' })
    await screen.findAllByTestId('post-row')
    expect(hidePastOf(api.find('GET', '/v1/posts'))).toEqual([null])
  })

  it('an empty list says the past posts are hidden, with a way to show them', async () => {
    const api = setupPosts((call) => ({
      data:
        call.query.get('hide_past') === 'true' ? [] : [post({ id: 'pst_2', content: 'Went out', status: 'published' })],
      next_cursor: null,
    }))
    const user = userEvent.setup()

    renderWithProviders(<PostsWorkspace />, { mode: 'local' })
    expect(await screen.findByText('No upcoming posts')).toBeInTheDocument()
    expect(screen.getByText('Posts that went out are hidden.')).toBeInTheDocument()
    expect(screen.queryByText('No posts yet')).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Show past posts' }))
    expect(await screen.findByText('Went out')).toBeInTheDocument()
    expect(hidePastOf(api.find('GET', '/v1/posts')).at(-1)).toBeNull()
  })

  it('a collection with nothing that went out still says No posts yet', async () => {
    setupPosts(() => ({ data: [], next_cursor: null }), { draft: 0, published: 0 })
    renderWithProviders(<PostsWorkspace />, { mode: 'local' })
    expect(await screen.findByText('No posts yet')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Show past posts' })).not.toBeInTheDocument()
  })
})

describe('past posts on the calendar', () => {
  it('always shows them, whatever the posts list hides', async () => {
    localStorage.removeItem('staqly.posts.showPast')
    const api = mockApi({
      'GET /v1/posts': { data: [] },
      'GET /v1/collections': { data: [] },
      'GET /v1/events': { data: [] },
    })

    renderWithProviders(<SchedulePage />, { mode: 'local' })
    const empty = await screen.findByTestId('schedule-empty')

    expect(hidePastOf(api.find('GET', '/v1/posts'))).toEqual([null])
    expect(empty).not.toHaveTextContent('Past posts are hidden.')
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
