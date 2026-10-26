'use client'

import { Download, ImageUp, Info } from 'lucide-react'
import { useEffect, useId, useRef, useState, type DragEvent } from 'react'

import { Select } from '@/components/tools/shared'
import { btnPrimary } from '@/lib/site'
import {
  API_SLIDES,
  GRID_SHAPES,
  GRID_VIEW,
  MAX_SLIDES,
  OUT_WIDTH,
  SLIDE_SHAPES,
  carouselPieces,
  cropShare,
  gridPieces,
  outSize,
  pieceName,
  suggestSlides,
  type Piece,
  type Rect,
} from '@/lib/split-image'
import { zip } from '@/lib/zip'

type Picture = { url: string; name: string; w: number; h: number; el: HTMLImageElement }

export type SplitMode = 'grid' | 'carousel'

// The sample shown before anyone picks a picture: a gradient landscape drawn on a canvas, w × h.
function samplePicture(w: number, h: number): Promise<Picture> {
  const canvas = document.createElement('canvas')

  canvas.width = w
  canvas.height = h

  const ctx = canvas.getContext('2d')!
  const sky = ctx.createLinearGradient(0, 0, 0, h)

  sky.addColorStop(0, '#0b3d2a')
  sky.addColorStop(1, '#1ed27b')
  ctx.fillStyle = sky
  ctx.fillRect(0, 0, w, h)
  ctx.fillStyle = 'rgb(255 255 255 / 0.85)'
  ctx.beginPath()
  ctx.arc(w * 0.74, h * 0.3, h * 0.13, 0, Math.PI * 2)
  ctx.fill()
  ctx.fillStyle = '#062a1c'
  ctx.beginPath()
  ctx.moveTo(0, h)

  for (let x = 0; x <= w; x += 40) {
    ctx.lineTo(x, h * 0.72 - Math.sin(x / 300) * h * 0.12 - Math.sin(x / 97) * h * 0.03)
  }

  ctx.lineTo(w, h)
  ctx.fill()

  return new Promise((resolve) => {
    canvas.toBlob((blob) => {
      const url = URL.createObjectURL(blob!)
      const el = new Image()

      el.addEventListener('load', () => resolve({ url, name: '', w, h, el }))
      el.src = url
    }, 'image/png')
  })
}

/** One piece of the picture, drawn in SVG: the viewBox is the piece, the whole picture sits under it. */
function PieceView({ picture, src }: { picture: Picture; src: Rect }) {
  return (
    <svg viewBox={`${src.x} ${src.y} ${src.w} ${src.h}`} className="block size-full" aria-hidden>
      <image href={picture.url} width={picture.w} height={picture.h} preserveAspectRatio="none" />
    </svg>
  )
}

/** A piece at 1080 pixels wide, as a JPEG. */
function render(picture: Picture, src: Rect, ratio: number): Promise<Blob> {
  const { width, height } = outSize(ratio)
  const canvas = document.createElement('canvas')

  canvas.width = width
  canvas.height = height

  const ctx = canvas.getContext('2d')!

  ctx.imageSmoothingQuality = 'high'
  ctx.drawImage(picture.el, src.x, src.y, src.w, src.h, 0, 0, width, height)

  return new Promise((resolve, reject) =>
    canvas.toBlob((b) => (b ? resolve(b) : reject(new Error('no blob'))), 'image/jpeg', 0.92),
  )
}

function save(blob: Blob, name: string) {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')

  a.href = url
  a.download = name
  a.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

const percent = (n: number) => `${Math.round(n * 100)}%`

export function ImageSplitter({ mode }: { mode: SplitMode }) {
  const id = useId()
  const grid = mode === 'grid'
  const shapes = grid ? GRID_SHAPES : SLIDE_SHAPES
  const [picture, setPicture] = useState<Picture | null>(null)
  const [shapeId, setShapeId] = useState(shapes[0]!.id)
  const [count, setCount] = useState(grid ? 2 : 3)
  const [focusX, setFocusX] = useState(50)
  const [focusY, setFocusY] = useState(50)
  const [dragging, setDragging] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const picks = useRef(0)
  const shape = shapes.find((s) => s.id === shapeId) ?? shapes[0]!
  const ratio = shape.w / shape.h
  const prefix = grid ? 'grid' : 'slide'

  // A sample until the first pick; every picture's object URL lives as long as it is shown.
  useEffect(() => {
    let live = true

    void (grid ? samplePicture(2430, 2160) : samplePicture(3240, 1350)).then((p) =>
      live && !picks.current ? setPicture(p) : URL.revokeObjectURL(p.url),
    )

    return () => {
      live = false
    }
  }, [grid])

  useEffect(
    () => () => {
      if (picture) {
        URL.revokeObjectURL(picture.url)
      }
    },
    [picture],
  )

  function load(file: File | undefined) {
    const pick = ++picks.current

    setError('')

    if (!file) {
      return
    }

    if (!file.type.startsWith('image/')) {
      setError('Pick a picture: JPEG, PNG, WebP or HEIC where your browser reads it.')

      return
    }

    const url = URL.createObjectURL(file)
    const el = new Image()

    el.addEventListener('load', () => {
      if (pick !== picks.current) {
        URL.revokeObjectURL(url)

        return
      }

      const p = { url, name: file.name, w: el.naturalWidth, h: el.naturalHeight, el }

      setPicture(p)
      setFocusX(50)
      setFocusY(50)

      if (!grid) {
        setCount(suggestSlides(p.w, p.h, ratio))
      }
    })
    el.addEventListener('error', () => {
      URL.revokeObjectURL(url)
      setError('Your browser could not read this picture.')
    })
    el.src = url
  }

  function onDrop(e: DragEvent) {
    e.preventDefault()
    setDragging(false)
    load(e.dataTransfer.files[0])
  }

  const split = picture
    ? grid
      ? gridPieces(picture.w, picture.h, count, ratio, focusX / 100, focusY / 100)
      : carouselPieces(picture.w, picture.h, count, ratio, focusX / 100, focusY / 100)
    : null
  const pieces = split?.pieces ?? []
  const byOrder = pieces.toSorted((a, b) => a.order - b.order)
  const pieceWidth = pieces[0]?.src.w ?? OUT_WIDTH
  const share = picture && split ? cropShare(picture.w, picture.h, split.crop) : 0
  // Which way the crop has room to move: the slider for the other axis does nothing.
  const slack =
    picture && split ? (split.crop.w < picture.w - 0.5 ? 'x' : split.crop.h < picture.h - 0.5 ? 'y' : '') : ''
  const out = outSize(ratio)

  async function downloadAll() {
    if (!picture) {
      return
    }

    setBusy(true)

    try {
      const files = await Promise.all(
        byOrder.map(async (p) => ({
          name: pieceName(prefix, p.order),
          data: new Uint8Array(await (await render(picture, p.src, ratio)).arrayBuffer()),
        })),
      )

      save(new Blob([zip(files) as Uint8Array<ArrayBuffer>], { type: 'application/zip' }), `instagram-${mode}.zip`)
    } catch {
      setError('Could not make the files; try a smaller picture.')
    } finally {
      setBusy(false)
    }
  }

  async function downloadOne(p: Piece) {
    if (picture) {
      save(await render(picture, p.src, ratio), pieceName(prefix, p.order))
    }
  }

  const label = (p: Piece) => (grid ? `Post ${p.order}` : `Slide ${p.order}`)

  return (
    <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)]">
      <div className="grid gap-4 rounded-2xl border border-border bg-card p-5 sm:p-6 lg:sticky lg:top-24">
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
          <span className="max-w-full truncate text-sm font-medium">
            {picture?.name || (grid ? 'Drop a picture, or pick one' : 'Drop a wide picture, or pick one')}
          </span>
          <span className="text-xs text-muted-foreground">It stays in your browser; nothing is uploaded.</span>
          <input
            id={`${id}-file`}
            type="file"
            accept="image/*"
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
            {grid ? 'Rows' : 'Slides'}
            <Select value={count} onChange={(e) => setCount(Number(e.target.value))}>
              {Array.from({ length: grid ? 5 : MAX_SLIDES - 1 }, (_, i) => (grid ? i + 1 : i + 2)).map((n) => (
                <option key={n} value={n}>
                  {grid ? `${n} ${n === 1 ? 'row' : 'rows'} (${n * 3} posts)` : `${n} slides`}
                </option>
              ))}
            </Select>
          </label>
          <label className="grid gap-1.5 text-sm font-medium">
            {grid ? 'Tile shape' : 'Slide shape'}
            <Select value={shapeId} onChange={(e) => setShapeId(e.target.value)}>
              {shapes.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.label}
                </option>
              ))}
            </Select>
          </label>
        </div>
        <p className="text-xs leading-relaxed text-muted-foreground">{shape.note}</p>

        {picture && (
          <label className="grid gap-1.5 text-sm font-medium">
            {slack === 'y' ? 'Move the crop up or down' : 'Move the crop left or right'}
            <input
              type="range"
              min={0}
              max={100}
              value={slack === 'y' ? focusY : focusX}
              disabled={!slack}
              onChange={(e) => (slack === 'y' ? setFocusY : setFocusX)(Number(e.target.value))}
              className="w-full accent-brand disabled:opacity-40"
            />
            <span className="text-xs font-normal text-muted-foreground" aria-live="polite">
              {share > 0.005
                ? `${percent(share)} of the picture is left out to fit the shape.`
                : 'The whole picture fits; nothing is cropped.'}
            </span>
          </label>
        )}

        {!grid && count > API_SLIDES && (
          <p className="flex gap-2 text-xs leading-relaxed text-amber-300">
            <Info className="mt-px size-3.5 shrink-0" aria-hidden />
            The Instagram app takes up to {MAX_SLIDES} slides, its API {API_SLIDES}: scheduling tools, staqly included,
            post at most {API_SLIDES} in one carousel.
          </p>
        )}

        {picture && pieceWidth < OUT_WIDTH * 0.75 && (
          <p className="flex gap-2 text-xs leading-relaxed text-amber-300">
            <Info className="mt-px size-3.5 shrink-0" aria-hidden />
            Each piece gets {Math.round(pieceWidth)} pixels of your picture and is enlarged to {OUT_WIDTH}; a larger
            picture will look sharper.
          </p>
        )}

        <button
          type="button"
          className={`${btnPrimary} justify-center`}
          disabled={!picture || busy}
          onClick={downloadAll}
        >
          <Download className="size-4" />
          {busy ? 'Making the files...' : `Download all ${pieces.length} (ZIP)`}
        </button>
        <p className="-mt-2 text-xs text-muted-foreground">
          JPEG, {out.width} × {out.height} px each, numbered in the order you post them.
        </p>
      </div>

      <section aria-labelledby={`${id}-preview`} className="grid gap-4">
        <div className="flex flex-wrap items-baseline justify-between gap-2">
          <h2 id={`${id}-preview`} className="text-lg font-semibold tracking-tight">
            {grid ? 'Your profile grid' : 'Your carousel'}
          </h2>
          <p className="text-xs text-muted-foreground">
            {grid
              ? 'As the profile shows it. Post 1 first: it ends up bottom right, the last post top left.'
              : 'One post, slides in this order. Swipe the preview.'}
          </p>
        </div>

        {picture && grid && (
          <ul className="grid grid-cols-3 gap-0.5 overflow-hidden rounded-xl border border-border bg-border">
            {pieces.map((p) => (
              <li key={p.order} className="relative bg-background" style={{ aspectRatio: GRID_VIEW }}>
                <PieceView picture={picture} src={p.view} />
                <PieceButton label={label(p)} onClick={() => downloadOne(p)} />
              </li>
            ))}
          </ul>
        )}

        {picture && !grid && (
          <>
            {/* The whole strip with the cuts, then one slide at a time as the feed shows it. */}
            <ul className="flex overflow-hidden rounded-xl border border-border bg-border" style={{ gap: 2 }}>
              {pieces.map((p) => (
                <li
                  key={p.order}
                  className="relative min-w-0 flex-1"
                  style={{ aspectRatio: `${shape.w} / ${shape.h}` }}
                >
                  <PieceView picture={picture} src={p.src} />
                  <PieceButton label={label(p)} onClick={() => downloadOne(p)} compact={pieces.length > 6} />
                </li>
              ))}
            </ul>
            <div
              role="region"
              aria-label="Carousel preview, one slide at a time"
              // oxlint-disable-next-line jsx-a11y/no-noninteractive-tabindex
              tabIndex={0}
              className="mx-auto flex w-full max-w-xs snap-x snap-mandatory overflow-x-auto rounded-xl border border-border bg-background focus-visible:ring-2 focus-visible:ring-brand focus-visible:outline-none"
            >
              {pieces.map((p) => (
                <div
                  key={p.order}
                  className="relative w-full shrink-0 snap-center"
                  style={{ aspectRatio: `${shape.w} / ${shape.h}` }}
                >
                  <PieceView picture={picture} src={p.src} />
                  <span className="absolute top-2 right-2 rounded-full bg-black/60 px-2 py-0.5 text-xs text-white tabular-nums">
                    {p.order}/{pieces.length}
                  </span>
                </div>
              ))}
            </div>
          </>
        )}
      </section>
    </div>
  )
}

function PieceButton({ label, onClick, compact }: { label: string; onClick: () => void; compact?: boolean }) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={`Download ${label.toLowerCase()}`}
      title={`Download ${label.toLowerCase()}`}
      className="absolute inset-x-1.5 bottom-1.5 inline-flex items-center justify-center gap-1 rounded-lg bg-black/60 px-2 py-1 text-xs font-medium text-white backdrop-blur transition-colors hover:bg-black/80 focus-visible:ring-2 focus-visible:ring-brand focus-visible:outline-none pointer-coarse:py-2"
    >
      <Download className="size-3.5 shrink-0" aria-hidden />
      {!compact && <span className="truncate">{label}</span>}
    </button>
  )
}
