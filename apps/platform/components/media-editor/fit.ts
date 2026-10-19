// Channel fit for a picture, without the editor: what a platform refuses (its hard limits from the API's
// provider catalog) apart from what only looks better (the recommended shapes in PLACEMENTS), and the math
// for making a version that fits (crop, pad).
import { PLACEMENTS } from '@staqly/brand/placements'

import type { ImageRules, Provider } from '@/lib/api'

export { PLACEMENTS }

export interface Picture {
  width: number
  height: number
  bytes: number
  mime: string
  /** Has see-through pixels (a PNG or WebP with alpha). */
  alpha: boolean
}

export type FitStatus = 'unsupported' | 'adjust' | 'ok'

export interface PlacementFit {
  provider: string
  platform: string
  name: string
  ratio: number
  label: string
  /** unsupported: the platform refuses it; adjust: it takes it but crops it; ok: fits as it is. */
  status: FitStatus
  reasons: string[]
  /** Share of the picture the placement crops when it fills the frame (0 = none). */
  crop: number
}

const MB = 1024 * 1024

export function formatBytes(n: number) {
  return n >= MB ? `${(n / MB).toFixed(n >= 10 * MB ? 0 : 1)} MB` : `${Math.max(1, Math.round(n / 1024))} KB`
}

export function ratioLabel(w: number, h: number) {
  const known: [number, string][] = [
    [1, '1:1'],
    [4 / 5, '4:5'],
    [9 / 16, '9:16'],
    [16 / 9, '16:9'],
    [1.91, '1.91:1'],
    [2 / 3, '2:3'],
    [3 / 4, '3:4'],
    [4 / 3, '4:3'],
  ]
  const r = w / h
  const hit = known.find(([k]) => Math.abs(k - r) / k < 0.01)

  return hit ? hit[1] : `${r.toFixed(2)}:1`
}

/** How much of the picture a frame of ratio target crops when the picture fills it. */
export function cropShare(width: number, height: number, target: number) {
  const r = width / height

  return Math.max(0, 1 - Math.min(r, target) / Math.max(r, target))
}

const FORMAT_NAMES: Record<string, string> = {
  'image/jpeg': 'JPG',
  'image/png': 'PNG',
  'image/webp': 'WebP',
  'image/gif': 'GIF',
  'image/bmp': 'BMP',
  'image/tiff': 'TIFF',
}

/** The platform's hard limits a picture breaks, in plain words (empty: it takes the picture). */
export function hardProblems(p: Picture, provider: Provider | undefined): string[] {
  const out: string[] = []
  const kinds = provider?.limits?.media_kinds

  if (provider && kinds && !kinds.includes('image')) {
    return [`${provider.name} takes no pictures`]
  }

  const rules: ImageRules = provider?.limits?.image ?? {}
  const ratio = p.width / p.height

  if (rules.formats?.length && !rules.formats.includes(p.mime)) {
    out.push(`Takes ${rules.formats.map((f) => FORMAT_NAMES[f] ?? f).join(', ')} only`)
  }

  if (rules.max_bytes && p.bytes > rules.max_bytes) {
    out.push(`Larger than ${formatBytes(rules.max_bytes)}`)
  }

  if (rules.min_ratio && ratio < rules.min_ratio - 0.005) {
    out.push(`Too tall: ${ratioLabel(p.width, p.height)}, at most ${ratioLabel(rules.min_ratio, 1)} upright`)
  }

  if (rules.max_ratio && ratio > rules.max_ratio + 0.005) {
    out.push(`Too wide: ${ratioLabel(p.width, p.height)}, at most ${ratioLabel(rules.max_ratio, 1)}`)
  }

  if (rules.min_width && p.width < rules.min_width) {
    out.push(`Narrower than ${rules.min_width} px`)
  }

  if (rules.max_pixels && p.width * p.height > rules.max_pixels) {
    out.push(`More than ${Math.round(rules.max_pixels / 1e6)} megapixels`)
  }

  return out
}

/** Notes that are not refusals: a JPG-only platform flattens see-through pixels onto white. */
export function softNotes(p: Picture, provider: Provider | undefined): string[] {
  const formats = provider?.limits?.image?.formats

  if (p.alpha && formats?.length && formats.every((f) => f === 'image/jpeg' || f === 'image/bmp')) {
    return ['See-through areas turn white']
  }

  return []
}

/** One row per placement: refused, takes it but crops it, or fits. */
export function placementFits(p: Picture, providers: Provider[]): PlacementFit[] {
  const byId = new Map(providers.map((x) => [x.id, x]))

  return PLACEMENTS.map((pl) => {
    const provider = byId.get(pl.provider)
    const ratio = pl.w / pl.h
    const hard = hardProblems(p, provider)
    const crop = cropShare(p.width, p.height, ratio)
    const status: FitStatus = hard.length ? 'unsupported' : crop >= 0.01 ? 'adjust' : 'ok'
    const reasons = hard.length
      ? hard
      : [
          ...(crop >= 0.01 ? [`Shown at ${ratioLabel(pl.w, pl.h)}: crops ${Math.round(crop * 100)}%`] : []),
          ...softNotes(p, provider),
        ]

    return { ...pl, ratio, label: ratioLabel(pl.w, pl.h), status, reasons, crop }
  })
}

export interface Rect {
  x: number
  y: number
  w: number
  h: number
}

const clamp = (v: number, max: number) => Math.min(Math.max(0, Math.round(v)), max)

/** The largest rect of the target ratio inside w x h, centred on focus (0..1 each way) as far as it can be. */
export function cropRect(w: number, h: number, target: number, focus = { x: 0.5, y: 0.5 }): Rect {
  let cw = w
  let ch = Math.round(w / target)

  if (ch > h) {
    ch = h
    cw = Math.round(h * target)
  }

  return { x: clamp(focus.x * w - cw / 2, w - cw), y: clamp(focus.y * h - ch / 2, h - ch), w: cw, h: ch }
}

/** The canvas of the target ratio that holds w x h whole, and where the picture sits in it (centred). */
export function padRect(w: number, h: number, target: number): { canvas: { w: number; h: number }; at: Rect } {
  const cw = Math.max(w, Math.round(h * target))
  const ch = Math.max(h, Math.round(w / target))
  const canvas = cw / ch > target ? { w: cw, h: Math.round(cw / target) } : { w: Math.round(ch * target), h: ch }

  return { canvas, at: { x: Math.round((canvas.w - w) / 2), y: Math.round((canvas.h - h) / 2), w, h } }
}

/**
 * Where the picture's subject probably is: the centre of mass of its detail (edge strength), from RGBA pixels.
 * A cheap stand-in for saliency: busy areas pull the crop, flat sky and walls do not. Returns 0..1 each way.
 */
export function detailCentre(rgba: Uint8ClampedArray, w: number, h: number): { x: number; y: number } {
  let sx = 0
  let sy = 0
  let sum = 0
  const lum = (i: number) => rgba[i] * 0.299 + rgba[i + 1] * 0.587 + rgba[i + 2] * 0.114

  for (let y = 1; y < h - 1; y++) {
    for (let x = 1; x < w - 1; x++) {
      const i = (y * w + x) * 4
      const e = Math.abs(lum(i + 4) - lum(i - 4)) + Math.abs(lum(i + w * 4) - lum(i - w * 4))

      sx += e * x
      sy += e * y
      sum += e
    }
  }

  return sum ? { x: sx / sum / w, y: sy / sum / h } : { x: 0.5, y: 0.5 }
}

/** Output size of a variant: the source's pixels kept, capped to a long edge (no upscaling). */
export function outputSize(rect: { w: number; h: number }, maxEdge = 4096) {
  const s = Math.min(1, maxEdge / Math.max(rect.w, rect.h))

  return { w: Math.round(rect.w * s), h: Math.round(rect.h * s) }
}
