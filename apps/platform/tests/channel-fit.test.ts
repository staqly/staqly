import { describe, expect, it } from 'vitest'

import { acceptFor, channelFit, fitMedia, takesWords } from '@/lib/channel-fit'

import { channel, provider } from './fixtures'

const file = (name: string, mime_type: string) => ({ id: name, original_name: name, mime_type }) as never
const pic = (name: string, width: number, height: number, mime_type = 'image/png') =>
  ({ id: name, original_name: name, mime_type, width, height }) as never
const pin = provider({
  id: 'pinterest',
  limits: { ...provider().limits, max_media: 1, media_kinds: ['image'] },
})

describe('channel fit', () => {
  it('keeps the files a platform takes, like the server', () => {
    const files = [file('clip.mp4', 'video/mp4'), file('a.png', 'image/png'), file('b.png', 'image/png')]

    expect(fitMedia(pin.limits, files).map((f: { original_name: string }) => f.original_name)).toEqual(['a.png'])
    expect(acceptFor(pin.limits)).toBe('image/*')
    expect(takesWords(pin.limits)).toBe('Takes pictures only, 1 file at most')
  })

  it('lists what a channel leaves out: files, comments, files in comments', () => {
    const media = [file('a.png', 'image/png'), file('clip.mp4', 'video/mp4')]
    const thread = [{ content: 'More', media: [file('b.png', 'image/png')] }]

    expect(channelFit(channel({ provider: 'pinterest' }), pin, media, thread)).toEqual([
      'Takes pictures only, 1 file at most: leaves out clip.mp4',
      "Posts no comments: the comment doesn't go out here",
    ])
    const yt = provider({ id: 'youtube', limits: { ...provider().limits, comments_text_only: true } })

    expect(channelFit(channel({ provider: 'youtube', comments: true }), yt, media, thread)).toEqual([
      'Comments take text only: their pictures and videos are left out',
    ])
    expect(channelFit(channel({ comments: true }), provider(), media, thread)).toEqual([])
    expect(channelFit(channel({ provider: 'custom' }), pin, media, thread)).toEqual([])
  })

  it('says a Dribbble picture of another size goes out fitted', () => {
    const dribbble = provider({
      id: 'dribbble',
      limits: { ...provider().limits, max_media: 1, media_kinds: ['image'] },
    })
    const fit = (m: never) => channelFit(channel({ provider: 'dribbble' }), dribbble, [m], [])

    expect(fit(pic('tall.png', 1418, 2408))).toEqual([
      'Takes 400x300 or 800x600 pictures: tall.png (1418x2408) goes out fitted into 800x600',
    ])
    expect(fit(pic('small.png', 200, 100))).toEqual([
      'Takes 400x300 or 800x600 pictures: small.png (200x100) goes out fitted into 400x300',
    ])
    expect(fit(pic('shot.png', 800, 600))).toEqual([])
    expect(fit(pic('anim.gif', 1000, 750, 'image/gif'))).toEqual([])
  })
})
