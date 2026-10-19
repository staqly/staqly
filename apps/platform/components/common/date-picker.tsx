'use client'

import { CalendarIcon, X } from 'lucide-react'
import dynamic from 'next/dynamic'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { ButtonGroup } from '@/components/ui/button-group'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { useNow } from '@/hooks/use-now'
import { dayFromKey, dayKey } from '@/lib/schedule'
import { cn } from '@/lib/utils'

const Calendar = dynamic(() => import('@/components/ui/calendar').then((m) => m.Calendar), { ssr: false })

/** One local day, picked on a calendar. The value is "YYYY-MM-DD" or "" (none). */
export function DatePicker({
  id,
  value,
  onChange,
  minDate,
  maxDate,
  placeholder = 'Pick a date',
  invalid,
  describedBy,
  className,
}: {
  id?: string
  value: string
  onChange: (value: string) => void
  minDate?: Date
  maxDate?: Date
  placeholder?: string
  invalid?: boolean
  describedBy?: string
  className?: string
}) {
  const [open, setOpen] = useState(false)
  const now = useNow(open, 60_000)
  const selected = value ? dayFromKey(value) : null
  const [month, setMonth] = useState(() => selected ?? new Date(now))
  const [wasOpen, setWasOpen] = useState(open)

  if (open !== wasOpen) {
    setWasOpen(open)

    if (open) {
      setMonth(selected ?? new Date(now))
    }
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <ButtonGroup className={cn('max-w-full', className)}>
        <PopoverTrigger asChild>
          <Button
            id={id}
            type="button"
            variant="outline"
            aria-haspopup="dialog"
            aria-invalid={invalid || undefined}
            aria-describedby={describedBy}
            data-testid="date-picker"
            className={cn('min-w-0 justify-start font-normal', !value && 'text-muted-foreground')}
          >
            <CalendarIcon className="text-muted-foreground" />
            <span className="truncate">
              {selected
                ? selected.toLocaleDateString(undefined, {
                    weekday: 'short',
                    day: 'numeric',
                    month: 'short',
                    year: 'numeric',
                  })
                : placeholder}
            </span>
          </Button>
        </PopoverTrigger>
        {value && (
          <Button type="button" variant="outline" size="icon" aria-label="Clear date" onClick={() => onChange('')}>
            <X />
          </Button>
        )}
      </ButtonGroup>
      <PopoverContent align="start" className="w-auto p-3" aria-label="Pick a date">
        <Calendar
          id={id ?? 'date'}
          mode="single"
          selected={selected}
          onSelect={(d) => {
            onChange(dayKey(d))
            setOpen(false)
          }}
          month={month}
          onMonthChange={setMonth}
          minDate={minDate}
          maxDate={maxDate}
        />
      </PopoverContent>
    </Popover>
  )
}
