'use client'

import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import {
  Eye,
  EyeOff,
  FilePlus2,
  Files,
  GripVertical,
  ImageIcon,
  Layers,
  LayoutTemplate,
  Loader2,
  Lock,
  MoreHorizontal,
  PaintBucket,
  Plus,
  Scaling,
  Search,
  Shapes,
  Sparkles,
  Stamp,
  Type,
  Unlock,
  Upload,
  X,
} from 'lucide-react'
import { useEffect, useRef, useState, type DragEvent, type ReactNode } from 'react'
import { toast } from 'sonner'

import { Callout } from '@/components/common/callout'
import { ColorInput } from '@/components/common/color-input'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { api, errorMessage, mediaThumbUrl, mediaUrl, type Media, type StockPhoto } from '@/lib/api'
import { cn } from '@/lib/utils'

import { AiPanel } from './ai-panel'
import { BrandPanel, useBrand } from './brand'
import { DesignView, dragged } from './canvas'
import {
  newImage,
  newPage,
  newShape,
  newText,
  resize,
  SIZES,
  type Design,
  type El,
  type Fill,
  type ImageEl,
  type Page,
  type ShapeKind,
  withGroups,
} from './doc'
import { loadFont } from './fonts'
import { IconsAndStickers } from './stickers'
import type { DesignStore } from './store'
import { templates } from './templates'

export type PanelId = 'templates' | 'brand' | 'text' | 'photos' | 'elements' | 'background' | 'ai' | 'layers' | 'size'

export const PANELS: { id: PanelId; label: string; icon: ReactNode }[] = [
  { id: 'templates', label: 'Templates', icon: <LayoutTemplate /> },
  { id: 'brand', label: 'Brand', icon: <Stamp /> },
  { id: 'text', label: 'Text', icon: <Type /> },
  { id: 'photos', label: 'Photos', icon: <ImageIcon /> },
  { id: 'elements', label: 'Elements', icon: <Shapes /> },
  { id: 'background', label: 'Background', icon: <PaintBucket /> },
  { id: 'ai', label: 'AI', icon: <Sparkles /> },
  { id: 'layers', label: 'Layers', icon: <Layers /> },
  { id: 'size', label: 'Size', icon: <Scaling /> },
]

export interface PanelCtx {
  store: DesignStore
  /** The page on screen, and changes to it (one undo step, or one per control in a row with coalesce). */
  page: Page
  updatePage: (fn: (p: Page) => void, coalesce?: string) => void
  selected: string[]
  onSelect: (ids: string[]) => void
  /** Adds elements on top and selects them. */
  add: (...els: El[]) => void
  /** Shows another page. */
  showPage: (id: string) => void
}

/** Moves elements of the page on screen to another page (on top of it), or to a new page after this one. */
export function moveToPage(ctx: PanelCtx, ids: string[], target: string | null) {
  const fresh = target ? null : newPage()
  const to = target ?? fresh!.id

  ctx.store.update((d) => {
    const from = d.pages.find((p) => p.id === ctx.page.id)!
    const moving = from.elements.filter((e) => ids.includes(e.id))

    from.elements = from.elements.filter((e) => !ids.includes(e.id))

    if (fresh) {
      d.pages.splice(d.pages.indexOf(from) + 1, 0, { ...fresh, background: structuredClone(from.background) })
    }

    d.pages.find((p) => p.id === to)!.elements.push(...moving)
  })
  ctx.showPage(to)
  ctx.onSelect(ids)
}

/** The menu items of "Move to page": every other page, and a new one. */
export function MoveToPageItems({ ctx, ids }: { ctx: PanelCtx; ids: string[] }) {
  const pages = ctx.store.design.pages

  return (
    <>
      {pages.map((p, i) =>
        p.id === ctx.page.id ? null : (
          <DropdownMenuItem key={p.id} onSelect={() => moveToPage(ctx, ids, p.id)}>
            <Files /> To page {i + 1}
          </DropdownMenuItem>
        ),
      )}
      <DropdownMenuItem onSelect={() => moveToPage(ctx, ids, null)}>
        <FilePlus2 /> To a new page
      </DropdownMenuItem>
    </>
  )
}

/** Seconds since it was shown, ticking: something slow is still running. */
export function Elapsed() {
  const [seconds, setSeconds] = useState(0)

  useEffect(() => {
    const t = window.setInterval(() => setSeconds((n) => n + 1), 1000)

    return () => window.clearInterval(t)
  }, [])

  return <span className="tabular-nums">{seconds} s</span>
}

/** A side panel tool that can also be dragged onto the page (dropped there instead of in the middle). */
export function dragProps(make: () => El | El[]) {
  return {
    draggable: true,
    onDragStart: (e: DragEvent) => {
      e.dataTransfer.effectAllowed = 'copy'
      e.dataTransfer.setData('text/plain', '')
      dragged.make = make
    },
    onDragEnd: () => {
      dragged.make = null
    },
  }
}

export function Section({ title, children, action }: { title: string; children: ReactNode; action?: ReactNode }) {
  return (
    <section className="flex flex-col gap-2">
      <div className="flex items-center justify-between">
        <h3 className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{title}</h3>
        {action}
      </div>
      {children}
    </section>
  )
}

/** The size of a picture file, without drawing it. */
export function measure(src: string): Promise<{ w: number; h: number }> {
  return new Promise((resolve, reject) => {
    const i = new window.Image()

    i.addEventListener('load', () => resolve({ w: i.naturalWidth, h: i.naturalHeight }))
    i.addEventListener('error', () => reject(new Error('The picture did not load')))
    i.src = src
  })
}

/** Puts a file into the library (so the design can be opened again anywhere) and on the page. */
export async function addFile(ctx: PanelCtx, file: Blob, name: string) {
  const media = await api.uploadMedia(file, name)
  const src = mediaUrl(media.id)
  const { w, h } = media.width && media.height ? { w: media.width, h: media.height } : await measure(src)

  ctx.add(newImage(ctx.store.design, src, w, h, { mediaId: media.id }))
}

export function Panel({ id, ctx }: { id: PanelId; ctx: PanelCtx }) {
  switch (id) {
    case 'templates':
      return <TemplatesPanel ctx={ctx} />
    case 'text':
      return <TextPanel ctx={ctx} />
    case 'photos':
      return <PhotosPanel ctx={ctx} />
    case 'elements':
      return <ElementsPanel ctx={ctx} />
    case 'background':
      return <BackgroundPanel ctx={ctx} />
    case 'ai':
      return <AiPanel ctx={ctx} />
    case 'layers':
      return <LayersPanel ctx={ctx} />
    case 'size':
      return <SizePanel ctx={ctx} />
    case 'brand':
      return <BrandPanel ctx={ctx} />
  }
}

function Preview({ design, onClick, label }: { design: Design; onClick: () => void; label: string }) {
  // Every template in a square of the same size, its first page fitted in the middle.
  const fit = 112
  const n = design.pages.length

  return (
    <button type="button" onClick={onClick} aria-label={label} className="group flex flex-col gap-1.5 text-left">
      <span className="relative flex aspect-square items-center justify-center rounded-lg border bg-muted/40 transition group-hover:border-brand group-hover:ring-2 group-hover:ring-brand">
        <span className="overflow-hidden rounded-sm shadow-md">
          <DesignView design={design} page={design.pages[0]!} scale={fit / Math.max(design.width, design.height)} />
        </span>
        {n > 1 && (
          <span className="absolute right-1.5 bottom-1.5 rounded bg-background/90 px-1.5 py-0.5 text-[10px] font-medium shadow-xs">
            {n} pages
          </span>
        )}
      </span>
      <span className="truncate text-xs text-muted-foreground">{label}</span>
    </button>
  )
}

function TemplatesPanel({ ctx }: { ctx: PanelCtx }) {
  const [list] = useState(templates)
  // The previews draw with the templates' fonts: draw them again once those have loaded.
  const [fonts, setFonts] = useState(0)

  useEffect(() => {
    const families = new Set(
      list.flatMap((d) =>
        d.pages[0]!.elements.flatMap((e) => (e.type === 'text' ? [`${e.fontFamily}|${e.bold}`] : [])),
      ),
    )

    void Promise.all([...families].map((f) => loadFont(f.split('|')[0]!, f.endsWith('true')))).then(() =>
      setFonts((n) => n + 1),
    )
  }, [list])
  const mine = useQuery({
    queryKey: ['design-projects'],
    queryFn: async () => {
      // Projects of both editors share the endpoint; only the design editor's are JSON.
      const all = (await api.mediaProjects()).data.slice(0, 12)
      const files = await Promise.all(
        all.map((p) =>
          api
            .mediaProjectFile(p.id)
            .then((b) => b.text())
            .catch(() => ''),
        ),
      )

      // Designs only: the video and sound editor's timelines are projects too (they have a kind).
      return all.flatMap((p, i) => (files[i]?.startsWith('{') && !/"kind":"(video|audio)"/.test(files[i]!) ? [p] : []))
    },
  })

  const apply = (d: Design) => {
    const had = ctx.store.design.pages.some((p) => p.elements.length > 0)

    ctx.store.replace({ ...d, name: ctx.store.design.name === 'Untitled design' ? d.name : ctx.store.design.name })
    ctx.onSelect([])

    if (had) {
      toast.success('Template applied', { action: { label: 'Undo', onClick: ctx.store.undo } })
    }
  }

  return (
    <div className="flex flex-col gap-5">
      {!!mine.data?.length && (
        <Section title="Your designs">
          <div className="grid grid-cols-2 gap-3">
            {mine.data.map((p) => (
              <a key={p.id} href={`/media/edit?project=${encodeURIComponent(p.id)}`} className="flex flex-col gap-1">
                <span className="flex aspect-square items-center justify-center overflow-hidden rounded-lg border bg-muted/40 p-3">
                  {p.output_id ? (
                    <img
                      src={mediaThumbUrl(p.output_id, 320)}
                      alt=""
                      className="max-h-full max-w-full rounded-sm object-contain shadow-md"
                    />
                  ) : (
                    <LayoutTemplate className="text-muted-foreground" />
                  )}
                </span>
                <span className="truncate text-xs text-muted-foreground">{p.name}</span>
              </a>
            ))}
          </div>
        </Section>
      )}
      <Section title="Templates">
        <div className="grid grid-cols-2 gap-3">
          {list.map((d) => (
            <Preview
              // Drawn anew (remounted) once the fonts are in: Konva draws only when its props change.
              key={`${d.name}-${fonts}`}
              design={d}
              label={d.name}
              onClick={() => apply(templates().find((x) => x.name === d.name)!)}
            />
          ))}
        </div>
      </Section>
    </div>
  )
}

const TEXT_STYLES: { label: string; font: string; bold: boolean; italic?: boolean; spacing?: number }[] = [
  { label: 'BOLD HEADLINE', font: 'Anton', bold: false, spacing: 2 },
  { label: 'Elegant title', font: 'Playfair Display', bold: true },
  { label: 'Modern', font: 'Space Grotesk', bold: true },
  { label: 'POSTER', font: 'Bebas Neue', bold: false, spacing: 6 },
  { label: 'Handwritten', font: 'Caveat', bold: true },
  { label: 'Retro', font: 'Lobster', bold: false },
]

function TextPanel({ ctx }: { ctx: PanelCtx }) {
  const d = ctx.store.design
  const styled = (s: (typeof TEXT_STYLES)[number]) => ({
    ...newText(d, 'heading'),
    text: s.label,
    fontFamily: s.font,
    bold: s.bold,
    letterSpacing: s.spacing ?? 0,
  })

  return (
    <div className="flex flex-col gap-5">
      <Section title="Add text">
        <Button
          variant="secondary"
          className="h-12 justify-start text-xl font-bold"
          onClick={() => ctx.add(newText(d, 'heading'))}
          {...dragProps(() => newText(d, 'heading'))}
        >
          Add a heading
        </Button>
        <Button
          variant="secondary"
          className="h-10 justify-start text-base font-semibold"
          onClick={() => ctx.add(newText(d, 'subheading'))}
          {...dragProps(() => newText(d, 'subheading'))}
        >
          Add a subheading
        </Button>
        <Button
          variant="secondary"
          className="justify-start"
          onClick={() => ctx.add(newText(d, 'body'))}
          {...dragProps(() => newText(d, 'body'))}
        >
          Add body text
        </Button>
      </Section>
      <Section title="Styles">
        <div className="grid grid-cols-2 gap-2">
          {TEXT_STYLES.map((s) => (
            <button
              key={s.label}
              type="button"
              className="flex h-16 items-center justify-center rounded-lg border bg-background px-2 text-lg hover:ring-2 hover:ring-brand"
              style={{
                fontFamily: `"${s.font}"`,
                fontWeight: s.bold ? 700 : 400,
                letterSpacing: s.spacing ? s.spacing / 2 : 0,
              }}
              onClick={() => ctx.add(styled(s))}
              {...dragProps(() => styled(s))}
            >
              {s.label}
            </button>
          ))}
        </div>
      </Section>
    </div>
  )
}

function UploadButton({ ctx }: { ctx: PanelCtx }) {
  const input = useRef<HTMLInputElement>(null)
  const [busy, setBusy] = useState(false)

  return (
    <>
      <input
        ref={input}
        type="file"
        accept="image/png,image/jpeg,image/webp,image/gif,image/svg+xml"
        multiple
        hidden
        onChange={async (e) => {
          const files = [...(e.target.files ?? [])]

          e.target.value = ''
          setBusy(true)

          for (const f of files) {
            await addFile(ctx, f, f.name).catch((err) =>
              toast.error(`Could not add ${f.name}`, { description: errorMessage(err) }),
            )
          }

          setBusy(false)
        }}
      />
      <Button className="w-full" onClick={() => input.current?.click()} disabled={busy}>
        {busy ? <Loader2 className="animate-spin" /> : <Upload />} Upload pictures
      </Button>
    </>
  )
}

function PhotosPanel({ ctx }: { ctx: PanelCtx }) {
  const [source, setSource] = useState<'library' | 'stock'>('library')
  const [q, setQ] = useState('')
  const [query, setQuery] = useState('')
  const stockOn = useQuery({ queryKey: ['stock', 'status'], queryFn: () => api.stockPhotos('', 1), staleTime: 60_000 })
  const hasStock = !!stockOn.data?.configured

  return (
    <div className="flex flex-col gap-4">
      <UploadButton ctx={ctx} />
      {hasStock && (
        <div className="grid grid-cols-2 rounded-lg bg-muted p-0.5 text-sm" role="tablist">
          {(['library', 'stock'] as const).map((s) => (
            <button
              key={s}
              type="button"
              role="tab"
              aria-selected={source === s}
              onClick={() => setSource(s)}
              className={cn('h-7 rounded-md', source === s && 'bg-background shadow-xs')}
            >
              {s === 'library' ? 'Your library' : 'Unsplash'}
            </button>
          ))}
        </div>
      )}
      <form
        className="relative"
        onSubmit={(e) => {
          e.preventDefault()
          setQuery(q.trim())
        }}
      >
        <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          aria-label="Search photos"
          placeholder={source === 'stock' ? 'Search Unsplash' : 'Search your library'}
          value={q}
          onChange={(e) => setQ(e.target.value)}
          className="pl-8"
        />
      </form>
      {source === 'stock' && hasStock ? <StockGrid ctx={ctx} query={query} /> : <LibraryGrid ctx={ctx} query={query} />}
    </div>
  )
}

function LibraryGrid({ ctx, query }: { ctx: PanelCtx; query: string }) {
  const lib = useInfiniteQuery({
    queryKey: ['design-library', query],
    queryFn: ({ pageParam, signal }) =>
      api.mediaLibrary({ kind: 'image', q: query || undefined, offset: pageParam, limit: 40 }, signal),
    initialPageParam: 0,
    getNextPageParam: (p) => p.next_offset,
  })
  const items = lib.data?.pages.flatMap((p) => p.data) ?? []

  if (lib.isError) {
    return <Callout tone="danger">{errorMessage(lib.error)}</Callout>
  }

  if (!lib.isPending && !items.length) {
    return (
      <p className="text-sm text-muted-foreground">{query ? 'No pictures match.' : 'No pictures yet. Upload one.'}</p>
    )
  }

  return (
    <>
      <div className="columns-2 gap-2 [&>*]:mb-2">
        {items.map((m) => (
          <button
            key={m.id}
            type="button"
            aria-label={`Add ${m.original_name}`}
            className="block w-full overflow-hidden rounded-md border hover:ring-2 hover:ring-brand"
            {...dragProps(() =>
              newImage(ctx.store.design, mediaUrl(m.id), m.width || 1000, m.height || 1000, { mediaId: m.id }),
            )}
            onClick={() =>
              ctx.add(newImage(ctx.store.design, mediaUrl(m.id), m.width || 1000, m.height || 1000, { mediaId: m.id }))
            }
          >
            <img
              draggable={false}
              src={mediaThumbUrl(m.id, 320)}
              alt={m.alt_text || m.original_name}
              loading="lazy"
              className="w-full"
              style={{ aspectRatio: m.width && m.height ? `${m.width}/${m.height}` : undefined }}
            />
          </button>
        ))}
      </div>
      {lib.hasNextPage && (
        <Button variant="outline" onClick={() => void lib.fetchNextPage()} disabled={lib.isFetchingNextPage}>
          Load more
        </Button>
      )}
    </>
  )
}

function StockGrid({ ctx, query }: { ctx: PanelCtx; query: string }) {
  const stock = useInfiniteQuery({
    queryKey: ['stock', query],
    queryFn: ({ pageParam, signal }) => api.stockPhotos(query, pageParam, signal),
    initialPageParam: 1,
    getNextPageParam: (p, all) => (p.data.length === 30 ? all.length + 1 : undefined),
  })
  const items = stock.data?.pages.flatMap((p) => p.data) ?? []

  // Unsplash counts a photo as used when it lands on a design (clicked or dropped).
  const place = (p: StockPhoto) => {
    const k = Math.min(1, 2000 / p.width)

    void api.stockPhotoUsed(p.id).catch(() => undefined)

    return newImage(ctx.store.design, p.url, p.width * k, p.height * k, {
      stock: { id: p.id, author: p.author, url: p.author_url },
    })
  }

  if (stock.isError) {
    return <Callout tone="danger">{errorMessage(stock.error)}</Callout>
  }

  return (
    <>
      <div className="columns-2 gap-2 [&>*]:mb-2">
        {items.map((p) => (
          <div key={p.id} className="group relative">
            <button
              type="button"
              aria-label={`Add ${p.alt || 'photo'} by ${p.author}`}
              className="block w-full overflow-hidden rounded-md hover:ring-2 hover:ring-brand"
              onClick={() => ctx.add(place(p))}
              {...dragProps(() => place(p))}
            >
              <img
                draggable={false}
                src={p.thumb}
                alt={p.alt}
                loading="lazy"
                className="w-full"
                style={{ aspectRatio: `${p.width}/${p.height}` }}
              />
            </button>
            <a
              href={p.author_url}
              target="_blank"
              rel="noopener noreferrer"
              className="absolute inset-x-0 bottom-0 truncate rounded-b-md bg-black/50 px-1.5 py-0.5 text-[11px] text-white opacity-0 transition group-hover:opacity-100 focus:opacity-100"
            >
              {p.author}
            </a>
          </div>
        ))}
      </div>
      {stock.isPending && <Loader2 className="mx-auto animate-spin text-muted-foreground" />}
      {stock.hasNextPage && (
        <Button variant="outline" onClick={() => void stock.fetchNextPage()} disabled={stock.isFetchingNextPage}>
          Load more
        </Button>
      )}
      <p className="text-xs text-muted-foreground">
        Photos from{' '}
        <a
          className="underline"
          href="https://unsplash.com/?utm_source=staqly&utm_medium=referral"
          target="_blank"
          rel="noopener noreferrer"
        >
          Unsplash
        </a>
        . Hover a photo to see who made it.
      </p>
    </>
  )
}

const SHAPES: { kind: ShapeKind; label: string; icon: ReactNode; round?: boolean }[] = [
  { kind: 'rect', label: 'Square', icon: <rect x="4" y="4" width="32" height="32" /> },
  { kind: 'rect', label: 'Rounded square', round: true, icon: <rect x="4" y="4" width="32" height="32" rx="8" /> },
  { kind: 'ellipse', label: 'Circle', icon: <circle cx="20" cy="20" r="16" /> },
  { kind: 'triangle', label: 'Triangle', icon: <path d="M20 4 36 36H4Z" /> },
  {
    kind: 'star',
    label: 'Star',
    icon: <path d="m20 3 4.7 10.5L36 14.6l-8.6 7.6 2.6 11.3L20 27.6 10 33.5l2.6-11.3L4 14.6l11.3-1.1Z" />,
  },
  { kind: 'line', label: 'Line', icon: <path d="M4 20h32" strokeWidth="4" stroke="currentColor" /> },
  {
    kind: 'arrow',
    label: 'Arrow',
    icon: <path d="M4 20h28m-8-8 8 8-8 8" strokeWidth="4" stroke="currentColor" fill="none" />,
  },
]

function ElementsPanel({ ctx }: { ctx: PanelCtx }) {
  const make = (s: (typeof SHAPES)[number]) => {
    const el = newShape(ctx.store.design, s.kind)

    return s.round ? { ...el, radius: el.width * 0.15 } : el
  }

  return (
    <div className="flex flex-col gap-5">
      <Section title="Shapes">
        <div className="grid grid-cols-3 gap-2">
          {SHAPES.map((s) => (
            <button
              key={s.label}
              type="button"
              aria-label={s.label}
              title={s.label}
              className="flex aspect-square items-center justify-center rounded-lg border bg-background text-brand hover:ring-2 hover:ring-brand"
              onClick={() => ctx.add(make(s))}
              {...dragProps(() => make(s))}
            >
              <svg viewBox="0 0 40 40" className="size-10 fill-current" aria-hidden>
                {s.icon}
              </svg>
            </button>
          ))}
        </div>
      </Section>
      <IconsAndStickers ctx={ctx} />
    </div>
  )
}

export const SWATCHES = [
  '#ffffff',
  '#f8fafc',
  '#e5e7eb',
  '#9ca3af',
  '#4b5563',
  '#111827',
  '#000000',
  '#fee2e2',
  '#ef4444',
  '#b91c1c',
  '#ffedd5',
  '#f97316',
  '#fef3c7',
  '#facc15',
  '#dcfce7',
  '#22c55e',
  '#065f46',
  '#cffafe',
  '#06b6d4',
  '#dbeafe',
  '#3b82f6',
  '#1e3a8a',
  '#e0e7ff',
  '#6366f1',
  '#ede9fe',
  '#8b5cf6',
  '#fce7f3',
  '#ec4899',
]

const GRADIENTS: Fill[] = [
  { kind: 'gradient', from: '#0f172a', to: '#312e81', angle: 135 },
  { kind: 'gradient', from: '#f43f5e', to: '#f97316', angle: 135 },
  { kind: 'gradient', from: '#7c3aed', to: '#db2777', angle: 0 },
  { kind: 'gradient', from: '#06b6d4', to: '#3b82f6', angle: 90 },
  { kind: 'gradient', from: '#22c55e', to: '#14b8a6', angle: 45 },
  { kind: 'gradient', from: '#fde68a', to: '#f9a8d4', angle: 135 },
  { kind: 'gradient', from: '#e0e7ff', to: '#fae8ff', angle: 90 },
  { kind: 'gradient', from: '#111827', to: '#4b5563', angle: 90 },
  { kind: 'gradient', from: '#f97316', via: '#ec4899', to: '#8b5cf6', angle: 45 },
  { kind: 'gradient', from: '#22d3ee', via: '#a78bfa', to: '#f472b6', angle: 0 },
  { kind: 'gradient', from: '#fde047', to: '#f97316', angle: 0, radial: true },
  { kind: 'gradient', from: '#6366f1', to: '#0f172a', angle: 0, radial: true },
]

export const css = (f: Fill) => {
  if (f.kind === 'color') {
    return f.color
  }

  const colours = [f.from, f.via, f.to].filter(Boolean).join(', ')

  return f.radial ? `radial-gradient(circle, ${colours})` : `linear-gradient(${f.angle + 90}deg, ${colours})`
}

/** A colour or a gradient (two or three colours, along an angle or out from the centre). */
export function FillPicker({ value, onChange }: { value: Fill; onChange: (f: Fill, key?: string) => void }) {
  const brand = useBrand().data?.colors ?? []

  return (
    <div className="flex flex-col gap-5">
      {brand.length > 0 && (
        <Section title="Brand colours">
          <div className="grid grid-cols-7 gap-1.5">
            {brand.map((c) => (
              <button
                key={c}
                type="button"
                aria-label={c}
                onClick={() => onChange({ kind: 'color', color: c })}
                className={cn(
                  'aspect-square rounded-md border',
                  value.kind === 'color' && value.color === c && 'ring-2 ring-brand ring-offset-1',
                )}
                style={{ background: c }}
              />
            ))}
          </div>
        </Section>
      )}
      <Section title="Colour">
        <div className="grid grid-cols-7 gap-1.5">
          {SWATCHES.map((c) => (
            <button
              key={c}
              type="button"
              aria-label={c}
              onClick={() => onChange({ kind: 'color', color: c })}
              className={cn(
                'aspect-square rounded-md border',
                value.kind === 'color' && value.color === c && 'ring-2 ring-brand ring-offset-1',
              )}
              style={{ background: c }}
            />
          ))}
        </div>
        <div className="flex items-center gap-2 text-sm">
          <ColorInput
            label="Custom colour"
            value={value.kind === 'color' ? value.color : value.from}
            onChange={(c) => onChange({ kind: 'color', color: c }, 'fill')}
          />
          Custom colour
        </div>
      </Section>
      <Section title="Gradient">
        <div className="grid grid-cols-6 gap-1.5">
          {GRADIENTS.map((g) => (
            <button
              key={css(g)}
              type="button"
              aria-label="Gradient"
              onClick={() => onChange(g)}
              className={cn(
                'aspect-square rounded-md border',
                css(value) === css(g) && 'ring-2 ring-brand ring-offset-1',
              )}
              style={{ background: css(g) }}
            />
          ))}
        </div>
        {value.kind === 'gradient' && (
          <div className="flex flex-col gap-3 text-sm">
            <div className="flex items-center gap-1.5">
              <ColorInput
                label="First colour"
                value={value.from}
                onChange={(c) => onChange({ ...value, from: c }, 'fill')}
              />
              {value.via ? (
                <span className="group relative">
                  <ColorInput
                    label="Middle colour"
                    value={value.via}
                    onChange={(c) => onChange({ ...value, via: c }, 'fill')}
                  />
                  <button
                    type="button"
                    aria-label="Remove the middle colour"
                    onClick={() => onChange({ ...value, via: undefined })}
                    className="absolute -top-1.5 -right-1.5 hidden size-4 items-center justify-center rounded-full border bg-background group-hover:flex focus-visible:flex"
                  >
                    <X className="size-3" />
                  </button>
                </span>
              ) : (
                <Button
                  variant="outline"
                  size="icon-sm"
                  aria-label="Add a middle colour"
                  title="Add a middle colour"
                  onClick={() => onChange({ ...value, via: '#ffffff' })}
                >
                  <Plus />
                </Button>
              )}
              <ColorInput
                label="Last colour"
                value={value.to}
                onChange={(c) => onChange({ ...value, to: c }, 'fill')}
              />
              <div className="ml-auto flex rounded-md border p-0.5" role="group" aria-label="Gradient type">
                {(['Linear', 'Radial'] as const).map((t) => (
                  <button
                    key={t}
                    type="button"
                    aria-pressed={(t === 'Radial') === !!value.radial}
                    onClick={() => onChange({ ...value, radial: t === 'Radial' })}
                    className="rounded px-2 py-0.5 text-xs aria-pressed:bg-brand-soft aria-pressed:text-brand"
                  >
                    {t}
                  </button>
                ))}
              </div>
            </div>
            {!value.radial && (
              <label className="flex items-center gap-2 text-xs text-muted-foreground">
                Angle
                <input
                  type="range"
                  min={0}
                  max={360}
                  value={value.angle}
                  onChange={(e) => onChange({ ...value, angle: +e.target.value }, 'fill-angle')}
                  className="min-w-0 flex-1 accent-[var(--brand)]"
                />
                <span className="w-9 text-right text-foreground tabular-nums">{value.angle}°</span>
              </label>
            )}
          </div>
        )}
      </Section>
    </div>
  )
}

function BackgroundPanel({ ctx }: { ctx: PanelCtx }) {
  const design = ctx.store.design
  const bg = ctx.page.background
  const set = (f: Fill, key?: string) => ctx.updatePage((p) => void (p.background = f), key)
  const sel = ctx.selected.length === 1 ? ctx.page.elements.find((e) => e.id === ctx.selected[0]) : undefined

  return (
    <div className="flex flex-col gap-5">
      <FillPicker value={bg} onChange={(f, key) => set(f, key && `bg-${key}`)} />
      {sel?.type === 'image' && (
        <Section title="Photo">
          <Button
            variant="secondary"
            onClick={() =>
              // The frame becomes the page; the picture crops to fill it.
              ctx.updatePage((p) => {
                const i = p.elements.findIndex((e) => e.id === sel.id)
                const el = p.elements[i] as ImageEl

                Object.assign(el, { x: 0, y: 0, width: design.width, height: design.height, rotation: 0, locked: true })
                p.elements.splice(i, 1)
                p.elements.unshift(el)
              })
            }
          >
            Use the selected photo as the background
          </Button>
        </Section>
      )}
    </div>
  )
}

/** A picture on the page shows another library file, in the same frame (undo brings the old one back). */
export function swapPicture(ctx: PanelCtx, id: string, media: Media) {
  ctx.updatePage((p) => {
    const el = p.elements.find((e) => e.id === id)

    if (el?.type === 'image') {
      el.src = mediaUrl(media.id)
      el.mediaId = media.id
      el.stock = undefined
    }
  })
}

export function elLabel(e: El) {
  if (e.type === 'text') {
    return e.text.split('\n')[0] || 'Text'
  }

  if (e.type === 'image') {
    return e.stock ? `Photo by ${e.stock.author}` : 'Picture'
  }

  return { rect: 'Rectangle', ellipse: 'Circle', triangle: 'Triangle', star: 'Star', line: 'Line', arrow: 'Arrow' }[
    e.shape
  ]
}

// A drag that moves less than this is a click (it picks the layer).
const DRAG_PX = 4

function LayersPanel({ ctx }: { ctx: PanelCtx }) {
  // Top of the page first, as the stack is seen.
  const list = ctx.page.elements.toReversed()
  const ul = useRef<HTMLUListElement>(null)
  // The layer being dragged: where it started, how far the pointer went, the slot it would land in.
  const [drag, setDrag] = useState<{ id: string; from: number; dy: number; to: number; row: number } | null>(null)
  const [menu, setMenu] = useState<string | null>(null)

  /** Moves a layer to slot `to` of the list (top first). */
  const moveTo = (id: string, to: number) =>
    ctx.updatePage((p) => {
      const shown = p.elements.toReversed()
      const from = shown.findIndex((e) => e.id === id)
      const [el] = shown.splice(from, 1)

      shown.splice(to, 0, el!)
      p.elements = shown.toReversed()
    })
  const toggle = (id: string, key: 'hidden' | 'locked') =>
    ctx.updatePage((p) => {
      const el = p.elements.find((e) => e.id === id)

      if (el) {
        el[key] = !el[key]
      }
    })

  // Pointer dragging: the row follows the pointer, the others slide aside to open its slot, and it drops there.
  const start = (e: React.PointerEvent<HTMLLIElement>, id: string, from: number) => {
    const target = e.target as HTMLElement

    // The eye and the lock are buttons of their own; on touch, only the handle drags (the rest scrolls).
    if (
      e.button !== 0 ||
      target.closest('[data-no-drag]') ||
      (e.pointerType !== 'mouse' && !target.closest('[data-grip]'))
    ) {
      return
    }

    const y0 = e.clientY
    const rows = [...(ul.current?.children ?? [])] as HTMLElement[]
    // One slot: a row and the gap under it.
    const row = rows.length > 1 ? rows[1]!.offsetTop - rows[0]!.offsetTop : (rows[0]?.offsetHeight ?? 40)
    let moved = false
    let to = from

    const move = (ev: PointerEvent) => {
      const dy = ev.clientY - y0

      if (!moved && Math.abs(dy) < DRAG_PX) {
        return
      }

      moved = true
      ev.preventDefault()
      to = Math.max(0, Math.min(list.length - 1, from + Math.round(dy / row)))
      setDrag({ id, from, dy, to, row })
    }
    const end = () => {
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', end)
      window.removeEventListener('pointercancel', end)
      setDrag(null)

      if (moved) {
        // The click that ends a drag doesn't pick the layer.
        window.addEventListener('click', (ev) => ev.stopPropagation(), { capture: true, once: true })

        if (to !== from) {
          moveTo(id, to)
        }
      }
    }

    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', end)
    window.addEventListener('pointercancel', end)
  }

  /** How far a row slides while another is dragged over it. */
  const shift = (i: number) => {
    if (!drag || i === drag.from) {
      return 0
    }

    if (drag.from < drag.to && i > drag.from && i <= drag.to) {
      return -drag.row
    }

    if (drag.from > drag.to && i >= drag.to && i < drag.from) {
      return drag.row
    }

    return 0
  }

  if (!list.length) {
    return <p className="text-sm text-muted-foreground">Nothing on the page yet.</p>
  }

  return (
    <div className={cn('relative', drag && 'select-none')}>
      {drag && (
        // Where the dragged layer will land.
        <div
          aria-hidden
          className="pointer-events-none absolute inset-x-0 rounded-md border-2 border-dashed border-brand/60 bg-brand-soft/40 transition-[top] duration-150"
          style={{ top: drag.to * drag.row, height: drag.row - 4 }}
        />
      )}
      <ul ref={ul} className="relative flex flex-col gap-1" aria-label="Layers, top first">
        {list.map((e, i) => {
          const lifted = drag?.id === e.id

          return (
            // oxlint-disable-next-line jsx-a11y/no-noninteractive-element-interactions -- dragging is for the pointer; the handle reorders by keyboard
            <li
              key={e.id}
              onPointerDown={(ev) => start(ev, e.id, i)}
              // Right-click: the same menu as the "..." button.
              onContextMenu={(ev) => {
                ev.preventDefault()
                setMenu(e.id)
              }}
              style={{ transform: `translateY(${lifted ? drag.dy : shift(i)}px)` }}
              className={cn(
                'flex items-center gap-1 rounded-md border bg-background py-1 pr-1 pl-0.5 text-sm',
                ctx.selected.includes(e.id) && 'border-brand ring-1 ring-brand',
                lifted
                  ? 'relative z-10 scale-[1.02] cursor-grabbing shadow-lg ring-2 ring-brand'
                  : drag && 'transition-transform duration-150',
              )}
            >
              <button
                type="button"
                data-grip
                aria-label={`Move ${elLabel(e)}: drag, or press the up and down arrows`}
                title="Drag to reorder"
                className="flex h-7 w-5 shrink-0 cursor-grab touch-none items-center justify-center rounded text-muted-foreground hover:text-foreground active:cursor-grabbing"
                onKeyDown={(ev) => {
                  if (ev.key === 'ArrowUp' && i > 0) {
                    ev.preventDefault()
                    moveTo(e.id, i - 1)
                  } else if (ev.key === 'ArrowDown' && i < list.length - 1) {
                    ev.preventDefault()
                    moveTo(e.id, i + 1)
                  }
                }}
              >
                <GripVertical className="size-4" />
              </button>
              <button type="button" className="min-w-0 flex-1 truncate text-left" onClick={() => ctx.onSelect([e.id])}>
                <span className={cn(e.hidden && 'text-muted-foreground line-through')}>{elLabel(e)}</span>
              </button>
              <Button
                variant="ghost"
                size="icon-sm"
                data-no-drag
                aria-label={e.hidden ? 'Show' : 'Hide'}
                onClick={() => toggle(e.id, 'hidden')}
              >
                {e.hidden ? <EyeOff /> : <Eye />}
              </Button>
              <Button
                variant="ghost"
                size="icon-sm"
                data-no-drag
                aria-label={e.locked ? 'Unlock' : 'Lock'}
                onClick={() => toggle(e.id, 'locked')}
              >
                {e.locked ? <Lock /> : <Unlock />}
              </Button>
              <DropdownMenu open={menu === e.id} onOpenChange={(o) => setMenu(o ? e.id : null)}>
                <DropdownMenuTrigger asChild>
                  <Button variant="ghost" size="icon-sm" data-no-drag aria-label={`More for ${elLabel(e)}`}>
                    <MoreHorizontal />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuLabel>Move to another page</DropdownMenuLabel>
                  <MoveToPageItems ctx={ctx} ids={withGroups(ctx.page.elements, [e.id])} />
                </DropdownMenuContent>
              </DropdownMenu>
            </li>
          )
        })}
      </ul>
    </div>
  )
}

function SizePanel({ ctx }: { ctx: PanelCtx }) {
  const { design, update } = ctx.store
  const [scaleContent, setScaleContent] = useState(true)
  const [w, setW] = useState(String(design.width))
  const [h, setH] = useState(String(design.height))

  const apply = (width: number, height: number) => {
    if (!(width >= 16 && height >= 16 && width <= 8000 && height <= 8000)) {
      toast.error('Width and height go from 16 to 8000 pixels')

      return
    }

    update((d) => (scaleContent ? resize(d, width, height) : { ...d, width, height }))
    setW(String(width))
    setH(String(height))
  }

  return (
    <div className="flex flex-col gap-5">
      <label className="flex items-center gap-2 text-sm">
        <Checkbox checked={scaleContent} onCheckedChange={(v) => setScaleContent(v === true)} />
        Fit the content to the new size
      </label>
      <Section title="For a channel">
        <ul className="flex flex-col gap-1">
          {SIZES.map((s) => {
            const on = s.width === design.width && s.height === design.height

            return (
              <li key={s.label}>
                <button
                  type="button"
                  onClick={() => apply(s.width, s.height)}
                  aria-pressed={on}
                  className={cn(
                    'flex w-full items-center gap-3 rounded-md px-2 py-1.5 text-left text-sm hover:bg-muted',
                    on && 'bg-muted font-medium',
                  )}
                >
                  <span className="flex size-7 shrink-0 items-center justify-center">
                    <span
                      className="block rounded-[3px] border-2 border-current text-muted-foreground"
                      style={{
                        width: s.width >= s.height ? 26 : (26 * s.width) / s.height,
                        height: s.height >= s.width ? 26 : (26 * s.height) / s.width,
                      }}
                    />
                  </span>
                  <span className="min-w-0 flex-1 truncate">{s.label}</span>
                  <span className="text-xs text-muted-foreground tabular-nums">
                    {s.width} x {s.height}
                  </span>
                </button>
              </li>
            )
          })}
        </ul>
      </Section>
      <Section title="Custom">
        <form
          className="flex items-end gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            apply(Math.round(+w), Math.round(+h))
          }}
        >
          <div className="flex flex-col gap-1">
            <Label htmlFor="design-w">Width</Label>
            <Input id="design-w" inputMode="numeric" value={w} onChange={(e) => setW(e.target.value)} />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="design-h">Height</Label>
            <Input id="design-h" inputMode="numeric" value={h} onChange={(e) => setH(e.target.value)} />
          </div>
          <Button type="submit" variant="secondary">
            Resize
          </Button>
        </form>
      </Section>
    </div>
  )
}

export { ColorInput }
