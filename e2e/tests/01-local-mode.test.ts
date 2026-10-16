// Local mode: one workspace, created automatically, no keys, but only for
// this machine. A foreign Origin (CSRF) or Host (DNS rebinding) is refused, and
// the dashboard, which is published on the network, needs a session.
import { describe, expect, it } from 'vitest'

import { sdk, signInBrowser } from '../lib/clients.js'
import { http } from '../lib/http.js'
import { API, DASHBOARD } from '../lib/stack.js'

describe('local mode', () => {
  it('creates the workspace automatically', async () => {
    const viaProxy = (await http(`${DASHBOARD}/api/v1/setup`)).json()

    expect(viaProxy).toMatchObject({ mode: 'local', initialized: true })
    expect(await sdk().setup()).toMatchObject({ mode: 'local', initialized: true })
  })

  it('disables workspace setup', async () => {
    const res = await http(`${API}/v1/workspaces`, { method: 'POST', json: {} })

    expect(res.status).toBe(409)
  })

  it('needs no key from this machine', async () => {
    expect((await http(`${API}/v1/collections`)).status).toBe(200)
    expect((await sdk().whoami()).mode).toBe('local')
  })

  it('refuses a cross-site request (foreign Origin)', async () => {
    const res = await http(`${API}/v1/collections`, {
      method: 'POST',
      json: { name: 'x' },
      headers: { Origin: 'https://evil.example' },
    })

    expect(res.status).toBe(403)
  })

  it('refuses a DNS-rebinding request (foreign Host)', async () => {
    const res = await http(`${API}/v1/collections`, { headers: { Host: 'evil.example' } })

    expect(res.status).toBe(403)
  })

  it('refuses the dashboard API without a session', async () => {
    expect((await http(`${DASHBOARD}/api/v1/collections`)).status).toBe(403)
  })

  it('signs a browser in with a one-time link from `server connect-link`', async () => {
    const browser = await signInBrowser()

    expect(browser.csrf).not.toBe('')
    expect((await browser.request('/api/v1/collections')).status).toBe(200)
  })
})
