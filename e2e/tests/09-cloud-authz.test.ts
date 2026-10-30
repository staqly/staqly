// Cloud-mode authorization against the real container: every scope allowed
// and denied on representative endpoints (REST and MCP), key lifecycle
// (expired, revoked, rotated, escalation), sessions with CSRF, logout and
// revocation, recovery key rotation, and isolation between two workspaces
// that guess each other's IDs. Needs the cloud-mode stack from 07.
import { beforeAll, describe, expect, it } from 'vitest'

import { api, fixture, type Post, unique } from '../lib/clients.js'
import { type Admin, adminFor, createKey, signup, type Signup, withKey, workspaces } from '../lib/cloud.js'
import { http, Session } from '../lib/http.js'
import { callTool } from '../lib/mcp.js'
import { API, RECEIVER_INTERNAL } from '../lib/stack.js'
import { sleep, waitFor } from '../lib/wait.js'

const ALL_SCOPES = [
  'collections:read',
  'collections:write',
  'collections:delete',
  'posts:read',
  'posts:write',
  'posts:delete',
  'posts:publish',
  'channels:read',
  'channels:manage',
  'schedules:manage',
  'media:read',
  'media:write',
  'webhooks:manage',
  'alerts:manage',
  'events:read',
  'admin',
] as const

type Scope = (typeof ALL_SCOPES)[number]

let a: Admin

beforeAll(async () => {
  if ((await api().setup()).mode !== 'cloud') {
    throw new Error('09-cloud-authz needs the cloud-mode stack from 07-cloud-mode')
  }

  // The workspace that started in local mode (the oldest one), on Pro since 07.
  const [first] = await workspaces()

  a = await adminFor(first!.id)
})

// Writing needs reading: creating or editing a post looks its collection up, scheduling reads the post. Every key
// in the matrix holds the two read scopes next to the one under test, so the rows check the write scopes alone.
const BASE: Scope[] = ['collections:read', 'posts:read']

describe('scope matrix (REST)', () => {
  const keys = {} as Record<Scope, string>
  let collectionId: string
  let channelId: string
  let postId: string
  let scheduleId: string

  beforeAll(async () => {
    for (const scope of ALL_SCOPES) {
      keys[scope] = (await createKey(a.key, [...new Set([...BASE, scope])])).key
    }

    const admin = api(a.key)
    const collection = await admin.collection({ name: unique('Scopes') })

    collectionId = collection.id
    channelId = (await admin.customChannel(unique('Scopes channel'))).id
    postId = (await admin.createPost(collectionId, { content: 'scope matrix', targets: [{ channel_id: channelId }] }))
      .id
    scheduleId = (
      await admin.createSchedule({
        collection: collectionId,
        name: 'Scopes',
        kind: 'recurring',
        rule: { every_minutes: 60 },
        channels: [channelId],
      })
    ).id
  })

  interface Row {
    name: string
    scope: Scope
    /** Builds the request; runs once per key, so mutating rows get fresh data. */
    request: () => Promise<{ method: string; path: string; json?: unknown }>
    ok: number
  }

  const admin = () => api(a.key)
  const freshDraft = async () =>
    (await admin().createPost(collectionId, { content: 'fresh', targets: [{ channel_id: channelId }] })).id
  const freshMedia = async () => (await admin().upload(fixture('avatar0.png'), 'doomed.png')).id
  const freshWaiting = async () => {
    const id = await freshDraft()

    await admin().publish(id)
    await waitFor(
      async () =>
        (await admin().getPost(id)).targets[0]!.status === 'publishing' &&
        (await admin().getPost(id)).targets[0]!.attempts > 0,
    )

    return id
  }
  const freshFailed = async () => {
    const id = await freshWaiting()

    await admin().result(id, channelId, { status: 'failed', error: 'no' })

    return id
  }
  const later = () => new Date(Date.now() + 3600_000).toISOString()

  const rows: Row[] = [
    {
      name: 'list collections',
      scope: 'collections:read',
      ok: 200,
      request: async () => ({ method: 'GET', path: '/v1/collections' }),
    },
    {
      name: 'get a collection',
      scope: 'collections:read',
      ok: 200,
      request: async () => ({ method: 'GET', path: `/v1/collections/${collectionId}` }),
    },
    {
      name: 'create a collection',
      scope: 'collections:write',
      ok: 201,
      request: async () => ({ method: 'POST', path: '/v1/collections', json: { name: unique('Scoped') } }),
    },
    {
      name: 'delete a collection',
      scope: 'collections:delete',
      ok: 204,
      request: async () => {
        const name = unique('Doomed')
        const { id } = await admin().collection({ name })

        return { method: 'DELETE', path: `/v1/collections/${id}?confirm=${encodeURIComponent(name)}` }
      },
    },
    { name: 'list posts', scope: 'posts:read', ok: 200, request: async () => ({ method: 'GET', path: '/v1/posts' }) },
    {
      name: 'get a post',
      scope: 'posts:read',
      ok: 200,
      request: async () => ({ method: 'GET', path: `/v1/posts/${postId}` }),
    },
    {
      name: 'preview a post',
      scope: 'posts:read',
      ok: 200,
      request: async () => ({ method: 'GET', path: `/v1/posts/${postId}/preview` }),
    },
    { name: 'list tags', scope: 'posts:read', ok: 200, request: async () => ({ method: 'GET', path: '/v1/tags' }) },
    {
      name: 'requested work',
      scope: 'posts:read',
      ok: 200,
      request: async () => ({ method: 'GET', path: '/v1/work' }),
    },
    {
      name: 'list schedules',
      scope: 'posts:read',
      ok: 200,
      request: async () => ({ method: 'GET', path: '/v1/schedules' }),
    },
    { name: 'list jobs', scope: 'posts:read', ok: 200, request: async () => ({ method: 'GET', path: '/v1/jobs' }) },
    {
      name: 'job stats',
      scope: 'posts:read',
      ok: 200,
      request: async () => ({ method: 'GET', path: '/v1/jobs/stats' }),
    },
    {
      name: 'create a post',
      scope: 'posts:write',
      ok: 201,
      request: async () => ({
        method: 'POST',
        path: '/v1/posts',
        json: { collection: collectionId, title: 'Post', content: 'by key' },
      }),
    },
    {
      name: 'edit a post',
      scope: 'posts:write',
      ok: 200,
      request: async () => ({ method: 'PATCH', path: `/v1/posts/${await freshDraft()}`, json: { content: 'edited' } }),
    },
    {
      name: 'duplicate a post',
      scope: 'posts:write',
      ok: 201,
      request: async () => ({ method: 'POST', path: `/v1/posts/${postId}/duplicate` }),
    },
    // Deleting needs its own scope: posts:write / collections:write alone get 403.
    {
      name: 'delete a post',
      scope: 'posts:delete',
      ok: 204,
      request: async () => ({ method: 'DELETE', path: `/v1/posts/${await freshDraft()}` }),
    },
    {
      name: 'delete a media file',
      scope: 'posts:delete',
      ok: 204,
      request: async () => ({ method: 'DELETE', path: `/v1/media/${await freshMedia()}` }),
    },
    {
      name: 'schedule a post',
      scope: 'posts:publish',
      ok: 200,
      request: async () => ({
        method: 'POST',
        path: `/v1/posts/${await freshDraft()}/schedule`,
        json: { publish_at: later() },
      }),
    },
    {
      name: 'publish a post',
      scope: 'posts:publish',
      ok: 202,
      request: async () => ({ method: 'POST', path: `/v1/posts/${await freshDraft()}/publish` }),
    },
    {
      name: 'cancel a post',
      scope: 'posts:publish',
      ok: 200,
      request: async () => {
        const id = await freshDraft()

        await admin().schedule(id, later())

        return { method: 'POST', path: `/v1/posts/${id}/cancel` }
      },
    },
    {
      name: 'report a result',
      scope: 'posts:publish',
      ok: 200,
      request: async () => ({
        method: 'POST',
        path: `/v1/posts/${await freshWaiting()}/targets/${channelId}/result`,
        json: { status: 'published', url: 'https://example.com/r' },
      }),
    },
    {
      name: 'retry a target',
      scope: 'posts:publish',
      ok: 202,
      request: async () => ({ method: 'POST', path: `/v1/posts/${await freshFailed()}/targets/${channelId}/retry` }),
    },
    {
      name: 'cancel a job',
      scope: 'posts:publish',
      ok: 200,
      request: async () => {
        const id = await freshDraft()

        await admin().schedule(id, later())
        const job = (await admin().jobs({ kind: 'publish_post', status: 'queued' })).find(
          (j) => j.payload.post_id === id,
        )!

        return { method: 'POST', path: `/v1/jobs/${job.id}/cancel` }
      },
    },
    {
      name: 'list providers',
      scope: 'channels:read',
      ok: 200,
      request: async () => ({ method: 'GET', path: '/v1/providers' }),
    },
    {
      name: 'list channels',
      scope: 'channels:read',
      ok: 200,
      request: async () => ({ method: 'GET', path: '/v1/channels' }),
    },
    {
      name: 'get a channel',
      scope: 'channels:read',
      ok: 200,
      request: async () => ({ method: 'GET', path: `/v1/channels/${channelId}` }),
    },
    {
      name: 'connect a channel',
      scope: 'channels:manage',
      ok: 201,
      request: async () => ({
        method: 'POST',
        path: '/v1/channels',
        json: { provider: 'custom', name: unique('Scoped channel') },
      }),
    },
    {
      name: 'rename a channel',
      scope: 'channels:manage',
      ok: 200,
      request: async () => ({ method: 'PATCH', path: `/v1/channels/${channelId}`, json: { name: unique('Renamed') } }),
    },
    {
      name: 'test a channel',
      scope: 'channels:manage',
      ok: 200,
      request: async () => ({ method: 'POST', path: `/v1/channels/${channelId}/test` }),
    },
    {
      name: 'create a schedule',
      scope: 'schedules:manage',
      ok: 201,
      request: async () => ({
        method: 'POST',
        path: '/v1/schedules',
        json: { collection: collectionId, name: unique('Scoped'), kind: 'recurring', rule: { every_minutes: 60 } },
      }),
    },
    {
      name: 'run a schedule',
      scope: 'schedules:manage',
      ok: 202,
      request: async () => ({ method: 'POST', path: `/v1/schedules/${scheduleId}/run` }),
    },
    {
      name: 'download media',
      scope: 'media:read',
      ok: 200,
      request: async () => ({ method: 'GET', path: `/v1/media/${await freshMedia()}` }),
    },
    {
      name: 'list webhooks',
      scope: 'webhooks:manage',
      ok: 200,
      request: async () => ({ method: 'GET', path: '/v1/webhooks' }),
    },
    {
      name: 'list deliveries',
      scope: 'webhooks:manage',
      ok: 200,
      request: async () => ({ method: 'GET', path: '/v1/webhook-deliveries' }),
    },
    {
      name: 'list alerts',
      scope: 'alerts:manage',
      ok: 200,
      request: async () => ({ method: 'GET', path: '/v1/alerts' }),
    },
    {
      name: 'list events',
      scope: 'events:read',
      ok: 200,
      request: async () => ({ method: 'GET', path: '/v1/events' }),
    },
    {
      name: 'post history',
      scope: 'events:read',
      ok: 200,
      request: async () => ({ method: 'GET', path: `/v1/posts/${postId}/events` }),
    },
    {
      name: 'export events',
      scope: 'events:read',
      ok: 200,
      request: async () => ({ method: 'GET', path: '/v1/events/export?limit=10' }),
    },
    { name: 'list API keys', scope: 'admin', ok: 200, request: async () => ({ method: 'GET', path: '/v1/api-keys' }) },
    { name: 'list sessions', scope: 'admin', ok: 200, request: async () => ({ method: 'GET', path: '/v1/sessions' }) },
    {
      name: 'list exports',
      scope: 'admin',
      ok: 200,
      request: async () => ({ method: 'GET', path: '/v1/workspace/exports' }),
    },
    { name: 'list agents', scope: 'admin', ok: 200, request: async () => ({ method: 'GET', path: '/v1/agents' }) },
    {
      name: 'purge a post',
      scope: 'admin',
      ok: 200,
      request: async () => ({ method: 'POST', path: `/v1/posts/${await freshDraft()}/purge` }),
    },
  ]

  for (const row of rows) {
    it(`${row.name}: allowed for ${row.scope} and admin, 403 for every other scope`, async () => {
      const got: Record<string, number> = {}
      const want: Record<string, number> = {}

      for (const scope of ALL_SCOPES) {
        const { method, path, json } = await row.request()

        got[scope] = (await withKey(keys[scope], path, { method, json })).status
        want[scope] = scope === row.scope || scope === 'admin' || BASE.includes(row.scope) ? row.ok : 403
      }

      expect(got).toEqual(want)
    })
  }

  it('a denied publish leaves the post a draft and records nothing', async () => {
    const id = await freshDraft()

    for (const scope of ALL_SCOPES.filter((sc) => sc !== 'posts:publish' && sc !== 'admin')) {
      expect((await withKey(keys[scope], `/v1/posts/${id}/publish`, { method: 'POST' })).status).toBe(403)
    }

    const post = await admin().getPost(id)

    expect(post.status).toBe('draft')
    expect(post.version).toBe(1)
    expect((await admin().history(id)).map((e) => e.type)).toEqual(['post.created'])
  })

  it('media upload needs media:write; a writer may not upload, an uploader may not post', async () => {
    const png = fixture('avatar0.png')

    for (const [scope, want] of [
      ['media:write', 201],
      ['posts:write', 403],
      ['admin', 201],
    ] as const) {
      expect((await api(keys[scope]).raw('/v1/media', { method: 'POST', form: form(png) })).status).toBe(want)
    }

    expect(
      (
        await withKey(keys['media:write'], '/v1/posts', {
          method: 'POST',
          json: { collection: collectionId, title: 'Post', content: 'x' },
        })
      ).status,
    ).toBe(403)
  })

  it('MCP applies the same scopes', async () => {
    const as = (key: string) => ({ Authorization: `Bearer ${key}` })
    const reader = await callTool(
      'create_post',
      { collection: collectionId, title: 'Post', content: 'over MCP' },
      as(keys['posts:read']),
    )

    expect(reader.isError).toBe(true)
    const writer = await callTool<Post>(
      'create_post',
      { collection: collectionId, title: 'Post', content: 'over MCP', targets: [{ channel_id: channelId }] },
      as(keys['posts:write']),
    )

    expect(writer.isError).toBeFalsy()
    expect(
      (await callTool('publish_post', { post_id: writer.structuredContent.id }, as(keys['posts:write']))).isError,
    ).toBe(true)
    expect(
      (await callTool('publish_post', { post_id: writer.structuredContent.id }, as(keys['posts:publish']))).isError,
    ).toBeFalsy()
    const hooksOnly = (await createKey(a.key, ['webhooks:manage'])).key

    expect((await callTool('list_posts', {}, as(hooksOnly))).isError).toBe(true)
    expect((await callTool('connect_channel', { provider: 'custom' }, as(keys['channels:read']))).isError).toBe(true)

    // Deleting needs posts:delete / collections:delete over MCP too, not :write.
    const post = await freshDraft()
    const media = await freshMedia()
    const name = unique('Doomed MCP')
    const doomed = (await admin().collection({ name })).id
    const writerKey = (await createKey(a.key, ['collections:read', 'collections:write', 'posts:read', 'posts:write']))
      .key

    expect((await callTool('delete_post', { post_id: post }, as(writerKey))).isError).toBe(true)
    expect((await callTool('delete_media', { media_id: media }, as(writerKey))).isError).toBe(true)
    expect((await callTool('delete_collection', { collection: doomed, confirm: name }, as(writerKey))).isError).toBe(
      true,
    )
    expect((await callTool('delete_post', { post_id: post }, as(keys['posts:delete']))).isError).toBeFalsy()
    expect((await callTool('delete_media', { media_id: media }, as(keys['posts:delete']))).isError).toBeFalsy()
    const deleter = (await createKey(a.key, ['collections:read', 'collections:delete'])).key

    expect(
      (await callTool('delete_collection', { collection: doomed, confirm: name }, as(deleter))).isError,
    ).toBeFalsy()
  })
})

function form(png: Buffer): FormData {
  const f = new FormData()

  f.set('file', new Blob([new Uint8Array(png)], { type: 'image/png' }), 'scoped.png')

  return f
}

describe('API key lifecycle', () => {
  it('refuses requests without a valid bearer key (401)', async () => {
    const cases: Record<string, string>[] = [
      {},
      { Authorization: 'Bearer' },
      { Authorization: 'Bearer ' },
      { Authorization: 'Basic dXNlcjpwYXNz' },
      { Authorization: `Bearer stk_${'x'.repeat(48)}` },
      { Authorization: `Bearer ${a.key}x` },
      { Authorization: `bearer ${a.key}` },
    ]

    for (const headers of cases) {
      expect((await http(`${API}/v1/posts`, { headers })).status).toBe(401)
    }
  })

  it('refuses unknown and retired scopes and empty scope lists (422)', async () => {
    for (const scopes of [
      [],
      ['posts:destroy'],
      ['ADMIN'],
      ['items:read'],
      ['items:review'],
      ['actions:execute'],
      ['watchers:manage'],
    ]) {
      const res = await withKey(a.key, '/v1/api-keys', { method: 'POST', json: { name: 'bad', scopes } })

      expect(res.status).toBe(422)
    }
  })

  it('refuses an expiry in the past (422)', async () => {
    const res = await withKey(a.key, '/v1/api-keys', {
      method: 'POST',
      json: { name: 'already dead', scopes: ['posts:read'], expires_at: '2020-01-01T00:00:00Z' },
    })

    expect(res.status).toBe(422)
  })

  it('an expired key stops working (401) and no longer counts as active', async () => {
    const expiresAt = new Date(Date.now() + 2500).toISOString()
    const key = await createKey(a.key, ['posts:read'], { expires_at: expiresAt })

    expect((await withKey(key.key, '/v1/posts')).status).toBe(200)
    await sleep(Math.max(0, Date.parse(expiresAt) - Date.now()) + 1000)
    expect((await withKey(key.key, '/v1/posts')).status).toBe(401)
  })

  it('a revoked key stops working at once; revoking twice is a conflict', async () => {
    const key = await createKey(a.key, ['posts:read'])

    expect((await withKey(key.key, '/v1/posts')).status).toBe(200)
    expect((await withKey(a.key, `/v1/api-keys/${key.id}`, { method: 'DELETE' })).status).toBe(204)
    expect((await withKey(key.key, '/v1/posts')).status).toBe(401)
    expect((await withKey(a.key, `/v1/api-keys/${key.id}`, { method: 'DELETE' })).status).toBe(409)
  })

  it('rotation replaces the key: the old one is dead, the new one has the same scopes', async () => {
    const key = await createKey(a.key, ['posts:read', 'collections:read'])
    const res = await withKey(a.key, `/v1/api-keys/${key.id}/rotate`, { method: 'POST' })

    expect(res.status).toBe(201)
    const rotated = res.json<{ id: string; key: string; scopes: string[] }>()

    expect(rotated.id).not.toBe(key.id)
    expect(rotated.key).not.toBe(key.key)
    expect(rotated.scopes).toEqual(['posts:read', 'collections:read'])
    expect((await withKey(key.key, '/v1/posts')).status).toBe(401)
    expect((await withKey(rotated.key, '/v1/posts')).status).toBe(200)
    expect((await withKey(rotated.key, '/v1/webhooks')).status).toBe(403)
    // The revoked original cannot be rotated again.
    expect((await withKey(a.key, `/v1/api-keys/${key.id}/rotate`, { method: 'POST' })).status).toBe(404)
  })

  it('no privilege escalation: a non-admin key cannot mint, rotate or revoke keys', async () => {
    const writer = await createKey(a.key, ['posts:write', 'webhooks:manage'])
    const minted = await withKey(writer.key, '/v1/api-keys', { method: 'POST', json: { name: 'x', scopes: ['admin'] } })

    expect(minted.status).toBe(403)
    expect((await withKey(writer.key, `/v1/api-keys/${writer.id}/rotate`, { method: 'POST' })).status).toBe(403)
    expect((await withKey(writer.key, `/v1/api-keys/${writer.id}`, { method: 'DELETE' })).status).toBe(403)
    expect((await withKey(writer.key, '/v1/recovery-key/rotate', { method: 'POST' })).status).toBe(403)
    // Audit events (sign-ins, key changes) need the admin scope to subscribe to.
    const audit = await withKey(writer.key, '/v1/webhooks', {
      method: 'POST',
      json: { url: `${RECEIVER_INTERNAL}/audit`, events: ['session.*'] },
    })

    expect(audit.status).toBe(403)
    // An agent key never opens a dashboard session.
    expect((await http(`${API}/v1/sessions/api-key`, { method: 'POST', json: { api_key: writer.key } })).status).toBe(
      403,
    )
  })
})

describe('sessions and CSRF', () => {
  it('sets a hardened session cookie', async () => {
    const res = await http(`${API}/v1/sessions/api-key`, { method: 'POST', json: { api_key: a.key } })
    const cookie = (res.headers['set-cookie'] ?? []).join('; ')

    expect(res.status).toBe(201)
    expect(cookie).toMatch(/HttpOnly/i)
    expect(cookie).toMatch(/SameSite=Lax/i)
    expect(cookie).toMatch(/Secure/i)
  })

  it('needs the session CSRF token on unsafe requests, not on reads', async () => {
    const s = await browserSession()
    const body = { json: { name: unique('CSRF') } }

    expect((await s.request('/v1/collections')).status).toBe(200)
    s.csrf = ''
    expect((await s.request('/v1/collections', { method: 'POST', ...body })).status).toBe(403)
    s.csrf = 'wrong-token'
    expect((await s.request('/v1/collections', { method: 'POST', ...body })).status).toBe(403)
    // Another session's token does not fit either.
    s.csrf = a.session.csrf
    expect((await s.request('/v1/collections', { method: 'POST', ...body })).status).toBe(403)
  })

  it('logout ends the session; logout itself needs CSRF', async () => {
    const s = await browserSession()
    const csrf = s.csrf

    s.csrf = ''
    expect((await s.request('/v1/session', { method: 'DELETE' })).status).toBe(403)
    expect((await s.request('/v1/session')).status).toBe(200)
    s.csrf = csrf
    const cookie = s.jar.header()

    expect((await s.request('/v1/session', { method: 'DELETE' })).status).toBe(204)
    // The old cookie value is dead on the server, not only cleared in the browser.
    expect((await http(`${API}/v1/session`, { headers: { Cookie: cookie } })).status).toBe(401)
  })

  it('an admin revokes another session, which stops working at once', async () => {
    const other = await browserSession()
    const sessions = (await other.request('/v1/sessions')).json<{ data: { id: string; current: boolean }[] }>().data
    const otherId = sessions.find((x) => x.current)!.id

    expect((await a.session.request(`/v1/sessions/${otherId}`, { method: 'DELETE' })).status).toBe(204)
    expect((await other.request('/v1/session')).status).toBe(401)
    expect((await a.session.request(`/v1/sessions/${otherId}`, { method: 'DELETE' })).status).toBe(409)
    expect((await a.session.request('/v1/sessions/ses_doesnotexist000000', { method: 'DELETE' })).status).toBe(404)
  })

  it('rotating the recovery key kills the old one; the new one works once', async () => {
    const res = await a.session.request('/v1/recovery-key/rotate', { method: 'POST' })

    expect(res.status).toBe(200)
    const fresh = res.json<{ recovery_key: string }>().recovery_key

    expect(fresh).toMatch(/^staqly-recovery-/)
    const garbage = await http(`${API}/v1/sessions/recover`, {
      method: 'POST',
      json: { recovery_key: 'staqly-recovery-AAAAA-AAAAA-AAAAA-AAAAA' },
    })

    expect(garbage.status).toBe(401)

    const other = await browserSession()
    const recovered = new Session(API)
    const used = await recovered.request('/v1/sessions/recover', { method: 'POST', json: { recovery_key: fresh } })

    expect(used.status).toBe(201)
    recovered.csrf = used.json<{ csrf_token: string }>().csrf_token
    expect((await http(`${API}/v1/sessions/recover`, { method: 'POST', json: { recovery_key: fresh } })).status).toBe(
      401,
    )
    // Recovery signs every other browser out (someone else may hold them); on Pro the API keys stay.
    expect((await other.request('/v1/session')).status).toBe(401)
    expect((await a.session.request('/v1/session')).status).toBe(401)
    expect((await recovered.request('/v1/session')).status).toBe(200)
    expect((await withKey(a.key, '/v1/posts')).status).toBe(200)
    a.session = recovered
  })
})

/** A second browser signed in with the admin key. */
async function browserSession(): Promise<Session> {
  const s = new Session(API)
  const res = await s.request('/v1/sessions/api-key', { method: 'POST', json: { api_key: a.key } })

  expect(res.status).toBe(201)
  s.csrf = res.json<{ csrf_token: string }>().csrf_token

  return s
}

describe('workspace isolation (IDs guessed from another workspace)', () => {
  let b: Signup
  const ids = {} as Record<
    | 'collection'
    | 'post'
    | 'channel'
    | 'schedule'
    | 'job'
    | 'webhook'
    | 'delivery'
    | 'media'
    | 'export'
    | 'alert'
    | 'key'
    | 'session',
    string
  >
  const marker = `isolation${Date.now().toString(36)}`

  beforeAll(async () => {
    b = await signup('E2E isolation B')

    const admin = api(a.key)
    const collection = await admin.collection({ name: unique('Secret A') })
    const webhook = await admin.webhook({ url: `${RECEIVER_INTERNAL}/a-only`, events: ['post.created'] })

    ids.collection = collection.id
    ids.webhook = webhook.id
    ids.channel = (await admin.customChannel(unique('A channel'))).id
    ids.post = (
      await admin.createPost(collection.id, { content: `A secret ${marker}`, targets: [{ channel_id: ids.channel }] })
    ).id
    ids.schedule = (
      await admin.createSchedule({
        collection: collection.id,
        name: 'A schedule',
        kind: 'recurring',
        rule: { every_minutes: 60 },
        channels: [ids.channel],
      })
    ).id
    ids.job = (await admin.jobs({ kind: 'run_schedule', status: 'queued' })).find(
      (j) => j.payload.schedule_id === ids.schedule,
    )!.id
    ids.media = (await admin.upload(fixture('avatar0.png'), 'a.png')).id
    ids.alert = (
      await admin.alert({
        name: unique('A phone'),
        type: 'ntfy',
        config: { server_url: 'https://ntfy.example.com', topic: 'a-secret' },
      })
    ).id
    ids.key = (await createKey(a.key, ['posts:read'])).id
    ids.session = (await (await browserSession()).request('/v1/sessions'))
      .json<{ data: { id: string; current: boolean }[] }>()
      .data.find((s) => s.current)!.id

    const delivery = await waitFor(
      async () => (await admin.deliveries({ webhook_id: webhook.id, post_id: ids.post }))[0],
      { message: 'no delivery for the A webhook' },
    )

    ids.delivery = delivery.id

    const exp = await withKey(a.key, '/v1/workspace/export', { method: 'POST' })

    ids.export =
      exp.status === 202
        ? exp.json<{ id: string }>().id
        : exp.json<{ error: { details: { export_id: string } } }>().error.details.export_id
  })

  it('cloud mode never delivers webhooks to private addresses (SSRF guard)', async () => {
    const failed = await waitFor(
      async () => {
        const [d] = await api(a.key).deliveries({ webhook_id: ids.webhook, post_id: ids.post })

        return d && d.attempts > 0 && d.last_error ? d : undefined
      },
      { message: 'delivery to the private receiver was not attempted' },
    )

    expect(failed.last_error).toMatch(/private destination/)
    expect(failed.last_status_code).toBeNull()
  })

  it('a schedule endpoint on a private address is never called in cloud mode (SSRF guard)', async () => {
    // A collection of its own: in A's main collection the schedule would publish A's waiting draft instead.
    const own = await api(a.key).collection({ name: unique('SSRF') })
    const res = await withKey(a.key, '/v1/schedules', {
      method: 'POST',
      json: {
        collection: own.id,
        name: 'private',
        kind: 'recurring',
        rule: { every_minutes: 60 },
        source: 'endpoint',
        endpoint_url: `${RECEIVER_INTERNAL}/posts`,
      },
    })

    expect(res.status).toBe(201)
    const ran = await api(a.key).runSchedule(res.json<{ id: string }>().id)

    expect(ran.last_result).toMatch(/^endpoint failed: .*private/)
  })

  it('every A resource is 404 for B, for reads and writes alike', async () => {
    const probes: [string, string, unknown?][] = [
      ['GET', `/v1/posts/${ids.post}`],
      ['PATCH', `/v1/posts/${ids.post}`, { content: 'hijacked' }],
      ['DELETE', `/v1/posts/${ids.post}`],
      ['POST', `/v1/posts/${ids.post}/publish`],
      ['POST', `/v1/posts/${ids.post}/schedule`, { publish_at: new Date(Date.now() + 3600_000).toISOString() }],
      ['POST', `/v1/posts/${ids.post}/cancel`],
      ['POST', `/v1/posts/${ids.post}/duplicate`],
      ['GET', `/v1/posts/${ids.post}/preview`],
      ['POST', `/v1/posts/${ids.post}/targets/${ids.channel}/result`, { status: 'published' }],
      ['POST', `/v1/posts/${ids.post}/targets/${ids.channel}/retry`],
      ['POST', `/v1/posts/${ids.post}/purge`],
      ['GET', `/v1/collections/${ids.collection}`],
      ['GET', `/v1/collections/${ids.collection}/posts`],
      ['POST', '/v1/posts', { collection: ids.collection, title: 'Post', content: 'planted' }],
      ['PATCH', `/v1/collections/${ids.collection}`, { name: 'hijacked' }],
      ['GET', `/v1/channels/${ids.channel}`],
      ['PATCH', `/v1/channels/${ids.channel}`, { name: 'hijacked' }],
      ['DELETE', `/v1/channels/${ids.channel}`],
      ['POST', `/v1/channels/${ids.channel}/test`],
      ['GET', `/v1/schedules/${ids.schedule}`],
      ['PATCH', `/v1/schedules/${ids.schedule}`, { enabled: false }],
      ['DELETE', `/v1/schedules/${ids.schedule}`],
      ['POST', `/v1/schedules/${ids.schedule}/run`],
      ['GET', `/v1/jobs/${ids.job}`],
      ['POST', `/v1/jobs/${ids.job}/cancel`],
      ['POST', `/v1/jobs/${ids.job}/retry`],
      ['GET', `/v1/webhooks/${ids.webhook}`],
      ['PATCH', `/v1/webhooks/${ids.webhook}`, { url: 'https://evil.example.com/' }],
      ['DELETE', `/v1/webhooks/${ids.webhook}`],
      ['POST', `/v1/webhooks/${ids.webhook}/test`],
      ['POST', `/v1/webhook-deliveries/${ids.delivery}/retry`],
      ['GET', `/v1/media/${ids.media}`],
      ['GET', `/v1/media/${ids.media}?w=160`],
      ['DELETE', `/v1/media/${ids.media}`],
      ['GET', `/v1/workspace/exports/${ids.export}`],
      ['GET', `/v1/workspace/exports/${ids.export}/download`],
      ['GET', `/v1/alerts/${ids.alert}`],
      ['PATCH', `/v1/alerts/${ids.alert}`, { name: 'hijacked' }],
      ['DELETE', `/v1/alerts/${ids.alert}`],
      ['GET', `/v1/alerts/${ids.alert}/deliveries`],
      ['DELETE', `/v1/api-keys/${ids.key}`],
      ['POST', `/v1/api-keys/${ids.key}/rotate`],
      ['DELETE', `/v1/sessions/${ids.session}`],
    ]
    const got: Record<string, number> = {}

    for (const [method, path, json] of probes) {
      got[`${method} ${path}`] = (await withKey(b.key, path, { method, json })).status
    }

    expect(got).toEqual(Object.fromEntries(probes.map(([m, p]) => [`${m} ${p}`, 404])))
    // The history of A's post is an empty list for B (the events are filtered by workspace, the post is not looked up).
    const history = await withKey(b.key, `/v1/posts/${ids.post}/events`)

    expect(history.status).toBe(200)
    expect(history.json<{ data: unknown[] }>().data).toEqual([])
    // B's own post cannot target A's channel either.
    const planted = await withKey(b.key, '/v1/posts', {
      method: 'POST',
      json: { collection: 'Default', title: 'Post', content: 'x', targets: [{ channel_id: ids.channel }] },
    })

    expect(planted.status).toBe(422)
  })

  it('A is unchanged after the probes', async () => {
    const admin = api(a.key)
    const post = await admin.getPost(ids.post)

    expect(post).toMatchObject({ status: 'draft', content: `A secret ${marker}`, version: 1 })
    expect((await admin.get<{ name: string }>(`/v1/collections/${ids.collection}`)).name).toMatch(/^Secret A/)
    expect((await admin.get<{ url: string }>(`/v1/webhooks/${ids.webhook}`)).url).toBe(`${RECEIVER_INTERNAL}/a-only`)
    expect((await withKey(a.key, `/v1/media/${ids.media}`)).status).toBe(200)
    expect((await admin.get<{ enabled: boolean }>(`/v1/alerts/${ids.alert}`)).enabled).toBe(true)
    expect((await admin.get<{ enabled: boolean }>(`/v1/schedules/${ids.schedule}`)).enabled).toBe(true)
    expect((await a.session.request('/v1/session')).status).toBe(200)
  })

  it('B lists, searches and filters see nothing of A', async () => {
    const bk = api(b.key)

    expect(await bk.listPosts()).toHaveLength(0)
    expect(await bk.listPosts({ q: marker })).toHaveLength(0)
    expect((await bk.raw(`/v1/posts?collection_id=${ids.collection}`)).status).toBe(404)
    expect((await bk.get<{ data: unknown[] }>('/v1/webhooks')).data).toHaveLength(0)
    expect(await bk.deliveries({ post_id: ids.post })).toHaveLength(0)
    expect((await bk.get<{ data: unknown[] }>('/v1/channels')).data).toHaveLength(0)
    expect((await bk.get<{ data: unknown[] }>('/v1/schedules')).data).toHaveLength(0)
    expect((await bk.get<{ data: unknown[] }>('/v1/alerts')).data).toHaveLength(0)
    expect((await bk.get<{ data: unknown[] }>('/v1/work')).data).toHaveLength(0)
    expect((await bk.jobs()).find((j) => j.id === ids.job)).toBeUndefined()
    expect(
      (await bk.get<{ data: { post_id: string | null }[] }>('/v1/events?limit=200')).data.filter(
        (e) => e.post_id === ids.post,
      ),
    ).toHaveLength(0)
    expect((await bk.get<{ data: unknown[] }>('/v1/workspace/exports')).data).toHaveLength(0)
    expect((await bk.get<{ data: { id: string }[] }>('/v1/api-keys')).data.map((k) => k.id)).not.toContain(ids.key)
  })

  it('MCP with A IDs touches nothing', async () => {
    const as = { Authorization: `Bearer ${b.key}` }

    expect((await callTool('get_post', { post_id: ids.post }, as)).isError).toBe(true)
    expect((await callTool('publish_post', { post_id: ids.post }, as)).isError).toBe(true)
    expect(
      (await callTool('report_publish_result', { post_id: ids.post, channel_id: ids.channel, status: 'published' }, as))
        .isError,
    ).toBe(true)
    expect((await callTool('run_schedule', { schedule_id: ids.schedule }, as)).isError).toBe(true)
    expect((await api(a.key).getPost(ids.post)).status).toBe('draft')
  })

  it("B's session cookie does not open A's data either", async () => {
    const s = new Session(API)

    expect((await s.request('/v1/sessions/api-key', { method: 'POST', json: { api_key: b.key } })).status).toBe(201)
    expect((await s.request(`/v1/posts/${ids.post}`)).status).toBe(404)
    expect((await s.request(`/v1/media/${ids.media}`)).status).toBe(404)
  })
})
