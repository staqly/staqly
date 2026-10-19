import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { beforeEach, describe, expect, it } from 'vitest'

import { NotificationsBell, NotificationsPopover, useNotifications } from '@/components/shell/notifications-bell'
import type { StashEvent } from '@/lib/api'
import { notificationFor } from '@/lib/notifications'

import { mockApi, renderWithProviders } from './utils'

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
  data: { ...postData, channel: { name: 'Brand on Mastodon' }, target: { external_url: 'https://x.test/p/1' } },
})
const failed = event({ type: 'post.failed', seq: 11, data: { ...postData, error: 'Rate limited' } })
const requested = event({ type: 'schedule.posts_requested', seq: 10, data: { schedule: { name: 'Mornings' } } })
const reconnect = event({
  type: 'channel.needs_reconnect',
  seq: 9,
  post_id: null,
  data: { channel: { name: 'News bot' }, error: 'Token revoked' },
})
const edited = event({ type: 'post.updated', seq: 8, data: postData })

/** The bell beside a menu: the list only anchored to it, opened by the button itself. */
function Anchored() {
  const notes = useNotifications()
  const [open, setOpen] = useState(false)

  return (
    <NotificationsPopover notes={notes} open={open} onOpenChange={setOpen} anchor>
      <button type="button" onClick={() => setOpen((o) => !o)}>
        Bell
      </button>
    </NotificationsPopover>
  )
}

beforeEach(() => localStorage.clear())

describe('notifications', () => {
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
  })

  it('opened beside a menu (anchored), Escape gives focus back to the bell, not to the page', async () => {
    mockApi({ 'GET /v1/events': { data: [published] } })
    const user = userEvent.setup()

    renderWithProviders(<Anchored />, { mode: 'local' })
    const bell = screen.getByRole('button', { name: 'Bell' })

    await user.click(bell)
    await screen.findByTestId('notifications-list')
    await user.keyboard('{Escape}')
    await waitFor(() => expect(screen.queryByTestId('notifications-list')).not.toBeInTheDocument())
    expect(bell).toHaveFocus()
  })

  it('says when there is nothing to show', async () => {
    mockApi({ 'GET /v1/events': { data: [edited] } })
    const user = userEvent.setup()

    renderWithProviders(<NotificationsBell />, { mode: 'local' })
    await user.click(await screen.findByRole('button', { name: 'Notifications' }))
    expect(
      await screen.findByText('Nothing new. Published, failed and scheduled runs show up here.'),
    ).toBeInTheDocument()
  })
})
