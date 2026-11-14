import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'

import { NotificationsPage } from '@/components/notifications/notifications-page'
import { NotificationsBell } from '@/components/shell/notifications-bell'
import type { StashEvent } from '@/lib/api'
import { formatDateTimeIn } from '@/lib/format'
import { notificationFor, notificationsFor } from '@/lib/notifications'
import { qk } from '@/lib/queries'

import { makeClient, mockApi, renderWithProviders, sessionFixture, type Call } from './utils'

function event(over: Partial<StashEvent> & { type: string; seq: number }): StashEvent {
  return {
    id: `evt_${over.seq}`,
    collection_id: 'col_1',
    post_id: 'pst_1',
    actor: { type: 'system', id: 'jobs', name: 'Publisher' },
    created_at: new Date(Date.now() - over.seq * 60_000).toISOString(),
    data: {},
    ...over,
  } as StashEvent
}

const postData = { post: { id: 'pst_1', content: 'Spring sale post\nmore' } }
const published = event({
  type: 'post.target_published',
  seq: 12,
  data: {
    ...postData,
    channel_id: 'ch_1',
    channel: { id: 'ch_1', name: 'Brand', provider: 'mastodon', provider_name: 'Mastodon' },
    url: 'https://x.test/p/1',
  },
})
const failed = event({
  type: 'post.failed',
  seq: 11,
  post_id: 'pst_2',
  data: { post: { id: 'pst_2', content: 'Spring sale post' }, error: 'Rate limited' },
})
const requested = event({ type: 'schedule.posts_requested', seq: 10, data: { schedule: { name: 'Mornings' } } })
const reconnect = event({
  type: 'channel.needs_reconnect',
  seq: 9,
  post_id: null,
  data: { channel: { name: 'News bot' }, error: 'Token revoked' },
})
const edited = event({ type: 'post.updated', seq: 8, data: postData })

/** The page's rows across its day sections. */
const pageRows = () =>
  screen.getAllByTestId('notifications-page-list').flatMap((l) => within(l).getAllByRole('listitem'))

/** About an hour ago, `secondsAgo` before that. */
const at = (secondsAgo: number) => new Date(Date.now() - 3_600_000 - secondsAgo * 1000).toISOString()

const channels = [
  ['ch_a', 'Brand', 'bluesky', 'Bluesky'],
  ['ch_b', 'Brand', 'mastodon', 'Mastodon'],
  ['ch_c', 'News', 'linkedin', 'LinkedIn'],
  ['ch_d', 'News', 'threads', 'Threads'],
  ['ch_e', 'Shop', 'x', 'X'],
] as const

function target(i: number, seq: number, outcome: 'published' | 'failed' = 'published') {
  const [id, name, provider, provider_name] = channels[i]!

  return event({
    type: outcome === 'published' ? 'post.target_published' : 'post.target_failed',
    seq,
    channel_id: id,
    created_at: at(60 - seq),
    data: {
      post: { id: 'pst_1', content: 'xxxxxxxxx' },
      channel_id: id,
      channel: { id, name, provider, provider_name, avatar_url: '' },
      url: outcome === 'published' ? `https://social.test/${id}` : '',
      error: outcome === 'failed' ? 'Media too large' : '',
    },
  })
}

const summary = (seq: number, type = 'post.published') =>
  event({
    type,
    seq,
    created_at: at(60 - seq),
    data: { post: { id: 'pst_1', content: 'xxxxxxxxx' }, error: type === 'post.failed' ? 'A channel failed' : '' },
  })

beforeEach(() => localStorage.clear())

describe('notifications', () => {
  it.each([true, false])('fetches the bell once after live events arrive (nonempty: %s)', async (nonempty) => {
    const events = nonempty ? [published] : []
    const calls = mockApi({ 'GET /v1/events': { data: events } })

    renderWithProviders(<NotificationsBell />, { mode: 'local' })
    await waitFor(() => expect(calls.find('GET', '/v1/events').some((c) => c.query.has('types'))).toBe(true))
    // Both queries have settled before counting: the live sequence must not trigger a second bell request.
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: events.length ? 'Notifications, 1 unread' : 'Notifications' }),
      ).toBeVisible(),
    )
    expect(calls.find('GET', '/v1/events').filter((c) => c.query.has('types'))).toHaveLength(1)
  })

  it('turns the events worth telling into sentences with a place to go', () => {
    expect(notificationFor(published)).toMatchObject({
      tone: 'success',
      text: '“Spring sale post” is out on Brand on Mastodon',
      href: '/posts/pst_1',
      link: 'https://x.test/p/1',
    })
    expect(notificationFor(failed)?.text).toBe('Publishing “Spring sale post” failed: Rate limited')
    expect(notificationFor(requested)).toMatchObject({ tone: 'info', href: '/posts?collection=col_1' })
    expect(notificationFor(reconnect)).toMatchObject({ tone: 'danger', href: '/channels' })
    expect(notificationFor(edited)).toBeNull()
  })

  it('counts the unread ones, lists them newest first, and Mark all as read clears the count', async () => {
    mockApi({ 'GET /v1/events': { data: [published, failed, requested, reconnect, edited] } })
    const user = userEvent.setup()

    renderWithProviders(<NotificationsBell />, { mode: 'local' })
    const bell = await screen.findByRole('button', { name: 'Notifications, 4 unread' })

    expect(bell).toHaveAttribute('data-unread', '4')
    await user.click(bell)
    const list = await screen.findByTestId('notifications-list')
    const rows = within(list).getAllByRole('listitem')

    expect(rows).toHaveLength(4)
    expect(rows[0]).toHaveTextContent('“Spring sale post” is out on Brand on Mastodon')
    expect(within(rows[0]!).getByRole('link')).toHaveAttribute('href', 'https://x.test/p/1')
    expect(rows[3]).toHaveTextContent('News bot needs to be reconnected: Token revoked')
    expect(within(list).getAllByRole('img', { name: 'New' })).toHaveLength(4)
    await user.click(screen.getByRole('button', { name: 'Mark all as read' }))
    expect(within(list).queryAllByRole('img', { name: 'New' })).toHaveLength(0)
    expect(screen.getByRole('button', { name: 'Notifications' })).toHaveAttribute('data-unread', '0')
    const key = Object.keys(localStorage).find((k) => k.startsWith('staqly.notifications.seen.'))

    expect(key && localStorage.getItem(key)).toBe('12')
    await user.keyboard('{Escape}')
    await waitFor(() => expect(screen.queryByTestId('notifications-list')).not.toBeInTheDocument())
    // Read ones leave the bell; All notifications leads to the rest.
    await user.click(screen.getByRole('button', { name: 'Notifications' }))
    expect(await screen.findByText(/You're all caught up/)).toBeInTheDocument()
    expect(screen.getByTestId('notifications-all')).toHaveTextContent('All notifications')
  })

  it('says when there is nothing to show', async () => {
    mockApi({ 'GET /v1/events': { data: [edited] } })
    const user = userEvent.setup()

    renderWithProviders(<NotificationsBell />, { mode: 'local' })
    await user.click(await screen.findByRole('button', { name: 'Notifications' }))
    expect(
      await screen.findByText("You're all caught up. Published, failed and scheduled runs show up here."),
    ).toBeInTheDocument()
  })

  it('reports an initial notifications error and can retry instead of claiming the list is empty', async () => {
    let broken = true

    mockApi({
      'GET /v1/events': () =>
        broken ? { status: 500, body: { error: { code: 'internal', message: 'Offline' } } } : { data: [published] },
    })
    const user = userEvent.setup()

    renderWithProviders(<NotificationsPage />, { mode: 'local' })
    expect(await screen.findByRole('alert')).toHaveTextContent('Could not load data')
    expect(screen.queryByText(/No notifications yet/)).not.toBeInTheDocument()
    broken = false
    await user.click(screen.getByRole('button', { name: 'Try again' }))
    await screen.findByTestId('notifications-page-list')
    expect(pageRows()).toHaveLength(1)
  })

  it('stops automatic pagination on error, keeps loaded notifications, and retries the failed page on demand', async () => {
    let broken = true
    let olderRequests = 0

    mockApi({
      'GET /v1/events': ({ query }: Call) => {
        if (!query.get('cursor')) {
          return { data: [reconnect], next_cursor: 'older' }
        }

        olderRequests++

        // A third automatic request would silently recover, hiding a retry loop from a mere final-list assertion.
        return broken && olderRequests < 3
          ? { status: 500, body: { error: { code: 'internal', message: 'Offline' } } }
          : { data: [published] }
      },
    })
    const user = userEvent.setup()

    renderWithProviders(<NotificationsPage />, { mode: 'local' })
    expect(await screen.findByRole('alert')).toHaveTextContent('Could not load more')
    expect(olderRequests).toBe(1)
    expect(pageRows()).toHaveLength(1)
    broken = false
    await user.click(screen.getByRole('button', { name: 'Try again' }))
    await waitFor(() => expect(pageRows()).toHaveLength(2))
    expect(olderRequests).toBe(2)
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('the page shows 15 notifications at a time, whatever their day, and loads 15 more', async () => {
    const many = Array.from({ length: 20 }, (_, i) =>
      event({ type: 'channel.needs_reconnect', seq: 100 - i, post_id: null, data: { channel: { name: `Bot ${i}` } } }),
    )

    mockApi({ 'GET /v1/events': { data: many } })
    const user = userEvent.setup()

    renderWithProviders(<NotificationsPage />, { mode: 'local' })
    await screen.findAllByTestId('notifications-page-list')
    expect(pageRows()).toHaveLength(15)
    await user.click(screen.getByRole('button', { name: 'Load more' }))
    expect(pageRows()).toHaveLength(20)
    expect(screen.queryByRole('button', { name: 'Load more' })).not.toBeInTheDocument()
  })

  it('the page lists every notification across days with its details and where to go from it', async () => {
    // Fixed dates cross a day boundary in every time zone, regardless of when the suite runs.
    const api = mockApi({
      'GET /v1/events': {
        data: [
          { ...published, created_at: '2026-10-30T12:00:00Z' },
          { ...failed, created_at: '2026-10-30T11:59:00Z' },
          { ...reconnect, created_at: '2026-10-29T12:00:00Z' },
        ],
      },
    })

    renderWithProviders(<NotificationsPage />, { mode: 'local' })
    const lists = await screen.findAllByTestId('notifications-page-list')
    const rows = pageRows()

    expect(lists).toHaveLength(2)

    expect(rows).toHaveLength(3)
    // Asked only for the events that make notifications.
    expect(api.find('GET', '/v1/events').some((c) => c.query.getAll('types').includes('post.published'))).toBe(true)
    const first = within(rows[0]!)

    expect(first.getByRole('link', { name: '“Spring sale post” is out on Brand on Mastodon' })).toHaveAttribute(
      'href',
      '/posts/pst_1',
    )
    expect(first.getByText('evt_12')).toBeInTheDocument()
    expect(first.getByText('Brand on Mastodon')).toBeInTheDocument()
    expect(first.getByRole('link', { name: /Open on the platform/ })).toHaveAttribute('href', 'https://x.test/p/1')
    expect(first.getByRole('link', { name: /In Activity/ })).toHaveAttribute('href', '/activity?post=pst_1')
    // A channel event has no post: Activity by its type.
    expect(within(rows[2]!).getByRole('link', { name: /In Activity/ })).toHaveAttribute(
      'href',
      '/activity?type=channel.needs_reconnect',
    )
  })

  describe('one publish to several channels', () => {
    it('stacks the channels of one publish into one entry with the latest time', () => {
      const evs = [summary(26), target(4, 25), target(3, 24), target(2, 23), target(1, 22), target(0, 21)]
      const list = notificationsFor(evs)

      expect(list).toHaveLength(1)
      expect(list[0]).toMatchObject({
        text: '“xxxxxxxxx” is out on 5 channels',
        tone: 'success',
        seq: 26,
        at: evs[0]!.created_at,
        href: '/posts/pst_1',
      })
      expect(list[0]!.link).toBeUndefined()
      expect(list[0]!.channels?.map((c) => c.url)).toEqual(
        ['ch_e', 'ch_d', 'ch_c', 'ch_b', 'ch_a'].map((id) => `https://social.test/${id}`),
      )
    })

    it('says plainly when some channels failed, and lists the failed ones first', () => {
      const evs = [
        summary(26, 'post.failed'),
        target(4, 25, 'failed'),
        target(3, 24),
        target(2, 23),
        target(1, 22),
        target(0, 21),
      ]
      const [n] = notificationsFor(evs)

      expect(n).toMatchObject({ text: '“xxxxxxxxx” is out on 4 channels, failed on 1', tone: 'danger' })
      expect(n!.channels![0]).toMatchObject({ id: 'ch_e', status: 'failed', error: 'Media too large' })
      expect(notificationsFor([target(1, 22, 'failed'), target(0, 21, 'failed')])[0]).toMatchObject({
        text: '“xxxxxxxxx” failed on 2 channels',
        tone: 'danger',
      })
    })

    it('never hides the post failing behind channels that went out', () => {
      const [n] = notificationsFor([summary(26, 'post.failed'), target(1, 22), target(0, 21)])

      expect(n).toMatchObject({
        text: '“xxxxxxxxx” is out on 2 channels, but publishing failed: A channel failed',
        tone: 'danger',
      })
    })

    it('a retry keeps the newest outcome of a channel', () => {
      const [n] = notificationsFor([target(0, 23), target(0, 22, 'failed'), target(1, 21)])

      expect(n).toMatchObject({ text: '“xxxxxxxxx” is out on 2 channels', tone: 'success' })
    })

    it('names the channel when there is only one', () => {
      const [n] = notificationsFor([summary(22), target(0, 21)])

      expect(n).toMatchObject({ text: '“xxxxxxxxx” is out on Brand on Bluesky', link: 'https://social.test/ch_a' })
      expect(notificationsFor([target(4, 21, 'failed')])[0]?.text).toBe(
        '“xxxxxxxxx” did not go out on Shop on X: Media too large',
      )
      // Events from before the API named the channel: the post's target has its name.
      const old = event({
        type: 'post.target_published',
        seq: 5,
        data: {
          post: {
            content: 'Old',
            targets: [
              { channel_id: 'ch_z', channel_name: 'Brand', provider: 'bluesky', external_url: 'https://o.test/1' },
            ],
          },
          channel_id: 'ch_z',
        },
      })

      expect(notificationFor(old)).toMatchObject({ text: '“Old” is out on Brand', link: 'https://o.test/1' })
    })

    it('keeps publishes apart: other posts, and the same post again later', () => {
      const other = event({
        type: 'post.published',
        seq: 30,
        post_id: 'pst_9',
        created_at: at(40),
        data: { post: { content: 'Other' } },
      })
      const later = { ...target(0, 40), created_at: new Date(Date.now() - 600_000).toISOString() }
      const list = notificationsFor([later, other, target(1, 22), requested, target(0, 21)])

      expect(list.map((n) => n.text)).toEqual([
        '“xxxxxxxxx” is out on Brand on Bluesky',
        '“Other” was published',
        '“xxxxxxxxx” is out on 2 channels',
        '“Mornings” found nothing to publish and asked for new posts',
      ])
    })

    it('the bell shows one stacked entry: unread, expandable, failures in view, a readable time', async () => {
      mockApi({
        'GET /v1/events': {
          data: [
            summary(26, 'post.failed'),
            target(4, 25, 'failed'),
            target(3, 24),
            target(2, 23),
            target(1, 22),
            target(0, 21),
          ],
        },
      })
      const client = makeClient()
      const session = sessionFixture('local')

      client.setQueryData(qk.setup, {
        initialized: true,
        mode: 'local',
        signup_open: false,
        setup_token_required: false,
      })
      client.setQueryData(qk.session, { ...session, workspace: { ...session.workspace, timezone: 'Asia/Tokyo' } })
      const user = userEvent.setup()

      renderWithProviders(<NotificationsBell />, { client })
      const bell = await screen.findByRole('button', { name: 'Notifications, 1 unread' })

      await user.click(bell)
      const rows = within(await screen.findByTestId('notifications-list')).getAllByRole('listitem')

      expect(rows).toHaveLength(1)
      const row = within(rows[0]!)

      expect(row.getByRole('button', { name: '“xxxxxxxxx” is out on 4 channels, failed on 1' })).toBeInTheDocument()
      expect(row.getByText('Failed on Shop on X')).toBeInTheDocument()
      const time = rows[0]!.querySelector('time')!

      expect(time.title).toBe(formatDateTimeIn(time.dateTime, 'Asia/Tokyo'))
      expect(time.title).not.toMatch(/\d{4}-\d{2}-\d{2}T/)
      expect(time.title).toMatch(/GMT\+9/)
      await user.click(row.getByRole('button', { name: /Show 5 channels/ }))
      const items = within(screen.getByTestId('publish-channels')).getAllByRole('listitem')

      expect(items).toHaveLength(5)
      expect(items[0]).toHaveTextContent(/Shop on X\s*Failed\s*Media too large/)
      expect(within(items[1]!).getByRole('link', { name: /Open on the platform/ })).toHaveAttribute(
        'href',
        'https://social.test/ch_d',
      )
      await user.click(screen.getByRole('button', { name: 'Mark all as read' }))
      expect(screen.getByRole('button', { name: 'Notifications' })).toHaveAttribute('data-unread', '0')
    })

    it('the page shows the stack once, with its channels', async () => {
      mockApi({ 'GET /v1/events': { data: [summary(26), target(2, 23), target(1, 22), target(0, 21)] } })
      const user = userEvent.setup()

      renderWithProviders(<NotificationsPage />, { mode: 'local' })
      const rows = within(await screen.findByTestId('notifications-page-list')).getAllByRole('listitem')

      expect(rows).toHaveLength(1)
      expect(within(rows[0]!).getByRole('link', { name: '“xxxxxxxxx” is out on 3 channels' })).toHaveAttribute(
        'href',
        '/posts/pst_1',
      )
      expect(within(rows[0]!).getByText('evt_26')).toBeInTheDocument()
      await user.click(within(rows[0]!).getByRole('button', { name: /Show 3 channels/ }))
      expect(
        within(screen.getByTestId('publish-channels')).getAllByRole('link', { name: /Open on the platform/ }),
      ).toHaveLength(3)
    })
  })
})
