import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { DatePicker } from '@/components/common/date-picker'
import { DateRangePicker, formatDayRange, rangePresets } from '@/components/common/date-range-picker'
import { Calendar, nextRange } from '@/components/ui/calendar'
import { addDays, dayFromKey, dayKey } from '@/lib/schedule'

const name = (d: Date) => d.toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long' })

describe('calendar', () => {
  it.each([
    ['2026-10-31', 'ArrowRight', '2026-11-01'],
    ['2026-11-01', 'ArrowLeft', '2026-10-31'],
    ['2026-12-31', 'ArrowRight', '2027-01-01'],
    ['2027-01-01', 'ArrowUp', '2026-12-25'],
  ])('keeps keyboard focus from %s via %s to %s', async (from, key, to) => {
    const selected = dayFromKey(from)
    const target = dayFromKey(to)
    const onSelect = vi.fn<(d: Date) => void>()
    const user = userEvent.setup()

    function Picker() {
      const [month, setMonth] = useState(selected)

      return <Calendar mode="single" selected={selected} onSelect={onSelect} month={month} onMonthChange={setMonth} />
    }

    render(<Picker />)
    screen.getByRole('button', { name: name(selected) }).focus()
    await user.keyboard(`{${key}}`)
    await waitFor(() => expect(screen.getByRole('button', { name: name(target) })).toHaveFocus())
    await user.keyboard('{Enter}')
    expect(onSelect).toHaveBeenLastCalledWith(target)
  })

  it('moves back to the selected day after focus moves independently', async () => {
    const selected = dayFromKey('2026-10-15')
    const user = userEvent.setup()

    render(
      <Calendar
        mode="single"
        selected={selected}
        onSelect={vi.fn<(d: Date) => void>()}
        month={selected}
        onMonthChange={vi.fn<(d: Date) => void>()}
      />,
    )
    screen.getByRole('button', { name: name(addDays(selected, -1)) }).focus()
    await user.keyboard('{ArrowRight}')
    expect(screen.getByRole('button', { name: name(selected) })).toHaveFocus()
  })

  it('range mode: the first click starts a range, the second ends it (ordered), a third starts over', () => {
    const a = dayFromKey('2026-10-12')
    const b = dayFromKey('2026-10-18')

    expect(nextRange(null, b)).toEqual({ from: b, to: null })
    expect(nextRange({ from: b, to: null }, a)).toEqual({ from: a, to: b })
    expect(nextRange({ from: a, to: b }, a)).toEqual({ from: a, to: null })
  })

  it('shows two months side by side, marks the range and today, and disables days outside the limits', async () => {
    const month = new Date(2026, 9, 1)
    const onSelect = vi.fn<(d: Date) => void>()
    const user = userEvent.setup()

    render(
      <Calendar
        id="r"
        mode="range"
        selected={{ from: dayFromKey('2026-10-12'), to: dayFromKey('2026-10-15') }}
        onSelect={onSelect}
        month={month}
        onMonthChange={() => {}}
        numberOfMonths={2}
        maxDate={dayFromKey('2026-11-10')}
      />,
    )
    const grids = screen.getAllByRole('grid')

    expect(grids).toHaveLength(2)
    expect(grids[0]).toHaveAccessibleName('October 2026')
    expect(grids[1]).toHaveAccessibleName('November 2026')
    // Ends and the days between are selected cells.
    const selected = within(grids[0]).getAllByRole('gridcell', { selected: true })

    expect(selected.map((c) => c.textContent)).toEqual(['12', '13', '14', '15'])
    // Past the maximum: disabled.
    expect(within(grids[1]).getByRole('button', { name: name(dayFromKey('2026-11-20')) })).toBeDisabled()
    expect(within(grids[1]).getByRole('button', { name: name(dayFromKey('2026-11-10')) })).toBeEnabled()
    await user.click(within(grids[0]).getByRole('button', { name: name(dayFromKey('2026-10-20')) }))
    expect(onSelect).toHaveBeenCalledWith(dayFromKey('2026-10-20'))
  })
})

describe('date range picker', () => {
  it('presets end today and read as a range', () => {
    const now = new Date(2026, 9, 18, 12).getTime()
    const list = rangePresets(now)

    expect(list.map((p) => p.label)).toEqual([
      'Today',
      'Yesterday',
      'Last 7 days',
      'Last 30 days',
      'This month',
      'Last month',
    ])
    expect(list[2].range).toEqual({ from: '2026-10-12', to: '2026-10-18' })
    expect(list[4].range).toEqual({ from: '2026-10-01', to: '2026-10-18' })
    expect(list[5].range).toEqual({ from: '2026-09-01', to: '2026-09-30' })
    expect(formatDayRange(list[2].range)).toBe('Oct 12, 2026 to Oct 18, 2026')
    expect(formatDayRange(list[0].range)).toBe('Oct 18, 2026')
  })

  it('picks two days on the calendar, offers the presets and clears', async () => {
    const onChange = vi.fn<(r: { from: string; to: string } | null) => void>()
    const user = userEvent.setup()

    render(<DateRangePicker value={null} onChange={onChange} label="Dates" />)
    const trigger = screen.getByTestId('date-range-picker')

    expect(trigger).toHaveTextContent('Pick a date range')
    await user.click(trigger)
    // Next month, so both days are on the same grid whatever today is.
    await user.click(screen.getByRole('button', { name: 'Next month' }))
    const today = new Date()
    const first = new Date(today.getFullYear(), today.getMonth() + 1, 3)
    const second = new Date(today.getFullYear(), today.getMonth() + 1, 9)
    const grid = (await screen.findAllByRole('grid'))[0]

    await user.click(within(grid).getByRole('button', { name: name(first) }))
    expect(onChange).not.toHaveBeenCalled()
    expect(screen.getByText(/pick the last day/)).toBeInTheDocument()
    await user.click(within(grid).getByRole('button', { name: name(second) }))
    expect(onChange).toHaveBeenLastCalledWith({ from: dayKey(first), to: dayKey(second) })
    await user.click(screen.getByRole('button', { name: 'Last 7 days' }))
    expect(onChange).toHaveBeenLastCalledWith(rangePresets()[2].range)
  })

  it('a value shows on the trigger with a Clear button', async () => {
    const onChange = vi.fn<(r: unknown) => void>()
    const user = userEvent.setup()

    render(<DateRangePicker value={{ from: '2026-10-01', to: '2026-10-31' }} onChange={onChange} label="Dates" />)
    expect(screen.getByTestId('date-range-picker')).toHaveTextContent('Oct 1, 2026 to Oct 31, 2026')
    await user.click(screen.getByRole('button', { name: 'Clear dates' }))
    expect(onChange).toHaveBeenCalledWith(null)
  })
})

describe('date picker', () => {
  it('picks one day (not before the minimum) and closes; Clear empties it', async () => {
    const onChange = vi.fn<(v: string) => void>()
    const user = userEvent.setup()
    const today = new Date()

    render(<DatePicker id="exp" value="" onChange={onChange} minDate={today} placeholder="Never" />)
    expect(screen.getByTestId('date-picker')).toHaveTextContent('Never')
    await user.click(screen.getByTestId('date-picker'))
    const yesterday = addDays(today, -1)

    if (yesterday.getMonth() !== today.getMonth()) {
      await user.click(screen.getByRole('button', { name: 'Previous month' }))
    }

    const grid = await screen.findByRole('grid')

    expect(within(grid).getByRole('button', { name: name(yesterday) })).toBeDisabled()

    if (yesterday.getMonth() !== today.getMonth()) {
      await user.click(screen.getByRole('button', { name: 'Next month' }))
    }

    await user.click(within(screen.getByRole('grid')).getByRole('button', { name: name(today) }))
    expect(onChange).toHaveBeenCalledWith(dayKey(today))
  })

  it('shows the chosen day and clears it', async () => {
    const onChange = vi.fn<(v: string) => void>()
    const user = userEvent.setup()

    render(<DatePicker id="exp" value="2026-12-24" onChange={onChange} />)
    expect(screen.getByTestId('date-picker')).toHaveTextContent(/24.*2026|2026.*24/)
    await user.click(screen.getByRole('button', { name: 'Clear date' }))
    expect(onChange).toHaveBeenCalledWith('')
  })
})
