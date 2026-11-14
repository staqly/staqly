// No keyless way into local mode: whatever Host, Origin or forwarding headers a
// client (or a DNS-rebinding page) sends, REST and MCP want the access token, and
// with it any of them work (it is a credential, not a cookie). The dashboard,
// published on the network, needs a signed-in browser, with CSRF on writes.
import { describe, expect, it } from 'vitest'

import { api, signInBrowser, unique } from '../lib/clients.js'
import { http, Session } from '../lib/http.js'
import { INITIALIZE, mcpHttp } from '../lib/mcp.js'
import { API, DASHBOARD, LOCAL_TOKEN } from '../lib/stack.js'

const PORT = new URL(API).port

async function status(headers: Record<string, string>, path = '/v1/collections'): Promise<number> {
  return (await http(`${API}${path}`, { headers })).status
}

const bearer = { Authorization: `Bearer ${LOCAL_TOKEN}` }

describe('local mode without the access token', () => {
  it('is refused from every Host, Origin and forwarded host', async () => {
    for (const headers of [
      {},
      { Host: `localhost:${PORT}` },
      { Host: `127.0.0.1:${PORT}` },
      { Host: 'api' },
      { Host: 'staqly' },
      { Origin: `http://localhost:${new URL(DASHBOARD).port}` },
      { Origin: 'null' },
      { 'X-Forwarded-Host': 'localhost' },
      { 'X-Forwarded-For': '127.0.0.1', 'X-Real-IP': '127.0.0.1' },
    ] as Record<string, string>[]) {
      expect(await status(headers)).toBe(401)
    }
  })

  it('a wrong or malformed credential is 401', async () => {
    for (const auth of [
      'Bearer stk_notakey',
      `Bearer ${LOCAL_TOKEN}x`,
      `Bearer stk_local_${'a'.repeat(52)}`,
      'Basic Zm9vOmJhcg==',
      LOCAL_TOKEN,
    ]) {
      expect(await status({ Authorization: auth })).toBe(401)
    }
  })

  it('MCP over HTTP wants it too', async () => {
    expect((await mcpHttp(INITIALIZE, { Authorization: '' })).status).toBe(401)
    expect((await mcpHttp(INITIALIZE, { Authorization: 'Bearer stk_local_wrong' })).status).toBe(401)
    expect((await mcpHttp(INITIALIZE)).status).toBe(200)
  })
})

describe('local mode with the access token', () => {
  it('works whatever Host or Origin the client sends', async () => {
    for (const headers of [
      {},
      { Host: 'evil.example' },
      { Host: '192.168.1.20:3333' },
      { Origin: 'https://evil.example' },
      { 'X-Forwarded-Host': 'evil.example' },
    ] as Record<string, string>[]) {
      expect(await status({ ...bearer, ...headers })).toBe(200)
    }

    expect((await api().collection({ name: unique('token') })).id).toBeTruthy()
  })
})

describe('the exposed dashboard needs a signed-in browser', () => {
  it('refuses the proxied API without a session, whatever the headers say', async () => {
    for (const headers of [
      {},
      { Origin: DASHBOARD },
      { Host: 'localhost' },
      { 'X-Forwarded-Host': 'localhost' },
      { 'X-Forwarded-For': '127.0.0.1' },
      { Cookie: 'staqly_session=forged' },
    ] as Record<string, string>[]) {
      const res = await http(`${DASHBOARD}/api/v1/posts`, { headers })

      expect(res.status).toBe(401)
    }
  })

  it('a wrong token signs nobody in', async () => {
    const res = await new Session(DASHBOARD, { Origin: DASHBOARD }).signInWithToken(
      'stk_local_wrongwrongwrongwrongwrong',
    )

    expect(res.status).toBe(401)
  })

  it('a signed-in browser works through the proxy, with CSRF on writes', async () => {
    const browser = await signInBrowser()

    expect((await browser.request('/api/v1/posts')).status).toBe(200)
    const csrf = browser.csrf

    browser.csrf = ''
    expect(
      (await browser.request('/api/v1/collections', { method: 'POST', json: { name: unique('nocsrf') } })).status,
    ).toBe(403)
    browser.csrf = csrf
    expect(
      (await browser.request('/api/v1/collections', { method: 'POST', json: { name: unique('csrf') } })).status,
    ).toBe(201)
  })

  it('a device link works once, and an unknown or reused token signs nobody in', async () => {
    const browser = await signInBrowser()
    const link = (await browser.request('/api/v1/device-links', { method: 'POST' })).json<{ url: string }>()
    const token = link.url.slice(link.url.indexOf('#') + 1)
    const phone = new Session(DASHBOARD, { Origin: DASHBOARD })

    expect((await phone.redeemDeviceLink(token)).status).toBe(201)
    expect((await new Session(DASHBOARD, { Origin: DASHBOARD }).redeemDeviceLink(token)).status).toBe(410)
    const unknown = await new Session(DASHBOARD, { Origin: DASHBOARD }).redeemDeviceLink(
      'dl_unknown_token_000000000000',
    )

    expect(unknown.status).toBe(401)
    expect(unknown.json<{ error: { code: string } }>().error.code).toBe('invalid_link')
  })

  it('logging out on the dashboard ends that browser only', async () => {
    const one = await signInBrowser()
    const two = await signInBrowser()

    expect((await one.request('/api/v1/session', { method: 'DELETE' })).status).toBe(204)
    expect((await one.request('/api/v1/posts')).status).toBe(401)
    expect((await two.request('/api/v1/posts')).status).toBe(200)
  })
})
