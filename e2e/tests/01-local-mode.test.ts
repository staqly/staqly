// Local mode: one workspace, created automatically, no API keys, but every
// request needs the install's access token (also from this machine). Browsers
// trade it for a session; the dashboard, published on the network, needs one.
import { describe, expect, it } from 'vitest'

import { api, signInBrowser, signInWithConnectLink } from '../lib/clients.js'
import { http } from '../lib/http.js'
import { API, API_CONTAINER, containerLogs, DASHBOARD, LOCAL_TOKEN, server } from '../lib/stack.js'

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

  it('needs the access token, also from this machine', async () => {
    const res = await http(`${API}/v1/collections`)

    expect(res.status).toBe(401)
    expect(res.json<{ error: { message: string } }>().error.message).toContain('access token')
    expect((await api(null).raw('/v1/collections')).status).toBe(401)
    expect((await api(`${LOCAL_TOKEN}x`).raw('/v1/collections')).status).toBe(401)
  })

  it('the token holds every scope', async () => {
    const session = await api().get<{
      mode: string
      onboarded: boolean
      principal: { type: string; scopes: string[] }
    }>('/v1/session')

    expect(session).toMatchObject({ mode: 'local', onboarded: true, principal: { type: 'local' } })
    expect(session.principal.scopes).toEqual(expect.arrayContaining(['posts:publish', 'channels:manage', 'admin']))
  })

  it('`server token` prints it, and the start printed a link that signs the browser in', async () => {
    expect(await server('token')).toBe(LOCAL_TOKEN)
    // Fixed by STAQLY_LOCAL_TOKEN here: a rotation would not survive the next start, so it is refused.
    await expect(server('token', '--rotate')).rejects.toThrow(/STAQLY_LOCAL_TOKEN/)
    const logs = (await containerLogs('2000-01-01T00:00:00Z', API_CONTAINER)).join('\n')

    expect(logs).toContain(`/#token=${LOCAL_TOKEN}`)
  })

  it('starts with the Default collection and the catalogue of platforms', async () => {
    const collections = (await api().get<{ data: { name: string; is_default: boolean }[] }>('/v1/collections')).data

    expect(collections.some((c) => c.name === 'Default' && c.is_default)).toBe(true)
    const providers = (await api().get<{ data: { id: string; status: string; auth: string }[] }>('/v1/providers')).data

    expect(providers.find((p) => p.id === 'custom')).toMatchObject({ status: 'available', auth: 'none' })
    expect(providers.length).toBeGreaterThan(5)
  })

  it('refuses the dashboard API without a session', async () => {
    expect((await http(`${DASHBOARD}/api/v1/collections`)).status).toBe(401)
  })

  it('signs a browser in with the access token (the #token= link)', async () => {
    const browser = await signInBrowser()

    expect(browser.csrf).not.toBe('')
    expect((await browser.request('/api/v1/collections')).status).toBe(200)
    expect((await browser.request('/api/v1/access-token')).json()).toEqual({ token: LOCAL_TOKEN })
  })

  it('signs a browser in with a one-time link from `server connect-link`', async () => {
    const browser = await signInWithConnectLink()

    expect((await browser.request('/api/v1/collections')).status).toBe(200)
  })
})
