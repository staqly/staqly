// The same volume, served in cloud mode: every request needs a session or a
// scoped API key, and scopes apply to REST and MCP alike.
//
// This file switches the stack: it re-creates the api with STAQLY_MODE=cloud
// and SETUP_TOKEN set, and leaves it that way for 08-signup-and-plans.
import { PermissionDeniedError } from '@staqly/sdk'
import { beforeAll, describe, expect, it } from 'vitest'

import { sdk, unique } from '../lib/clients.js'
import { http, Session } from '../lib/http.js'
import { callTool, mcpHttp } from '../lib/mcp.js'
import { API, compose, server, SETUP_TOKEN, waitForHttp } from '../lib/stack.js'

async function allItemIds(apiKey: string | null): Promise<string[]> {
  const ids: string[] = []

  for await (const item of sdk(apiKey).items.iterate({ limit: 100 })) {
    ids.push(item.id)
  }

  return ids.toSorted()
}

describe('cloud mode on the local data', () => {
  const operator = new Session(API)
  let localIds: string[]
  let pendingItem: string
  let csrf: string
  let key: string
  let recoveryKey: string
  let adminKey: string

  beforeAll(async () => {
    // Still local mode: remember what the workspace holds.
    const local = sdk()
    const collection = await local.collections.create({ name: unique('Before cloud') })

    pendingItem = (await local.items.create(collection.id, { title: 'Made in local mode' })).id
    localIds = await allItemIds(null)

    await compose(['up', '-d', '--wait', '--no-build', '--force-recreate', 'staqly'], {
      STAQLY_MODE: 'cloud',
      SETUP_TOKEN,
    })
    await waitForHttp(`${API}/health`)
  })

  it('runs in cloud mode', async () => {
    expect((await sdk().setup()).mode).toBe('cloud')
  })

  it('requires credentials', async () => {
    expect((await http(`${API}/v1/collections`)).status).toBe(401)
  })

  it('opens a session with a recovery key from `server recovery-key`', async () => {
    recoveryKey = await server('recovery-key')
    const res = await operator.request('/v1/sessions/recover', { method: 'POST', json: { recovery_key: recoveryKey } })

    expect(res.status).toBe(201)
    expect(operator.jar.size).toBeGreaterThan(0)
    const body = res.json<{ csrf_token: string; api_key: { key: string; scopes: string[] }; recovery_key: string }>()

    csrf = body.csrf_token
    adminKey = body.api_key.key
    expect(adminKey).toMatch(/^stk_/)
    expect(body.api_key.scopes).toEqual(['admin'])
    expect(body.recovery_key).toMatch(/^staqly-recovery-/)
    expect(body.recovery_key).not.toBe(recoveryKey)
  })

  it('the recovery key works once', async () => {
    const res = await http(`${API}/v1/sessions/recover`, { method: 'POST', json: { recovery_key: recoveryKey } })

    expect(res.status).toBe(401)
  })

  it('signs another browser in with the admin key from recovery, keeping the first one', async () => {
    const browser = new Session(API)
    const res = await browser.request('/v1/sessions/api-key', { method: 'POST', json: { api_key: adminKey } })

    expect(res.status).toBe(201)
    expect((await browser.request('/v1/session')).status).toBe(200)
    expect((await operator.request('/v1/session')).status).toBe(200)
  })

  it('enforces CSRF on session requests', async () => {
    const res = await operator.request('/v1/api-keys', { method: 'POST', json: { name: 'x', scopes: ['items:read'] } })

    expect(res.status).toBe(403)
  })

  it('creates a scoped API key for an agent', async () => {
    // Free allows one active key, and recovery just issued the admin key: the operator upgrades first.
    const ws = (await operator.request('/v1/workspace')).json<{ id: string }>()

    await server('set-plan', ws.id, 'pro')
    const res = await operator.request('/v1/api-keys', {
      method: 'POST',
      json: { name: 'agent', scopes: ['collections:read', 'items:read', 'items:write'] },
      headers: { 'X-CSRF-Token': csrf },
    })

    expect(res.status).toBe(201)
    key = res.json<{ key: string }>().key
    expect(key).toMatch(/^stk_/)
  })

  it('an agent key cannot sign in to the dashboard', async () => {
    const res = await http(`${API}/v1/sessions/api-key`, { method: 'POST', json: { api_key: key } })

    expect(res.status).toBe(403)
  })

  it('the key sees all the data created in local mode', async () => {
    expect(localIds).toContain(pendingItem)
    expect(await allItemIds(key)).toEqual(localIds)
  })

  it('a creator key may not review over REST', async () => {
    const err = await sdk(key)
      .items.reject(pendingItem)
      .catch((e: unknown) => e)

    expect(err).toBeInstanceOf(PermissionDeniedError)
    expect((err as PermissionDeniedError).status).toBe(403)
  })

  it('a creator key may not review over MCP', async () => {
    const result = await callTool('approve_item', { item_id: pendingItem }, { Authorization: `Bearer ${key}` })

    expect(result.isError).toBe(true)
    expect((await sdk(key).items.get(pendingItem)).status).toBe('pending')
  })

  it('refuses keyless MCP', async () => {
    const { status } = await mcpHttp({ jsonrpc: '2.0', id: 5, method: 'tools/list' })

    expect(status).toBe(401)
  })
})
