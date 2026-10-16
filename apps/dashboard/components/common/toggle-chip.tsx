'use client'

import { Check } from 'lucide-react'

import { safeCssColor } from '@/lib/safe'
import { cn } from '@/lib/utils'

/** Toggle chip used for multi-selects (collections, types). */
export function ToggleChip({
  pressed,
  onClick,
  children,
  color,
}: {
  pressed: boolean
  onClick: () => void
  children: React.ReactNode
  color?: string
}) {
  return (
    <button
      type="button"
      aria-pressed={pressed}
      onClick={onClick}
      className={cn(
        'inline-flex h-8 max-w-full items-center gap-1.5 rounded-full border px-3 text-[13px] transition-colors pointer-coarse:h-10',
        pressed
          ? 'border-brand/50 bg-brand-soft font-medium text-foreground'
          : 'bg-background text-muted-foreground hover:bg-muted hover:text-foreground',
      )}
    >
      {pressed ? (
        <Check className="size-3.5 shrink-0 text-brand" />
      ) : color !== undefined ? (
        <span
          className="size-2 shrink-0 rounded-[3px] bg-muted-foreground/40"
          style={{ background: safeCssColor(color) }}
        />
      ) : null}
      <span className="truncate leading-none">{children}</span>
    </button>
  )
}
