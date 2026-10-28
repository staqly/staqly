'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import {
  Check,
  Eraser,
  Expand,
  ImageIcon,
  ImagePlus,
  Languages,
  Loader2,
  MousePointerClick,
  Paintbrush,
  RotateCcw,
  Scissors,
  Sparkles,
  Type,
  Wand2,
  X,
  ZoomIn,
  ExternalLink,
  type LucideIcon,
} from 'lucide-react'
import dynamic from 'next/dynamic'
import { useEffect, useState, useSyncExternalStore } from 'react'
import { toast } from 'sonner'

import { Callout } from '@/components/common/callout'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { AI_OPS, allowanceNote, allowanceUsedUp, madeBy, requestKey } from '@/lib/ai'
import { isLive, useAiJob, useAiStatus } from '@/lib/ai-status'
import {
  aiResultUrl,
  ApiError,
  api,
  errorMessage,
  mediaUrl,
  type AIOperation,
  type AIOperationId,
  type Media,
} from '@/lib/api'
import { qk } from '@/lib/queries'
import { cn } from '@/lib/utils'

import { newImage, type Design, type ImageEl, type TextEl } from './doc'
import { Elapsed, swapPicture, type PanelCtx } from './panels'
import { downscale } from './raster'

const MaskDialog = dynamic(() => import('./mask-dialog').then((m) => m.MaskDialog), { ssr: false })

// The design editor's AI panel. A request runs on the server (GET /v1/ai says which operations it offers); its
// result waits as a preview until the person keeps it (it becomes a library file and goes on the page as one undo
// step, still an ordinary picture that can be moved, cropped or changed again) or discards it. Nothing on the
// page changes before that. The run outlives the panel (switching tools keeps it) and is discarded when the
// editor closes.

/** Expand: where the picture sits in the new size (to turn the frame into the new picture's frame). */
interface Placement {
  x: number
  y: number
  w: number
  h: number
  cw: number
  ch: number
}

interface AiRun {
  key: string
  op: AIOperationId
  jobId: string | null
  /** The picture it changes, on this page. */
  targetId?: string
  pageId: string
  prompt: string
  fields: Record<string, number | string>
  image?: Blob
  mask?: Blob
  place?: Placement
  /** Starting failed (the request did not arrive): a retry sends the same key. */
  error?: string
  /** It failed on this month's allowance: the panel then offers the tools that run on this device. */
  limited?: boolean
}

// One run per editor, outside React state so switching panels keeps it.
let current: AiRun | null = null
const listeners = new Set<() => void>()

function setRun(r: AiRun | null) {
  current = r
  listeners.forEach((f) => f())
}

function subscribe(f: () => void) {
  listeners.add(f)

  return () => void listeners.delete(f)
}

function useRun() {
  return useSyncExternalStore(
    subscribe,
    () => current,
    () => null,
  )
}

/** The editor closes: a run that is still going is cancelled and its files deleted. */
export function discardAiRun() {
  if (current?.jobId) {
    void api.discardAiJob(current.jobId).catch(() => undefined)
  }

  setRun(null)
}

function send(r: AiRun) {
  setRun({ ...r, error: undefined })
  void api
    .startAiJob({ operation: r.op, prompt: r.prompt, fields: r.fields, image: r.image, mask: r.mask }, r.key)
    .then((job) => current?.key === r.key && setRun({ ...current, jobId: job.id }))
    .catch(
      (e: unknown) =>
        current?.key === r.key &&
        setRun({ ...current, error: errorMessage(e), limited: e instanceof ApiError && e.code === 'ai_limit_reached' }),
    )
}

/** The picture as the AI service gets it: a PNG, at most `edge` pixels on its long side. */
async function pictureOf(el: ImageEl, edge: number) {
  const blob = await downscale(await (await fetch(el.src)).blob(), edge)
  const bmp = await createImageBitmap(blob)
  const size = { width: bmp.width, height: bmp.height }

  bmp.close()

  return { blob, ...size }
}

/** A size for a new picture in the page's shape (about 1.3 megapixels: cheap and quick). */
function generateSize(d: Design) {
  const k = 1344 / Math.max(d.width, d.height)

  return { width: Math.max(256, Math.round(d.width * k)), height: Math.max(256, Math.round(d.height * k)) }
}

/** Expand: the picture (w x h) grown to the page's shape, centred; a picture already in that shape gets half again. */
export function expandPlacement(w: number, h: number, page: { width: number; height: number }): Placement {
  const r = page.width / page.height
  let cw = w
  let ch = h

  if (Math.abs(r / (w / h) - 1) < 0.02) {
    cw = Math.round(w * 1.5)
    ch = Math.round(h * 1.5)
  } else if (r > w / h) {
    cw = Math.round(h * r)
  } else {
    ch = Math.round(w / r)
  }

  const k = Math.min(1, 4096 / Math.max(cw, ch))
  const pw = Math.round(w * k)
  const ph = Math.round(h * k)

  cw = Math.round(cw * k)
  ch = Math.round(ch * k)

  return { x: Math.round((cw - pw) / 2), y: Math.round((ch - ph) / 2), w: pw, h: ph, cw, ch }
}

/** The kept result goes on the page: in the photo's place (its frame grows for an expansion) or as a new picture. */
function apply(ctx: PanelCtx, r: AiRun, media: Media) {
  const target = r.targetId ? ctx.page.elements.find((e) => e.id === r.targetId) : undefined

  if (r.op !== 'generate' && target?.type === 'image' && ctx.page.id === r.pageId) {
    if (r.op === 'expand' && r.place) {
      const p = r.place

      ctx.updatePage((pg) => {
        const el = pg.elements.find((e) => e.id === target.id)

        if (el?.type === 'image') {
          const sx = el.width / p.w
          const sy = el.height / p.h

          Object.assign(el, {
            x: el.x - p.x * sx,
            y: el.y - p.y * sy,
            width: p.cw * sx,
            height: p.ch * sy,
            src: mediaUrl(media.id),
            mediaId: media.id,
            stock: undefined,
            crop: undefined,
          })
        }
      })
    } else {
      swapPicture(ctx, target.id, media)
    }

    return
  }

  ctx.add(
    newImage(ctx.store.design, mediaUrl(media.id), media.width || 1024, media.height || 1024, { mediaId: media.id }),
  )
}

const NONE: AIOperation[] = []

/** Every picture tool, in the order the panel lists them: `device` runs in the browser when the server can't. */
const TOOLS: { id: AIOperationId | 'cutout'; photo: boolean; device?: LocalOp; icon: LucideIcon }[] = [
  { id: 'generate', photo: false, icon: ImagePlus },
  { id: 'inpaint', photo: true, icon: Paintbrush },
  { id: 'edit', photo: true, icon: Wand2 },
  { id: 'erase', photo: true, device: 'erase', icon: Eraser },
  { id: 'cutout', photo: true, device: 'cutout', icon: Scissors },
  { id: 'replace_background', photo: true, icon: ImageIcon },
  { id: 'expand', photo: true, icon: Expand },
  { id: 'upscale', photo: true, device: 'upscale', icon: ZoomIn },
  { id: 'enhance', photo: true, icon: Sparkles },
]

function toolLabel(id: AIOperationId | 'cutout') {
  return id === 'cutout' ? 'Remove background' : AI_OPS[id].label
}

/** What the person has selected, in the panel's terms: it decides which tools show. */
function selectionKind(ctx: PanelCtx) {
  const els = ctx.selected.map((id) => ctx.page.elements.find((e) => e.id === id)).filter(Boolean)

  if (els.length === 0) {
    return { kind: 'none' as const }
  }

  if (els.length > 1) {
    return { kind: 'many' as const }
  }

  const el = els[0]!

  return el.type === 'image'
    ? { kind: 'photo' as const, picture: el }
    : el.type === 'text'
      ? { kind: 'text' as const, text: el }
      : { kind: 'other' as const }
}

const CONTEXT: Record<string, { icon: LucideIcon; title: string; hint: string }> = {
  none: {
    icon: MousePointerClick,
    title: 'Nothing selected',
    hint: 'Make a new picture, or select a photo or a text on the page to change it.',
  },
  many: {
    icon: MousePointerClick,
    title: 'Several things selected',
    hint: 'Select one photo or one text to change it with AI.',
  },
  other: {
    icon: MousePointerClick,
    title: 'A shape is selected',
    hint: 'AI changes photos and texts: select one of those.',
  },
  photo: { icon: ImageIcon, title: 'Photo selected', hint: 'Pick what AI should do with it.' },
  text: { icon: Type, title: 'Text selected', hint: 'Rewrite it with AI; Undo brings the old text back.' },
}

export function AiPanel({ ctx }: { ctx: PanelCtx }) {
  const status = useAiStatus()
  const run = useRun()
  const sel = selectionKind(ctx)
  const picture = sel.kind === 'photo' ? sel.picture : undefined
  const usedUp = allowanceUsedUp(status.data)
  // With the allowance used up the server's tools wait for next month.
  const ops = usedUp ? NONE : (status.data?.operations ?? NONE).filter((o) => o.kind === 'image')
  const canWrite = !usedUp && !!status.data?.operations.some((o) => o.id === 'assistant')
  const tools = TOOLS.map((t) => {
    const server = t.id === 'cutout' ? undefined : ops.find((o) => o.id === t.id)
    const where = server ? 'server' : t.device ? 'device' : null

    return { ...t, server, where }
  }).filter((t) => t.where)
  const [chosen, setChosen] = useState<string | null>(null)
  const photoTools = tools.filter((t) => t.photo)
  const make = tools.find((t) => t.id === 'generate')
  const tool = picture
    ? (photoTools.find((t) => t.id === chosen) ?? null)
    : chosen === 'generate' || sel.kind === 'none'
      ? make
      : null

  if (status.isPending) {
    return <p className="text-sm text-muted-foreground">Loading the AI tools...</p>
  }

  const note = usedUp ? '' : allowanceNote(status.data)
  const context = CONTEXT[sel.kind]!
  const tile = (t: (typeof tools)[number]) => {
    const Icon = t.icon

    return (
      <button
        key={t.id}
        type="button"
        role="radio"
        aria-checked={tool?.id === t.id}
        onClick={() => setChosen(tool?.id === t.id ? null : t.id)}
        className={cn(
          'flex min-h-16 flex-col items-start gap-1 rounded-lg border bg-card p-2.5 text-left transition-colors outline-none hover:border-brand/40 hover:bg-muted/50 focus-visible:ring-2 focus-visible:ring-ring',
          tool?.id === t.id && 'border-brand bg-brand-soft/60 hover:bg-brand-soft/60',
        )}
      >
        <Icon className={cn('size-4', tool?.id === t.id ? 'text-brand' : 'text-muted-foreground')} />
        <span className="text-sm leading-tight font-medium">{toolLabel(t.id)}</span>
        {t.where === 'device' && <span className="text-[11px] text-muted-foreground">On this device, free</span>}
      </button>
    )
  }

  return (
    <div className="flex flex-col gap-4 text-sm">
      {usedUp && (
        <Callout tone="info">
          This month&apos;s AI allowance is used up; it starts again on {usedUp.renews}. Removing an object or the
          background and upscaling still work, on this device.
        </Callout>
      )}
      {run ? (
        <RunView ctx={ctx} run={run} />
      ) : (
        <>
          {/* What is selected, so it is clear what AI can do now. */}
          <div className="flex items-start gap-2.5 rounded-lg border bg-muted/30 p-2.5" data-testid="ai-context">
            <span className="grid size-8 shrink-0 place-items-center rounded-md bg-background text-muted-foreground">
              <context.icon className="size-4" />
            </span>
            <div className="grid gap-0.5">
              <p className="font-medium">{context.title}</p>
              <p className="text-xs text-muted-foreground">{context.hint}</p>
            </div>
          </div>

          {sel.kind === 'text' &&
            (canWrite ? (
              <TextTools key={sel.text.id} ctx={ctx} el={sel.text} />
            ) : (
              <p className="text-xs text-muted-foreground">
                {usedUp
                  ? `Writing with AI is back on ${usedUp.renews}.`
                  : 'Writing with AI needs AI set up on the server.'}
              </p>
            ))}

          {picture && (
            <section className="grid gap-2" aria-label="Change the photo">
              {photoTools.length === 0 ? (
                <p className="text-xs text-muted-foreground">No photo tools are set up on this server.</p>
              ) : (
                <div role="radiogroup" aria-label="AI tool" className="grid grid-cols-2 gap-1.5">
                  {photoTools.map(tile)}
                </div>
              )}
            </section>
          )}

          {tool?.server && (
            <AiForm
              key={`${tool.id}:${picture?.id ?? ''}`}
              ctx={ctx}
              op={tool.server}
              picture={tool.photo ? picture : undefined}
            />
          )}
          {tool &&
            !tool.server &&
            tool.device &&
            picture &&
            (ctx.inDrawer ? (
              <InFullEditor picture={picture} />
            ) : (
              <LocalForm key={`${tool.id}:${picture.id}`} ctx={ctx} op={tool.device} picture={picture} />
            ))}

          {/* Making something new is always at hand; with nothing selected it is the open form above. */}
          {make && sel.kind !== 'none' && tool?.id !== 'generate' && (
            <section className="grid gap-2 border-t pt-3" aria-label="Make something new">
              <h3 className="text-xs font-medium tracking-wide text-muted-foreground uppercase">Make something new</h3>
              <div role="radiogroup" aria-label="New" className="grid grid-cols-2 gap-1.5">
                {tile(make)}
              </div>
            </section>
          )}
          {!make && sel.kind === 'none' && (
            <p className="text-xs text-muted-foreground">
              {usedUp
                ? `Making pictures is back on ${usedUp.renews}.`
                : status.data?.configured
                  ? 'Making pictures is not offered on this server.'
                  : 'Making pictures needs AI set up on the server.'}
            </p>
          )}
        </>
      )}
      {note && <p className="text-xs text-muted-foreground">{note}</p>}
    </div>
  )
}

/** One-click rewrites of the selected text, a language to translate to, or the person's own words. */
const REWRITES = [
  { label: 'Shorter', instruction: 'Make it shorter and punchier.' },
  { label: 'Longer', instruction: 'Make it a little longer and more detailed.' },
  { label: 'Friendlier', instruction: 'Make it warmer and friendlier.' },
  { label: 'More professional', instruction: 'Make it more professional.' },
  { label: 'Fix spelling', instruction: 'Fix spelling, grammar and punctuation only.' },
  { label: 'Catchier', instruction: 'Make it a catchy headline.' },
]

const LANGUAGES = [
  'English',
  'Polish',
  'German',
  'French',
  'Spanish',
  'Italian',
  'Portuguese',
  'Dutch',
  'Ukrainian',
  'Japanese',
]

function TextTools({ ctx, el }: { ctx: PanelCtx; el: TextEl }) {
  const [own, setOwn] = useState('')
  const [lang, setLang] = useState('English')
  const [result, setResult] = useState<{ text: string; instruction: string } | null>(null)
  const ask = useMutation({
    mutationFn: (instruction: string) => api.aiText(el.text, instruction).then((r) => ({ text: r.text, instruction })),
    onSuccess: setResult,
    onError: (e) => toast.error('Could not rewrite the text', { description: errorMessage(e) }),
  })
  const busy = ask.isPending

  const use = () => {
    ctx.updatePage((pg) => {
      const t = pg.elements.find((e) => e.id === el.id)

      if (t?.type === 'text') {
        t.text = result!.text
      }
    })
    setResult(null)
  }

  if (result) {
    return (
      <section className="grid gap-2" aria-label="The new text">
        <p className="text-xs text-muted-foreground">The new text</p>
        <p className="rounded-lg border bg-card p-3 whitespace-pre-wrap">{result.text}</p>
        <div className="flex flex-wrap gap-2">
          <Button type="button" size="sm" onClick={use}>
            <Check /> Use it
          </Button>
          <Button
            type="button"
            size="sm"
            variant="outline"
            onClick={() => ask.mutate(result.instruction)}
            disabled={busy}
          >
            {busy ? <Loader2 className="animate-spin" /> : <RotateCcw />} Try again
          </Button>
          <Button type="button" size="sm" variant="ghost" onClick={() => setResult(null)} disabled={busy}>
            Discard
          </Button>
        </div>
      </section>
    )
  }

  return (
    <section className="grid gap-3" aria-label="Rewrite the text">
      <div className="grid grid-cols-2 gap-1.5">
        {REWRITES.map((r) => (
          <Button
            key={r.label}
            type="button"
            variant="outline"
            size="sm"
            className="justify-start"
            disabled={busy}
            onClick={() => ask.mutate(r.instruction)}
          >
            {r.label}
          </Button>
        ))}
      </div>
      <div className="flex gap-1.5">
        <select
          aria-label="Language"
          value={lang}
          onChange={(e) => setLang(e.target.value)}
          className="h-8 min-w-0 flex-1 rounded-md border bg-background px-2 text-sm"
        >
          {LANGUAGES.map((l) => (
            <option key={l}>{l}</option>
          ))}
        </select>
        <Button
          type="button"
          size="sm"
          variant="outline"
          disabled={busy}
          onClick={() => ask.mutate(`Translate it to ${lang}.`)}
        >
          <Languages /> Translate
        </Button>
      </div>
      <form
        className="grid gap-1.5"
        onSubmit={(e) => {
          e.preventDefault()

          if (own.trim()) {
            ask.mutate(own.trim())
          }
        }}
      >
        <Textarea
          aria-label="Your own instruction"
          placeholder="Or say how to change it, e.g. add an emoji at the end"
          value={own}
          maxLength={500}
          rows={2}
          onChange={(e) => setOwn(e.target.value)}
        />
        <Button type="submit" size="sm" disabled={busy || !own.trim()}>
          {busy ? <Loader2 className="animate-spin" /> : <Sparkles />} Rewrite
        </Button>
      </form>
      {busy && (
        <p className="flex items-center gap-1.5 text-xs text-muted-foreground" role="status">
          <Loader2 className="size-3.5 animate-spin" /> Writing...
        </p>
      )}
    </section>
  )
}

function AiForm({ ctx, op, picture }: { ctx: PanelCtx; op: AIOperation; picture?: ImageEl }) {
  const [prompt, setPrompt] = useState('')
  const [preparing, setPreparing] = useState(false)
  // The picture the mask is painted on (the mask matches its pixels, so it is also what is sent).
  const [source, setSource] = useState<{ width: number; height: number; blob: Blob } | null>(null)
  const [painting, setPainting] = useState(false)
  const [mask, setMask] = useState<Blob | null>(null)
  const copy = AI_OPS[op.id]
  const needsPrompt = op.prompt === 'required'

  const paint = () => {
    if (source) {
      setPainting(true)

      return
    }

    if (!picture) {
      return
    }

    setPreparing(true)
    void pictureOf(picture, 2048)
      .then((p) => {
        setSource(p)
        setPainting(true)
      })
      .catch((e: unknown) => toast.error('Could not open the photo', { description: errorMessage(e) }))
      .finally(() => setPreparing(false))
  }

  const start = () => {
    const base: AiRun = {
      key: requestKey(),
      op: op.id,
      jobId: null,
      pageId: ctx.page.id,
      prompt: prompt.trim(),
      fields: {},
    }

    if (!op.image) {
      send({ ...base, fields: generateSize(ctx.store.design) })

      return
    }

    if (!picture) {
      return
    }

    setPreparing(true)
    // Upscaling and enhancing make it bigger: they start from at most 1024 px (cheaper, and plenty).
    const edge = op.id === 'upscale' || op.id === 'enhance' ? 1024 : 2048
    const ready = op.mask && source ? Promise.resolve(source) : pictureOf(picture, edge)

    void ready
      .then((p) => {
        const r: AiRun = { ...base, targetId: picture.id, image: p.blob, mask: mask ?? undefined }

        if (op.id === 'expand') {
          const place = expandPlacement(p.width, p.height, ctx.store.design)

          r.place = place
          r.fields = {
            canvas_width: place.cw,
            canvas_height: place.ch,
            image_x: place.x,
            image_y: place.y,
            place_width: place.w,
            place_height: place.h,
          }
        }

        send(r)
      })
      .catch((e: unknown) => toast.error('Could not read the photo', { description: errorMessage(e) }))
      .finally(() => setPreparing(false))
  }

  const ready = (!needsPrompt || !!prompt.trim()) && (!op.mask || !!mask)

  return (
    <div className="flex flex-col gap-3">
      <p className="text-xs text-muted-foreground">{copy.hint}</p>
      {op.mask && (
        <Button type="button" variant="outline" onClick={paint} disabled={preparing}>
          <Paintbrush /> {mask ? 'Paint the area again' : 'Paint the area'}
        </Button>
      )}
      {op.mask && mask && <p className="text-xs text-muted-foreground">Area painted.</p>}
      {op.prompt !== 'none' && (
        <Textarea
          aria-label={needsPrompt ? 'What should AI do?' : 'What should AI do? (optional)'}
          placeholder={op.image ? 'A sunny beach, soft morning light' : 'A cosy coffee shop at sunrise, photo'}
          value={prompt}
          maxLength={4000}
          rows={3}
          onChange={(e) => setPrompt(e.target.value)}
        />
      )}
      <Button type="button" onClick={start} disabled={!ready || preparing}>
        {preparing ? <Loader2 className="animate-spin" /> : <Sparkles />} {copy.run}
      </Button>
      {source && painting && (
        <MaskDialog open onOpenChange={setPainting} picture={source} title={copy.label} onDone={setMask} />
      )}
    </div>
  )
}

type LocalOp = 'erase' | 'upscale' | 'cutout'

const LOCAL_COPY: Record<LocalOp, { hint: string; model: string; run: string; done: string }> = {
  erase: {
    hint: 'Paint over what should go; the rest of the photo stays as it is.',
    model: 'about 200 MB',
    run: 'Remove it',
    done: 'Object removed',
  },
  cutout: {
    hint: 'Keeps the subject and makes the background transparent.',
    model: 'a few hundred MB',
    run: 'Remove the background',
    done: 'Background removed',
  },
  upscale: {
    hint: 'Four times the pixels, sharper than stretching (up to about 3000 pixels across).',
    model: 'about 5 MB',
    run: 'Upscale',
    done: 'Upscaled four times',
  },
}

/** In a drawer over a post the on-device tools can't fetch their models: the photo opens in the full editor. */
function InFullEditor({ picture }: { picture: ImageEl }) {
  return (
    <div className="flex flex-col gap-3">
      <p className="text-xs text-muted-foreground">
        This tool runs in the full editor. It opens the photo in a new tab and saves the result in your library.
      </p>
      {picture.mediaId ? (
        <Button asChild variant="outline">
          <a href={`/media/edit?ids=${encodeURIComponent(picture.mediaId)}`} target="_blank" rel="noopener">
            <ExternalLink /> Open in the full editor
          </a>
        </Button>
      ) : (
        <p className="text-xs text-muted-foreground">Only a photo from your library opens there.</p>
      )}
    </div>
  )
}

/** Removing an object or upscaling on this device (components/design/local-ai.ts): free, nothing uploaded. */
function LocalForm({ ctx, op, picture }: { ctx: PanelCtx; op: LocalOp; picture: ImageEl }) {
  const [source, setSource] = useState<{ width: number; height: number; blob: Blob } | null>(null)
  const [painting, setPainting] = useState(false)
  const [mask, setMask] = useState<Blob | null>(null)
  const [busy, setBusy] = useState<string | null>(null)

  const paint = () => {
    if (source) {
      setPainting(true)

      return
    }

    setBusy('Opening the photo')
    void pictureOf(picture, 2048)
      .then((p) => {
        setSource(p)
        setPainting(true)
      })
      .catch((e: unknown) => toast.error('Could not open the photo', { description: errorMessage(e) }))
      .finally(() => setBusy(null))
  }

  const run = () => {
    setBusy('Getting the model ready')

    const progress = (share: number) =>
      setBusy(share < 1 ? `Downloading the model ${Math.round(share * 100)}%` : 'Working on it')

    const made = async () => {
      if (op === 'cutout') {
        const { removeBackground } = await import('./background')

        return removeBackground(await (await fetch(picture.src)).blob(), progress)
      }

      const m = await import('./local-ai')

      return op === 'erase'
        ? m.eraseOnDevice(source!.blob, mask!, progress)
        : m.upscaleOnDevice((await pictureOf(picture, m.UPSCALE_FROM)).blob, progress)
    }

    void made()
      .then((blob) => api.uploadMedia(blob, `${LOCAL_COPY[op].done}.png`))
      .then((media) => {
        swapPicture(ctx, picture.id, media)
        toast.success(LOCAL_COPY[op].done, {
          description: 'Made on this device and saved in your library. Undo brings the photo back.',
        })
        setMask(null)
      })
      .catch((e: unknown) => toast.error('It did not work', { description: errorMessage(e) }))
      .finally(() => setBusy(null))
  }

  const ready = op !== 'erase' || (!!mask && !!source)

  return (
    <div className="flex flex-col gap-3">
      <p className="text-xs text-muted-foreground">
        {LOCAL_COPY[op].hint} Runs on this device: free, nothing is uploaded. The first time downloads the model (
        {LOCAL_COPY[op].model}).
      </p>
      {op === 'erase' && (
        <Button type="button" variant="outline" onClick={paint} disabled={!!busy}>
          <Paintbrush /> {mask ? 'Paint the area again' : 'Paint the area'}
        </Button>
      )}
      {op === 'erase' && mask && <p className="text-xs text-muted-foreground">Area painted.</p>}
      {/* No spinner while it works: an animation redrawn on a slow graphics card starves the model. */}
      <Button type="button" onClick={run} disabled={!ready || !!busy}>
        <Sparkles /> {busy ?? LOCAL_COPY[op].run}
      </Button>
      {source && painting && (
        <MaskDialog open onOpenChange={setPainting} picture={source} title={AI_OPS.erase.label} onDone={setMask} />
      )}
    </div>
  )
}

function RunView({ ctx, run }: { ctx: PanelCtx; run: AiRun }) {
  const job = useAiJob(run.jobId)
  const j = job.data
  const qc = useQueryClient()

  // The allowance ran out with this request: the status says so, and the on-device tools show.
  useEffect(() => {
    if (run.limited) {
      void qc.invalidateQueries({ queryKey: qk.ai })
    }
  }, [run.limited, qc])
  const [keeping, setKeeping] = useState(false)
  const copy = AI_OPS[run.op]

  const discard = () => {
    if (run.jobId) {
      void api.discardAiJob(run.jobId).catch(() => undefined)
    }

    setRun(null)
  }

  const again = () => {
    if (run.jobId) {
      void api.discardAiJob(run.jobId).catch(() => undefined)
    }

    send({ ...run, key: requestKey(), jobId: null })
  }

  const keep = () => {
    if (!run.jobId) {
      return
    }

    setKeeping(true)
    void api
      .acceptAiJob(run.jobId)
      .then((media) => {
        apply(ctx, run, media)
        setRun(null)
        toast.success('Added to the design', { description: 'It is in your library too. Undo takes it back.' })
      })
      .catch((e: unknown) => toast.error('Could not keep it', { description: errorMessage(e) }))
      .finally(() => setKeeping(false))
  }

  if (run.error) {
    return (
      <div className="flex flex-col gap-3">
        <Callout tone="danger" role="alert">
          {run.error}
        </Callout>
        <div className="flex gap-2">
          <Button type="button" onClick={() => send(run)}>
            <RotateCcw /> Try again
          </Button>
          <Button type="button" variant="ghost" onClick={() => setRun(null)}>
            Back
          </Button>
        </div>
      </div>
    )
  }

  if (!j || isLive(j)) {
    return (
      <div className="flex flex-col gap-3" role="status">
        <p className="font-medium">{copy.label}</p>
        <div className="flex items-center gap-2 text-muted-foreground">
          <Loader2 className="animate-spin" />
          <span>
            {j?.status === 'running' ? 'Working on it' : 'Starting'}, usually under a minute: <Elapsed />
          </span>
        </div>
        <p className="text-xs text-muted-foreground">
          You can keep editing meanwhile; the page does not change until you keep the result.
        </p>
        <Button type="button" variant="outline" onClick={discard}>
          <X /> Cancel
        </Button>
      </div>
    )
  }

  if (j.status !== 'succeeded' || !j.result) {
    return (
      <div className="flex flex-col gap-3">
        <Callout tone={j.status === 'canceled' ? 'neutral' : 'danger'} role="alert">
          {j.error?.message ?? 'It did not work.'}
        </Callout>
        <div className="flex gap-2">
          <Button type="button" onClick={again}>
            <RotateCcw /> Try again
          </Button>
          <Button type="button" variant="ghost" onClick={() => setRun(null)}>
            Back
          </Button>
        </div>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-3">
      <p className="font-medium">{copy.label}: the result</p>
      <div className="overflow-hidden rounded-md border bg-[repeating-conic-gradient(var(--muted)_0_25%,transparent_0_50%)] bg-[length:16px_16px]">
        <img src={aiResultUrl(j.id)} alt="What AI made" className="mx-auto block max-h-72 w-auto" />
      </div>
      <p className="text-xs text-muted-foreground">{madeBy(j)}</p>
      <div className="flex flex-wrap gap-2">
        <Button type="button" onClick={keep} disabled={keeping}>
          {keeping ? <Loader2 className="animate-spin" /> : <Check />} Use it
        </Button>
        <Button type="button" variant="outline" onClick={again} disabled={keeping}>
          <RotateCcw /> Try again
        </Button>
        <Button type="button" variant="ghost" onClick={discard} disabled={keeping}>
          Discard
        </Button>
      </div>
    </div>
  )
}
