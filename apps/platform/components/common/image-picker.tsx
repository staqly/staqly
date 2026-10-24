'use client'

import { ImageUp, Trash2 } from 'lucide-react'
import { useId, useRef, type ReactNode } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { imageToDataUrl } from '@/lib/image-upload'
import { safeImageSrc } from '@/lib/safe'

/**
 * A logo or picture: the current one (or `fallback`), Upload / Replace and Remove. The file is scaled to a small
 * square in the browser and handed to `onChange` as a data: URL; Remove hands it ''.
 */
export function ImagePicker({
  label,
  value,
  fallback,
  pending,
  onChange,
  hint,
}: {
  label: string
  value: string
  fallback: ReactNode
  pending: boolean
  onChange: (dataUrl: string) => void
  hint?: string
}) {
  const input = useRef<HTMLInputElement>(null)
  const hintId = useId()
  const src = safeImageSrc(value)

  return (
    <div className="flex flex-wrap items-center gap-3">
      <span className="flex size-12 shrink-0 items-center justify-center overflow-hidden rounded-xl border bg-muted">
        {src ? <img src={src} alt="" className="size-full object-cover" /> : fallback}
      </span>
      <div className="grid gap-1">
        <div className="flex flex-wrap gap-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={pending}
            onClick={() => input.current?.click()}
            aria-describedby={hint ? hintId : undefined}
          >
            <ImageUp /> {src ? `Replace ${label}` : `Upload ${label}`}
          </Button>
          {src && (
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="text-destructive hover:text-destructive"
              aria-label={`Remove ${label}`}
              tooltip={`Remove ${label}`}
              disabled={pending}
              onClick={() => onChange('')}
            >
              <Trash2 />
            </Button>
          )}
        </div>
        {hint && (
          <p id={hintId} className="text-xs text-muted-foreground">
            {hint}
          </p>
        )}
      </div>
      <input
        ref={input}
        type="file"
        accept="image/png,image/jpeg,image/webp,image/gif"
        className="hidden"
        tabIndex={-1}
        aria-hidden
        onChange={(e) => {
          const file = e.target.files?.[0]

          e.target.value = ''

          if (file) {
            imageToDataUrl(file).then(onChange, () =>
              toast.error('Could not read that image', { description: 'Pick a PNG, JPEG, WebP or GIF file.' }),
            )
          }
        }}
      />
    </div>
  )
}
