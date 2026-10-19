import { describe, expect, it } from 'vitest'

import { baseName, outputFormat } from '@/components/media-editor/editor-bar'
import {
  cropRect,
  detailCentre,
  hardProblems,
  outputSize,
  padRect,
  placementFits,
  ratioLabel,
  softNotes,
} from '@/components/media-editor/fit'
import { readParams } from '@/components/media-editor/hooks'
import type { Provider } from '@/lib/api'

const provider = (id: string, image?: NonNullable<Provider['limits']>['image'], kinds = ['image', 'video']) =>
  ({ id, name: id, limits: { media_kinds: kinds, image } }) as unknown as Provider

const providers = [
  provider('instagram', { max_bytes: 8 << 20, formats: ['image/jpeg'], min_ratio: 0.8, max_ratio: 1.91 }),
  provider('x', { max_bytes: 5 << 20, formats: ['image/jpeg', 'image/png', 'image/gif', 'image/webp'] }),
  provider('tiktok', undefined, ['video']),
  provider('linkedin', { max_pixels: 36_152_320 }),
]

const pic = (w: number, h: number, extra: Partial<{ bytes: number; mime: string; alpha: boolean }> = {}) => ({
  width: w,
  height: h,
  bytes: 200_000,
  mime: 'image/jpeg',
  alpha: false,
  ...extra,
})

describe('channel fit', () => {
  it('tells refusals (hard limits) apart from shapes that only crop', () => {
    const fits = placementFits(pic(1080, 1080), providers)
    const by = (p: string, n: string) => fits.find((f) => f.provider === p && f.name === n)!

    expect(by('instagram', 'Feed square').status).toBe('ok')
    // 4:5 is a recommended shape: a square is taken and cropped, not refused.
    expect(by('instagram', 'Feed portrait')).toMatchObject({ status: 'adjust', label: '4:5' })
    expect(by('instagram', 'Feed portrait').reasons[0]).toMatch(/crops 20%/)
    expect(by('tiktok', 'Video')).toMatchObject({ status: 'unsupported', reasons: ['tiktok takes no pictures'] })
  })

  it('reports each broken hard limit in plain words', () => {
    const ig = providers[0]

    expect(hardProblems(pic(1080, 1080, { mime: 'image/png' }), ig)).toEqual(['Takes JPG only'])
    expect(hardProblems(pic(1080, 1080, { bytes: 9 << 20 }), ig)).toEqual(['Larger than 8.0 MB'])
    expect(hardProblems(pic(1080, 1920), ig)[0]).toMatch(/^Too tall: 9:16/)
    expect(hardProblems(pic(3000, 1000), ig)[0]).toMatch(/^Too wide/)
    expect(hardProblems(pic(8000, 6000), providers[3])).toEqual(['More than 36 megapixels'])
    // A platform with no stated limits takes any picture.
    expect(hardProblems(pic(10, 5000, { mime: 'image/png' }), provider('mastodon'))).toEqual([])
  })

  it('notes see-through pixels on JPG-only platforms without refusing', () => {
    expect(softNotes(pic(100, 100, { alpha: true, mime: 'image/png' }), providers[0])).toEqual([
      'See-through areas turn white',
    ])
    expect(softNotes(pic(100, 100, { alpha: true, mime: 'image/png' }), providers[1])).toEqual([])
  })

  it('names common ratios', () => {
    expect(ratioLabel(1080, 1350)).toBe('4:5')
    expect(ratioLabel(1920, 1080)).toBe('16:9')
    expect(ratioLabel(1000, 1000)).toBe('1:1')
    expect(ratioLabel(1000, 300)).toBe('3.33:1')
  })
})

describe('optimize math', () => {
  it('crops the largest rect of the ratio, centred on the focus but inside the picture', () => {
    expect(cropRect(1920, 1080, 1)).toEqual({ x: 420, y: 0, w: 1080, h: 1080 })
    expect(cropRect(1920, 1080, 1, { x: 0.95, y: 0.5 })).toEqual({ x: 840, y: 0, w: 1080, h: 1080 })
    expect(cropRect(1920, 1080, 9 / 16, { x: 0, y: 0 })).toMatchObject({ x: 0, y: 0, h: 1080, w: 608 })
  })

  it('pads to the ratio without cropping, the picture centred', () => {
    const { canvas, at } = padRect(1920, 1080, 9 / 16)

    expect(canvas.w).toBe(1920)
    expect(Math.abs(canvas.w / canvas.h - 9 / 16)).toBeLessThan(0.01)
    expect(at).toEqual({ x: 0, y: Math.round((canvas.h - 1080) / 2), w: 1920, h: 1080 })
  })

  it('finds where the detail is', () => {
    const w = 40
    const h = 20
    const rgba = new Uint8ClampedArray(w * h * 4)

    // A checkered block on the right, flat elsewhere.
    for (let y = 5; y < 15; y++) {
      for (let x = 30; x < 38; x++) {
        const v = (x + y) % 2 ? 255 : 0

        rgba.set([v, v, v, 255], (y * w + x) * 4)
      }
    }

    const c = detailCentre(rgba, w, h)

    expect(c.x).toBeGreaterThan(0.7)
    expect(Math.abs(c.y - 0.5)).toBeLessThan(0.1)
    expect(detailCentre(new Uint8ClampedArray(w * h * 4), w, h)).toEqual({ x: 0.5, y: 0.5 })
  })

  it('never upscales a variant and caps its long edge', () => {
    expect(outputSize({ w: 800, h: 600 })).toEqual({ w: 800, h: 600 })
    expect(outputSize({ w: 8000, h: 4000 })).toEqual({ w: 4096, h: 2048 })
  })
})

describe('editor saving and links', () => {
  it('keeps the original format when it can hold the result', () => {
    expect(outputFormat({ mime_type: 'image/jpeg' })).toEqual({ format: 'image/jpeg', ext: 'jpg' })
    expect(outputFormat({ mime_type: 'image/gif' })).toEqual({ format: 'image/png', ext: 'png' })
    expect(outputFormat(undefined)).toEqual({ format: 'image/png', ext: 'png' })
    expect(baseName('photo.final.JPG')).toBe('photo.final')
  })

  it('returns only to dashboard paths', () => {
    expect(readParams('?ids=a,b,,c&return=/posts?compose=1')).toEqual({
      ids: ['a', 'b', 'c'],
      project: '',
      back: '/posts?compose=1',
      fromComposer: false,
    })
    expect(readParams('?ids=a&return=https://evil.example').back).toBe('/media')
    expect(readParams('?return=//evil.example').back).toBe('/media')
    expect(readParams('?ids=a&from=composer').fromComposer).toBe(true)
  })
})
