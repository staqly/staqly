'use client'

import { Pencil } from 'lucide-react'
import { useState } from 'react'

/** A document's name (a design, a video, a sound): a label with a pencil that opens into a field (Enter or leaving it keeps the name). */
export function NameField({
  value,
  onChange,
  placeholder = 'Untitled design',
}: {
  value: string
  onChange: (name: string) => void
  placeholder?: string
}) {
  const [editing, setEditing] = useState(false)

  if (!editing) {
    return (
      <button
        type="button"
        aria-label={`Rename ${value || placeholder}`}
        onClick={() => setEditing(true)}
        className="group flex h-8 max-w-40 min-w-0 items-center gap-1.5 rounded-md px-2 text-sm font-medium hover:bg-muted sm:max-w-64 pointer-coarse:h-11 pointer-coarse:min-w-11"
      >
        <span className="truncate">{value || placeholder}</span>
        <Pencil
          className="size-3.5 shrink-0 text-muted-foreground transition-colors group-hover:text-foreground"
          aria-hidden
        />
      </button>
    )
  }

  return (
    <input
      aria-label="Design name"
      // oxlint-disable-next-line jsx-a11y/no-autofocus -- the person just clicked to rename it
      autoFocus
      value={value}
      maxLength={150}
      onFocus={(e) => e.currentTarget.select()}
      onChange={(e) => onChange(e.target.value)}
      onBlur={() => setEditing(false)}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === 'Escape') {
          e.currentTarget.blur()
        }
      }}
      className="h-8 w-40 min-w-0 origin-left animate-in rounded-md border border-ring bg-transparent px-2 text-sm font-medium duration-200 fade-in-0 zoom-in-95 slide-in-from-left-1 focus:outline-none sm:w-64"
    />
  )
}
