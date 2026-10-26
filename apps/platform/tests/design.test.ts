import { describe, expect, it } from 'vitest'

import {
  align,
  bounds,
  copyPage,
  credits,
  cropRect,
  curvePath,
  distribute,
  newDesign,
  newImage,
  newText,
  parseDesign,
  resize,
  SIZES,
  withGroups,
} from '@/components/design/doc'
import { jpegSize, jpegsToPdf } from '@/components/design/pdf'
import { snap } from '@/components/design/snap'
import { step } from '@/components/design/store'
import { templates } from '@/components/design/templates'

describe('the design editor', () => {
  it('resizes every page keeping the content centred and in proportion', () => {
    const d = newDesign(1000, 1000)

    d.pages[0]!.elements = [{ ...newText(d, 'heading'), x: 100, y: 100, width: 800, fontSize: 100 }]
    d.pages.push(copyPage(d.pages[0]!))

    const story = resize(d, 1080, 1920)

    expect([story.width, story.height]).toEqual([1080, 1920])

    for (const p of story.pages) {
      const t = p.elements[0]!

      expect(t.type === 'text' && t.fontSize).toBe(108)
      expect(t.x).toBeCloseTo(108)
      // 1000 * 1.08 = 1080 tall content, centred in 1920: 420 above it.
      expect(t.y).toBeCloseTo(420 + 108)
    }
  })

  it('crops a picture to fill its frame, zoomed and moved, never past its edges', () => {
    // A 2000 x 1000 picture in a square frame: the middle 1000 x 1000.
    expect(cropRect(2000, 1000, 500, 500)).toEqual({ x: 500, y: 0, width: 1000, height: 1000 })
    // Zoomed in twice around the left quarter.
    expect(cropRect(2000, 1000, 500, 500, { cx: 0.25, cy: 0.5, zoom: 2 })).toEqual({
      x: 250,
      y: 250,
      width: 500,
      height: 500,
    })
    // A centre past the edge stays inside the picture.
    expect(cropRect(2000, 1000, 500, 500, { cx: 0, cy: 0, zoom: 1 })).toMatchObject({ x: 0, y: 0 })
    // A frame of the picture's own shape shows all of it.
    expect(cropRect(2000, 1000, 400, 200)).toEqual({ x: 0, y: 0, width: 2000, height: 1000 })
  })

  it('snaps a box to the page centre and to another element, within the threshold only', () => {
    const page = { width: 1000, height: 1000 }

    expect(snap({ x: 447, y: 10, width: 100, height: 50 }, [], page, 5)).toMatchObject({ x: 450, v: [500] })
    expect(snap({ x: 440, y: 10, width: 100, height: 50 }, [], page, 5)).toMatchObject({ x: 440, v: [] })
    expect(
      snap({ x: 300, y: 203, width: 50, height: 50 }, [{ x: 0, y: 200, width: 100, height: 100 }], page, 5),
    ).toMatchObject({ y: 200, h: [200] })
  })

  it('makes one undo step of a run of changes from the same control', () => {
    let h: Parameters<typeof step>[0] = { past: [], present: newDesign(), future: [], key: '', at: 0 }

    h = step(h, (d) => void (d.name = 'a'), 'name', 1000)
    h = step(h, (d) => void (d.name = 'ab'), 'name', 1300)
    h = step(h, (d) => void (d.width = 500), '', 1400)

    expect(h.past.map((d) => d.name)).toEqual(['Untitled design', 'ab'])
    expect(step(h, () => undefined).past).toHaveLength(2)
  })

  it('opens its own project files (one-page files of the first day too) and credits stock photos', () => {
    const d = newDesign()

    d.pages[0]!.elements = [
      newImage(d, 'https://images.example/p', 4000, 3000, { stock: { id: 'p', author: 'Jane Doe', url: '' } }),
    ]

    expect(parseDesign(JSON.stringify(d))?.pages[0]!.elements).toHaveLength(1)
    expect(parseDesign('GPEX\u0001')).toBeNull()

    const old = parseDesign(
      JSON.stringify({
        version: 1,
        name: 'x',
        width: 10,
        height: 10,
        background: { kind: 'color', color: '#fff' },
        elements: [],
      }),
    )

    expect(old?.pages).toHaveLength(1)
    expect(credits(d)).toEqual(['Photo by Jane Doe on Unsplash'])
    expect(d.pages[0]!.elements[0]!.width).toBeCloseTo(648)
  })

  it('copies a page with new ids and without the file it saved as', () => {
    const d = newDesign()

    d.pages[0]!.elements = [newText(d, 'body')]
    d.pages[0]!.mediaId = 'med_1'

    const c = copyPage(d.pages[0]!)

    expect(c.id).not.toBe(d.pages[0]!.id)
    expect(c.elements[0]!.id).not.toBe(d.pages[0]!.elements[0]!.id)
    expect(c.mediaId).toBeUndefined()
  })

  it('has a size per placement and templates with unique element ids', () => {
    expect(SIZES.find((s) => s.label === 'Instagram Stories, Reels')).toMatchObject({ width: 1080, height: 1920 })

    const ids = templates().flatMap((t) => t.pages.flatMap((p) => p.elements.map((e) => e.id)))

    expect(new Set(ids).size).toBe(ids.length)
    expect(templates().find((t) => t.name === 'Carousel')?.pages).toHaveLength(4)
  })

  it('lines elements up, spaces them evenly and picks whole groups', () => {
    const els = [
      { x: 0, y: 0, width: 100, height: 50 },
      { x: 300, y: 40, width: 50, height: 50 },
      { x: 120, y: 90, width: 30, height: 10 },
    ]

    expect(bounds(els)).toEqual({ x: 0, y: 0, width: 350, height: 100 })

    align(els, 'right', bounds(els))
    expect(els.map((e) => e.x)).toEqual([250, 300, 320])

    align(els, 'middle', { x: 0, y: 0, width: 1000, height: 1000 })
    expect(els.map((e) => e.y)).toEqual([475, 475, 495])

    const row = [
      { x: 0, y: 0, width: 10, height: 10 },
      { x: 15, y: 0, width: 10, height: 10 },
      { x: 90, y: 0, width: 10, height: 10 },
    ]

    distribute(row, 'x')
    expect(row.map((e) => e.x)).toEqual([0, 45, 90])

    const d = newDesign()
    const [a, b, c] = [newText(d, 'body'), newText(d, 'body'), newText(d, 'body')]

    a.group = 'g'
    b.group = 'g'
    expect(withGroups([a, b, c], [a.id])).toEqual([a.id, b.id])
    expect(withGroups([a, b, c], [c.id])).toEqual([c.id])
  })

  it('bends text along an arch or a smile that keeps the line inside its box', () => {
    expect(curvePath(400, 100, 40)).toMatch(/^M [\d.]+ [\d.]+ A [\d.]+ [\d.]+ 0 0 1 /)
    expect(curvePath(400, -50, 40)).toMatch(/ 0 0 0 /)
    // A half circle 400 long: radius 400/pi, ends at the box's sides.
    const [, x0, , , r] = curvePath(400, 100, 40).split(' ')

    expect(+r!).toBeCloseTo(400 / Math.PI, 1)
    expect(+x0!).toBeCloseTo(200 - 400 / Math.PI, 1)
  })

  it('writes a PDF with a page per picture at its size', async () => {
    // A JPEG's SOF0 marker for a 3 x 2 picture (the rest is not read).
    const jpg = new Uint8Array([
      0xff, 0xd8, 0xff, 0xc0, 0, 17, 8, 0, 2, 0, 3, 3, 1, 0x11, 0, 2, 0x11, 1, 3, 0x11, 1, 0xff, 0xd9,
    ])

    expect(jpegSize(jpg)).toEqual({ width: 3, height: 2 })

    const pdf = await jpegsToPdf([jpg, jpg]).text()

    expect(pdf.startsWith('%PDF-1.4')).toBe(true)
    expect(pdf).toContain('/Count 2')
    expect(pdf).toContain('/MediaBox [0 0 3 2]')
    expect(pdf.trimEnd().endsWith('%%EOF')).toBe(true)
    // The cross-reference points at each object.
    const xref = +pdf.slice(pdf.lastIndexOf('startxref') + 10).split('\n')[0]!

    expect(pdf.slice(xref, xref + 4)).toBe('xref')
  })
})
