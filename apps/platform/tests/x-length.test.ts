import { describe, expect, it } from 'vitest'

import { applyLinks } from '@/lib/links'
import { xLength } from '@/lib/posts'

// The same cases as the API's TestXWeightedLength and TestLinksEdgeCases: the composer shows what the API checks.
describe('xLength, the way X counts', () => {
  it('counts CJK and emoji as 2, an emoji sequence as 2 in all and a link as 23', () => {
    expect(xLength('日'.repeat(140))).toBe(280)
    expect(xLength('😀'.repeat(141))).toBe(282)
    expect(xLength('👨‍👩‍👧'.repeat(60))).toBe(120)
    expect(xLength('🇵🇱'.repeat(140))).toBe(280)
    expect(xLength('a'.repeat(250) + ' https://example.com/' + 'p'.repeat(60))).toBe(274)
    expect(xLength('Hello, world!')).toBe(13)
  })
})

describe('links the API takes out', () => {
  it('keeps file names and the text of a markdown link', () => {
    expect(applyLinks('remove', 'Update your README.md and setup.py today').text).toBe(
      'Update your README.md and setup.py today',
    )
    expect(applyLinks('remove', 'Read [the docs](https://example.com/d) now').text).toBe('Read the docs now')
    expect(applyLinks('defang', 'Read [the docs](https://example.com/d)').text).toBe('Read the docs (example[.]com/d)')
    expect(applyLinks('remove', 'see mastodon.social/@staqly now').text).toBe('see now')
    expect(applyLinks('remove', 'Run deploy.sh then see example.co').text).toBe('Run deploy.sh then see')
  })
})
