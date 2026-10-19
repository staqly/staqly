'use client'

import { ProviderTile } from '@staqly/brand/providers'
import { ImageUp, Lightbulb, Ratio } from 'lucide-react'
import { useEffect, useId, useState, type DragEvent } from 'react'

import { CopyButton, fieldClass } from '@/components/tools/shared'
import {
  bars,
  closest,
  crop,
  fits,
  PLACEMENTS,
  percent,
  ratioLabel,
  shapeLabel,
  type Placement,
} from '@/lib/aspect-ratio'

type Media = { url: string; kind: 'image' | 'video'; name: string }
type Mode = 'fill' | 'fit'

/** The picture (or a placeholder for a video or typed size) in a placement's frame, drawn in SVG so it scales exactly. */
function Preview({ media, w, h, p, mode }: { media: Media | null; w: number; h: number; p: Placement; mode: Mode }) {
  const target = p.w / p.h
  const ratio = w / h
  const id = useId()
  // Fill: the whole picture, with the part outside the frame dimmed. Fit: the frame, with the picture inside it.
  const frame =
    mode === 'fill'
      ? ratio > target
        ? { x: (w - h * target) / 2, y: 0, fw: h * target, fh: h }
        : { x: 0, y: (h - w / target) / 2, fw: w, fh: w / target }
      : ratio > target
        ? { x: 0, y: 0, fw: w, fh: w / target }
        : { x: 0, y: 0, fw: h * target, fh: h }
  const view = mode === 'fill' ? { vw: w, vh: h } : { vw: frame.fw, vh: frame.fh }
  const img = mode === 'fill' ? { ix: 0, iy: 0 } : { ix: (frame.fw - w) / 2, iy: (frame.fh - h) / 2 }
  const picture =
    media?.kind === 'image' ? (
      <image href={media.url} x={img.ix} y={img.iy} width={w} height={h} preserveAspectRatio="none" />
    ) : (
      <rect x={img.ix} y={img.iy} width={w} height={h} fill={`url(#${id}-g)`} />
    )

  return (
    <svg viewBox={`0 0 ${view.vw} ${view.vh}`} className="size-full" preserveAspectRatio="xMidYMid meet" aria-hidden>
      <defs>
        <linearGradient id={`${id}-g`} x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor="#1ed27b" stopOpacity="0.55" />
          <stop offset="1" stopColor="#0b3d2a" stopOpacity="0.9" />
        </linearGradient>
      </defs>
      {mode === 'fit' && <rect width={view.vw} height={view.vh} fill="#000" />}
      {picture}
      {mode === 'fill' && (
        <path
          d={`M0 0H${w}V${h}H0Z M${frame.x} ${frame.y}v${frame.fh}h${frame.fw}v${-frame.fh}Z`}
          fill="rgb(0 0 0 / 0.68)"
          fillRule="evenodd"
        />
      )}
      <rect
        x={mode === 'fill' ? frame.x : 0}
        y={mode === 'fill' ? frame.y : 0}
        width={frame.fw}
        height={frame.fh}
        fill="none"
        stroke="#63f08e"
        strokeWidth={2}
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  )
}

export function AspectRatioChecker() {
  const id = useId()
  const [width, setWidth] = useState('1920')
  const [height, setHeight] = useState('1080')
  const [media, setMedia] = useState<Media | null>(null)
  const [mode, setMode] = useState<Mode>('fill')
  const [dragging, setDragging] = useState(false)
  const [error, setError] = useState('')
  const w = Number(width)
  const h = Number(height)
  const valid = Number.isFinite(w) && Number.isFinite(h) && w > 0 && h > 0 && w <= 100_000 && h <= 100_000
  const ratio = valid ? w / h : 1
  const near = valid ? closest(ratio, w) : null

  // The object URL lives as long as the file is shown.
  useEffect(
    () => () => {
      if (media) {
        URL.revokeObjectURL(media.url)
      }
    },
    [media],
  )

  function load(file: File | undefined) {
    setError('')

    if (!file) {
      return
    }

    const kind = file.type.startsWith('video/') ? 'video' : file.type.startsWith('image/') ? 'image' : null

    if (!kind) {
      setError('Pick a picture or a video.')

      return
    }

    const url = URL.createObjectURL(file)
    const done = (nw: number, nh: number) => {
      if (!nw || !nh) {
        URL.revokeObjectURL(url)
        setError('Could not read the size of this file.')

        return
      }

      setWidth(String(nw))
      setHeight(String(nh))
      setMedia({ url, kind, name: file.name })
    }

    if (kind === 'image') {
      const el = new Image()

      el.addEventListener('load', () => done(el.naturalWidth, el.naturalHeight))
      el.addEventListener('error', () => done(0, 0))
      el.src = url
    } else {
      const el = document.createElement('video')

      el.preload = 'metadata'
      el.addEventListener('loadedmetadata', () => done(el.videoWidth, el.videoHeight))
      el.addEventListener('error', () => done(0, 0))
      el.src = url
    }
  }

  function onDrop(e: DragEvent) {
    e.preventDefault()
    setDragging(false)
    load(e.dataTransfer.files[0])
  }

  const typed = (set: (v: string) => void) => (v: string) => {
    set(v.replace(/\D/g, ''))
    setMedia(null) // a typed size no longer describes the file
  }

  return (
    <div className="grid gap-6">
      <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
        <div className="grid gap-4 rounded-2xl border border-border bg-card p-5 sm:p-6">
          {/* A real label for the file input: click and keyboard reach the input; dropping a file is a mouse extra. */}
          {/* oxlint-disable-next-line jsx-a11y/no-noninteractive-element-interactions */}
          <label
            htmlFor={`${id}-file`}
            onDragOver={(e) => {
              e.preventDefault()
              setDragging(true)
            }}
            onDragLeave={() => setDragging(false)}
            onDrop={onDrop}
            className={`flex cursor-pointer flex-col items-center justify-center gap-2 rounded-xl border border-dashed px-4 py-8 text-center transition-colors has-focus-visible:ring-2 has-focus-visible:ring-brand ${
              dragging ? 'border-brand bg-brand-soft/30' : 'border-border bg-background hover:border-foreground/30'
            }`}
          >
            <ImageUp className="size-6 text-brand" aria-hidden />
            <span className="text-sm font-medium">{media ? media.name : 'Drop a picture or video, or pick one'}</span>
            <span className="text-xs text-muted-foreground">It stays in your browser; nothing is uploaded.</span>
            <input
              id={`${id}-file`}
              type="file"
              accept="image/*,video/*"
              className="sr-only"
              onChange={(e) => {
                load(e.target.files?.[0])
                e.target.value = ''
              }}
            />
          </label>
          {error && (
            <p className="text-sm text-red-400" role="alert">
              {error}
            </p>
          )}
          <div className="grid grid-cols-2 gap-3">
            <label className="grid gap-1.5 text-sm font-medium">
              Width (px)
              <input
                className={fieldClass}
                inputMode="numeric"
                value={width}
                onChange={(e) => typed(setWidth)(e.target.value)}
              />
            </label>
            <label className="grid gap-1.5 text-sm font-medium">
              Height (px)
              <input
                className={fieldClass}
                inputMode="numeric"
                value={height}
                onChange={(e) => typed(setHeight)(e.target.value)}
              />
            </label>
          </div>
          <fieldset className="grid gap-1.5">
            <legend className="mb-1.5 text-sm font-medium">Preview as</legend>
            <div className="inline-flex w-fit rounded-xl border border-border bg-background p-1">
              {(
                [
                  ['fill', 'Fill the frame (crop)'],
                  ['fit', 'Fit inside (bars)'],
                ] as const
              ).map(([value, label]) => (
                <label
                  key={value}
                  className="cursor-pointer rounded-lg px-3 py-1.5 text-sm text-muted-foreground transition-colors has-checked:bg-card has-checked:text-foreground has-focus-visible:ring-2 has-focus-visible:ring-brand pointer-coarse:py-2.5"
                >
                  <input
                    type="radio"
                    name={`${id}-mode`}
                    value={value}
                    checked={mode === value}
                    onChange={() => setMode(value)}
                    className="sr-only"
                  />
                  {label}
                </label>
              ))}
            </div>
          </fieldset>
        </div>

        <section className="flex flex-col rounded-2xl border border-border bg-card" aria-labelledby={`${id}-ratio`}>
          <header className="flex items-center gap-3 border-b border-border px-5 py-4 sm:px-6">
            <span className="flex size-8 items-center justify-center rounded-lg bg-brand-soft text-brand-strong">
              <Ratio className="size-4" />
            </span>
            <div className="min-w-0">
              <h2 id={`${id}-ratio`} className="font-semibold tracking-tight">
                Your aspect ratio
              </h2>
              <p className="truncate text-xs text-muted-foreground">
                {valid
                  ? `${w.toLocaleString('en-US')} × ${h.toLocaleString('en-US')} px`
                  : 'Enter a width and a height'}
              </p>
            </div>
          </header>
          <div className="grid gap-4 px-5 py-5 sm:px-6" aria-live="polite">
            {valid ? (
              <>
                <div className="flex flex-wrap items-baseline gap-x-4 gap-y-1">
                  <span className="font-mono text-4xl font-semibold tracking-tight tabular-nums">
                    {ratioLabel(w, h)}
                  </span>
                  <span className="font-mono text-sm text-muted-foreground tabular-nums">{ratio.toFixed(3)}</span>
                  <span className="text-sm text-muted-foreground">
                    {fits(ratio, 1) ? 'Square' : ratio > 1 ? 'Landscape' : 'Portrait'}
                  </span>
                </div>
                {near && (
                  <div className="relative rounded-xl border border-border bg-background p-4 pr-12">
                    <p className="flex items-center gap-2 text-sm font-medium">
                      <Lightbulb className="size-4 text-brand" aria-hidden />
                      {fits(ratio, near.shape.w / near.shape.h)
                        ? `Already a common ratio: ${shapeLabel(near.shape)}`
                        : `Closest common ratio: ${shapeLabel(near.shape)}`}
                    </p>
                    <p className="mt-1 text-sm text-muted-foreground">
                      At the same width:{' '}
                      <span className="font-mono text-foreground tabular-nums">
                        {near.width} × {near.height}
                      </span>{' '}
                      px.
                    </p>
                    <CopyButton
                      text={`${near.width}x${near.height}`}
                      label="Copy the size"
                      className="absolute top-2 right-2"
                    />
                  </div>
                )}
              </>
            ) : (
              <p className="text-sm text-muted-foreground">Both numbers, above zero.</p>
            )}
          </div>
        </section>
      </div>

      {valid && (
        <section aria-labelledby={`${id}-places`} className="grid gap-3">
          <div className="flex flex-wrap items-baseline justify-between gap-2">
            <h2 id={`${id}-places`} className="text-lg font-semibold tracking-tight">
              Per platform
            </h2>
            <p className="text-xs text-muted-foreground">Common ratios; platforms change their specs now and then.</p>
          </div>
          <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {PLACEMENTS.map((p) => {
              const target = p.w / p.h
              const ok = fits(ratio, target)
              const c = crop(ratio, target)
              const b = bars(ratio, target)
              const verdict = ok
                ? 'Fits as it is'
                : mode === 'fill'
                  ? `Crops ${percent(c.share)} from the ${c.from}`
                  : `Adds bars: ${percent(b.share)} at the ${b.where}`

              return (
                <li key={p.id} className="flex flex-col overflow-hidden rounded-xl border border-border bg-card">
                  <div className="flex items-center gap-2.5 px-3 py-3 sm:px-3.5">
                    <ProviderTile id={p.provider} name={p.name} className="size-7 shrink-0 rounded-lg" />
                    <div className="min-w-0">
                      <p className="truncate text-sm font-medium">{p.name}</p>
                      <p className="text-xs text-muted-foreground">Common ratio {shapeLabel(p)}</p>
                    </div>
                  </div>
                  <div
                    role="img"
                    aria-label={`${media?.kind === 'image' ? 'Your picture' : 'A placeholder'} in ${p.name}: ${verdict.toLowerCase()}`}
                    className="flex h-32 items-center justify-center bg-background p-2 sm:h-44 sm:p-3"
                  >
                    <Preview media={media} w={w} h={h} p={p} mode={mode} />
                  </div>
                  <p
                    className={`border-t border-border px-3.5 py-2.5 text-xs font-medium ${ok ? 'text-brand-strong' : 'text-amber-300'}`}
                  >
                    {verdict}
                  </p>
                </li>
              )
            })}
          </ul>
        </section>
      )}
    </div>
  )
}
