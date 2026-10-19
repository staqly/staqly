// The same volume, served in cloud mode: every request needs a session or a
// scoped API key, and scopes apply to REST and MCP alike.
//
// This file switches the stack: it re-creates the api with STAQLY_MODE=cloud,
// SETUP_TOKEN and DATABASE_URL (the stack's PostgreSQL) set, moves the local
// SQLite data over with `server import-sqlite`, and leaves it that way for
// 08-signup-and-plans.
import { beforeAll, describe, expect, it } from 'vitest'

import { api, type Post, unique } from '../lib/clients.js'
import { http, Session } from '../lib/http.js'
import { callTool, mcpHttp } from '../lib/mcp.js'
import { API, CLOUD_DATABASE_URL, compose, server, SETUP_TOKEN, waitForHttp } from '../lib/stack.js'

async function allPostIds(apiKey: string | null): Promise<string[]> {
  const ids: string[] = []
  let cursor: string | undefined

  do {
    const page = await api(apiKey).get<{ data: Post[]; next_cursor?: string }>(
      `/v1/posts?limit=100${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`,
    )

    ids.push(...page.data.map((p) => p.id))
    cursor = page.next_cursor
  } while (cursor)

  return ids.toSorted()
}

describe('cloud mode on the local data', () => {
  const operator = new Session(API)
  let localIds: string[]
  let draft: string
  let channel: string
  let csrf: string
  let key: string
  let recoveryKey: string
  let adminKey: string

  beforeAll(async () => {
    // Still local mode: remember what the workspace holds.
    const local = api()
    const collection = await local.collection({ name: unique('Before cloud') })

    channel = (await local.customChannel(unique('Before cloud channel'))).id
    draft = (
      await local.createPost(collection.id, { content: 'Made in local mode', targets: [{ channel_id: channel }] })
    ).id
    localIds = await allPostIds(null)

    await compose(['up', '-d', '--wait', '--no-build', '--force-recreate', 'staqly'], {
      STAQLY_MODE: 'cloud',
      AUTH_DEV_SIGNIN: 'true', // no Google client in the suite: the sign-in from before accounts
      SETUP_TOKEN,
      DATABASE_URL: CLOUD_DATABASE_URL,
    })
    await waitForHttp(`${API}/health`)
    // The local SQLite data (the same /data volume) moves into the empty PostgreSQL database.
    expect(await server('import-sqlite', '/data/workspace.db')).toMatch(/^imported \d+ rows from \d+ tables/)
  })

  it('runs in cloud mode', async () => {
    expect((await api().setup()).mode).toBe('cloud')
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
    expect(
      (await http(`${API}/v1/sessions/recover`, { method: 'POST', json: { recovery_key: recoveryKey } })).status,
    ).toBe(401)
  })

  it('signs another browser in with the admin key from recovery, keeping the first one', async () => {
    const browser = new Session(API)
    const res = await browser.request('/v1/sessions/api-key', { method: 'POST', json: { api_key: adminKey } })

    expect(res.status).toBe(201)
    expect((await browser.request('/v1/session')).status).toBe(200)
    expect((await operator.request('/v1/session')).status).toBe(200)
  })

  it('enforces CSRF on session requests', async () => {
    const res = await operator.request('/v1/api-keys', { method: 'POST', json: { name: 'x', scopes: ['posts:read'] } })

    expect(res.status).toBe(403)
  })

  it('creates a scoped API key for a writing agent', async () => {
    // Free allows one active key, and recovery just issued the admin key: the operator upgrades first.
    const ws = (await operator.request('/v1/workspace')).json<{ id: string }>()

    await server('set-plan', ws.id, 'pro')
    const res = await operator.request('/v1/api-keys', {
      method: 'POST',
      json: { name: 'writer', scopes: ['collections:read', 'posts:read', 'posts:write', 'channels:read'] },
      headers: { 'X-CSRF-Token': csrf },
    })

    expect(res.status).toBe(201)
    key = res.json<{ key: string }>().key
    expect(key).toMatch(/^stk_/)
  })

  it('an agent key cannot sign in to the dashboard', async () => {
    expect((await http(`${API}/v1/sessions/api-key`, { method: 'POST', json: { api_key: key } })).status).toBe(403)
  })

  it('the key sees all the data created in local mode', async () => {
    expect(localIds).toContain(draft)
    expect(await allPostIds(key)).toEqual(localIds)
  })

  it('a writer key may write but not publish over REST', async () => {
    const edited = await api(key).patch<Post>(`/v1/posts/${draft}`, { content: 'Edited by the writer' })

    expect(edited.content).toBe('Edited by the writer')
    expect((await api(key).raw(`/v1/posts/${draft}/publish`, { method: 'POST' })).status).toBe(403)
    expect(
      (
        await api(key).raw(`/v1/posts/${draft}/schedule`, {
          method: 'POST',
          json: { publish_at: new Date(Date.now() + 3600_000).toISOString() },
        })
      ).status,
    ).toBe(403)
    // Creating a post that is already scheduled needs posts:publish too.
    const created = await api(key).raw('/v1/posts', {
      method: 'POST',
      json: {
        collection: (await api(key).getPost(draft)).collection_id,
        content: 'x',
        targets: [{ channel_id: channel }],
        publish_at: new Date(Date.now() + 3600_000).toISOString(),
        status: 'scheduled',
      },
    })

    expect(created.status).toBe(403)
    expect((await api(key).getPost(draft)).status).toBe('draft')
  })

  it('a writer key may not publish over MCP', async () => {
    const result = await callTool('publish_post', { post_id: draft }, { Authorization: `Bearer ${key}` })

    expect(result.isError).toBe(true)
    expect((await api(key).getPost(draft)).status).toBe('draft')
  })

  it('refuses keyless MCP', async () => {
    const { status } = await mcpHttp({ jsonrpc: '2.0', id: 5, method: 'tools/list' })

    expect(status).toBe(401)
  })
})
