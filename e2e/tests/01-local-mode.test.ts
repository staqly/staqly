// Local mode: one workspace, created automatically, no keys, but only for
// this machine. A foreign Origin (CSRF) or Host (DNS rebinding) is refused, and
// the dashboard, which is published on the network, needs a session.
import { describe, expect, it } from 'vitest'

import { api, signInBrowser } from '../lib/clients.js'
import { http } from '../lib/http.js'
import { API, DASHBOARD } from '../lib/stack.js'

describe('local mode', () => {
  it('creates the workspace automatically', async () => {
    const viaProxy = (await http(`${DASHBOARD}/api/v1/setup`)).json()

    expect(viaProxy).toMatchObject({ mode: 'local', initialized: true })
    expect(await api().setup()).toMatchObject({ mode: 'local', initialized: true })
  })

  it('disables workspace setup', async () => {
    const res = await http(`${API}/v1/workspaces`, { method: 'POST', json: {} })

    expect(res.status).toBe(409)
  })

  it('needs no key from this machine, and the session holds every scope', async () => {
    expect((await http(`${API}/v1/collections`)).status).toBe(200)
    const session = await api().get<{ mode: string; principal: { scopes: string[] } }>('/v1/session')

    expect(session.mode).toBe('local')
    expect(session.principal.scopes).toEqual(expect.arrayContaining(['posts:publish', 'channels:manage', 'admin']))
  })

  it('starts with the Default collection and the catalogue of platforms', async () => {
    const collections = (await api().get<{ data: { name: string; is_default: boolean }[] }>('/v1/collections')).data

    expect(collections.some((c) => c.name === 'Default' && c.is_default)).toBe(true)
    const providers = (await api().get<{ data: { id: string; status: string; auth: string }[] }>('/v1/providers')).data

    expect(providers.find((p) => p.id === 'custom')).toMatchObject({ status: 'available', auth: 'none' })
    expect(providers.length).toBeGreaterThan(5)
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
