'use client'

import { useQuery } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { useRef, useState } from 'react'

import { Callout } from '@/components/common/callout'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { errorMessage, type Provider } from '@/lib/api'

import { aiExpand } from './ai-ops'
import { BlobImage } from './blob-image'
import type { Facts } from './channels-panel'
import { cropRect, detailCentre, outputSize, type PlacementFit } from './fit'
import { cropTo, encodeFor, padTo, type PadFill } from './raster'

export type Strategy = 'crop' | 'padding' | 'AI expand'

export type VariantPlan =
  | { kind: 'crop'; focus?: { x: number; y: number } }
  | { kind: 'pad'; fill: PadFill }
  | { kind: 'expand'; model: string; signal?: AbortSignal }

/** A version of the picture for one placement, encoded the way the platform takes it. */
export async function makeVariant(facts: Facts, fit: PlacementFit, plan: VariantPlan, provider?: Provider) {
  let out: Blob

  if (plan.kind === 'crop') {
    const focus = plan.focus ?? detailCentre(facts.small.data, facts.small.w, facts.small.h)
    const rect = cropRect(facts.width, facts.height, fit.ratio, focus)

    out = await cropTo(facts.blob, rect, outputSize(rect))
  } else if (plan.kind === 'pad') {
    out = await padTo(facts.blob, fit.ratio, plan.fill)
  } else {
    out = await aiExpand(facts.blob, fit.ratio, plan.model, plan.signal)
  }

  const rules = provider?.limits?.image

  return encodeFor(out, rules?.formats, rules?.max_bytes)
}

/**
 * Optimize for one placement: smart crop (centred on the picture's detail; click the original to move it), fit
 * with padding (blurred copy, a colour, or see-through where the platform keeps it) or AI expand. Shows the
 * original and the result side by side before anything changes.
 */
export function OptimizeDialog({
  facts,
  fit,
  provider,
  canExpand,
  model,
  onClose,
  onApply,
}: {
  facts: Facts
  fit: PlacementFit
  provider?: Provider
  canExpand: boolean
  model: string
  onClose: () => void
  onApply: (blob: Blob, strategy: Strategy) => Promise<void>
}) {
  const [strategy, setStrategy] = useState<Strategy>('crop')
  const [focus, setFocus] = useState<{ x: number; y: number } | undefined>()
  const [fill, setFill] = useState<'blur' | 'color' | 'transparent'>('blur')
  const [color, setColor] = useState('#ffffff')
  const [expanded, setExpanded] = useState<Blob | null>(null)
  const [expanding, setExpanding] = useState(false)
  const [expandError, setExpandError] = useState('')
  const [applying, setApplying] = useState(false)
  const [applyError, setApplyError] = useState('')
  const abort = useRef<AbortController | null>(null)
  const jpegOnly =
    !!provider?.limits?.image?.formats?.length && provider.limits.image.formats.every((f) => f === 'image/jpeg')
  // Crop and padding are free and quick: the preview follows every change. AI expand is a paid request: only
  // when asked.
  const local = useQuery({
    queryKey: ['media-editor', 'optimize', fit.provider, fit.name, strategy, focus, fill, color],
    queryFn: () =>
      makeVariant(
        facts,
        fit,
        strategy === 'crop'
          ? { kind: 'crop', focus }
          : { kind: 'pad', fill: fill === 'color' ? { kind: 'color', color } : { kind: fill } },
        provider,
      ),
    enabled: strategy !== 'AI expand',
    gcTime: 0,
    retry: false,
  })
  const result = strategy === 'AI expand' ? expanded : (local.data ?? null)
  const busy = strategy === 'AI expand' ? expanding : local.isFetching
  const error = applyError || (strategy === 'AI expand' ? expandError : local.error ? errorMessage(local.error) : '')

  const expand = () => {
    const ctrl = new AbortController()

    abort.current = ctrl
    setExpanding(true)
    setExpandError('')
    makeVariant(facts, fit, { kind: 'expand', model, signal: ctrl.signal }, provider)
      .then(setExpanded, (e: unknown) => {
        if (!(e instanceof DOMException && e.name === 'AbortError')) {
          setExpandError(errorMessage(e))
        }
      })
      .then(() => setExpanding(false))
  }

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>
            Optimize for {fit.platform} {fit.name} ({fit.label})
          </DialogTitle>
          <DialogDescription>{fit.reasons.join(' · ') || 'Make a version in this shape.'}</DialogDescription>
        </DialogHeader>
        <Tabs value={strategy} onValueChange={(v) => setStrategy(v as Strategy)}>
          <TabsList>
            <TabsTrigger value="crop">Smart crop</TabsTrigger>
            <TabsTrigger value="padding">Fit with padding</TabsTrigger>
            <TabsTrigger value="AI expand" disabled={!canExpand}>
              AI expand
            </TabsTrigger>
          </TabsList>
        </Tabs>
        {strategy === 'padding' && (
          <RadioGroup
            value={fill}
            onValueChange={(v) => setFill(v as typeof fill)}
            className="flex flex-wrap gap-4 text-sm"
            aria-label="Fill the rest with"
          >
            <Label className="flex items-center gap-2 font-normal">
              <RadioGroupItem value="blur" /> A blurred copy
            </Label>
            <Label className="flex items-center gap-2 font-normal">
              <RadioGroupItem value="color" /> A colour
              <input
                type="color"
                value={color}
                aria-label="Padding colour"
                onChange={(e) => {
                  setColor(e.target.value)
                  setFill('color')
                }}
                className="size-6 rounded border"
              />
            </Label>
            <Label className="flex items-center gap-2 font-normal">
              <RadioGroupItem value="transparent" disabled={jpegOnly} /> See-through
              {jpegOnly && <span className="text-xs text-muted-foreground">({fit.platform} takes JPG only)</span>}
            </Label>
          </RadioGroup>
        )}
        {!canExpand && strategy !== 'AI expand' && (
          <p className="text-xs text-muted-foreground">
            AI expand uses your workspace&apos;s OpenAI key:{' '}
            {/* oxlint-disable-next-line nextjs/no-html-link-for-pages -- leaves the isolated editor page */}
            <a className="text-brand underline-offset-2 hover:underline" href="/integrations/ai">
              set up AI
            </a>
            .
          </p>
        )}
        <div className="grid grid-cols-2 gap-3">
          <figure className="flex flex-col gap-1">
            <figcaption className="text-xs text-muted-foreground">
              Original{strategy === 'crop' ? ': click where the crop should centre' : ''}
            </figcaption>
            <button
              type="button"
              className={`rounded-md bg-muted ${strategy === 'crop' ? 'cursor-crosshair' : 'cursor-default'}`}
              aria-label={strategy === 'crop' ? 'Centre the crop where you click' : 'Original'}
              onClick={(e) => {
                if (strategy !== 'crop') {
                  return
                }

                const r = e.currentTarget.getBoundingClientRect()
                const s = Math.min(r.width / facts.width, r.height / facts.height)
                const ox = (r.width - facts.width * s) / 2
                const oy = (r.height - facts.height * s) / 2

                setFocus({
                  x: Math.min(1, Math.max(0, (e.clientX - r.left - ox) / (facts.width * s))),
                  y: Math.min(1, Math.max(0, (e.clientY - r.top - oy) / (facts.height * s))),
                })
              }}
            >
              <BlobImage blob={facts.blob} alt="Before" className="max-h-80 w-full object-contain" />
            </button>
          </figure>
          <figure className="flex flex-col gap-1">
            <figcaption className="text-xs text-muted-foreground">
              {fit.platform} {fit.name}
              {result ? ` · ${result.type === 'image/jpeg' ? 'JPG' : 'PNG'}` : ''}
            </figcaption>
            <div className="flex max-h-80 min-h-40 items-center justify-center rounded-md bg-muted">
              {busy ? (
                <Loader2 className="size-5 animate-spin text-muted-foreground" aria-label="Working" />
              ) : result ? (
                <BlobImage blob={result} alt="After" className="max-h-80 w-full object-contain" />
              ) : strategy === 'AI expand' ? (
                <Button variant="outline" onClick={expand}>
                  Extend with AI
                </Button>
              ) : null}
            </div>
          </figure>
        </div>
        {error && <Callout tone="danger">{error}</Callout>}
        <DialogFooter>
          <Button
            variant="outline"
            onClick={() => {
              abort.current?.abort()
              onClose()
            }}
          >
            Cancel
          </Button>
          <Button
            disabled={!result || busy || applying}
            onClick={() => {
              setApplying(true)
              setApplyError('')
              onApply(result!, strategy)
                .catch((e: unknown) => setApplyError(errorMessage(e)))
                .then(() => setApplying(false))
            }}
            data-testid="optimize-apply"
          >
            Use this version
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
