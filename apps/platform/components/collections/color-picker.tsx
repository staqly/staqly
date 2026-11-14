'use client'

import { Check } from 'lucide-react'
import { useRef, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { contrastText, hexToHsv, hsvToHex, normalizeHex, type HSV } from '@/lib/color'
import { cn } from '@/lib/utils'

/** Collection colours (the dot in the collections pane and on chips). '' = no colour. */
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

const clamp = (n: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, n))

/**
 * A colour picked by hand, without a library: a saturation/brightness square and a hue slider (CSS
 * gradients; the pointer or the arrow keys move them), a hex field, a live preview, OK and Cancel.
 */
function CustomColorPopover({
  value,
  onPick,
  children,
}: {
  value: string
  onPick: (hex: string) => void
  children: React.ReactNode
}) {
  const [open, setOpen] = useState(false)
  const [hsv, setHsv] = useState<HSV>(() => hexToHsv(value) ?? { h: 150, s: 70, v: 80 })
  const [hex, setHex] = useState(() => normalizeHex(value) ?? hsvToHex({ h: 150, s: 70, v: 80 }))
  const [hexError, setHexError] = useState('')
  const square = useRef<HTMLDivElement>(null)
  const [wasOpen, setWasOpen] = useState(open)

  if (open !== wasOpen) {
    setWasOpen(open)

    if (open) {
      const start = hexToHsv(value) ?? hsv

      setHsv(start)
      setHex(hsvToHex(start))
      setHexError('')
    }
  }

  function apply(next: HSV) {
    setHsv(next)
    setHex(hsvToHex(next))
    setHexError('')
  }

  function fromPointer(e: React.PointerEvent) {
    const el = square.current

    if (!el) {
      return
    }

    const r = el.getBoundingClientRect()
    const s = clamp(((e.clientX - r.left) / r.width) * 100, 0, 100)
    const v = clamp(100 - ((e.clientY - r.top) / r.height) * 100, 0, 100)

    apply({ ...hsv, s: Math.round(s), v: Math.round(v) })
  }

  function onSquareKey(e: React.KeyboardEvent) {
    const step = e.shiftKey ? 10 : 2
    const by: Record<string, Partial<HSV>> = {
      ArrowLeft: { s: clamp(hsv.s - step, 0, 100) },
      ArrowRight: { s: clamp(hsv.s + step, 0, 100) },
      ArrowUp: { v: clamp(hsv.v + step, 0, 100) },
      ArrowDown: { v: clamp(hsv.v - step, 0, 100) },
    }
    const patch = by[e.key]

    if (patch) {
      e.preventDefault()
      apply({ ...hsv, ...patch })
    }
  }

  function onHexChange(text: string) {
    setHex(text)
    const n = normalizeHex(text)

    if (n) {
      const parsed = hexToHsv(n)

      if (parsed) {
        setHsv(parsed)
      }

      setHexError('')
    } else {
      setHexError('Use a hex colour such as #3b82f6')
    }
  }

  const current = hsvToHex(hsv)
  const hue = hsvToHex({ h: hsv.h, s: 100, v: 100 })

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>{children}</PopoverTrigger>
      <PopoverContent align="start" className="w-[min(100vw-1rem,18rem)] gap-3 p-3" aria-label="Custom colour">
        <div
          ref={square}
          role="slider"
          tabIndex={0}
          aria-label="Saturation and brightness"
          aria-valuetext={`Saturation ${hsv.s}%, brightness ${hsv.v}%`}
          aria-valuenow={hsv.v}
          aria-valuemin={0}
          aria-valuemax={100}
          data-testid="color-square"
          onPointerDown={(e) => {
            e.currentTarget.setPointerCapture(e.pointerId)
            fromPointer(e)
          }}
          onPointerMove={(e) => e.buttons > 0 && fromPointer(e)}
          onKeyDown={onSquareKey}
          className="relative h-36 w-full cursor-crosshair touch-none rounded-md border outline-none focus-visible:ring-2 focus-visible:ring-ring"
          style={{
            background: `linear-gradient(to top, #000, transparent), linear-gradient(to right, #fff, ${hue})`,
          }}
        >
          <span
            aria-hidden
            className="pointer-events-none absolute size-3.5 -translate-x-1/2 translate-y-1/2 rounded-full border-2 border-white shadow-[0_0_0_1px_rgb(0_0_0/0.5)]"
            style={{ left: `${hsv.s}%`, bottom: `${hsv.v}%`, background: current }}
          />
        </div>
        <label className="grid gap-1 text-xs text-muted-foreground">
          Hue
          <input
            type="range"
            min={0}
            max={360}
            value={hsv.h}
            onChange={(e) => apply({ ...hsv, h: Number(e.target.value) })}
            aria-label="Hue"
            data-testid="color-hue"
            className="h-3 w-full cursor-pointer appearance-none rounded-full pointer-coarse:h-10 [&::-webkit-slider-thumb]:size-4 [&::-webkit-slider-thumb]:appearance-none [&::-webkit-slider-thumb]:rounded-full [&::-webkit-slider-thumb]:border-2 [&::-webkit-slider-thumb]:border-white [&::-webkit-slider-thumb]:bg-transparent [&::-webkit-slider-thumb]:shadow-[0_0_0_1px_rgb(0_0_0/0.5)]"
            style={{
              background:
                'linear-gradient(to right, #f00, #ff0, #0f0, #0ff, #00f, #f0f, #f00) center / 100% 12px no-repeat',
            }}
          />
        </label>
        <div className="flex items-center gap-2">
          <span
            aria-hidden
            data-testid="color-preview"
            className="size-9 shrink-0 rounded-md border"
            style={{ background: current }}
          />
          <div className="grid min-w-0 flex-1 gap-1">
            <Input
              value={hex}
              onChange={(e) => onHexChange(e.target.value)}
              aria-label="Hex colour"
              aria-invalid={!!hexError || undefined}
              className="font-mono"
              spellCheck={false}
              autoCapitalize="off"
            />
            {hexError && <p className="text-xs text-destructive">{hexError}</p>}
          </div>
        </div>
        <div className="flex justify-end gap-1.5">
          <Button type="button" size="sm" variant="ghost" onClick={() => setOpen(false)}>
            Cancel
          </Button>
          <Button
            type="button"
            size="sm"
            disabled={!!hexError}
            onClick={() => {
              onPick(current)
              setOpen(false)
            }}
          >
            OK
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  )
}

export function ColorPicker({ value, onChange }: { value: string; onChange: (color: string) => void }) {
  const normalized = normalizeHex(value) ?? ''
  const preset = COLLECTION_COLORS.some((c) => c.value === normalized)
  const custom = !preset && !!normalized

  return (
    <div role="radiogroup" aria-label="Colour" className="flex flex-wrap gap-2">
      {COLLECTION_COLORS.map((c) => {
        const on = normalized === c.value

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
              'flex size-7 items-center justify-center rounded-full border outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background pointer-coarse:size-10',
              on && 'ring-2 ring-foreground/70 ring-offset-2 ring-offset-background',
              !c.value && 'bg-muted-foreground/30',
            )}
            style={c.value ? { background: c.value, borderColor: c.value } : undefined}
          >
            {on && <Check className={cn('size-3.5', c.value ? 'text-white' : 'text-foreground')} />}
          </button>
        )
      })}
      <CustomColorPopover value={custom ? normalized : ''} onPick={onChange}>
        <button
          type="button"
          aria-pressed={custom}
          aria-label={custom ? `Custom colour ${normalized}` : 'Custom colour'}
          title={custom ? `Custom: ${normalized}` : 'Custom colour'}
          data-testid="color-custom"
          className={cn(
            'flex size-7 items-center justify-center rounded-full border outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background pointer-coarse:size-10',
            custom && 'ring-2 ring-foreground/70 ring-offset-2 ring-offset-background',
          )}
          style={
            custom
              ? { background: normalized, borderColor: normalized }
              : // A soft rainbow: pastel hues, so it reads as "any colour" next to the preset swatches.
                {
                  background: 'conic-gradient(#f87171, #fbbf24, #a3e635, #34d399, #38bdf8, #a78bfa, #f472b6, #f87171)',
                  borderColor: 'transparent',
                }
          }
        >
          {custom && <Check className="size-3.5" style={{ color: contrastText(normalized) }} />}
        </button>
      </CustomColorPopover>
    </div>
  )
}
