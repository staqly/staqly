import { findLinks } from '@/lib/links'

/**
 * The part of a text a platform takes and the part past its limit, counted the platform's way (X's own count, or
 * characters). No limit, or within it: everything is kept.
 */
export function splitAtLimit(text: string, max: number, count: (t: string) => number): [string, string] {
  if (max <= 0 || count(text) <= max) {
    return [text, '']
  }

  const chars = [...text]
  let lo = 0
  let hi = chars.length

  // The longest prefix within the limit (counts only grow with the prefix).
  while (lo < hi) {
    const mid = (lo + hi + 1) >> 1

    if (count(chars.slice(0, mid).join('')) <= max) {
      lo = mid
    } else {
      hi = mid - 1
    }
  }

  return [chars.slice(0, lo).join(''), chars.slice(lo).join('')]
}

/**
 * Where a feed folds a long text ("...see more"): after `maxLines` lines or `maxChars` characters, at a word when
 * one is near. Null when the whole text shows.
 */
export function foldText(text: string, maxChars: number, maxLines: number): string | null {
  const lines = text.split('\n')
  let cut = lines.length > maxLines ? lines.slice(0, maxLines).join('\n') : text
  const chars = [...cut]

  if (chars.length > maxChars) {
    cut = chars.slice(0, maxChars).join('')
    const space = cut.search(/\s\S*$/)

    if (space > maxChars * 0.6) {
      cut = cut.slice(0, space)
    }
  }

  return cut === text ? null : cut.trimEnd()
}

export type Segment = { kind: 'text' | 'link' | 'tag' | 'mention'; text: string }

const TAGS = /([#@][\p{L}\p{N}_](?:[\p{L}\p{N}_.]*[\p{L}\p{N}_])?(?:@[\w.-]+\.[a-z]{2,})?)/gu

function words(text: string, out: Segment[]) {
  let at = 0

  for (const m of text.matchAll(TAGS)) {
    const i = m.index ?? 0
    // A # or @ inside a word (an email address, C#) is plain text.
    const before = text[i - 1]

    if (before && /[\p{L}\p{N}_]/u.test(before)) {
      continue
    }

    if (i > at) {
      out.push({ kind: 'text', text: text.slice(at, i) })
    }

    out.push({ kind: m[0].startsWith('#') ? 'tag' : 'mention', text: m[0] })
    at = i + m[0].length
  }

  if (at < text.length) {
    out.push({ kind: 'text', text: text.slice(at) })
  }
}

/** A post's text in runs: links, hashtags and mentions apart, so a platform colours them its way. */
export function segments(text: string): Segment[] {
  const out: Segment[] = []
  let at = 0

  for (const link of findLinks(text)) {
    const i = text.indexOf(link, at)

    if (i < 0) {
      continue
    }

    words(text.slice(at, i), out)
    out.push({ kind: 'link', text: link })
    at = i + link.length
  }

  words(text.slice(at), out)

  return out
}

/** How a feed shows a link: no scheme, no www, long paths shortened. */
export function displayLink(url: string, max = 30) {
  const bare = url.replace(/^https?:\/\//i, '').replace(/^www\./i, '')

  return bare.length > max ? `${bare.slice(0, max - 3)}...` : bare
}

/** The address's host, for a link card. */
export function linkHost(url: string) {
  const bare = url.replace(/^https?:\/\//i, '').replace(/^www\./i, '')

  return bare.split(/[/?#]/)[0] ?? bare
}
