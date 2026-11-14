/**
 * A local install prints a link that signs the browser in: `http://localhost:8080/#token=stk_local_...`. The
 * token rides in the URL fragment (browsers never send it to a server). <Providers> takes it out of the address
 * bar as soon as the app's code loads, before any page redirects (the root page sends `/` on to the calendar);
 * it stays in memory until the auth gate trades it for the usual session cookie. Connect-a-device links (`/connect#...`)
 * carry another kind of token and are left to their page.
 */
const PREFIX = 'stk_local_'

let pending: string | null = null

/** Reads `#token=stk_local_...` from the current URL and removes it from the address bar. */
export function captureLinkToken(): string | null {
  if (typeof window === 'undefined' || window.location.pathname.startsWith('/connect')) {
    return null
  }

  const hash = window.location.hash.replace(/^#/, '')

  if (!hash) {
    return null
  }

  const token = (new URLSearchParams(hash).get('token') ?? '').trim()

  if (!token.startsWith(PREFIX)) {
    return null
  }

  window.history.replaceState(window.history.state, '', window.location.pathname + window.location.search)
  pending = token

  return token
}

/**
 * Takes a `#token=stk_local_...` out of the address bar again. Next's router writes the URL it started with back
 * when it hydrates, after captureLinkToken ran, so <Providers> calls this once the page is up.
 */
export function scrubLinkToken() {
  if (typeof window !== 'undefined' && /(^#|&)token=stk_local_/.test(window.location.hash)) {
    window.history.replaceState(window.history.state, '', window.location.pathname + window.location.search)
  }
}

/** The token from the link, until the sign-in with it has started (clearLinkToken). */
export function peekLinkToken(): string | null {
  return pending
}

export function clearLinkToken() {
  pending = null
}
