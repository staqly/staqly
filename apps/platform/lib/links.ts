import type { Channel } from '@/lib/api'

export type LinksMode = Channel['links']

export const LINKS_LABELS: Record<LinksMode, string> = {
  keep: 'Keep links',
  remove: 'Remove links',
  defang: 'Show links as plain text',
}

export const LINKS_HELP: Record<LinksMode, string> = {
  keep: 'Links go out as written.',
  remove: 'Links are taken out of the text before it is published.',
  defang: 'example.com/page goes out as example[.]com/page: readable, but not a link.',
}

// A link with http(s), or a bare host with a letters-only top-level domain (example.com/a, www.example.co.uk).
// ponytail: mirrors the API (mvdan.cc/xurls against the IANA list) closely enough for the live counter; the
// publish and the preview use the API's result, so a rare difference only shows while typing.
const LINK =
  /(?<![\w@.[\]/-])(?:https?:\/\/[^\s<>"]+|(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,}(?::\d+)?(?:[/?#][^\s<>"]*)?)/gi
// Extensions that look like domains but are not top-level domains.
const NOT_TLD = new Set([
  'js',
  'ts',
  'tsx',
  'jsx',
  'json',
  'txt',
  'yml',
  'yaml',
  'html',
  'css',
  'png',
  'jpg',
  'gif',
  'pdf',
])

/** Trailing punctuation is not part of a link; a closing parenthesis is only when the link opened one. */
function trimLink(u: string) {
  let out = u

  for (;;) {
    const last = out.at(-1)

    if (last && '.,;:!?\'"'.includes(last)) {
      out = out.slice(0, -1)
    } else if (last === ')' && out.split('(').length < out.split(')').length) {
      out = out.slice(0, -1)
    } else if (last === ']' && out.split('[').length < out.split(']').length) {
      out = out.slice(0, -1)
    } else {
      return out
    }
  }
}

function hostTld(u: string) {
  const host = u.replace(/^https?:\/\//i, '').split(/[/?#:]/)[0] ?? ''

  return host.split('.').at(-1)?.toLowerCase() ?? ''
}

function defang(u: string) {
  const rest = u.replace(/^https?:\/\//i, '')
  const end = rest.search(/[/?#]/)
  const host = end < 0 ? rest : rest.slice(0, end)

  return host.replaceAll('.', '[.]') + (end < 0 ? '' : rest.slice(end))
}

/** What a channel with this links mode receives, and the links it changes (as written). */
// A markdown link, [text](url): its text stays when the url goes.
const MARKDOWN_LINK = /\[([^\]\n]+)\]\(([^)\s]+)\)/g

/**
 * A web link, as the API decides: a bare name on a two-letter top-level domain without a path (README.md,
 * setup.py) is a file name, not a link (X links those only with a scheme or a path; .co is the exception).
 */
function isWebLink(u: string) {
  if (/^https?:\/\//i.test(u)) {
    return true
  }

  const tld = hostTld(u)

  if (NOT_TLD.has(tld)) {
    return false
  }

  const bare = !/[/?#]/.test(u)

  return !(bare && tld.length === 2 && tld !== 'co' && !/^www\./i.test(u))
}

/** The web links in text, as written (no scheme needed). */
export function findLinks(text: string): string[] {
  return [...text.matchAll(LINK)].map((m) => trimLink(m[0])).filter(isWebLink)
}

export function applyLinks(mode: LinksMode | undefined, text: string): { text: string; found: string[] } {
  if (mode !== 'remove' && mode !== 'defang') {
    return { text, found: [] }
  }

  const found: string[] = []
  let out = text.replace(MARKDOWN_LINK, (m, label: string, url: string) => {
    if (!isWebLink(url)) {
      return m
    }

    found.push(url)

    return mode === 'remove' ? label : `${label} (${defang(url)})`
  })

  out = out.replace(LINK, (raw) => {
    const u = trimLink(raw)

    if (!isWebLink(u)) {
      return raw
    }

    found.push(u)

    return (mode === 'remove' ? '' : defang(u)) + raw.slice(u.length)
  })

  if (mode === 'remove' && found.length) {
    out = out
      .split('\n')
      .map((l) => l.replace(/[ \t]{2,}/g, ' ').trimEnd())
      .join('\n')
      .trim()
  }

  return { text: out, found }
}

/** The line the composer and the preview show for a channel that changes links. */
export function linksNotice(mode: LinksMode | undefined, channel: string) {
  return mode === 'defang'
    ? `Links will be shown as plain text on: ${channel}`
    : `Links will be removed from: ${channel}`
}
