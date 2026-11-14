'use client'

import { ChevronDown } from 'lucide-react'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from '@/components/ui/command'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { cn } from '@/lib/utils'

/** Every zone the browser knows, with `current` first when the list lacks it. */
function timeZoneOptions(current: string) {
  const list = typeof Intl.supportedValuesOf === 'function' ? Intl.supportedValuesOf('timeZone') : ['UTC']

  // Some browsers leave UTC out of the list.
  const all = list.includes('UTC') ? list : ['UTC', ...list]

  return !current || all.includes(current) ? all : [current, ...all]
}

/** Pick one time zone from a searchable list. */
export function TimeZoneSelect({
  id,
  value,
  onChange,
  invalid,
  disabled,
  className,
}: {
  id?: string
  value: string
  onChange: (v: string) => void
  invalid?: boolean
  disabled?: boolean
  className?: string
}) {
  const [open, setOpen] = useState(false)
  const options = timeZoneOptions(value)

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          id={id}
          type="button"
          variant="outline"
          aria-invalid={invalid}
          disabled={disabled}
          className={cn('justify-between gap-1.5 font-normal', className)}
          data-testid="time-zone-select"
        >
          <span className="truncate">{value.replaceAll('_', ' ')}</span>
          <ChevronDown className="shrink-0 text-muted-foreground" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-(--radix-popover-trigger-width) min-w-64 p-0">
        <Command>
          <CommandInput placeholder="Search, e.g. Warsaw" />
          <CommandList>
            <CommandEmpty>No time zone found.</CommandEmpty>
            {Object.entries(Object.groupBy(options, (z) => (z.includes('/') ? z.split('/')[0] : ''))).map(
              ([continent, zones]) => (
                <CommandGroup key={continent} heading={continent || undefined}>
                  {zones!.map((z) => (
                    <CommandItem
                      key={z}
                      value={z.replaceAll('_', ' ')}
                      data-checked={z === value}
                      onSelect={() => {
                        onChange(z)
                        setOpen(false)
                      }}
                    >
                      {/* The group heading names the continent. */}
                      {z.slice(continent ? continent.length + 1 : 0).replaceAll('_', ' ')}
                    </CommandItem>
                  ))}
                </CommandGroup>
              ),
            )}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}
