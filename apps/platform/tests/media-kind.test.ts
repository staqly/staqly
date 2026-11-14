import { describe, expect, it } from 'vitest'

import { mediaKind } from '@/lib/media'

describe('mediaKind', () => {
  it('reads pictures, videos and audio; older documents are files', () => {
    expect(mediaKind('image/png')).toBe('image')
    expect(mediaKind('video/quicktime')).toBe('video')
    expect(mediaKind('audio/ogg')).toBe('audio')
    expect(mediaKind('audio/mp4')).toBe('audio')
    expect(mediaKind('application/pdf')).toBe('file')
  })
})
