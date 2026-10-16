'use client'

import { ChevronDown, X } from 'lucide-react'
import { useState } from 'react'

import { StatusBadge } from '@/components/common/status-badge'
import { Button } from '@/components/ui/button'
import { Command, CommandGroup, CommandItem, CommandList } from '@/components/ui/command'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { ITEM_STATUSES, type ItemStatus } from '@/lib/api'
import { STATUS_META } from '@/lib/status'
import { cn } from '@/lib/utils'

/** Pick any number of statuses from a dropdown; none picked reads as "Any status". */
export function StatusMultiSelect({
  value,
  onChange,
  id,
  'aria-labelledby': labelledBy,
  exclude,
}: {
  value: ItemStatus[]
  onChange: (v: ItemStatus[]) => void
  /** Statuses not offered here (a run can't start from pending or draft). */
  exclude?: ItemStatus[]
  id?: string
  'aria-labelledby'?: string
}) {
  const [open, setOpen] = useState(false)
  const toggle = (s: ItemStatus) => onChange(value.includes(s) ? value.filter((x) => x !== s) : [...value, s])

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          id={id}
          type="button"
          variant="outline"
          aria-labelledby={labelledBy}
          className="h-auto min-h-8 w-full justify-between py-1 pr-2 pl-1.5 font-normal"
        >
          <span className="flex flex-wrap gap-1">
            {value.length === 0 ? (
              <span className="px-1 text-muted-foreground">Any status</span>
            ) : (
              ITEM_STATUSES.filter((s) => value.includes(s)).map((s) => <StatusBadge key={s} status={s} />)
            )}
          </span>
          <ChevronDown className="shrink-0 text-muted-foreground" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-(--radix-popover-trigger-width) min-w-56 p-0">
        <Command>
          <CommandList>
            <CommandGroup>
              {ITEM_STATUSES.filter((st) => !exclude?.includes(st)).map((s) => (
                <CommandItem key={s} value={s} data-checked={value.includes(s)} onSelect={() => toggle(s)}>
                  <span aria-hidden className={cn('size-2 rounded-full', STATUS_META[s].dot)} />
                  {STATUS_META[s].label}
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
        {value.length > 0 && (
          <div className="border-t p-1">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="w-full justify-start"
              onClick={() => onChange([])}
            >
              <X /> Any status
            </Button>
          </div>
        )}
      </PopoverContent>
    </Popover>
  )
}
