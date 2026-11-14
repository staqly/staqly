import { describe, expect, it } from 'vitest'

import { applyLinks } from '@/lib/links'

// The same cases as the API's TestApplyLinks (apps/api/internal/providers/links_test.go).
describe('applyLinks', () => {
  it.each([
    ['keep', 'see https://example.com', 'see https://example.com', 0],
    ['defang', 'see https://google.com/path?q=1', 'see google[.]com/path?q=1', 1],
    ['defang', 'bare example.com and www.example.co.uk/a.b', 'bare example[.]com and www[.]example[.]co[.]uk/a.b', 2],
    ['defang', 'port https://example.com:8080/x#top', 'port example[.]com:8080/x#top', 1],
    ['defang', 'end https://example.com/x.', 'end example[.]com/x.', 1],
    ['defang', '(see https://example.com/a), then', '(see example[.]com/a), then', 1],
    ['defang', 'already example[.]com/path', 'already example[.]com/path', 0],
    ['defang', 'mail me@example.com', 'mail me@example.com', 0],
    ['defang', 'v1.4 is out, 3.5 times faster', 'v1.4 is out, 3.5 times faster', 0],
    ['defang', 'built with node.js', 'built with node.js', 0],
    ['remove', 'Read more: https://example.com/post', 'Read more:', 1],
    ['remove', 'a https://x.com/1 b example.org c', 'a b c', 2],
    ['remove', 'line one https://example.com\nline two', 'line one\nline two', 1],
  ] as const)('%s %j', (mode, input, want, n) => {
    const out = applyLinks(mode, input)

    expect(out.text).toBe(want)
    expect(out.found).toHaveLength(n)
  })
})
