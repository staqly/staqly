'use client'

import { ChevronDown } from 'lucide-react'
import { useRef, useState } from 'react'

import { Button } from '@/components/ui/button'
import {
  Command,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from '@/components/ui/command'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { cn } from '@/lib/utils'

export interface MultiOption {
  value: string
  label: string
  /** Small coloured dot (a status tone), optional. */
  dot?: string
  /** Second line under the label, optional. */
  hint?: string
  /** Options with the same group are listed under that heading. */
  group?: string
  /** Shown ticked and locked: covered by another pick (e.g. a wildcard). */
  implied?: boolean
  /** A picture before the label (a channel's avatar). */
  icon?: React.ReactNode
  /** Listed but not pickable (a channel that needs reconnecting). */
  disabled?: boolean
  testId?: string
}

function byGroup(options: MultiOption[]) {
  const groups = new Map<string | undefined, MultiOption[]>()

  for (const o of options) {
    groups.set(o.group, [...(groups.get(o.group) ?? []), o])
  }

  return [...groups]
}

/**
 * Pick any number of options from a dropdown. With `anyLabel`, nothing picked
 * is a choice of its own ("All collections"); without it, `placeholder` shows.
 */
export function MultiSelect({
  id,
  label,
  value,
  onChange,
  options: given,
  anyLabel,
  placeholder = 'Choose...',
  invalid,
  summary,
  mono,
  size = 'default',
  className,
  creatable,
  disabled,
}: {
  id?: string
  label: string
  value: string[]
  onChange: (v: string[]) => void
  options: MultiOption[]
  anyLabel?: string
  placeholder?: string
  invalid?: boolean
  /** Replaces the trigger's text, e.g. "Every event" when a wildcard covers everything. */
  summary?: React.ReactNode
  /** Labels are identifiers (event names): monospace. */
  mono?: boolean
  size?: 'sm' | 'default'
  className?: string
  /** Typed values can be added (tags): the search box is always there and offers "Add". */
  creatable?: { normalize?: (v: string) => string }
  disabled?: boolean
}) {
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  // The highlighted item, and what was typed to find it in a list without a search box (type-ahead).
  const [active, setActive] = useState('')
  const typing = useRef({ text: '', at: 0 })
  const list = useRef<HTMLDivElement>(null)

  // Values added by typing are options too, so they show ticked and can be removed.
  const options: MultiOption[] = creatable
    ? [...given, ...value.filter((v) => !given.some((o) => o.value === v)).map((v) => ({ value: v, label: v }))]
    : given

  // Picks covered by another pick (implied, e.g. under a wildcard) are not counted twice.
  const picked = options.filter((o) => value.includes(o.value) && !o.implied)
  const typed = creatable ? (creatable.normalize ?? ((v: string) => v.trim()))(search) : ''
  const canAdd = !!typed && !options.some((o) => o.value === typed)
  const toggle = (v: string) => onChange(value.includes(v) ? value.filter((x) => x !== v) : [...value, v])
  const searchable = !!creatable || given.length > 8
  const itemValue = (o: MultiOption) => `${o.label} ${o.value} ${o.hint ?? ''}`

  /** Short lists have no search box: typed letters move to the first item whose name starts with them. */
  const typeAhead = (e: React.KeyboardEvent) => {
    if (searchable || e.key.length !== 1 || e.metaKey || e.ctrlKey || e.altKey) {
      return
    }

    const now = Date.now()
    const t = typing.current

    t.text = (now - t.at < 800 ? t.text : '') + e.key.toLowerCase()
    t.at = now

    const hit = options.find((o) => !o.disabled && !o.implied && o.label.toLowerCase().startsWith(t.text))

    if (hit) {
      e.preventDefault()
      setActive(itemValue(hit))
    }
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          id={id}
          type="button"
          variant="outline"
          size={size}
          aria-invalid={invalid}
          disabled={disabled}
          className={cn('justify-between gap-1.5 font-normal', className)}
        >
          {/* Named by its label and then what it shows (no aria-label hiding the visible text); a <Label htmlFor>
              names it instead when there is one. */}
          <span className="sr-only">{label}: </span>
          <span className="flex min-w-0 items-center gap-1.5 truncate">
            {summary ? (
              summary
            ) : picked.length === 0 ? (
              <span className="text-muted-foreground">{anyLabel ?? placeholder}</span>
            ) : picked.length > 2 ? (
              `${picked.length} selected`
            ) : (
              picked.map((o) => (
                // Same size as the placeholder: a trigger reads the same empty or filled.
                <span key={o.value} className={cn('inline-flex items-center gap-1', mono && 'font-mono')}>
                  {o.dot && <span aria-hidden className={cn('size-1.5 rounded-full', o.dot)} />}
                  {o.label}
                </span>
              ))
            )}
          </span>
          <ChevronDown className="shrink-0 text-muted-foreground" />
        </Button>
      </PopoverTrigger>
      <PopoverContent
        align="start"
        className="w-(--radix-popover-trigger-width) min-w-56 p-0"
        onKeyDown={typeAhead}
        // Without a search box the list takes the focus, so the arrows and Enter work in it.
        onOpenAutoFocus={(e) => {
          if (!searchable) {
            e.preventDefault()
            list.current?.focus()
          }
        }}
      >
        <Command ref={list} tabIndex={-1} value={active} onValueChange={setActive} className="outline-none">
          {/* Long lists (collections) get a search box; short ones find an item by typing its name. */}
          {searchable && (
            <CommandInput
              placeholder={creatable ? 'Search or add...' : 'Search...'}
              value={search}
              onValueChange={setSearch}
              // Tab adds what was typed (or picks the tag of that name), like Enter on "Add"; empty, it moves on.
              onKeyDown={(e) => {
                if (e.key !== 'Tab' || e.shiftKey || !typed) {
                  return
                }

                e.preventDefault()

                if (!value.includes(typed)) {
                  onChange([...value, typed])
                }

                setSearch('')
              }}
            />
          )}
          <CommandList>
            {creatable && !canAdd && options.length === 0 && (
              <p className="px-3 py-4 text-center text-xs text-muted-foreground">Type a name to add the first one.</p>
            )}
            {canAdd && (
              <CommandGroup forceMount>
                {/* Always listed: the search filter would hide it whenever the typed text was normalized. */}
                <CommandItem
                  forceMount
                  value={`__add ${typed}`}
                  onSelect={() => {
                    onChange([...value, typed])
                    setSearch('')
                  }}
                >
                  Add “{typed}”
                </CommandItem>
              </CommandGroup>
            )}
            {anyLabel && (
              <>
                <CommandGroup>
                  {/* "Any" is a choice like the others: ticked when nothing narrows the list. */}
                  <CommandItem
                    value={`__any ${anyLabel}`}
                    data-checked={value.length === 0}
                    onSelect={() => onChange([])}
                  >
                    {anyLabel}
                  </CommandItem>
                </CommandGroup>
                {options.length > 0 && <CommandSeparator />}
              </>
            )}
            {byGroup(options).map(([group, opts]) => (
              <CommandGroup key={group ?? ''} heading={group}>
                {opts.map((o) => (
                  <CommandItem
                    key={o.value}
                    value={itemValue(o)}
                    data-checked={o.implied || value.includes(o.value)}
                    disabled={o.implied || o.disabled}
                    data-testid={o.testId}
                    onSelect={() => toggle(o.value)}
                  >
                    {o.dot && <span aria-hidden className={cn('size-2 rounded-full', o.dot)} />}
                    {o.icon}
                    {o.hint ? (
                      <span className="grid min-w-0">
                        <span className={cn(mono && 'font-mono text-xs')}>{o.label}</span>
                        <span className="text-xs text-muted-foreground">{o.hint}</span>
                      </span>
                    ) : (
                      <span className={cn(mono && 'font-mono text-xs')}>{o.label}</span>
                    )}
                  </CommandItem>
                ))}
              </CommandGroup>
            ))}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}
