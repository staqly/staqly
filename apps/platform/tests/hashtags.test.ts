import { describe, expect, it } from 'vitest'

import { appendHashtags, hashtagsFor } from '@/lib/tags'

// The same cases as the API's TestAppendHashtags: the counter shows what the channel receives.
describe('appendHashtags', () => {
  it('ends the text with the tags the text does not have yet', () => {
    expect(appendHashtags('Launch day\n', ['launch', 'client-a', '#Ünïcode'])).toBe(
      'Launch day\n\n#launch #clienta #Ünïcode',
    )
    expect(appendHashtags('Out now #Launch', ['launch', 'dev', 'Dev'])).toBe('Out now #Launch\n\n#dev')
    expect(appendHashtags('Out now', ['--', ''])).toBe('Out now')
    expect(appendHashtags('', ['solo'])).toBe('#solo')
    expect(appendHashtags('See #launches', ['launch'])).toBe('See #launches\n\n#launch')
  })
})

describe('hashtagsFor', () => {
  it('leaves hashtags out for your own automation and platforms with tags of their own', () => {
    expect(hashtagsFor({ provider: 'x' }, { settings_fields: [] })).toBe(true)
    expect(hashtagsFor({ provider: 'youtube' }, { settings_fields: [{ key: 'title' }, { key: 'tags' }] })).toBe(false)
    expect(hashtagsFor({ provider: 'custom' })).toBe(false)
  })
})
