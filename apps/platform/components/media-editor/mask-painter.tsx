'use client'

import { useRef, useState } from 'react'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

import { BlobImage } from './blob-image'
import { maskFrom } from './raster'

/** Where the pointer is on the canvas, in the picture's pixels (k: pixels per screen pixel). */
function at(e: React.PointerEvent<HTMLCanvasElement>) {
  const c = e.currentTarget
  const r = c.getBoundingClientRect()

  return {
    x: ((e.clientX - r.left) / r.width) * c.width,
    y: ((e.clientY - r.top) / r.height) * c.height,
    k: c.width / r.width,
  }
}

/**
 * Marks what the AI should remove: the picture with a brush on top (pointer, pen or touch). The painted area
 * becomes the request's mask, at the picture's own size.
 */
export function MaskPainter({
  picture,
  prompt,
  onPrompt,
  onCancel,
  onDone,
}: {
  picture: Blob
  prompt: string
  onPrompt: (v: string) => void
  onCancel: () => void
  onDone: (mask: Blob, width: number, height: number) => void
}) {
  const [size, setSize] = useState<{ w: number; h: number } | null>(null)
  const [brush, setBrush] = useState(40)
  const [painted, setPainted] = useState(false)
  const paint = useRef<HTMLCanvasElement>(null)
  const drawing = useRef(false)

  const dot = (e: React.PointerEvent<HTMLCanvasElement>) => {
    const ctx = e.currentTarget.getContext('2d')
    const p = at(e)

    if (!ctx) {
      return
    }

    ctx.fillStyle = 'rgba(239, 68, 68, 0.55)'
    ctx.beginPath()
    ctx.arc(p.x, p.y, (brush / 2) * p.k, 0, Math.PI * 2)
    ctx.fill()
    setPainted(true)
  }

  return (
    <Dialog open onOpenChange={(o) => !o && onCancel()}>
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>Mark what to remove</DialogTitle>
          <DialogDescription>
            Paint over the object. The AI fills in what was behind it as a new layer.
          </DialogDescription>
        </DialogHeader>
        <div className="relative mx-auto max-h-[60vh] w-fit overflow-hidden rounded-lg bg-muted">
          <BlobImage
            blob={picture}
            alt="What to mark"
            className="block max-h-[60vh] max-w-full"
            onLoad={(e) => setSize({ w: e.currentTarget.naturalWidth, h: e.currentTarget.naturalHeight })}
          />
          {size && (
            <canvas
              ref={paint}
              width={size.w}
              height={size.h}
              aria-label="Paint over what to remove"
              className="absolute inset-0 h-full w-full cursor-crosshair touch-none"
              onPointerDown={(e) => {
                drawing.current = true
                e.currentTarget.setPointerCapture(e.pointerId)
                dot(e)
              }}
              onPointerMove={(e) => drawing.current && dot(e)}
              onPointerUp={() => (drawing.current = false)}
            />
          )}
        </div>
        <div className="flex flex-wrap items-center gap-3 text-sm">
          <Label htmlFor="mask-brush">Brush</Label>
          <input
            id="mask-brush"
            type="range"
            min={8}
            max={160}
            value={brush}
            onChange={(e) => setBrush(Number(e.target.value))}
            className="w-32"
          />
          <Button
            size="sm"
            variant="ghost"
            disabled={!painted}
            onClick={() => {
              const c = paint.current

              c?.getContext('2d')?.clearRect(0, 0, c.width, c.height)
              setPainted(false)
            }}
          >
            Clear
          </Button>
          <Input
            aria-label="What fills the area (optional)"
            placeholder="What fills the area (optional), e.g. grass"
            value={prompt}
            onChange={(e) => onPrompt(e.target.value)}
            className="min-w-48 flex-1"
          />
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onCancel}>
            Cancel
          </Button>
          <Button
            disabled={!painted || !size}
            onClick={async () => {
              if (paint.current && size) {
                onDone(await maskFrom(paint.current, size.w, size.h), size.w, size.h)
              }
            }}
          >
            Remove
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
