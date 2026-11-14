import { act, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ActivityFeed } from '@/components/activity/activity-feed'
import { SchedulePage } from '@/components/schedule/schedule-page'
import type { Post, StashEvent } from '@/lib/api'
import { qk, refreshEventLists, syncPostInCache } from '@/lib/queries'
import { dayKey, startOfWeek } from '@/lib/schedule'

import { collection, post, target } from './fixtures'
import { nav } from './navigation'
import { renders } from './render-count'
import { mockApi, renderWithProviders } from './utils'

/** Let the queries settle (events, schedules) before counting. */
async function settle() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 50))
  })
}

// Wednesday 21 Oct 2026, 10:30:10 local: a tick of the minute clock does not cross an hour.
const NOW = new Date(2026, 9, 21, 10, 30, 10)

async function renderWeek() {
  const out = renderWithProviders(<SchedulePage />, { mode: 'local' })

  await waitFor(() => expect(screen.getAllByTestId('schedule-entry')).toHaveLength(42))
  await settle()

  return out
}

describe('render budgets: the schedule week (42 posts)', () => {
  let posts: Post[]

  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true, toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'Date'] })
    vi.setSystemTime(NOW)
    nav.set('/schedule')
    // Six posts a day across the week, at 09:00 to 14:00, one channel each.
    const monday = startOfWeek(NOW)

    posts = Array.from({ length: 42 }, (_, i) => {
      const at = new Date(monday)

      at.setDate(at.getDate() + Math.floor(i / 6))
      at.setHours(9 + (i % 6), 15)

      return post({
        id: `pst_${i}`,
        content: `Post ${i}`,
        status: 'scheduled',
        publish_at: at.toISOString(),
        targets: [target()],
      })
    })
    mockApi({
      'GET /v1/posts': { data: posts, next_cursor: null },
      'GET /v1/collections': { data: [collection()] },
      'GET /v1/schedules': { data: [] },
      'GET /v1/events': { data: [] },
    })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('a tick of the clock renders the now line and the header time, not the grid', async () => {
    await renderWeek()
    renders.reset()
    await act(async () => {
      vi.advanceTimersByTime(60_000)
    })

    const grid = ['SchedulePage', 'TimeGrid', 'HourRow', 'HourCell', 'Slot', 'CellItems', 'Entry']

    expect(grid.map((n) => renders.of(n))).toEqual([0, 0, 0, 0, 0, 0, 0])
    expect(renders.of('NowLine') + renders.of('NowText')).toBeLessThanOrEqual(2)
  })

  it('a changed post count of a collection renders no card', async () => {
    const { client } = await renderWeek()

    renders.reset()
    act(() => client.setQueryData(qk.collections(), [collection({ post_count: 43 })]))
    await settle()
    expect(renders.of('Entry')).toBe(0)
    expect(renders.of('HourCell')).toBe(0)
  })

  it('one changed post renders its card and its cell only', async () => {
    const { client } = await renderWeek()
    const changed = posts[8]

    renders.reset()
    act(() => syncPostInCache(client, { ...changed, content: 'Post 8, edited' }))
    await settle()
    expect(renders.of('Entry')).toBe(1)
    expect(renders.of('HourCell')).toBeLessThanOrEqual(1)
    expect(renders.of('HourRow')).toBeLessThanOrEqual(1)
    expect(screen.getByTestId(`schedule-slot-${dayKey(new Date(changed.publish_at!))}-11`)).toHaveTextContent(
      'Post 8, edited',
    )
  })
})

function event(seq: number): StashEvent {
  return {
    id: `evt_${seq}`,
    seq,
    type: 'post.scheduled',
    collection_id: 'col_1',
    post_id: `pst_${seq}`,
    actor: { type: 'api_key', id: 'key_agent1', name: 'Writer agent' },
    data: {
      post: { id: `pst_${seq}`, content: `Post ${seq}`, status: 'scheduled' },
      collection: { id: 'col_1', name: 'Blog' },
    },
    created_at: '2026-10-14T09:30:00Z',
  }
}

describe('render budgets: the activity feed (50 events)', () => {
  beforeEach(() => nav.set('/activity'))

  it('a new live event renders one row, not the header', async () => {
    let events = Array.from({ length: 50 }, (_, i) => event(50 - i))

    mockApi({
      'GET /v1/events': () => ({ data: events.slice(0, 50), next_cursor: 'c2' }),
      'GET /v1/collections': { data: [collection()] },
    })
    const { client } = renderWithProviders(<ActivityFeed />, { mode: 'local' })

    await waitFor(() => expect(screen.getAllByTestId('audit-event')).toHaveLength(50))
    await settle()

    events = [event(51), ...events]
    renders.reset()
    await act(() => refreshEventLists(client))
    await waitFor(() => expect(screen.getByText('Post 51')).toBeInTheDocument())
    await settle()
    expect(renders.of('AuditEventRow')).toBeLessThanOrEqual(1)
    expect(renders.of('PageHeader')).toBe(0)
  })

  it('"Load older" renders the new page\'s rows only', async () => {
    const all = Array.from({ length: 150 }, (_, i) => event(150 - i))

    mockApi({
      'GET /v1/events': (call: { query: URLSearchParams }) => {
        const from = Number(call.query.get('cursor') || 0)

        return { data: all.slice(from, from + 50), next_cursor: from + 50 < all.length ? String(from + 50) : null }
      },
      'GET /v1/collections': { data: [collection()] },
    })
    renderWithProviders(<ActivityFeed />, { mode: 'local' })

    await waitFor(() => expect(screen.getAllByTestId('audit-event')).toHaveLength(50))
    act(() => screen.getByRole('button', { name: 'Load older events' }).click())
    await waitFor(() => expect(screen.getAllByTestId('audit-event')).toHaveLength(100))
    await settle()

    renders.reset()
    act(() => screen.getByRole('button', { name: 'Load older events' }).click())
    await waitFor(() => expect(screen.getAllByTestId('audit-event')).toHaveLength(150))
    await settle()
    expect(renders.of('AuditEventRow')).toBe(50)
    expect(renders.of('PageHeader')).toBe(0)
  })
})
