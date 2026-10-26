import { describe, expect, it } from 'vitest'

import {
  copyPage,
  credits,
  cropRect,
  newDesign,
  newImage,
  newText,
  parseDesign,
  resize,
  SIZES,
} from '@/components/design/doc'
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
})
