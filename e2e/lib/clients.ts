import { Staqly } from '@staqly/sdk'

import { Session } from './http.js'
import { API, DASHBOARD, server } from './stack.js'

/**
 * SDK client as a local agent uses it (keyless) or with an API key in cloud
 * mode. It retries 429 and 5xx answers like any SDK user would.
 */
export function sdk(apiKey: string | null = null, maxRetries = 2): Staqly {
  return new Staqly({ baseUrl: API, apiKey, maxRetries })
}

/**
 * Signs a browser in to the dashboard the way `make up` does: a one-time link
 * from `server connect-link`, redeemed through the dashboard's /api proxy.
 */
export async function signInBrowser(): Promise<Session> {
  const link = await server('connect-link')
  const token = link.slice(link.indexOf('#') + 1)
  const browser = new Session(DASHBOARD, { Origin: DASHBOARD })
  const res = await browser.redeemDeviceLink(token)

  if (res.status !== 201) {
    throw new Error(`redeeming the connect link failed: ${res.status} ${res.text()}`)
  }

  return browser
}

/** Unique names, so every test file creates its own data. */
export function unique(prefix: string): string {
  return `${prefix} ${Date.now().toString(36)}`
}
