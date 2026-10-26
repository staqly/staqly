// Cutting one picture into Instagram posts: a profile grid (tiles in rows of three) or a carousel whose slides join
// up when swiped. Only the arithmetic lives here; the page draws the pieces on a canvas.
// No imports, so scripts/check-tools.mjs can run it with plain Node.

/** A region of the source picture, in its own pixels. */
export type Rect = { x: number; y: number; w: number; h: number }

/**
 * One piece: where it comes from, the part of it a profile shows (`view`, the whole piece in a carousel) and its place
 * in the order you post it (1 goes first).
 */
export type Piece = { src: Rect; view: Rect; row: number; col: number; order: number }

/** Width of every exported piece: Instagram shows posts at 1080 pixels wide and resizes anything else. */
export const OUT_WIDTH = 1080

/** Instagram's profile grid is always three columns wide. */
export const GRID_COLS = 3

/** The shape of every post on an Instagram profile (since 2025): the middle of a wider post, top to bottom. */
export const GRID_VIEW = 3 / 4

/** The most pictures one Instagram carousel takes in the app... */
export const MAX_SLIDES = 20

/** ...and through Instagram's API, which scheduling tools use. */
export const API_SLIDES = 10

export type Shape = { id: string; label: string; w: number; h: number; note: string }

export const GRID_SHAPES: Shape[] = [
  {
    id: '4:5',
    label: '4:5 portrait',
    w: 4,
    h: 5,
    note: 'The tallest shape scheduling tools can post. The profile trims each post to 3:4; every post carries the sliver of its neighbours that gets trimmed, so the grid still joins up.',
  },
  {
    id: '3:4',
    label: '3:4 portrait',
    w: 3,
    h: 4,
    note: 'Exactly what the profile shows. Instagram’s API takes 4:5 at the tallest, so post these from the Instagram app.',
  },
  {
    id: '1:1',
    label: '1:1 square',
    w: 1,
    h: 1,
    note: 'Squares in the feed. The profile trims the sides to 3:4; the slivers it trims overlap the neighbours, so the grid joins up there too.',
  },
]

export const SLIDE_SHAPES: Shape[] = [
  { id: '4:5', label: '4:5 portrait', w: 4, h: 5, note: 'The tallest shape the feed shows in full.' },
  { id: '1:1', label: '1:1 square', w: 1, h: 1, note: 'Square slides, a shorter strip.' },
]

const clamp = (n: number) => Math.min(1, Math.max(0, n))

/**
 * The largest region of shape `ratio` (width / height) inside a w × h picture. `focusX` and `focusY` (0 to 1) place
 * it where the picture has room to spare: 0 is the left or top edge, 0.5 the middle.
 */
export function coverRect(w: number, h: number, ratio: number, focusX = 0.5, focusY = 0.5): Rect {
  if (w / h > ratio) {
    const cw = h * ratio

    return { x: (w - cw) * clamp(focusX), y: 0, w: cw, h }
  }

  const ch = w / ratio

  return { x: 0, y: (h - ch) * clamp(focusY), w, h: ch }
}

/**
 * A profile grid of `rows` rows of three posts of shape `tileRatio`, laid out so the parts the profile shows (`view`,
 * 3:4) join up. A post wider than that reaches into its neighbours by the strip the profile trims. Instagram puts the
 * newest post top left, so the bottom-right post goes first and the top-left one last.
 */
export function gridPieces(
  w: number,
  h: number,
  rows: number,
  tileRatio: number,
  focusX = 0.5,
  focusY = 0.5,
  view = GRID_VIEW,
): { crop: Rect; pieces: Piece[] } {
  // In units of one row's height: three cells of `view`, plus half the extra width beyond each outer edge.
  const extra = Math.max(0, tileRatio - view)
  const crop = coverRect(w, h, (GRID_COLS * view + extra) / rows, focusX, focusY)
  const th = crop.h / rows
  const cw = view * th
  const tw = Math.max(tileRatio, view) * th
  const pieces: Piece[] = []

  for (let row = 0; row < rows; row++) {
    for (let col = 0; col < GRID_COLS; col++) {
      const x = crop.x + col * cw
      const y = crop.y + row * th

      pieces.push({
        src: { x, y, w: tw, h: th },
        view: { x: x + (extra * th) / 2, y, w: cw, h: th },
        row,
        col,
        order: rows * GRID_COLS - (row * GRID_COLS + col),
      })
    }
  }

  return { crop, pieces }
}

/** A carousel of `count` slides of shape `slideRatio`, side by side from left to right, posted in that order. */
export function carouselPieces(
  w: number,
  h: number,
  count: number,
  slideRatio: number,
  focusX = 0.5,
  focusY = 0.5,
): { crop: Rect; pieces: Piece[] } {
  const crop = coverRect(w, h, count * slideRatio, focusX, focusY)
  const sw = crop.w / count
  const pieces = Array.from({ length: count }, (_, col) => {
    const src = { x: crop.x + col * sw, y: crop.y, w: sw, h: crop.h }

    return { src, view: src, row: 0, col, order: col + 1 }
  })

  return { crop, pieces }
}

/** How many slides of shape `slideRatio` a w × h picture fills with the least cropped away (2 to 20). */
export function suggestSlides(w: number, h: number, slideRatio: number) {
  return Math.min(MAX_SLIDES, Math.max(2, Math.round(w / h / slideRatio)))
}

/** The exported size of a piece of shape `ratio`: 1080 wide. */
export function outSize(ratio: number) {
  return { width: OUT_WIDTH, height: Math.round(OUT_WIDTH / ratio) }
}

/** The share (0 to 1) of the picture's area left out by the crop. */
export function cropShare(w: number, h: number, crop: Rect) {
  return 1 - (crop.w * crop.h) / (w * h)
}

/** A file name for a piece: grid-01.jpg sorts in posting order. */
export function pieceName(prefix: string, order: number) {
  return `${prefix}-${String(order).padStart(2, '0')}.jpg`
}
