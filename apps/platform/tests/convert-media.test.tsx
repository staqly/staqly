import { act, renderHook, waitFor } from '@testing-library/react'
import type { Dispatch, SetStateAction } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useConvertedMedia } from '@/components/posts/convert-media'
import { api, type Media } from '@/lib/api'

import { provider } from './fixtures'

const ig = provider({
  id: 'instagram',
  name: 'Instagram',
  limits: { ...provider().limits, image: { formats: ['image/jpeg'] } },
})
const png = { id: 'med_png', mime_type: 'image/png', original_name: 'a.png', alt_text: 'A chart' } as Media
const jpg = { id: 'med_jpg', mime_type: 'image/jpeg', original_name: 'a.jpg', alt_text: '' } as Media

describe('useConvertedMedia', () => {
  beforeEach(() => {
    vi.stubGlobal('createImageBitmap', async () => ({ width: 2, height: 2, close() {} }))
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue({ fillRect() {}, drawImage() {} } as never)
    vi.spyOn(HTMLCanvasElement.prototype, 'toBlob').mockImplementation((cb, type) => cb(new Blob(['x'], { type })))
    vi.spyOn(api, 'mediaBlob').mockResolvedValue(new Blob(['png']))
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('swaps a picture a picked platform refuses for a converted copy, once', async () => {
    const upload = vi.spyOn(api, 'uploadMedia').mockResolvedValue(jpg)
    const setMedia = vi.fn<Dispatch<SetStateAction<Media[]>>>()
    const { rerender } = renderHook(({ media }) => useConvertedMedia(media, [ig], setMedia, false), {
      initialProps: { media: [png] },
    })

    await waitFor(() => expect(setMedia).toHaveBeenCalled())
    const sent = upload.mock.calls[0][0] as File

    expect([sent.name, sent.type]).toEqual(['a.jpg', 'image/jpeg'])
    expect((setMedia.mock.calls[0][0] as (ms: Media[]) => Media[])([png])).toEqual([{ ...jpg, alt_text: 'A chart' }])
    act(() => rerender({ media: [png] }))
    expect(upload).toHaveBeenCalledTimes(1)
  })

  it('leaves pictures alone when every platform takes them, or the post is locked', () => {
    const upload = vi.spyOn(api, 'uploadMedia')

    renderHook(() => useConvertedMedia([jpg], [ig], vi.fn<() => void>(), false))
    renderHook(() => useConvertedMedia([png], [ig], vi.fn<() => void>(), true))
    expect(upload).not.toHaveBeenCalled()
  })
})
