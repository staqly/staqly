'use client'

import { Brush, RotateCcw } from 'lucide-react'
import { useEffect, useRef, useState, type PointerEvent } from 'react'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

/** Brush sizes, as a share of the picture's long side. */
const BRUSHES = [
  { label: 'Small', share: 0.02 },
  { label: 'Medium', share: 0.05 },
  { label: 'Large', share: 0.1 },
]

/**
 * Paint the area AI may change on a picture. The mask is a PNG of the picture's own size: white where it was
 * painted, black elsewhere (what the AI service reads).
 */
export function MaskDialog({
  open,
  onOpenChange,
  picture,
  title,
  onDone,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** The picture as it is sent (its pixels set the mask's size). */
  picture: { blob: Blob; width: number; height: number } | null
  title: string
  onDone: (mask: Blob) => void
}) {
  const backdrop = useRef<HTMLCanvasElement | null>(null)
  const shown = useRef<HTMLCanvasElement | null>(null)
  const mask = useRef<HTMLCanvasElement | null>(null)
  const last = useRef<{ x: number; y: number } | null>(null)
  const [brush, setBrush] = useState(1)
  const [painted, setPainted] = useState(false)
  const w = picture?.width ?? 1
  const h = picture?.height ?? 1
  const blob = picture?.blob

  // The picture is drawn on a canvas from its pixels (no blob: URL in an <img>, which code scanning treats as
  // DOM text reinterpreted as HTML).
  useEffect(() => {
    if (!blob || typeof createImageBitmap !== 'function') {
      return
    }

    let gone = false

    void createImageBitmap(blob)
      .then((bmp) => {
        const c = backdrop.current?.getContext('2d')

        if (!gone && c) {
          c.drawImage(bmp, 0, 0, c.canvas.width, c.canvas.height)
        }

        bmp.close()
      })
      .catch(() => undefined)

    return () => {
      gone = true
    }
  }, [blob])

  // The mask starts black, the picture's size (the dialog mounts fresh for every painting).
  const maskCanvas = () => {
    if (!mask.current) {
      const c = document.createElement('canvas')

      c.width = w
      c.height = h

      const g = c.getContext('2d')

      if (g) {
        g.fillStyle = '#000'
        g.fillRect(0, 0, w, h)
      }

      mask.current = c
    }

    return mask.current
  }

  const at = (e: PointerEvent<HTMLCanvasElement>) => {
    const r = e.currentTarget.getBoundingClientRect()

    return { x: ((e.clientX - r.left) / r.width) * w, y: ((e.clientY - r.top) / r.height) * h }
  }

  const stroke = (from: { x: number; y: number }, to: { x: number; y: number }) => {
    const size = BRUSHES[brush]!.share * Math.max(w, h)

    // On screen in the brand colour, see-through; in the mask white.
    const brand = getComputedStyle(document.documentElement).getPropertyValue('--brand').trim() || '#888'

    for (const [canvas, color, alpha] of [
      [shown.current, brand, 0.5],
      [maskCanvas(), '#fff', 1],
    ] as const) {
      const c = canvas?.getContext('2d')

      if (!c) {
        continue
      }

      c.globalAlpha = alpha
      c.strokeStyle = color
      c.lineWidth = size
      c.lineCap = 'round'
      c.lineJoin = 'round'
      c.beginPath()
      c.moveTo(from.x, from.y)
      c.lineTo(to.x + 0.01, to.y)
      c.stroke()
    }

    setPainted(true)
  }

  const clear = () => {
    shown.current?.getContext('2d')?.clearRect(0, 0, w, h)

    const c = maskCanvas().getContext('2d')

    if (c) {
      c.fillStyle = '#000'
      c.fillRect(0, 0, w, h)
    }

    setPainted(false)
  }

  const done = () => {
    maskCanvas().toBlob((b) => {
      if (b) {
        onDone(b)
        onOpenChange(false)
      }
    }, 'image/png')
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl" data-testid="mask-dialog">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>Paint over the area with your finger or the mouse.</DialogDescription>
        </DialogHeader>
        {picture && (
          <div
            className="relative mx-auto overflow-hidden rounded-md border bg-muted"
            style={{ aspectRatio: `${w} / ${h}`, width: `min(100%, calc(60vh * ${w / h}))` }}
          >
            <canvas ref={backdrop} width={w} height={h} aria-hidden className="absolute inset-0 size-full" />
            <canvas
              ref={shown}
              width={w}
              height={h}
              aria-label="Paint the area here"
              className="absolute inset-0 size-full cursor-crosshair touch-none"
              onPointerDown={(e) => {
                e.currentTarget.setPointerCapture(e.pointerId)
                last.current = at(e)
                stroke(last.current, last.current)
              }}
              onPointerMove={(e) => {
                if (last.current) {
                  const p = at(e)

                  stroke(last.current, p)
                  last.current = p
                }
              }}
              onPointerUp={() => (last.current = null)}
              onPointerCancel={() => (last.current = null)}
            />
          </div>
        )}
        <div className="flex flex-wrap items-center gap-2">
          <span className="flex items-center gap-1.5 text-sm text-muted-foreground">
            <Brush className="size-4" /> Brush
          </span>
          <div role="radiogroup" aria-label="Brush size" className="flex gap-1">
            {BRUSHES.map((b, i) => (
              <Button
                key={b.label}
                type="button"
                size="sm"
                variant={brush === i ? 'secondary' : 'ghost'}
                role="radio"
                aria-checked={brush === i}
                onClick={() => setBrush(i)}
              >
                {b.label}
              </Button>
            ))}
          </div>
          <Button type="button" size="sm" variant="ghost" className="ml-auto" onClick={clear} disabled={!painted}>
            <RotateCcw /> Clear
          </Button>
        </div>
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button type="button" onClick={done} disabled={!painted}>
            Use this area
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
