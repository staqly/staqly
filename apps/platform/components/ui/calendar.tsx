'use client'

import { ChevronLeft, ChevronRight } from 'lucide-react'
import { useLayoutEffect, useRef, useState } from 'react'

import { Button } from '@/components/ui/button'
import { useNow } from '@/hooks/use-now'
import { addDays, dayKey, monthGrid, sameDay } from '@/lib/schedule'
import { cn } from '@/lib/utils'

export interface DateRange {
  from: Date | null
  to: Date | null
}

/**
 * A month calendar without a library: one or two months side by side, a single day or a range, keyboard
 * arrows between days (roving focus), today marked, days outside `minDate`..`maxDate` disabled, a range
 * preview while the second end is being picked. The day buttons carry full names ("Tuesday 21 October") and
 * the grid the month's name, so a screen reader hears where it is.
 */
export function Calendar({
  id = 'calendar',
  mode,
  selected,
  onSelect,
  month,
  onMonthChange,
  numberOfMonths = 1,
  minDate,
  maxDate,
  className,
}: {
  id?: string
  mode: 'single' | 'range'
  selected: Date | DateRange | null
  /** The day clicked or picked with Enter (local midnight). */
  onSelect: (day: Date) => void
  /** The first month on show (any day of it). */
  month: Date
  onMonthChange: (month: Date) => void
  numberOfMonths?: 1 | 2
  minDate?: Date
  maxDate?: Date
  className?: string
}) {
  const now = useNow(true, 60_000)
  const todayKey = dayKey(new Date(now))
  const minKey = minDate ? dayKey(minDate) : ''
  const maxKey = maxDate ? dayKey(maxDate) : ''
  const range = mode === 'range' ? (selected as DateRange | null) : null
  const single = mode === 'single' ? (selected as Date | null) : null
  const [hover, setHover] = useState<string | null>(null)
  const [focus, setFocus] = useState(() => dayKey(single ?? range?.from ?? month))
  const pendingFocus = useRef(false)

  useLayoutEffect(() => {
    if (pendingFocus.current) {
      // Changing months replaces the day buttons, including the adjacent month's visible days.
      document.getElementById(`${id}-day-${focus}`)?.focus()
      pendingFocus.current = false
    }
  })

  const months = Array.from(
    { length: numberOfMonths },
    (_, i) => new Date(month.getFullYear(), month.getMonth() + i, 1),
  )
  const disabledKey = (k: string) => (!!minKey && k < minKey) || (!!maxKey && k > maxKey)
  const shift = (n: number) => onMonthChange(new Date(month.getFullYear(), month.getMonth() + n, 1))

  // The range as shown: a chosen pair, or the first end plus the day under the pointer.
  const fromKey = range?.from ? dayKey(range.from) : ''
  const toKey = range?.to ? dayKey(range.to) : fromKey && hover ? hover : ''
  const [lo, hi] = fromKey && toKey ? (fromKey <= toKey ? [fromKey, toKey] : [toKey, fromKey]) : ['', '']

  function moveFocus(e: React.KeyboardEvent, from: Date) {
    const by: Record<string, number> = { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -7, ArrowDown: 7 }
    const delta = by[e.key]

    if (delta === undefined) {
      return
    }

    e.preventDefault()
    const next = addDays(from, delta)
    const k = dayKey(next)

    if (disabledKey(k)) {
      return
    }

    setFocus(k)
    const lastShown = new Date(month.getFullYear(), month.getMonth() + numberOfMonths, 0)

    if (next < new Date(month.getFullYear(), month.getMonth(), 1)) {
      pendingFocus.current = true
      onMonthChange(new Date(next.getFullYear(), next.getMonth(), 1))
    } else if (next > lastShown) {
      pendingFocus.current = true
      onMonthChange(new Date(next.getFullYear(), next.getMonth() - numberOfMonths + 1, 1))
    } else {
      document.getElementById(`${id}-day-${k}`)?.focus()
    }
  }

  return (
    <div className={cn('flex flex-col gap-3 sm:flex-row sm:gap-5', className)} data-testid={`${id}-calendar`}>
      {months.map((m, i) => {
        const days = monthGrid(m)
        const label = m.toLocaleDateString(undefined, { month: 'long', year: 'numeric' })

        return (
          <div key={dayKey(m)} className="w-[17.5rem] max-w-full">
            <div className="flex items-center justify-between">
              {i === 0 ? (
                <Button
                  type="button"
                  size="icon-sm"
                  variant="ghost"
                  aria-label="Previous month"
                  onClick={() => shift(-1)}
                >
                  <ChevronLeft />
                </Button>
              ) : (
                <span className="size-7" aria-hidden />
              )}
              <span className="text-sm font-medium" aria-live="polite">
                {label}
              </span>
              {i === months.length - 1 ? (
                <Button type="button" size="icon-sm" variant="ghost" aria-label="Next month" onClick={() => shift(1)}>
                  <ChevronRight />
                </Button>
              ) : (
                <span className="size-7" aria-hidden />
              )}
            </div>
            <div
              role="grid"
              tabIndex={-1}
              aria-label={label}
              aria-multiselectable={mode === 'range' || undefined}
              className="mt-1 grid grid-cols-7 gap-y-0.5 text-center text-sm"
              onMouseLeave={() => setHover(null)}
            >
              <div role="row" className="contents">
                {days.slice(0, 7).map((d) => (
                  <div
                    key={d.getDay()}
                    role="columnheader"
                    className="py-1 text-[11px] font-medium text-muted-foreground"
                    aria-label={d.toLocaleDateString(undefined, { weekday: 'long' })}
                  >
                    {d.toLocaleDateString(undefined, { weekday: 'narrow' })}
                  </div>
                ))}
              </div>
              {Array.from({ length: days.length / 7 }, (_, r) => (
                <div key={r} role="row" className="contents">
                  {days.slice(r * 7, r * 7 + 7).map((d) => {
                    const k = dayKey(d)
                    const disabled = disabledKey(k)
                    const outside = d.getMonth() !== m.getMonth()
                    const isEnd = mode === 'range' ? k === fromKey || (!!range?.to && k === toKey) : false
                    const inRange = mode === 'range' && !!lo && k > lo && k < hi
                    const selectedDay = mode === 'single' ? !!single && sameDay(d, single) : isEnd
                    const isStart = mode === 'range' && lo === k && hi !== k
                    const isFinish = mode === 'range' && hi === k && lo !== k

                    return (
                      <div
                        key={k}
                        role="gridcell"
                        aria-selected={selectedDay || inRange}
                        className={cn(
                          'flex justify-center',
                          !outside && inRange && 'bg-brand-soft',
                          !outside &&
                            isStart &&
                            'rounded-l-full bg-[linear-gradient(to_right,transparent_50%,var(--brand-soft)_50%)]',
                          !outside &&
                            isFinish &&
                            'rounded-r-full bg-[linear-gradient(to_left,transparent_50%,var(--brand-soft)_50%)]',
                        )}
                      >
                        <button
                          type="button"
                          id={`${id}-day-${k}`}
                          tabIndex={focus === k ? 0 : -1}
                          disabled={disabled}
                          aria-label={d.toLocaleDateString(undefined, {
                            weekday: 'long',
                            day: 'numeric',
                            month: 'long',
                          })}
                          aria-current={k === todayKey ? 'date' : undefined}
                          onClick={() => {
                            setFocus(k)
                            onSelect(new Date(d.getFullYear(), d.getMonth(), d.getDate()))
                          }}
                          onKeyDown={(e) => moveFocus(e, d)}
                          onMouseEnter={() => mode === 'range' && setHover(k)}
                          onFocus={() => mode === 'range' && setHover(k)}
                          className={cn(
                            'size-9 rounded-full text-sm tabular-nums outline-none pointer-coarse:size-10',
                            'hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring',
                            outside && 'text-muted-foreground/60',
                            disabled && 'text-muted-foreground/50 hover:bg-transparent',
                            k === todayKey && !selectedDay && 'font-semibold text-brand ring-1 ring-brand/60',
                            inRange && !outside && 'hover:bg-brand/20',
                            selectedDay && 'bg-brand font-semibold text-brand-foreground hover:bg-brand',
                          )}
                        >
                          {d.getDate()}
                        </button>
                      </div>
                    )
                  })}
                </div>
              ))}
            </div>
          </div>
        )
      })}
    </div>
  )
}

/** Picks the next end of a range: a first click sets `from`, the second `to` (ordered), a third starts over. */
export function nextRange(current: DateRange | null, day: Date): DateRange {
  if (!current?.from || current.to) {
    return { from: day, to: null }
  }

  return day < current.from ? { from: day, to: current.from } : { from: current.from, to: day }
}
