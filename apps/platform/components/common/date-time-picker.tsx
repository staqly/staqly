'use client'

import { CalendarIcon, Clock, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'

import { Button } from '@/components/ui/button'
import { ButtonGroup } from '@/components/ui/button-group'
import { Calendar } from '@/components/ui/calendar'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { useNow } from '@/hooks/use-now'
import { formatTime, nextFullHour, presets, sameDay } from '@/lib/schedule'
import { cn } from '@/lib/utils'

const HOURS = Array.from({ length: 24 }, (_, h) => String(h).padStart(2, '0'))
const MINUTES = Array.from({ length: 12 }, (_, i) => String(i * 5).padStart(2, '0'))

/** "Tue 21 Oct 2026" for the date half of the trigger. */
function formatDay(d: Date) {
  return d.toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short', year: 'numeric' })
}

/** "Mon 19 Oct 2026, 02:00": the footer's summary. */
export function formatShort(d: Date) {
  return `${formatDay(d)}, ${formatTime(d)}`
}

/**
 * One column of the clock: a stack of values, the chosen one filled, about five rows high with the chosen
 * value centred; arrow keys move within it; values that have passed today cannot be picked.
 */
function TimeColumn({
  label,
  values,
  value,
  disabledBefore,
  onPick,
  autoFocusRef,
}: {
  label: string
  values: string[]
  value: string
  /** Values below this one (same as the strings' numbers) are in the past. */
  disabledBefore?: number
  onPick: (v: string) => void
  autoFocusRef?: React.RefObject<HTMLButtonElement | null>
}) {
  const viewport = useRef<HTMLDivElement>(null)

  // On mount (the column remounts on every opening, keyed by the parent): the chosen value centred. The
  // viewport is positioned, so offsetTop is measured from it.
  useEffect(() => {
    const el = viewport.current
    const on = el?.querySelector<HTMLElement>('[aria-checked="true"]')

    if (el && on) {
      el.scrollTop = on.offsetTop - el.clientHeight / 2 + on.clientHeight / 2
    }
  }, [])

  const disabled = (v: string) => disabledBefore !== undefined && Number(v) < disabledBefore

  function onKey(e: React.KeyboardEvent, i: number) {
    const by: Record<string, number> = { ArrowUp: -1, ArrowDown: 1, Home: -i, End: values.length - 1 - i }
    const delta = by[e.key]

    if (delta === undefined) {
      return
    }

    e.preventDefault()
    let j = Math.min(values.length - 1, Math.max(0, i + delta))

    while (disabled(values[j]) && j < values.length - 1) {
      j++
    }

    const next = values[j]

    if (disabled(next)) {
      return
    }

    onPick(next)
    requestAnimationFrame(() => viewport.current?.querySelector<HTMLElement>(`[data-value="${next}"]`)?.focus())
  }

  return (
    // As tall as the calendar beside it, so the hours and minutes use the room.
    <div className="grid w-16 gap-1 px-1">
      <span className="text-center text-xs font-medium text-muted-foreground" aria-hidden>
        {label}
      </span>
      <div
        ref={viewport}
        role="radiogroup"
        aria-label={label}
        className="relative h-52 overflow-y-auto sm:h-64 overscroll-contain p-0.5 [mask-image:linear-gradient(to_bottom,transparent,black_12%,black_88%,transparent)] [scrollbar-width:none]"
      >
        <div className="grid gap-0.5">
          {values.map((v, i) => {
            const on = v === value
            const off = disabled(v)

            return (
              <Button
                key={v}
                ref={on ? autoFocusRef : undefined}
                type="button"
                role="radio"
                aria-checked={on}
                data-value={v}
                tabIndex={on ? 0 : -1}
                size="sm"
                variant={on ? 'default' : 'ghost'}
                disabled={off}
                className={cn('h-10 w-full justify-center text-base tabular-nums', off && 'text-muted-foreground/50')}
                onClick={() => onPick(v)}
                onKeyDown={(e) => onKey(e, i)}
              >
                {v}
              </Button>
            )
          })}
        </div>
      </div>
    </div>
  )
}

/**
 * The inside of a date-and-time popover: the month calendar with quick picks on the left, hours and minutes
 * on the right. Days and times that have passed cannot be picked. Mount it once per opening (key it), so the
 * month and the clock columns start at the value.
 */
export function DateTimeFields({
  id,
  value,
  onChange,
  hourRef,
}: {
  id?: string
  value: Date | null
  onChange: (value: Date) => void
  hourRef?: React.RefObject<HTMLButtonElement | null>
}) {
  const now = useNow(true, 30_000)
  const today = new Date(now)
  const [month, setMonth] = useState(() => value ?? today)
  const time = value ?? nextFullHour(now)
  const hour = HOURS[time.getHours()]
  const minute = String(Math.floor(time.getMinutes() / 5) * 5).padStart(2, '0')
  // Today: the hours before now, and the minutes before now within the current hour, have passed.
  const isToday = sameDay(time, today)
  const hourFloor = isToday ? today.getHours() : undefined
  const minuteFloor = isToday && time.getHours() === today.getHours() ? today.getMinutes() : undefined

  function pickDay(d: Date) {
    const next = new Date(d)

    next.setHours(time.getHours(), time.getMinutes(), 0, 0)

    // A day picked today with a time already gone: the next full hour instead.
    if (next.getTime() <= now && sameDay(next, today)) {
      const n = nextFullHour(now)

      next.setHours(n.getHours(), n.getMinutes(), 0, 0)
    }

    onChange(next)
  }

  function setTime(hours: number, minutes: number) {
    const next = new Date(value ?? today)

    next.setHours(hours, minutes, 0, 0)
    onChange(next)
  }

  return (
    // One divider between the calendar and the clock, one between hours and minutes; nothing at the edges.
    <div className="flex flex-col divide-y sm:flex-row sm:divide-x sm:divide-y-0">
      <div className="p-3">
        <Calendar
          id={id ?? 'date'}
          mode="single"
          selected={value}
          onSelect={pickDay}
          month={month}
          onMonthChange={setMonth}
          minDate={today}
        />
        <div className="mt-2 flex flex-wrap gap-1" data-testid="schedule-presets">
          {presets(now).map((p) => (
            <Button
              key={p.label}
              type="button"
              size="xs"
              variant="ghost"
              className="h-7 rounded-full border px-2.5 text-xs"
              onClick={() => {
                onChange(p.at)
                setMonth(p.at)
              }}
            >
              {p.label}
            </Button>
          ))}
        </div>
      </div>
      <div className="flex justify-center divide-x p-3">
        <TimeColumn
          label="Hour"
          values={HOURS}
          value={hour}
          disabledBefore={hourFloor}
          onPick={(h) => setTime(Number(h), time.getMinutes())}
          autoFocusRef={hourRef}
        />
        <TimeColumn
          label="Minute"
          values={MINUTES}
          value={minute}
          disabledBefore={minuteFloor}
          onPick={(m) => setTime(time.getHours(), Number(m))}
        />
      </div>
    </div>
  )
}

/**
 * A date and time, picked, not typed: a split trigger (the date, the time, Clear) opening a popover below it
 * with DateTimeFields; the choice in words and Done in the footer.
 */
export function DateTimePicker({
  id,
  value,
  onChange,
  allowClear,
  invalid,
  describedBy,
  className,
}: {
  id?: string
  value: Date | null
  onChange: (value: Date | null) => void
  /** Offers Clear (the schedule may be removed). */
  allowClear?: boolean
  invalid?: boolean
  describedBy?: string
  className?: string
}) {
  const [open, setOpen] = useState(false)
  const [wasOpen, setWasOpen] = useState(open)
  // Counts the openings, so the fields start at the value each time.
  const [opening, setOpening] = useState(0)
  const [focusClock, setFocusClock] = useState(false)
  const hourRef = useRef<HTMLButtonElement>(null)

  if (open !== wasOpen) {
    setWasOpen(open)

    if (open) {
      setOpening((n) => n + 1)
    }
  }

  // Opened from the time half: the hour column takes focus once it has rendered.
  useEffect(() => {
    if (open && focusClock) {
      const t = requestAnimationFrame(() => hourRef.current?.focus())

      return () => cancelAnimationFrame(t)
    }
  }, [open, focusClock])

  const openFrom = (clock: boolean) => {
    setFocusClock(clock)
    setOpen(true)
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <ButtonGroup className={cn('max-w-full', className)} aria-label="Publish time">
        <PopoverTrigger asChild>
          <Button
            id={id}
            type="button"
            variant="outline"
            aria-haspopup="dialog"
            aria-invalid={invalid || undefined}
            aria-describedby={describedBy}
            data-testid="date-time-picker"
            onClick={() => setFocusClock(false)}
            className={cn(
              'h-8 min-w-0 flex-1 justify-start px-2.5 font-normal pointer-coarse:h-11 sm:flex-none',
              !value && 'text-muted-foreground',
            )}
          >
            <CalendarIcon className="text-muted-foreground" />
            <span className="truncate">{value ? formatDay(value) : 'Pick a date'}</span>
          </Button>
        </PopoverTrigger>
        <Button
          type="button"
          variant="outline"
          aria-haspopup="dialog"
          aria-label={value ? `Time, ${formatTime(value)}` : 'Time'}
          data-testid="date-time-picker-time"
          onClick={() => (open ? setFocusClock(true) : openFrom(true))}
          className={cn('h-8 px-2.5 font-normal tabular-nums pointer-coarse:h-11', !value && 'text-muted-foreground')}
        >
          <Clock className="text-muted-foreground" />
          {value ? formatTime(value) : '--:--'}
        </Button>
        {allowClear && value && (
          <Button
            type="button"
            variant="outline"
            size="icon"
            aria-label="Clear schedule"
            className="size-8 pointer-coarse:size-11"
            onClick={() => onChange(null)}
          >
            <X />
          </Button>
        )}
      </ButtonGroup>
      <PopoverContent
        side="bottom"
        align="start"
        collisionPadding={8}
        className="w-auto max-w-[calc(100vw-1rem)] gap-0 p-0 text-sm"
        aria-label="Pick a date and time"
        onOpenAutoFocus={(e) => focusClock && e.preventDefault()}
      >
        <DateTimeFields key={opening} id={id} value={value} onChange={onChange} hourRef={hourRef} />
        <div className="flex items-center justify-between gap-3 border-t px-3 py-2">
          <p className="min-w-0 truncate text-sm" aria-live="polite" data-testid="date-time-summary">
            {value ? formatShort(value) : <span className="text-muted-foreground">No time picked</span>}
          </p>
          <div className="flex shrink-0 gap-1.5">
            {allowClear && value && (
              <Button type="button" size="sm" variant="ghost" onClick={() => onChange(null)}>
                Clear
              </Button>
            )}
            <Button type="button" size="sm" onClick={() => setOpen(false)}>
              Done
            </Button>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  )
}
