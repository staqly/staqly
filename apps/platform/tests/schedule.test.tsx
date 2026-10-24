import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'

import { SchedulePage } from '@/components/schedule/schedule-page'
import { dayKey, moveToDay, moveToHour, rangeOf, scheduleState, startOfWeek } from '@/lib/schedule'

import { collection, post, target } from './fixtures'
import { nav } from './navigation'
import { type Call, mockApi, renderWithProviders } from './utils'

/** jsdom has no DataTransfer: the smallest stand-in the calendar's drag and drop needs. */
function dataTransfer() {
  const data = new Map<string, string>()

  return {
    setData: (t: string, v: string) => data.set(t, v),
    getData: (t: string) => data.get(t) ?? '',
    get types() {
      return [...data.keys()]
    },
    effectAllowed: 'all',
    dropEffect: 'none',
  }
}

function at(dayOffset: number, hour: number) {
  const d = new Date()

  d.setDate(d.getDate() + dayOffset)
  d.setHours(hour, 30, 0, 0)

  return d
}

describe('schedule helpers', () => {
  it('tells what a post on the calendar is doing', () => {
    const future = new Date(Date.now() + 3_600_000).toISOString()
    const past = new Date(Date.now() - 3_600_000).toISOString()

    expect(scheduleState(post({ status: 'scheduled', publish_at: future }))).toBe('scheduled')
    expect(scheduleState(post({ status: 'publishing', publish_at: past }))).toBe('publishing')
    expect(scheduleState(post({ status: 'published', publish_at: past }))).toBe('published')
    expect(scheduleState(post({ status: 'failed', publish_at: past }))).toBe('failed')
    expect(scheduleState(post({ status: 'draft' }))).toBeNull()
  })

  it('moves a time to another day or hour in local time, keeping the minutes', () => {
    const iso = new Date(2026, 9, 18, 14, 30).toISOString()

    expect(new Date(moveToDay(iso, '2026-10-21')).getTime()).toBe(new Date(2026, 9, 21, 14, 30).getTime())
    expect(new Date(moveToHour(iso, '2026-10-21', 9)).getTime()).toBe(new Date(2026, 9, 21, 9, 30).getTime())
    expect(startOfWeek(new Date(2026, 9, 18)).getDay()).toBe(1) // Monday
    expect(rangeOf('week', new Date(2026, 9, 18)).days).toHaveLength(7)
    expect(rangeOf('month', new Date(2026, 9, 18)).days.length % 7).toBe(0)
    expect(rangeOf('day', new Date(2026, 9, 18, 15)).days).toHaveLength(1)
    expect(rangeOf('day', new Date(2026, 9, 18, 15)).days[0].getHours()).toBe(0)
  })
})

describe('schedule page', () => {
  beforeEach(() => nav.set('/schedule'))

  it('places posts in the week grid by local day and hour, with their state and channels, and counts the scheduled ones', async () => {
    // Inside the current week: today's hour 9 and tomorrow (or yesterday on a Sunday) at 15.
    const today = new Date()
    const other = today.getDay() === 0 ? -1 : 1
    const scheduled = post({
      id: 'pst_s',
      content: 'Spring sale post',
      status: 'scheduled',
      publish_at: at(0, 9).toISOString(),
      targets: [
        target(),
        target({ channel_id: 'ch_2', channel_name: 'Brand on Bluesky', provider: 'bluesky', status: 'failed' }),
      ],
    })
    const failed = post({
      id: 'pst_f',
      content: 'Teaser',
      status: 'failed',
      publish_at: at(other, 15).toISOString(),
      targets: [target({ status: 'failed', error: 'Rate limited' })],
    })
    const api = mockApi({
      'GET /v1/posts': (call: Call) => {
        expect(call.query.get('sort')).toBe('publish_asc')
        expect(call.query.get('publish_after')).toBeTruthy()
        expect(call.query.get('publish_before')).toBeTruthy()

        return { data: [scheduled, failed] }
      },
      'GET /v1/collections': { data: [collection({ color: '#ec4899' })] },
      'GET /v1/events': { data: [] },
    })

    renderWithProviders(<SchedulePage />, { mode: 'local' })
    expect(await screen.findByRole('heading', { name: 'Schedule' })).toBeInTheDocument()
    expect(await screen.findByTestId('schedule-count')).toHaveTextContent('1 post scheduled')

    const slotA = screen.getByTestId(`schedule-slot-${dayKey(today)}-9`)
    const slotB = screen.getByTestId(`schedule-slot-${dayKey(at(other, 15))}-15`)

    // One card per channel, each with that channel's own state. A cell shows the first; "+1 more" opens the rest.
    expect(within(slotA).getAllByTestId('schedule-entry')).toHaveLength(1)
    fireEvent.click(within(slotA).getByRole('button', { name: '+1 more' }))
    const cards = within(within(slotA).getByRole('group', { name: /Everything at/ })).getAllByTestId('schedule-entry')

    expect(cards).toHaveLength(2)
    expect(cards.map((c) => c.dataset.channelId)).toEqual(['ch_1', 'ch_2'])
    expect(cards.map((c) => c.dataset.state)).toEqual(['scheduled', 'failed'])
    expect(cards[1]).toHaveAccessibleName(/Spring sale post on Brand on Bluesky: Failed/)
    expect(within(slotB).getByTestId('schedule-entry')).toHaveAttribute('data-state', 'failed')
    // One line per post: the state is its icon, named in the card's accessible name.
    expect(within(slotB).getByTestId('schedule-entry')).toHaveAccessibleName(/: Failed/)
    expect(api.find('GET', '/v1/posts')).toHaveLength(1)
  })

  it('drags an entry to another hour: the post is rescheduled with the same minutes, shown at once', async () => {
    const today = new Date()
    const p1 = post({
      id: 'pst_s',
      content: 'Spring sale post',
      status: 'scheduled',
      publish_at: at(0, 9).toISOString(),
    })
    const done = post({ id: 'pst_d', content: 'Posted', status: 'published', publish_at: at(0, 11).toISOString() })
    const api = mockApi({
      'GET /v1/posts': { data: [p1, done] },
      'GET /v1/collections': { data: [collection()] },
      'GET /v1/events': { data: [] },
      'POST /v1/posts/pst_s/schedule': (call: Call) => ({
        ...p1,
        publish_at: (call.body as { publish_at: string }).publish_at,
        version: 2,
      }),
    })

    renderWithProviders(<SchedulePage />, { mode: 'local' })
    const from = await screen.findByTestId(`schedule-slot-${dayKey(today)}-9`)
    const entry = within(from).getByTestId('schedule-entry')

    expect(entry).toHaveAttribute('draggable', 'true')
    // A published post is not draggable.
    expect(
      within(screen.getByTestId(`schedule-slot-${dayKey(today)}-11`)).getByTestId('schedule-entry'),
    ).toHaveAttribute('draggable', 'false')

    // A slot still ahead of now: two hours from now today, or tomorrow morning late in the evening (the week
    // grid holds tomorrow unless today is Sunday; then yesterday is the other day, so use today's last hour).
    // Never 9, the entry's own slot: a drop there changes nothing.
    const hourNow = new Date().getHours()
    const ahead = hourNow + 2 === 9 ? 10 : hourNow + 2
    const dropAt =
      ahead < 24 ? { day: dayKey(today), hour: ahead } : { day: dayKey(at(today.getDay() === 0 ? 0 : 1, 9)), hour: 23 }
    const dt = dataTransfer()
    const to = screen.getByTestId(`schedule-slot-${dropAt.day}-${dropAt.hour}`)

    fireEvent.dragStart(entry, { dataTransfer: dt })
    fireEvent.dragOver(to, { dataTransfer: dt })
    fireEvent.drop(to, { dataTransfer: dt })

    await waitFor(() => expect(api.find('POST', '/v1/posts/pst_s/schedule')).toHaveLength(1))
    expect(api.find('POST', '/v1/posts/pst_s/schedule')[0].body).toEqual({
      publish_at: moveToHour(p1.publish_at!, dropAt.day, dropAt.hour),
    })
    expect(within(to).getByText('Spring sale post')).toBeInTheDocument()
    expect(await screen.findByText(/goes out/)).toBeInTheDocument()
  })

  it('month view: a drop on a day keeps the time of day; the view and month live in the URL', async () => {
    nav.set('/schedule', 'view=month')
    const today = new Date()
    const p1 = post({
      id: 'pst_s',
      content: 'Spring sale post',
      status: 'scheduled',
      publish_at: at(0, 9).toISOString(),
    })
    const api = mockApi({
      'GET /v1/posts': { data: [p1] },
      'GET /v1/collections': { data: [collection()] },
      'GET /v1/events': { data: [] },
      'POST /v1/posts/pst_s/schedule': (call: Call) => ({
        ...p1,
        publish_at: (call.body as { publish_at: string }).publish_at,
        version: 2,
      }),
    })

    renderWithProviders(<SchedulePage />, { mode: 'local' })
    const day = await screen.findByTestId(`schedule-day-${dayKey(today)}`)

    expect(within(day).getByText('Spring sale post')).toBeInTheDocument()
    // The last day of the grid is a Sunday after today (or today itself).
    const days = rangeOf('month', today).days
    const dropAt = days[days.length - 1]
    const dt = dataTransfer()

    fireEvent.dragStart(within(day).getByTestId('schedule-entry'), { dataTransfer: dt })
    fireEvent.drop(screen.getByTestId(`schedule-day-${dayKey(dropAt)}`), { dataTransfer: dt })
    await waitFor(() => expect(api.find('POST', '/v1/posts/pst_s/schedule')).toHaveLength(1))
    expect(api.find('POST', '/v1/posts/pst_s/schedule')[0].body).toMatchObject({
      publish_at: moveToDay(p1.publish_at!, dayKey(dropAt)),
    })

    await userEvent.setup().click(screen.getByRole('button', { name: 'Next month' }))
    expect(nav.replace).toHaveBeenLastCalledWith(expect.stringMatching(/view=month/), expect.anything())
    expect(nav.replace).toHaveBeenLastCalledWith(expect.stringMatching(/at=\d{4}-\d{2}-01/), expect.anything())
  })

  it('explains how to schedule when the range is empty, without identifiers', async () => {
    mockApi({ 'GET /v1/posts': { data: [] }, 'GET /v1/collections': { data: [] }, 'GET /v1/events': { data: [] } })
    renderWithProviders(<SchedulePage />, { mode: 'local' })
    expect(await screen.findByText('Nothing scheduled this week')).toBeInTheDocument()
    expect(screen.getByText(/give a collection a schedule/)).toBeInTheDocument()
    expect(screen.queryByText(/publish_at/)).not.toBeInTheDocument()
  })

  it('day view shows one column; the list layout groups the range by day', async () => {
    nav.set('/schedule', 'view=day')
    const today = new Date()
    const p1 = post({
      id: 'pst_s',
      content: 'Spring sale post',
      status: 'scheduled',
      publish_at: at(0, 9).toISOString(),
    })

    mockApi({
      'GET /v1/posts': { data: [p1] },
      'GET /v1/collections': { data: [collection({ name: 'Launches' })] },
      'GET /v1/events': { data: [] },
    })
    renderWithProviders(<SchedulePage />, { mode: 'local' })
    const slot = await screen.findByTestId(`schedule-slot-${dayKey(today)}-9`)

    expect(within(slot).getByText('Spring sale post')).toBeInTheDocument()
    expect(screen.queryByTestId(`schedule-slot-${dayKey(at(1, 9))}-9`)).not.toBeInTheDocument()
    expect(screen.getByRole('radio', { name: 'Day' })).toHaveAttribute('aria-checked', 'true')
    // The now line is decorative; the time is text in the header.
    expect(screen.getByTestId('now-line')).toHaveAttribute('aria-hidden', 'true')
    expect(screen.getByText(/^now/)).toBeInTheDocument()

    await userEvent.setup().click(screen.getByRole('radio', { name: 'List' }))
    expect(nav.replace).toHaveBeenLastCalledWith(expect.stringMatching(/list=1/), expect.anything())
    nav.set('/schedule', 'view=day&list=1')
    renderWithProviders(<SchedulePage />, { mode: 'local' })
    const list = await screen.findByTestId('schedule-list')

    expect(within(list).getByRole('heading', { level: 2 })).toHaveTextContent(
      today.toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long' }),
    )
    expect(within(list).getAllByTestId('schedule-entry')[0]).toHaveAccessibleName(/Launches/)
  })

  it('on a phone: a week is a list, a month stays a calendar even when it is empty', async () => {
    const original = window.matchMedia

    window.matchMedia = (query: string) =>
      ({
        matches: query.startsWith('(max-width'),
        media: query,
        addEventListener() {},
        removeEventListener() {},
      }) as never

    try {
      mockApi({ 'GET /v1/posts': { data: [] }, 'GET /v1/collections': { data: [] }, 'GET /v1/events': { data: [] } })
      renderWithProviders(<SchedulePage />, { mode: 'local' })
      expect(await screen.findByTestId('schedule-list')).toBeInTheDocument()

      nav.set('/schedule', 'view=month')
      renderWithProviders(<SchedulePage />, { mode: 'local' })
      expect(await screen.findByTestId('schedule-grid')).toBeInTheDocument()
      expect(screen.getByText('Nothing scheduled this month')).toBeInTheDocument()
    } finally {
      window.matchMedia = original
    }
  })
})
