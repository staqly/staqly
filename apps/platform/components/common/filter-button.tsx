'use client'

import { ListFilter } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'

/**
 * A page's filters behind one "Filter" button next to its main action: search, sort and the selects open in a
 * popover; the button counts the filters in use and Clear all resets them.
 */
export function FilterButton({
  active,
  onClear,
  children,
}: {
  /** How many filters are in use (search counts as one). */
  active: number
  onClear: () => void
  children: React.ReactNode
}) {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="outline" className="shrink-0" data-testid="filter-button">
          <ListFilter /> Filter
          {active > 0 && (
            <span className="rounded-full bg-brand px-1.5 text-[11px] leading-4 font-semibold text-brand-foreground tabular-nums">
              {active}
            </span>
          )}
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="grid w-72 gap-3">
        {children}
        {active > 0 && (
          <Button variant="ghost" className="justify-self-start" onClick={onClear}>
            Clear all
          </Button>
        )}
      </PopoverContent>
    </Popover>
  )
}
