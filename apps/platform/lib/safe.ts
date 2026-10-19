// Validation for values that come from the API (or the URL) and end up in
// navigation targets, hrefs or inline styles.

// Backslashes and control characters: browsers treat "\" as "/" and strip
// tabs/newlines, so "/\evil.example" or "/\t/evil.example" can
// become a protocol-relative URL.
// oxlint-disable-next-line no-control-regex -- matching control characters is the point
const UNSAFE_PATH_CHARS = /[\\\u0000-\u001f\u007f]/

/**
 * Same-origin path for a `?next=` redirect, or `fallback`. Only absolute paths
 * on this origin are accepted; the result is pathname + search + hash.
 */
export function safeNextPath(next: string | null | undefined, origin: string, fallback = '/schedule'): string {
  if (!next || UNSAFE_PATH_CHARS.test(next) || !next.startsWith('/') || next.startsWith('//')) {
    return fallback
  }

  let url: URL

  try {
    url = new URL(next, origin)
  } catch {
    return fallback
  }

  if (url.origin !== new URL(origin).origin) {
    return fallback
  }

  if (url.pathname === '/login') {
    return fallback
  }

  return url.pathname + url.search + url.hash
}

/** The URL if it is an absolute http(s) URL, else null (never javascript:, data:, ...). */
export function safeHttpUrl(value: string | null | undefined): string | null {
  const v = value?.trim()

  if (!v || UNSAFE_PATH_CHARS.test(v)) {
    return null
  }

  try {
    const url = new URL(v)

    return url.protocol === 'http:' || url.protocol === 'https:' ? v : null
  } catch {
    return null
  }
}

/** decodeURIComponent that keeps the raw text on a malformed escape (a stray "%" would throw a URIError). */
export function safeDecodeURIComponent(text: string): string {
  try {
    return decodeURIComponent(text)
  } catch {
    return text
  }
}

const HEX_COLOR = /^#(?:[0-9a-f]{3,4}|[0-9a-f]{6}|[0-9a-f]{8})$/i
// Functional notations; the argument list may not contain parentheses, quotes,
// semicolons or colons, so nothing else (url(), var(), expressions) fits.
const FUNC_COLOR = /^(?:rgba?|hsla?|hwb|oklch|oklab|lab|lch)\([-+0-9.%\s,/a-z]*\)$/i
const NAMED_COLOR = /^[a-z]{3,20}$/i

/** A CSS color safe to put in `style`, or undefined. */
export function safeCssColor(value: string | null | undefined): string | undefined {
  const v = value?.trim()

  if (!v || v.length > 64) {
    return undefined
  }

  return HEX_COLOR.test(v) || FUNC_COLOR.test(v) || NAMED_COLOR.test(v) ? v : undefined
}
