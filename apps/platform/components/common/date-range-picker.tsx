'use client'

import { CalendarIcon, X } from 'lucide-react'
import dynamic from 'next/dynamic'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { ButtonGroup } from '@/components/ui/button-group'
import { nextRange, type DateRange } from '@/components/ui/calendar'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { useIsMobile } from '@/hooks/use-mobile'
import { useNow } from '@/hooks/use-now'
import { addDays, dayFromKey, dayKey } from '@/lib/schedule'
import { cn } from '@/lib/utils'

// The calendar grid loads with the first popover, not with every page that has a date filter.
const Calendar = dynamic(() => import('@/components/ui/calendar').then((m) => m.Calendar), { ssr: false })

/** Local calendar days as the API and the URL carry them: "2026-10-12". */
export interface DayRange {
  from: string
  to: string
}

const fmt = (key: string) =>
  dayFromKey(key).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })

/** "Oct 12, 2026 to Oct 18, 2026", or one day. */
export function formatDayRange(r: DayRange) {
  return r.from === r.to ? fmt(r.from) : `${fmt(r.from)} to ${fmt(r.to)}`
}

/** The quick picks, as local days ending today (or last month). */
export function rangePresets(now: number = Date.now()): { label: string; range: DayRange }[] {
  const today = new Date(now)
  const t = dayKey(today)
  const firstOfMonth = new Date(today.getFullYear(), today.getMonth(), 1)
  const lastMonthStart = new Date(today.getFullYear(), today.getMonth() - 1, 1)
  const lastMonthEnd = new Date(today.getFullYear(), today.getMonth(), 0)

  return [
    { label: 'Today', range: { from: t, to: t } },
    { label: 'Yesterday', range: { from: dayKey(addDays(today, -1)), to: dayKey(addDays(today, -1)) } },
    { label: 'Last 7 days', range: { from: dayKey(addDays(today, -6)), to: t } },
    { label: 'Last 30 days', range: { from: dayKey(addDays(today, -29)), to: t } },
    { label: 'This month', range: { from: dayKey(firstOfMonth), to: t } },
    { label: 'Last month', range: { from: dayKey(lastMonthStart), to: dayKey(lastMonthEnd) } },
  ]
}

/**
 * A range of days, picked on a calendar (two months side by side on desktop, one on phones) or from the
 * presets. Days are local; the consumer gets "YYYY-MM-DD" ends, both inclusive, or null.
 */
export function DateRangePicker({
  value,
  onChange,
  label = 'Date range',
  placeholder = 'Pick a date range',
  className,
  size = 'sm',
}: {
  value: DayRange | null
  onChange: (value: DayRange | null) => void
  label?: string
  placeholder?: string
  className?: string
  size?: 'sm' | 'default'
}) {
  const [open, setOpen] = useState(false)
  const isMobile = useIsMobile()
  const now = useNow(open, 60_000)
  const [draft, setDraft] = useState<DateRange | null>(null)
  const [month, setMonth] = useState(() => (value ? dayFromKey(value.from) : new Date(now)))
  const [wasOpen, setWasOpen] = useState(open)

  if (open !== wasOpen) {
    setWasOpen(open)

    if (open) {
      setDraft(value ? { from: dayFromKey(value.from), to: dayFromKey(value.to) } : null)
      setMonth(value ? dayFromKey(value.from) : new Date(now))
    }
  }

  function pickDay(day: Date) {
    const next = nextRange(draft, day)

    setDraft(next)

    if (next.from && next.to) {
      onChange({ from: dayKey(next.from), to: dayKey(next.to) })
    }
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <ButtonGroup className={cn('max-w-full', className)}>
        <PopoverTrigger asChild>
          <Button
            type="button"
            variant="outline"
            size={size}
            aria-haspopup="dialog"
            aria-label={label}
            data-testid="date-range-picker"
            className={cn('min-w-0 justify-start font-normal', !value && 'text-muted-foreground')}
          >
            <CalendarIcon className="text-muted-foreground" />
            <span className="truncate">{value ? formatDayRange(value) : placeholder}</span>
          </Button>
        </PopoverTrigger>
        {value && (
          <Button
            type="button"
            variant="outline"
            size={size === 'sm' ? 'icon-sm' : 'icon'}
            aria-label={`Clear ${label.toLowerCase()}`}
            onClick={() => onChange(null)}
          >
            <X />
          </Button>
        )}
      </ButtonGroup>
      <PopoverContent
        align="end"
        collisionPadding={8}
        className="w-auto max-w-[calc(100vw-1rem)] p-0"
        aria-label={label}
      >
        <div className="flex flex-col gap-3 p-3 sm:flex-row">
          <div className="flex flex-row flex-wrap gap-1.5 sm:w-32 sm:flex-col" data-testid="range-presets">
            {rangePresets(now).map((p) => (
              <Button
                key={p.label}
                type="button"
                size="sm"
                variant={value && value.from === p.range.from && value.to === p.range.to ? 'secondary' : 'ghost'}
                className="justify-start"
                onClick={() => {
                  onChange(p.range)
                  setDraft({ from: dayFromKey(p.range.from), to: dayFromKey(p.range.to) })
                  setMonth(dayFromKey(p.range.from))
                }}
              >
                {p.label}
              </Button>
            ))}
          </div>
          <div aria-hidden className="border-t sm:border-t-0 sm:border-l" />
          <Calendar
            id="range"
            mode="range"
            selected={draft}
            onSelect={pickDay}
            month={month}
            onMonthChange={setMonth}
            numberOfMonths={isMobile ? 1 : 2}
          />
        </div>
        <div className="flex items-center justify-between gap-3 border-t px-3 py-2">
          <p className="min-w-0 truncate text-xs text-muted-foreground" aria-live="polite">
            {draft?.from && !draft.to
              ? `From ${fmt(dayKey(draft.from))}: pick the last day`
              : value
                ? formatDayRange(value)
                : 'Pick the first and the last day, or a quick range.'}
          </p>
          <div className="flex gap-1.5">
            {value && (
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
