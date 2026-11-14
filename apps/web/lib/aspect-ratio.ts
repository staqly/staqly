// The aspect ratio checker (/tools/aspect-ratio-checker): pure math, checked by scripts/check-tools.mjs.

/**
 * Common ratios per placement, creator platforms first. Ratios only, not pixel sizes: sizes change often, the shapes
 * much less. Platforms do change their specs, so this is the one table to update.
 */
export type Placement = { id: string; provider: string; name: string; w: number; h: number; note?: string }

export const PLACEMENTS: Placement[] = [
  { id: 'ig-square', provider: 'instagram', name: 'Instagram feed, square', w: 1, h: 1 },
  { id: 'ig-portrait', provider: 'instagram', name: 'Instagram feed, portrait', w: 4, h: 5 },
  { id: 'ig-landscape', provider: 'instagram', name: 'Instagram feed, landscape', w: 1.91, h: 1 },
  { id: 'ig-story', provider: 'instagram', name: 'Instagram Stories and Reels', w: 9, h: 16 },
  { id: 'tiktok', provider: 'tiktok', name: 'TikTok', w: 9, h: 16 },
  { id: 'yt-video', provider: 'youtube', name: 'YouTube video', w: 16, h: 9 },
  { id: 'yt-shorts', provider: 'youtube', name: 'YouTube Shorts', w: 9, h: 16 },
  { id: 'x', provider: 'x', name: 'X, single image', w: 16, h: 9 },
  { id: 'linkedin', provider: 'linkedin', name: 'LinkedIn, landscape image', w: 1.91, h: 1 },
  { id: 'facebook', provider: 'facebook', name: 'Facebook feed, square', w: 1, h: 1 },
  { id: 'threads', provider: 'threads', name: 'Threads, portrait', w: 4, h: 5 },
  { id: 'pinterest', provider: 'pinterest', name: 'Pinterest pin', w: 2, h: 3 },
]

/** The shapes worth suggesting, from tall to wide. */
export const COMMON: { w: number; h: number }[] = [
  { w: 9, h: 16 },
  { w: 2, h: 3 },
  { w: 4, h: 5 },
  { w: 1, h: 1 },
  { w: 16, h: 9 },
  { w: 1.91, h: 1 },
]

const gcd = (a: number, b: number): number => (b === 0 ? a : gcd(b, a % b))

/** 1080x1350 -> "4:5". Odd sizes that don't reduce well (1921x1080) are named after the common ratio they are within 1% of. */
export function ratioLabel(width: number, height: number): string {
  const w = Math.round(width)
  const h = Math.round(height)
  const g = gcd(w, h) || 1
  const [a, b] = [w / g, h / g]

  if (a <= 32 && b <= 32) {
    return `${a}:${b}`
  }

  const near = COMMON.find((c) => Math.abs(w / h / (c.w / c.h) - 1) < 0.01)

  return near ? `about ${shapeLabel(near)}` : `${(w / h).toFixed(2)}:1`
}

export const shapeLabel = (s: { w: number; h: number }) => `${s.w}:${s.h}`

/** Within 1% of the target counts as a fit: platforms round sizes too. */
export const fits = (ratio: number, target: number) => Math.abs(ratio / target - 1) < 0.01

/**
 * Filling a frame of ratio `target` with a picture of ratio `ratio`: the share of the picture cut off, and from where.
 * Fitting it instead (letterbox): the share of the frame left as bars.
 */
export function crop(ratio: number, target: number): { share: number; from: 'sides' | 'top and bottom' | 'none' } {
  if (fits(ratio, target)) {
    return { share: 0, from: 'none' }
  }

  return ratio > target
    ? { share: 1 - target / ratio, from: 'sides' }
    : { share: 1 - ratio / target, from: 'top and bottom' }
}

export function bars(ratio: number, target: number): { share: number; where: 'top and bottom' | 'sides' | 'none' } {
  if (fits(ratio, target)) {
    return { share: 0, where: 'none' }
  }

  return ratio > target
    ? { share: 1 - target / ratio, where: 'top and bottom' }
    : { share: 1 - ratio / target, where: 'sides' }
}

/** The common shape closest to `ratio` (compared on a log scale, so 2:1 and 1:2 are as far from 1:1), and its height at `width`. */
export function closest(ratio: number, width: number) {
  const best = COMMON.reduce((a, c) =>
    Math.abs(Math.log(ratio / (c.w / c.h))) < Math.abs(Math.log(ratio / (a.w / a.h))) ? c : a,
  )

  return { shape: best, width: Math.round(width), height: Math.round(width / (best.w / best.h)) }
}

/** Percent with no decimals, but never "0%" for a real crop. */
export const percent = (share: number) => (share > 0 && share < 0.01 ? '<1%' : `${Math.round(share * 100)}%`)
