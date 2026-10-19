import { act, renderHook, waitFor } from '@testing-library/react'
import { useState } from 'react'
import { describe, expect, it } from 'vitest'

import { announceEdited, useEditedMedia } from '@/components/media-editor/edited-media'
import type { Media } from '@/lib/api'

import { mockApi } from './utils'

const media = (id: string, name: string) => ({ id, original_name: name, mime_type: 'image/png' }) as Media

describe('a post takes the picture saved in the media editor', () => {
  it('swaps the attachment the editor saved as another file, and nothing else', async () => {
    mockApi({ 'GET /v1/media/med_new/info': media('med_new', 'photo (edited).png') })
    const { result } = renderHook(() => {
      const [ms, setMs] = useState([media('med_old', 'photo.png'), media('med_other', 'other.png')])

      useEditedMedia(setMs)

      return ms
    })

    act(() => announceEdited({ from: 'med_old', to: 'med_new' }))
    await waitFor(() => expect(result.current.map((m) => m.id)).toEqual(['med_new', 'med_other']))

    act(() => announceEdited({ from: 'med_absent', to: 'med_new' }))
    await new Promise((r) => setTimeout(r, 50))
    expect(result.current.map((m) => m.id)).toEqual(['med_new', 'med_other'])
  })
})
