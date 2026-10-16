import { Check } from 'lucide-react'

import { cn } from '@/lib/utils'

/** Collection colours (the dot in the sidebar and on chips). '' = no colour. */
const COLLECTION_COLORS = [
  { value: '', label: 'No colour' },
  { value: '#22c55e', label: 'Green' },
  { value: '#3b82f6', label: 'Blue' },
  { value: '#8b5cf6', label: 'Violet' },
  { value: '#ec4899', label: 'Pink' },
  { value: '#ef4444', label: 'Red' },
  { value: '#f97316', label: 'Orange' },
  { value: '#eab308', label: 'Yellow' },
  { value: '#14b8a6', label: 'Teal' },
]

export function ColorPicker({ value, onChange }: { value: string; onChange: (color: string) => void }) {
  return (
    <div role="radiogroup" aria-label="Colour" className="flex flex-wrap gap-2">
      {COLLECTION_COLORS.map((c) => {
        const on = value.toLowerCase() === c.value

        return (
          <button
            key={c.value || 'none'}
            type="button"
            role="radio"
            aria-checked={on}
            aria-label={c.label}
            title={c.label}
            onClick={() => onChange(c.value)}
            className={cn(
              'flex size-7 items-center justify-center rounded-full border outline-none focus-visible:ring-3 focus-visible:ring-ring/50 pointer-coarse:size-10',
              on && 'ring-2 ring-foreground/70 ring-offset-2 ring-offset-background',
              !c.value && 'bg-muted-foreground/30',
            )}
            style={c.value ? { background: c.value, borderColor: c.value } : undefined}
          >
            {on && <Check className={cn('size-3.5', c.value ? 'text-white' : 'text-foreground')} />}
          </button>
        )
      })}
    </div>
  )
}
