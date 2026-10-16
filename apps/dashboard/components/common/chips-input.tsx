'use client'

import { Plus, X } from 'lucide-react'
import { useId, useState } from 'react'

import { Button } from '@/components/ui/button'
import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

/**
 * A list of short strings edited as chips: type and press Enter (or comma) to
 * add, Backspace on an empty input removes the last chip.
 */
export function ChipsInput({
  value,
  onChange,
  placeholder,
  label,
  invalid,
  required,
  max = 50,
  maxLength = 100,
  className,
  id,
  tone = 'default',
}: {
  value: string[]
  onChange: (v: string[]) => void
  placeholder?: string
  /** Accessible name of the text input. */
  label: string
  invalid?: boolean
  /** Sets aria-required on the text input; the visible mark belongs on the label. */
  required?: boolean
  max?: number
  maxLength?: number
  className?: string
  id?: string
  tone?: 'default' | 'exclude'
}) {
  const [draft, setDraft] = useState('')
  const autoId = useId()
  const inputId = id ?? autoId

  function add(raw = draft) {
    const parts = raw
      .split(',')
      .map((p) => p.trim().slice(0, maxLength))
      .filter(Boolean)

    if (!parts.length) {
      return
    }

    const next = [...value]

    for (const p of parts) {
      if (!next.includes(p) && next.length < max) {
        next.push(p)
      }
    }

    onChange(next)
    setDraft('')
  }

  return (
    // oxlint-disable-next-line jsx-a11y/click-events-have-key-events, jsx-a11y/no-static-element-interactions -- clicking the empty area focuses the input; keyboard users tab to the input directly
    <div
      className={cn(
        'flex min-h-8 w-full min-w-0 flex-wrap items-center gap-1 rounded-lg border border-input bg-transparent p-1 transition-colors focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/50 pointer-coarse:min-h-11 dark:bg-input/30',
        invalid && 'border-destructive ring-3 ring-destructive/20',
        className,
      )}
      onClick={(e) => {
        if (e.target === e.currentTarget) {
          document.getElementById(inputId)?.focus()
        }
      }}
    >
      {value.map((v) => (
        <span
          key={v}
          data-testid="chip"
          className={cn(
            'inline-flex h-6 max-w-full items-center gap-0.5 rounded-md py-0.5 pr-0.5 pl-2 text-xs font-medium pointer-coarse:h-8',
            tone === 'exclude' ? TONES.danger.badge : 'bg-muted text-foreground',
          )}
        >
          <span className="truncate">{v}</span>
          <button
            type="button"
            onClick={() => onChange(value.filter((x) => x !== v))}
            aria-label={`Remove ${v}`}
            className="flex size-5 shrink-0 items-center justify-center rounded opacity-60 hover:bg-foreground/10 hover:opacity-100 pointer-coarse:size-7"
          >
            <X className="size-3" />
          </button>
        </span>
      ))}
      <input
        id={inputId}
        value={draft}
        aria-label={label}
        aria-invalid={invalid || undefined}
        aria-required={required || undefined}
        onChange={(e) => {
          const v = e.target.value

          if (v.endsWith(',')) {
            add(v)
          } else {
            setDraft(v)
          }
        }}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault()
            add()
          } else if (e.key === 'Backspace' && !draft && value.length) {
            onChange(value.slice(0, -1))
          }
        }}
        onBlur={() => add()}
        placeholder={value.length ? '' : placeholder}
        className="h-6 min-w-24 flex-1 bg-transparent px-1.5 text-base outline-none placeholder:text-muted-foreground md:text-sm pointer-coarse:h-8"
      />
      {draft.trim() && (
        <Button type="button" size="icon-xs" variant="ghost" onClick={() => add()} aria-label={`Add ${draft.trim()}`}>
          <Plus />
        </Button>
      )}
    </div>
  )
}
