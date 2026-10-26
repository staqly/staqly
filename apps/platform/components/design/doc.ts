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
  /** Elements with the same group are picked, moved and changed together. */
  group?: string
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
  /** Paints the letters instead of `fill`. */
  gradient?: Gradient
  /** Bends the line along an arc: up to 100 (an arch) or -100 (a smile); 0 or unset is straight. */
  curve?: number
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
  /** An icon: its SVG (drawn in currentColor) and colour; `src` is the two together. */
  icon?: { svg: string; color: string }
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

/** The ids with every other member of their groups. */
export function withGroups(els: El[], ids: string[]) {
  const groups = new Set(els.flatMap((e) => (ids.includes(e.id) && e.group ? [e.group] : [])))

  return groups.size ? els.flatMap((e) => (ids.includes(e.id) || (e.group && groups.has(e.group)) ? [e.id] : [])) : ids
}

type Box = Pick<Base, 'x' | 'y' | 'width' | 'height'>

export type AlignTo = 'left' | 'center' | 'right' | 'top' | 'middle' | 'bottom'

/**
 * Lines the elements up on one edge or centre of `frame` (the selection's box, or the page for one element).
 * ponytail: by their unrotated boxes; a rotated element lines up by its frame, not what is drawn.
 */
export function align(els: Box[], to: AlignTo, frame: Box) {
  const k = { left: 0, top: 0, center: 0.5, middle: 0.5, right: 1, bottom: 1 }[to]

  for (const e of els) {
    if (to === 'left' || to === 'center' || to === 'right') {
      e.x = frame.x + (frame.width - e.width) * k
    } else {
      e.y = frame.y + (frame.height - e.height) * k
    }
  }
}

/** Equal gaps between the elements, the outermost staying where they are. */
export function distribute(els: Box[], axis: 'x' | 'y') {
  const size = axis === 'x' ? 'width' : 'height'
  const sorted = els.toSorted((a, b) => a[axis] - b[axis])
  const first = sorted[0]!
  const last = sorted.at(-1)!
  const total = sorted.reduce((n, e) => n + e[size], 0)
  const gap = (last[axis] + last[size] - first[axis] - total) / (sorted.length - 1)
  let at = first[axis]

  for (const e of sorted) {
    e[axis] = at
    at += e[size] + gap
  }
}

/** The box around them all. */
export function bounds(els: Box[]): Box {
  const x = Math.min(...els.map((e) => e.x))
  const y = Math.min(...els.map((e) => e.y))

  return {
    x,
    y,
    width: Math.max(...els.map((e) => e.x + e.width)) - x,
    height: Math.max(...els.map((e) => e.y + e.height)) - y,
  }
}

/** An icon's picture: its SVG in that colour, as a data URL. */
export const iconSrc = (svg: string, color: string) =>
  `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg.replaceAll('currentColor', color))}`

/**
 * The arc a curved text sits on, as SVG path data in a box `width` wide, with the letters (fontSize high) inside the
 * box: the arc spans `curve` % of a half circle and keeps the line's length.
 */
export function curvePath(width: number, curve: number, fontSize: number) {
  const theta = (Math.min(100, Math.abs(curve)) / 100) * Math.PI
  const r = width / theta
  const half = r * Math.sin(theta / 2)
  const sag = r * (1 - Math.cos(theta / 2))
  const c = width / 2
  const y = (v: number) => (v + fontSize).toFixed(2)

  return curve > 0
    ? `M ${(c - half).toFixed(2)} ${y(sag)} A ${r.toFixed(2)} ${r.toFixed(2)} 0 0 1 ${(c + half).toFixed(2)} ${y(sag)}`
    : `M ${(c - half).toFixed(2)} ${y(0)} A ${r.toFixed(2)} ${r.toFixed(2)} 0 0 0 ${(c + half).toFixed(2)} ${y(0)}`
}

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
