// Links in post text, the way a Staqly channel handles them (apps/api/internal/providers/links.go): find http(s)
// links and bare hosts, then remove them or show them as plain text (example.com becomes example[.]com).
// No imports, so scripts/check-tools.mjs can run it with plain Node.

export type LinksMode = 'remove' | 'defang'

// A link with http(s), or a bare host with a letters-only top-level domain (example.com/a, www.example.co.uk).
// ponytail: the API matches against the IANA list of top-level domains (mvdan.cc/xurls); this regex plus a short
// list of file extensions is close enough for a browser tool.
const LINK =
  /(?<![\w@.[\]/-])(?:https?:\/\/[^\s<>"]+|(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,}(?::\d+)?(?:[/?#][^\s<>"]*)?)/gi
// File names that look like hosts (notes.txt, app.js) are not links.
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

/** Trailing punctuation is not part of a link; a closing parenthesis only when the link opened one. */
function trimLink(u: string) {
  let out = u

  for (;;) {
    const last = out.at(-1)

    if (last && '.,;:!?\'"'.includes(last)) {
      out = out.slice(0, -1)
    } else if (last === ')' && out.split('(').length < out.split(')').length) {
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

/** https://www.example.com:8080/a.b?q=1 becomes www[.]example[.]com:8080/a.b?q=1: the scheme goes, the host's dots are bracketed. */
export function defangLink(u: string) {
  const rest = u.replace(/^https?:\/\//i, '')
  const end = rest.search(/[/?#]/)
  const host = end < 0 ? rest : rest.slice(0, end)

  return host.replaceAll('.', '[.]') + (end < 0 ? '' : rest.slice(end))
}

/** The text a channel receives in this mode, and the links it changed (as written). */
export function applyLinks(mode: LinksMode, text: string): { text: string; found: string[] } {
  const found: string[] = []
  let out = text.replace(LINK, (raw) => {
    const u = trimLink(raw)

    if (!/^https?:\/\//i.test(u) && NOT_TLD.has(hostTld(u))) {
      return raw
    }

    found.push(u)

    return (mode === 'remove' ? '' : defangLink(u)) + raw.slice(u.length)
  })

  if (mode === 'remove' && found.length) {
    // A removed link leaves its spaces behind: fold them per line, keep the line breaks.
    out = out
      .split('\n')
      .map((l) => l.replace(/[ \t]{2,}/g, ' ').trimEnd())
      .join('\n')
      .trim()
  }

  return { text: out, found }
}

/** The way back: [.] (.) {.} become dots and hxxp(s) becomes http(s); a bare host stays without a scheme. */
export function refang(text: string) {
  return text
    .replace(/\[\.\]|\(\.\)|\{\.\}|\[dot\]|\(dot\)/gi, '.')
    .replace(/\bhxxp(s?):\/\//gi, 'http$1://')
    .replace(/\[:\]\/\//g, '://')
}
