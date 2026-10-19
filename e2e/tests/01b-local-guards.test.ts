// The local-mode guard in detail: which Host, Origin and X-Forwarded-Host
// values count as "this machine" for keyless requests, over REST and MCP,
// and the dashboard (published on the network, DASHBOARD_EXPOSED=true) that
// always needs a signed-in browser, whatever headers a client forges.
import { describe, expect, it } from 'vitest'

import { signInBrowser, unique } from '../lib/clients.js'
import { http, Session } from '../lib/http.js'
import { INITIALIZE } from '../lib/mcp.js'
import { API, DASHBOARD } from '../lib/stack.js'

const PORT = new URL(API).port

async function keyless(headers: Record<string, string>, path = '/v1/collections'): Promise<number> {
  return (await http(`${API}${path}`, { headers })).status
}

describe('local-mode guard (keyless API)', () => {
  it('admits local Host names', async () => {
    for (const host of [
      `localhost:${PORT}`,
      `127.0.0.1:${PORT}`,
      `[::1]:${PORT}`,
      'staqly',
      'api',
      'app.localhost',
      'LOCALHOST.',
    ]) {
      expect(await keyless({ Host: host })).toBe(200)
    }
  })

  it('refuses every other Host (DNS rebinding)', async () => {
    for (const host of [
      'evil.example',
      'localhost.evil.example',
      '127.0.0.1.nip.io',
      `evil.example:${PORT}`,
      '10.0.0.5',
      '192.168.1.20:3333',
      'staqly.evil.example',
    ]) {
      const res = await http(`${API}/v1/collections`, { headers: { Host: host } })

      expect(res.status).toBe(403)
      expect(res.json<{ error: { code: string } }>().error.code).toBe('not_local')
    }
  })

  it("admits only the dashboard's own Origins", async () => {
    const dash = new URL(DASHBOARD).port

    for (const origin of [`http://localhost:${dash}`, `http://127.0.0.1:${dash}`, `http://[::1]:${dash}`]) {
      expect(await keyless({ Origin: origin })).toBe(200)
    }

    for (const origin of [
      'null',
      'https://evil.example',
      `http://localhost:${PORT}`,
      'http://localhost:3000',
      `http://localhost:${dash}.evil.example`,
      `http://evil.example:${dash}`,
      'file://',
      'chrome-extension://abcdef',
    ]) {
      expect(await keyless({ Origin: origin })).toBe(403)
    }
  })

  it('refuses any X-Forwarded-Host while the dashboard is exposed (no spoofing past the proxy)', async () => {
    for (const fwd of ['localhost', `localhost:${PORT}`, '127.0.0.1', 'evil.example', '']) {
      expect(await keyless({ 'X-Forwarded-Host': fwd })).toBe(403)
    }

    // A forged client address changes nothing either (TRUST_PROXY is off).
    expect(await keyless({ 'X-Forwarded-For': '127.0.0.1', 'X-Real-IP': '127.0.0.1' })).toBe(200)
    expect(await keyless({ 'X-Forwarded-For': '127.0.0.1', 'X-Forwarded-Host': 'localhost' })).toBe(403)
  })

  it('refuses a cross-site write even with a JSON body and a local Host', async () => {
    const res = await http(`${API}/v1/collections`, {
      method: 'POST',
      json: { name: unique('csrf') },
      headers: { Origin: 'https://evil.example', Host: `localhost:${PORT}` },
    })

    expect(res.status).toBe(403)
  })

  it('applies the same guard to MCP (403 not_local)', async () => {
    const mcp = (headers: Record<string, string>) =>
      http(`${API}/mcp`, {
        method: 'POST',
        json: INITIALIZE,
        headers: { Accept: 'application/json, text/event-stream', ...headers },
      })

    expect((await mcp({})).status).toBe(200)
    expect((await mcp({ Origin: 'https://evil.example' })).status).toBe(403)
    expect((await mcp({ Host: 'evil.example' })).status).toBe(403)
    expect((await mcp({ 'X-Forwarded-Host': 'localhost' })).status).toBe(403)
  })

  it('a bogus bearer key is refused, not ignored in favour of keyless access', async () => {
    expect(await keyless({ Authorization: 'Bearer stk_notakey' })).toBe(401)
    expect(await keyless({ Authorization: 'Basic Zm9vOmJhcg==' })).toBe(401)
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

      expect(res.status).toBe(403)
    }
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
    expect((await one.request('/api/v1/posts')).status).toBe(403)
    expect((await two.request('/api/v1/posts')).status).toBe(200)
  })
})
