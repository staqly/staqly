import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterAll, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

import { DateTimePicker } from '@/components/common/date-time-picker'
import { SchedulePage } from '@/components/schedule/schedule-page'
import { WorkspaceZone } from '@/components/shell/workspace-zone'
import { qk } from '@/lib/queries'
import { dayKey, minutesOfDay, moveToHour, nextFullHour, rangeOf, weekDays } from '@/lib/schedule'
import { displayZone, fromWall, setDisplayZone, wallOf } from '@/lib/zone'

import { post } from './fixtures'
import { nav } from './navigation'
import { makeClient, mockApi, renderWithProviders, sessionFixture } from './utils'

// The browser in New York, the workspace in Warsaw: nothing below may depend on the browser's zone.
const savedTz = process.env.TZ

beforeAll(() => {
  process.env.TZ = 'America/New_York'
})

beforeEach(() => setDisplayZone('Europe/Warsaw'))

afterAll(() => {
  process.env.TZ = savedTz
  setDisplayZone(null)
})

const iso = (d: Date) => d.toISOString()

describe('zone conversion', () => {
  it('runs in a browser zone other than the workspace', () => {
    expect(new Date(Date.UTC(2026, 0, 15)).getTimezoneOffset()).toBe(300) // New York, EST
    expect(displayZone()).toBe('Europe/Warsaw')
  })

  it('reads and builds wall times in the workspace zone', () => {
    const at = new Date('2026-10-19T22:30:00Z') // Tue 00:30 in Warsaw, Mon 18:30 in New York

    expect(wallOf(at)).toEqual({ year: 2026, month: 10, day: 20, hour: 0, minute: 30, second: 0, weekday: 2 })
    expect(iso(fromWall(2026, 10, 20, 0, 30))).toBe('2026-10-19T22:30:00.000Z')
    // Overflowing fields roll over like Date.UTC.
    expect(iso(fromWall(2026, 12, 31, 24))).toBe('2026-12-31T23:00:00.000Z')
    expect(iso(fromWall(2026, 1, 1, 9, 0, 0, 'America/New_York'))).toBe('2026-01-01T14:00:00.000Z')
  })

  it('resolves DST gaps forward and repeated hours to the earlier one', () => {
    // Warsaw: 2026-03-29 02:00 → 03:00; 2026-10-25 03:00 → 02:00.
    expect(iso(fromWall(2026, 3, 29, 2, 30))).toBe('2026-03-29T01:30:00.000Z') // = 03:30 CEST
    expect(iso(fromWall(2026, 10, 25, 2, 30))).toBe('2026-10-25T00:30:00.000Z') // the first 02:30 (CEST)
    expect(iso(fromWall(2026, 10, 25, 3, 30))).toBe('2026-10-25T02:30:00.000Z')
    // New York: 2026-03-08 02:00 → 03:00; 2026-11-01 02:00 → 01:00.
    const ny = 'America/New_York'

    expect(iso(fromWall(2026, 3, 8, 2, 30, 0, ny))).toBe('2026-03-08T07:30:00.000Z') // = 03:30 EDT
    expect(iso(fromWall(2026, 11, 1, 1, 30, 0, ny))).toBe('2026-11-01T05:30:00.000Z') // the first 01:30 (EDT)
  })

  it('round-trips every quarter hour of a year, in zones with whole, half and no DST', () => {
    const wrong: string[] = []

    for (const tz of ['Europe/Warsaw', 'America/New_York', 'Asia/Kolkata', 'Australia/Lord_Howe']) {
      for (let t = Date.UTC(2026, 0, 1); t < Date.UTC(2027, 0, 1); t += 15 * 60_000) {
        const w = wallOf(t, tz)
        const back = fromWall(w.year, w.month, w.day, w.hour, w.minute, w.second, tz).getTime()
        // Only the second pass through a repeated wall time maps back elsewhere: to the first one.
        const ok = back === t || (back < t && JSON.stringify(wallOf(back, tz)) === JSON.stringify(w))

        if (!ok) {
          wrong.push(`${tz} ${new Date(t).toISOString()}`)
        }
      }
    }

    expect(wrong).toEqual([])
  })

  it('a week across the DST change has 7 days, each starting at midnight, and hours stay right', () => {
    const days = weekDays(new Date('2026-10-22T12:00:00Z'))

    expect(days.map(dayKey)).toEqual([
      '2026-10-19',
      '2026-10-20',
      '2026-10-21',
      '2026-10-22',
      '2026-10-23',
      '2026-10-24',
      '2026-10-25',
    ])
    expect(days.every((d) => minutesOfDay(d) === 0)).toBe(true)
    expect(days[6].getTime() - days[5].getTime()).toBe(24 * 3_600_000)
    expect(rangeOf('week', days[0]).publish_before).toBe('2026-10-25T23:00:00.000Z') // Mon 26th, 00:00 CET
    // Noon UTC on the 25th is 13:00 in Warsaw after the change (14:00 the day before).
    expect(minutesOfDay(new Date('2026-10-25T12:00:00Z'))).toBe(13 * 60)
    expect(minutesOfDay(new Date('2026-10-24T12:00:00Z'))).toBe(14 * 60)
    expect(moveToHour('2026-10-24T12:15:00Z', '2026-10-25', 9)).toBe('2026-10-25T08:15:00.000Z')
    expect(iso(nextFullHour(Date.parse('2026-10-25T00:40:00Z')))).toBe('2026-10-25T01:00:00.000Z') // 02:00 CET
  })

  it('an unknown zone falls back to the browser', () => {
    setDisplayZone('Mars/Olympus')
    expect(displayZone()).toBe('America/New_York')
  })
})

describe('workspace zone in the dashboard', () => {
  beforeEach(() => nav.set('/schedule', 'view=month&at=2099-01-15'))

  it('the calendar puts a post on its workspace day and asks for the workspace month', async () => {
    // 23:30 UTC on the 15th: the 16th in Warsaw, still the 15th (18:30) in New York.
    const late = post({
      id: 'pst_late',
      content: 'Late night post',
      status: 'scheduled',
      publish_at: '2099-01-15T23:30:00Z',
    })
    const api = mockApi({
      'GET /v1/posts': { data: [late] },
      'GET /v1/collections': { data: [] },
      'GET /v1/schedules': { data: [] },
      'GET /v1/events': { data: [] },
    })
    const client = makeClient()
    const session = sessionFixture('local')

    client.setQueryData(qk.session, { ...session, workspace: { ...session.workspace, timezone: 'Europe/Warsaw' } })
    setDisplayZone(null) // WorkspaceZone sets it from the session
    renderWithProviders(
      <WorkspaceZone>
        <SchedulePage />
      </WorkspaceZone>,
      { client },
    )

    const day16 = await screen.findByTestId('schedule-day-2099-01-16')

    expect(await within(day16).findByText(/Late night post/)).toBeInTheDocument()
    expect(within(screen.getByTestId('schedule-day-2099-01-15')).queryByText(/Late night post/)).toBeNull()
    // January 2099 starts on a Thursday: the grid runs from Monday Dec 29, 00:00 in Warsaw.
    const q = api.find('GET', '/v1/posts')[0].query

    expect(q.get('publish_after')).toBe('2098-12-28T23:00:00.000Z')
    expect(q.get('publish_before')).toBe('2099-02-01T23:00:00.000Z') // after Sunday Feb 1
  })

  it('the date-time picker shows Warsaw hours and sends the UTC instant of the picked Warsaw time', async () => {
    const value = new Date('2099-01-15T08:00:00Z') // 09:00 in Warsaw, 03:00 in New York
    const onChange = vi.fn<(d: Date | null) => void>()
    const user = userEvent.setup()

    renderWithProviders(<DateTimePicker id="when" value={value} onChange={onChange} />)
    expect(screen.getByTestId('date-time-picker-time')).toHaveTextContent('09:00')
    await user.click(screen.getByTestId('date-time-picker-time'))
    const hours = await screen.findByRole('radiogroup', { name: 'Hour' })

    expect(within(hours).getByRole('radio', { name: '09' })).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByTestId('date-time-summary')).toHaveTextContent(/09:00 GMT\+1$/)
    await user.click(within(hours).getByRole('radio', { name: '14' }))
    expect(iso(onChange.mock.calls.at(-1)![0]!)).toBe('2099-01-15T13:00:00.000Z')
    // The day picked (same popover) keeps the Warsaw time.
    await user.click(await screen.findByRole('button', { name: 'Tuesday, January 20' }))
    expect(iso(onChange.mock.calls.at(-1)![0]!)).toBe('2099-01-20T08:00:00.000Z')
  })
})
