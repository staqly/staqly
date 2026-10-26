// The design editor's document: pages of one pixel size, each with a background and elements stacked bottom
// to top (several pages make a carousel). Plain JSON, saved as the file of a media project (/v1/media-projects)
// next to the pictures it exports, one per page.
import { PLACEMENTS } from '@staqly/brand/placements'

export type Fill = { kind: 'color'; color: string } | Gradient

/** From one colour to another (through a third one when `via` is set), along an angle or out from the centre. */
export interface Gradient {
  kind: 'gradient'
  from: string
  via?: string
  to: string
  angle: number
  radial?: boolean
}

/** The element turns see-through towards one side: fully at that edge, not at all `size` (a share) away. */
export interface Fade {
  side: 'left' | 'right' | 'top' | 'bottom'
  size: number
}

interface Base {
  id: string
  x: number
  y: number
  width: number
  height: number
  rotation: number
  opacity: number
  locked?: boolean
  hidden?: boolean
  shadow?: { color: string; blur: number; x: number; y: number; opacity: number }
  /** Pictures and filled shapes only. */
  fade?: Fade
}

export interface TextEl extends Base {
  type: 'text'
  text: string
  fontFamily: string
  fontSize: number
  bold: boolean
  italic: boolean
  underline: boolean
  fill: string
  align: 'left' | 'center' | 'right'
  lineHeight: number
  letterSpacing: number
  stroke?: { color: string; width: number }
  /** A box behind the text (a highlight, a label). */
  box?: { color: string; padding: number; radius: number }
}

export interface ImageEl extends Base {
  type: 'image'
  src: string
  /** The library file it came from, or the Unsplash photo (credited on export). */
  mediaId?: string
  stock?: { id: string; author: string; url: string }
  flipX?: boolean
  flipY?: boolean
  radius: number
  filters: { brightness: number; contrast: number; saturation: number; blur: number; grayscale: boolean }
  /** What part of the picture the frame shows: it always fills the frame, centred on (cx, cy), zoomed in. */
  crop?: Crop
  /** A shape the picture is cut to. */
  mask?: MaskKind
}

export interface Crop {
  /** The centre, as a share of the picture's width and height. */
  cx: number
  cy: number
  /** 1 = as much of the picture as fits the frame's shape; 2 = half of that. */
  zoom: number
}

export type MaskKind = 'circle' | 'triangle' | 'star' | 'hexagon' | 'heart' | 'arch'

export type ShapeKind = 'rect' | 'ellipse' | 'triangle' | 'star' | 'line' | 'arrow'

export interface ShapeEl extends Base {
  type: 'shape'
  shape: ShapeKind
  fill: string
  /** Paints the shape instead of `fill` (not lines and arrows). */
  gradient?: Gradient
  stroke: string
  strokeWidth: number
  radius: number
}

export type El = TextEl | ImageEl | ShapeEl

export interface Page {
  id: string
  background: Fill
  elements: El[]
  /** The library file this page last saved as (saving again replaces it). */
  mediaId?: string
}

export interface Design {
  version: 1
  name: string
  width: number
  height: number
  pages: Page[]
}

/** The size of a page, what new elements are placed by. */
export type Size = Pick<Design, 'width' | 'height'>

export const uid = () => Math.random().toString(36).slice(2, 10)

export const NO_FILTERS: ImageEl['filters'] = { brightness: 0, contrast: 0, saturation: 0, blur: 0, grayscale: false }

export function newPage(): Page {
  return { id: uid(), background: { kind: 'color', color: '#ffffff' }, elements: [] }
}

export function newDesign(width = 1080, height = 1080): Design {
  return { version: 1, name: 'Untitled design', width, height, pages: [newPage()] }
}

/** A copy of a page with new ids, not tied to the file the original saved as. */
export function copyPage(p: Page): Page {
  return {
    ...structuredClone(p),
    id: uid(),
    mediaId: undefined,
    elements: p.elements.map((e) => ({ ...structuredClone(e), id: uid() })),
  }
}

/**
 * The part of a picture (in its pixels) a frame of w x h shows: the largest area of the frame's shape, divided
 * by the zoom, around the crop's centre, kept inside the picture.
 */
export function cropRect(imgW: number, imgH: number, w: number, h: number, c: Crop = { cx: 0.5, cy: 0.5, zoom: 1 }) {
  const r = w / h
  const fitW = imgW / imgH > r ? imgH * r : imgW
  const width = fitW / Math.max(1, c.zoom)
  const height = width / r
  const x = Math.min(imgW - width, Math.max(0, c.cx * imgW - width / 2))
  const y = Math.min(imgH - height, Math.max(0, c.cy * imgH - height / 2))

  return { x, y, width, height }
}

export function newText(d: Size, preset: 'heading' | 'subheading' | 'body'): TextEl {
  const size = { heading: 0.09, subheading: 0.055, body: 0.035 }[preset] * Math.min(d.width, d.height)
  const width = d.width * 0.8

  return {
    id: uid(),
    type: 'text',
    text: { heading: 'Add a heading', subheading: 'Add a subheading', body: 'Add a little bit of body text' }[preset],
    x: (d.width - width) / 2,
    y: d.height / 2 - size / 2,
    width,
    height: size * 1.2,
    rotation: 0,
    opacity: 1,
    fontFamily: preset === 'body' ? 'Inter' : 'Montserrat',
    fontSize: Math.round(size),
    bold: preset !== 'body',
    italic: false,
    underline: false,
    fill: '#111827',
    align: 'center',
    lineHeight: 1.2,
    letterSpacing: 0,
  }
}

export function newShape(d: Size, shape: ShapeKind): ShapeEl {
  const s = Math.min(d.width, d.height) * 0.3
  const line = shape === 'line' || shape === 'arrow'

  return {
    id: uid(),
    type: 'shape',
    shape,
    x: (d.width - s) / 2,
    y: (d.height - (line ? 20 : s)) / 2,
    width: s,
    height: line ? 20 : s,
    rotation: 0,
    opacity: 1,
    fill: line ? '#111827' : '#6366f1',
    stroke: '#111827',
    strokeWidth: line ? 8 : 0,
    radius: 0,
  }
}

/** A picture placed in the middle, at most 60 % of the page (or covering it, as the background). */
export function newImage(
  d: Size,
  src: string,
  w: number,
  h: number,
  extra: Partial<ImageEl> = {},
  cover = false,
): ImageEl {
  const k = cover ? Math.max(d.width / w, d.height / h) : Math.min(1, (0.6 * d.width) / w, (0.6 * d.height) / h)
  const width = w * k
  const height = h * k

  return {
    id: uid(),
    type: 'image',
    src,
    x: (d.width - width) / 2,
    y: (d.height - height) / 2,
    width,
    height,
    rotation: 0,
    opacity: 1,
    radius: 0,
    filters: { ...NO_FILTERS },
    ...extra,
  }
}

/** Page sizes per placement: the long side 1080 px for portrait and square, the short side 1080 for landscape. */
export const SIZES = [
  ...PLACEMENTS.map((p) => {
    const r = p.w / p.h

    return {
      label: `${p.platform} ${p.name}`,
      width: r <= 1 ? 1080 : Math.round(1080 * r),
      height: r <= 1 ? Math.round(1080 / r) : 1080,
    }
  }),
  { label: 'YouTube Thumbnail', width: 1280, height: 720 },
  { label: 'Open Graph image', width: 1200, height: 630 },
]

/**
 * A new page size with the content kept: everything scales by the smaller factor and the layout stays
 * centred, so a square post becomes a story without anything falling off.
 */
export function resize(d: Design, width: number, height: number): Design {
  const k = Math.min(width / d.width, height / d.height)
  const dx = (width - d.width * k) / 2
  const dy = (height - d.height * k) / 2

  const fit = (els: El[]) =>
    els.map((e) => {
      const out = { ...e, x: e.x * k + dx, y: e.y * k + dy, width: e.width * k, height: e.height * k }

      if (out.type === 'text') {
        out.fontSize = Math.max(1, Math.round(out.fontSize * k))
      }

      if (out.type === 'shape') {
        out.strokeWidth *= k
        out.radius *= k
      }

      if (out.type === 'image') {
        out.radius *= k
      }

      if (out.type === 'text' && out.box) {
        out.box = { ...out.box, padding: out.box.padding * k, radius: out.box.radius * k }
      }

      return out
    })

  return { ...d, width, height, pages: d.pages.map((p) => ({ ...p, elements: fit(p.elements) })) }
}

/** A document read from a project file, or null when the file is not one (a .gpex project of the former photo editor). */
export function parseDesign(text: string): Design | null {
  try {
    const d = JSON.parse(text) as Design & Partial<Page>

    if (d?.version !== 1 || !(d.width > 0 && d.height > 0)) {
      return null
    }

    // Designs of the first day had one page, its background and elements at the top.
    if (!Array.isArray(d.pages) && Array.isArray(d.elements) && d.background) {
      const { background, elements, ...rest } = d

      return { ...rest, pages: [{ id: uid(), background, elements }] }
    }

    return Array.isArray(d.pages) && d.pages.length ? d : null
  } catch {
    return null
  }
}

/** The "Photo by X on Unsplash" lines a design owes its stock photos (empty without any). */
export function credits(d: Design): string[] {
  return [
    ...new Set(
      d.pages.flatMap((p) =>
        p.elements.flatMap((e) => (e.type === 'image' && e.stock ? [`Photo by ${e.stock.author} on Unsplash`] : [])),
      ),
    ),
  ]
}
