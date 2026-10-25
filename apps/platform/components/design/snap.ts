// Smart guides: a box being dragged snaps its edges and centre to the page's edges and centre and to the other
// elements' edges and centres, within a few screen pixels, and the lines it snapped to are shown.

export interface Box {
  x: number
  y: number
  width: number
  height: number
}

export interface Guides {
  x: number
  y: number
  /** Page coordinates of the vertical (v) and horizontal (h) lines snapped to. */
  v: number[]
  h: number[]
}

const stops = (start: number, size: number) => [start, start + size / 2, start + size]

function axis(own: number[], targets: number[], threshold: number) {
  let best: { delta: number; line: number } | null = null

  for (const t of targets) {
    for (const o of own) {
      const delta = t - o

      if (Math.abs(delta) <= threshold && (!best || Math.abs(delta) < Math.abs(best.delta))) {
        best = { delta, line: t }
      }
    }
  }

  return best
}

// ponytail: every target against every edge, O(elements); fine for the dozens of elements a post has.
export function snap(box: Box, others: Box[], page: { width: number; height: number }, threshold: number): Guides {
  const xs = [0, page.width / 2, page.width, ...others.flatMap((o) => stops(o.x, o.width))]
  const ys = [0, page.height / 2, page.height, ...others.flatMap((o) => stops(o.y, o.height))]
  const bx = axis(stops(box.x, box.width), xs, threshold)
  const by = axis(stops(box.y, box.height), ys, threshold)

  return {
    x: box.x + (bx?.delta ?? 0),
    y: box.y + (by?.delta ?? 0),
    v: bx ? [bx.line] : [],
    h: by ? [by.line] : [],
  }
}
