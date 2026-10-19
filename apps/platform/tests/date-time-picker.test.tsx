import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { DateTimePicker } from '@/components/common/date-time-picker'
import { dayKey, presets } from '@/lib/schedule'

const dayButton = (d: Date) =>
  screen.getByRole('button', {
    name: d.toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long' }),
  })

describe('date and time picker', () => {
  it('hours and minutes are two columns; a click sets the time at once and the arrow keys move within a column', async () => {
    const value = new Date()

    value.setDate(value.getDate() + 2)
    value.setHours(9, 0, 0, 0)
    const onChange = vi.fn<(d: Date | null) => void>()
    const user = userEvent.setup()

    render(<DateTimePicker id="when" value={value} onChange={onChange} />)
    await user.click(screen.getByTestId('date-time-picker-time'))
    const hours = await screen.findByRole('radiogroup', { name: 'Hour' })
    const minutes = screen.getByRole('radiogroup', { name: 'Minute' })

    expect(within(hours).getByRole('radio', { name: '09' })).toHaveAttribute('aria-checked', 'true')
    expect(within(hours).getAllByRole('radio')).toHaveLength(24)
    expect(within(minutes).getAllByRole('radio')).toHaveLength(12)
    await user.click(within(hours).getByRole('radio', { name: '14' }))
    expect((onChange.mock.calls.at(-1)![0] as Date).getHours()).toBe(14)
    await user.click(within(minutes).getByRole('radio', { name: '35' }))
    expect((onChange.mock.calls.at(-1)![0] as Date).getMinutes()).toBe(35)
    within(hours).getByRole('radio', { name: '09' }).focus()
    await user.keyboard('{ArrowDown}')
    expect((onChange.mock.calls.at(-1)![0] as Date).getHours()).toBe(10)
    // The footer says it in words, with the time zone.
    expect(screen.getByTestId('date-time-summary')).toHaveTextContent(/, 09:00$/)
  })

  it('reads the chosen time on the trigger and opens a calendar where past days cannot be picked', async () => {
    const value = new Date()

    value.setDate(value.getDate() + 3)
    value.setHours(9, 0, 0, 0)
    const onChange = vi.fn<(d: Date | null) => void>()
    const user = userEvent.setup()

    render(<DateTimePicker id="when" value={value} onChange={onChange} />)
    const trigger = screen.getByTestId('date-time-picker')

    // The split trigger: the day on one half, the time on the other.
    expect(trigger).toHaveTextContent(
      value.toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short', year: 'numeric' }),
    )
    expect(screen.getByTestId('date-time-picker-time')).toHaveTextContent('09:00')
    await user.click(trigger)
    const grid = await screen.findByRole('grid')

    // Last month is all in the past: its first day cannot be picked.
    await user.click(screen.getByRole('button', { name: 'Previous month' }))
    const firstOfLastMonth = new Date(value.getFullYear(), value.getMonth() - 1, 1)

    expect(dayButton(firstOfLastMonth)).toBeDisabled()
    await user.click(screen.getByRole('button', { name: 'Next month' }))

    const todayCell = within(grid).getByRole('button', { name: dayButton(new Date()).getAttribute('aria-label')! })

    expect(todayCell).toHaveAttribute('aria-current', 'date')
    expect(todayCell).toBeEnabled()
    expect(within(grid).getByRole('gridcell', { selected: true })).toHaveTextContent(String(value.getDate()))

    // Picking a day keeps the time of day.
    const other = new Date(value)

    other.setDate(other.getDate() + 1)
    await user.click(dayButton(other))
    expect(onChange).toHaveBeenLastCalledWith(expect.any(Date))
    const picked = onChange.mock.calls.at(-1)![0] as Date

    expect(dayKey(picked)).toBe(dayKey(other))
    expect(picked.getHours()).toBe(9)
  })

  it('moves between days with the arrow keys and picks with Enter', async () => {
    const value = new Date()

    value.setDate(value.getDate() + 7)
    value.setHours(14, 30, 0, 0)
    const onChange = vi.fn<(d: Date | null) => void>()
    const user = userEvent.setup()

    render(<DateTimePicker id="when" value={value} onChange={onChange} />)
    await user.click(screen.getByTestId('date-time-picker'))
    const start = await screen.findByRole('button', { name: dayButton(value).getAttribute('aria-label')! })

    expect(start).toHaveAttribute('tabindex', '0')
    start.focus()
    await user.keyboard('{ArrowRight}')
    const next = new Date(value)

    next.setDate(next.getDate() + 1)
    expect(dayButton(next)).toHaveFocus()
    await user.keyboard('{ArrowDown}')
    next.setDate(next.getDate() + 7)
    // A week down may turn the month: the new grid renders first.
    await waitFor(() => expect(dayButton(next)).toHaveFocus())
    await user.keyboard('{Enter}')
    const picked = onChange.mock.calls.at(-1)![0] as Date

    expect(dayKey(picked)).toBe(dayKey(next))
    expect(picked.getHours()).toBe(14)
    expect(picked.getMinutes()).toBe(30)
  })

  it('presets produce the right times', async () => {
    const now = Date.now()
    const list = presets(now)
    const tomorrow = new Date(now)

    tomorrow.setDate(tomorrow.getDate() + 1)
    tomorrow.setHours(9, 0, 0, 0)
    expect(list[1].at.getTime()).toBe(tomorrow.getTime())
    expect(list[2].at.getDay()).toBe(1)
    expect(list[2].at.getHours()).toBe(9)
    expect(list[2].at.getTime()).toBeGreaterThan(now)
    expect(list[0].at.getTime()).toBeGreaterThanOrEqual(now + 3_600_000)
    expect(list[0].at.getMinutes() % 5).toBe(0)

    const onChange = vi.fn<(d: Date | null) => void>()
    const user = userEvent.setup()

    render(<DateTimePicker id="when" value={null} onChange={onChange} allowClear />)
    await user.click(screen.getByTestId('date-time-picker'))
    await user.click(await screen.findByRole('button', { name: 'Tomorrow 09:00' }))
    expect((onChange.mock.calls.at(-1)![0] as Date).getTime()).toBe(tomorrow.getTime())
  })
})
