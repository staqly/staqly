import { describe, expect, it } from 'vitest'

import { safeCssColor, safeHttpUrl, safeImageSrc, safeNextPath } from '@/lib/safe'

const ORIGIN = 'https://app.staqly.test'

describe('safeNextPath', () => {
  it('keeps same-origin paths with query and hash', () => {
    expect(safeNextPath('/items/itm_1?tab=events#top', ORIGIN)).toBe('/items/itm_1?tab=events#top')
    expect(safeNextPath('/collections/col_1', ORIGIN)).toBe('/collections/col_1')
  })

  it.each([
    ['/\\evil.example'],
    ['/\\/evil.example'],
    ['/\t/evil.example'],
    ['/\n/evil.example'],
    ['//evil.example'],
    ['//evil.example/schedule'],
    ['https://evil.example'],
    ['http://app.staqly.test.evil.example/'],
    ['javascript:alert(1)'],
    ['evil.example'],
    [''],
    [null],
    ['/login'],
    ['/login?next=/schedule'],
  ])('rejects %j', (next) => {
    expect(safeNextPath(next, ORIGIN)).toBe('/schedule')
  })

  it('normalizes dot segments without leaving the origin', () => {
    expect(safeNextPath('/../../schedule', ORIGIN)).toBe('/schedule')
    expect(safeNextPath('/a/../settings', ORIGIN)).toBe('/settings')
  })
})

describe('safeHttpUrl', () => {
  it('accepts absolute http(s) URLs', () => {
    expect(safeHttpUrl('https://example.com/post')).toBe('https://example.com/post')
    expect(safeHttpUrl(' http://localhost:3000/x ')).toBe('http://localhost:3000/x')
  })

  it.each([
    'javascript:alert(1)',
    'JavaScript:alert(1)',
    'java\tscript:alert(1)',
    'data:text/html,<script>alert(1)</script>',
    'vbscript:msgbox(1)',
    'file:///etc/passwd',
    '/relative/path',
    '//evil.example',
    'not a url',
    '',
    null,
    undefined,
  ])('rejects %j', (v) => {
    expect(safeHttpUrl(v)).toBeNull()
  })
})

describe('safeCssColor', () => {
  it.each([
    '#abc',
    '#a1b2c3',
    '#a1b2c3d4',
    'rgb(1, 2, 3)',
    'rgba(1 2 3 / 50%)',
    'hsl(120deg 50% 50%)',
    'oklch(0.7 0.1 200)',
    'teal',
  ])('accepts %s', (c) => {
    expect(safeCssColor(c)).toBe(c)
  })

  it.each([
    'red; background-image: url(https://evil.example/x)',
    'url(https://evil.example/x)',
    'var(--x)',
    'expression(alert(1))',
    '#12345',
    'rgb(1,2,3) url(x)',
    'rgb((1))',
    '"red"',
    '',
    null,
    undefined,
  ])('rejects %j', (c) => {
    expect(safeCssColor(c)).toBeUndefined()
  })
})

describe('safeImageSrc', () => {
  it('keeps http(s) pictures and uploaded PNG/JPEG data: URLs', () => {
    expect(safeImageSrc('https://example.com/a.png')).toBe('https://example.com/a.png')
    expect(safeImageSrc('data:image/png;base64,iVBORw0KGgo=')).toBe('data:image/png;base64,iVBORw0KGgo=')
    expect(safeImageSrc('data:image/jpeg;base64,/9j/4AAQ')).toBe('data:image/jpeg;base64,/9j/4AAQ')
  })

  it.each([
    ['data:image/svg+xml;base64,PHN2Zz4='],
    ['data:text/html;base64,PHNjcmlwdD4='],
    ['data:image/png;base64,"onerror="alert(1)'],
    ['javascript:alert(1)'],
    [''],
  ])('drops %s', (v) => {
    expect(safeImageSrc(v)).toBeNull()
  })
})
