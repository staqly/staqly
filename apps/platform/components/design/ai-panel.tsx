'use client'

import { useQueryClient } from '@tanstack/react-query'
import { Check, Loader2, Paintbrush, RotateCcw, Sparkles, X } from 'lucide-react'
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

import { newImage, type Design, type ImageEl } from './doc'
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

export function AiPanel({ ctx }: { ctx: PanelCtx }) {
  const status = useAiStatus()
  const run = useRun()
  const sel = ctx.selected.length === 1 ? ctx.page.elements.find((e) => e.id === ctx.selected[0]) : undefined
  const picture = sel?.type === 'image' ? sel : undefined
  const usedUp = allowanceUsedUp(status.data)
  // With the allowance used up the server's tools wait for next month; without AI on the server there are none.
  const ops = usedUp ? NONE : (status.data?.operations ?? NONE).filter((o) => o.kind === 'image')
  const usable = ops.filter((o) => !o.image || picture)
  // Removing an object and upscaling also run on this device: offered when the server doesn't do them now.
  const local = LOCAL_OPS.filter((id) => !ops.some((o) => o.id === id))
  const [chosen, setChosen] = useState<string | null>(null)
  const op =
    usable.find((o) => o.id === chosen) ??
    (local.includes(chosen as LocalOp) ? undefined : usable.find((o) => o.id === (picture ? 'edit' : 'generate'))) ??
    (local.includes(chosen as LocalOp) ? undefined : usable[0])
  const localOp = !op && picture ? (local.find((id) => id === chosen) ?? local[0]) : undefined

  if (status.isPending) {
    return <p className="text-sm text-muted-foreground">Loading the AI tools...</p>
  }

  const note = usedUp ? '' : allowanceNote(status.data)

  return (
    <div className="flex flex-col gap-4 text-sm">
      {usedUp && (
        <Callout tone="info">
          This month&apos;s AI allowance is used up; it starts again on {usedUp.renews}. Removing an object and
          upscaling still work, on this device.
        </Callout>
      )}
      {run ? (
        <RunView ctx={ctx} run={run} />
      ) : (
        <>
          <div role="radiogroup" aria-label="AI tool" className="flex flex-wrap gap-1.5">
            {local.map((id) => (
              <button
                key={id}
                type="button"
                role="radio"
                aria-checked={localOp === id}
                disabled={!picture}
                onClick={() => setChosen(id)}
                className={cn(
                  'rounded-full border px-2.5 py-1 text-xs hover:bg-muted disabled:opacity-50 pointer-coarse:py-2',
                  localOp === id && 'border-brand bg-brand-soft text-brand',
                )}
              >
                {AI_OPS[id].label} <span className="text-muted-foreground">· this device</span>
              </button>
            ))}
            {usable.map((o) => (
              <button
                key={o.id}
                type="button"
                role="radio"
                aria-checked={op?.id === o.id}
                onClick={() => setChosen(o.id)}
                className={cn(
                  'rounded-full border px-2.5 py-1 text-xs hover:bg-muted pointer-coarse:py-2',
                  op?.id === o.id && 'border-brand bg-brand-soft text-brand',
                )}
              >
                {AI_OPS[o.id].label}
              </button>
            ))}
          </div>
          {op && <AiForm key={`${op.id}:${picture?.id ?? ''}`} ctx={ctx} op={op} picture={picture} />}
          {localOp && picture && (
            <LocalForm key={`${localOp}:${picture.id}`} ctx={ctx} op={localOp} picture={picture} />
          )}
          {!picture && (ops.some((o) => o.image) || local.length > 0) && (
            <p className="text-xs text-muted-foreground">Select a photo on the page to change it with AI.</p>
          )}
        </>
      )}
      {note && <p className="text-xs text-muted-foreground">{note}</p>}
    </div>
  )
}

function AiForm({ ctx, op, picture }: { ctx: PanelCtx; op: AIOperation; picture?: ImageEl }) {
  const [prompt, setPrompt] = useState('')
  const [preparing, setPreparing] = useState(false)
  // The picture the mask is painted on (the mask matches its pixels, so it is also what is sent).
  const [source, setSource] = useState<{ url: string; width: number; height: number; blob: Blob } | null>(null)
  const [painting, setPainting] = useState(false)
  const [mask, setMask] = useState<Blob | null>(null)
  const copy = AI_OPS[op.id]
  const needsPrompt = op.prompt === 'required'

  // The picture shown for painting is a local copy: let it go when done.
  useEffect(() => () => void (source && URL.revokeObjectURL(source.url)), [source])

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
        setSource({ ...p, url: URL.createObjectURL(p.blob) })
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

const LOCAL_OPS = ['erase', 'upscale'] as const

type LocalOp = (typeof LOCAL_OPS)[number]

/** Removing an object or upscaling on this device (components/design/local-ai.ts): free, nothing uploaded. */
function LocalForm({ ctx, op, picture }: { ctx: PanelCtx; op: LocalOp; picture: ImageEl }) {
  const [source, setSource] = useState<{ url: string; width: number; height: number; blob: Blob } | null>(null)
  const [painting, setPainting] = useState(false)
  const [mask, setMask] = useState<Blob | null>(null)
  const [busy, setBusy] = useState<string | null>(null)

  useEffect(() => () => void (source && URL.revokeObjectURL(source.url)), [source])

  const paint = () => {
    if (source) {
      setPainting(true)

      return
    }

    setBusy('Opening the photo')
    void pictureOf(picture, 2048)
      .then((p) => {
        setSource({ ...p, url: URL.createObjectURL(p.blob) })
        setPainting(true)
      })
      .catch((e: unknown) => toast.error('Could not open the photo', { description: errorMessage(e) }))
      .finally(() => setBusy(null))
  }

  const run = () => {
    setBusy('Getting the model ready')

    const progress = (share: number) =>
      setBusy(share < 1 ? `Downloading the model ${Math.round(share * 100)}%` : 'Working on it')

    void import('./local-ai')
      .then(async (m) => {
        const blob =
          op === 'erase'
            ? await m.eraseOnDevice(source!.blob, mask!, progress)
            : await m.upscaleOnDevice((await pictureOf(picture, m.UPSCALE_FROM)).blob, progress)
        const name = `${op === 'erase' ? 'Object removed' : 'Upscaled'}.png`

        return api.uploadMedia(blob, name)
      })
      .then((media) => {
        swapPicture(ctx, picture.id, media)
        toast.success(op === 'erase' ? 'Object removed' : 'Upscaled four times', {
          description: 'Made on this device and saved in your library. Undo brings the photo back.',
        })
        setMask(null)
      })
      .catch((e: unknown) => toast.error('It did not work', { description: errorMessage(e) }))
      .finally(() => setBusy(null))
  }

  const ready = op === 'upscale' || (!!mask && !!source)

  return (
    <div className="flex flex-col gap-3">
      <p className="text-xs text-muted-foreground">
        {op === 'erase'
          ? 'Paint over what should go; the rest of the photo stays as it is.'
          : 'Four times the pixels, sharper than stretching (up to about 3000 pixels across).'}{' '}
        Runs on this device: free, nothing is uploaded. The first time downloads the model (
        {op === 'erase' ? 'about 200 MB' : 'about 5 MB'}).
      </p>
      {op === 'erase' && (
        <Button type="button" variant="outline" onClick={paint} disabled={!!busy}>
          <Paintbrush /> {mask ? 'Paint the area again' : 'Paint the area'}
        </Button>
      )}
      {op === 'erase' && mask && <p className="text-xs text-muted-foreground">Area painted.</p>}
      {/* No spinner while it works: an animation redrawn on a slow graphics card starves the model. */}
      <Button type="button" onClick={run} disabled={!ready || !!busy}>
        <Sparkles /> {busy ?? (op === 'erase' ? 'Remove it' : 'Upscale')}
      </Button>
      {source && painting && (
        <MaskDialog open onOpenChange={setPainting} picture={source} title={AI_OPS[op].label} onDone={setMask} />
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
