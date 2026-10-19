// Plan limits on a Free workspace in cloud mode (402 with details) and races
// against them: parallel creates near a count limit never overshoot, parallel
// posts near the monthly quota stop exactly at it, N parallel publishes of
// one post have one winner. Also the Free data-only export and a suspended
// (expired Pro) workspace. Needs the cloud-mode stack from 07.
import { beforeAll, describe, expect, it } from 'vitest'

import { api, fixture, unique } from '../lib/clients.js'
import { signup, type Signup, withKey } from '../lib/cloud.js'
import { type HttpResponse } from '../lib/http.js'
import { RECEIVER_INTERNAL, server } from '../lib/stack.js'
import { waitFor } from '../lib/wait.js'
import { ndjson, unzip } from '../lib/zip.js'

let ws: Signup

const png = fixture('avatar0.png')

function counts(responses: HttpResponse[]): Record<number, number> {
  const out: Record<number, number> = {}

  for (const r of responses) {
    out[r.status] = (out[r.status] ?? 0) + 1
  }

  return out
}

function limitOf(res: HttpResponse): unknown {
  return res.json<{ error: { details?: { limit?: string } } }>().error.details?.limit
}

async function usage(): Promise<Record<string, number>> {
  return (await withKey(ws.key, '/v1/workspace/usage')).json<{ usage: Record<string, number> }>().usage
}

async function exportZip(): Promise<{ job: { includes_media: boolean }; files: Map<string, Buffer> }> {
  const started = await withKey(ws.key, '/v1/workspace/export', { method: 'POST' })

  expect(started.status).toBe(202)
  const id = started.json<{ id: string }>().id
  const job = await waitFor(
    async () => {
      const j = (await withKey(ws.key, `/v1/workspace/exports/${id}`)).json<{
        status: string
        includes_media: boolean
      }>()

      return j.status === 'ready' ? j : undefined
    },
    { message: 'export did not become ready', timeout: 60_000 },
  )
  const zip = await withKey(ws.key, `/v1/workspace/exports/${id}/download`)

  expect(zip.status).toBe(200)
  expect(zip.headers['content-type']).toBe('application/zip')

  return { job, files: unzip(zip.body) }
}

beforeAll(async () => {
  if ((await api().setup()).mode !== 'cloud') {
    throw new Error('10-cloud-plans-and-races needs the cloud-mode stack from 07-cloud-mode')
  }

  ws = await signup('E2E Free plan')
  await server('set-plan', ws.workspaceId, 'free')
})

describe('Free plan limits (402)', () => {
  it('reports the Free plan in usage', async () => {
    const res = (await withKey(ws.key, '/v1/workspace/usage')).json<{
      plan: { id: string }
      plan_expires_at: unknown
    }>()

    expect(res.plan.id).toBe('free')
    expect(res.plan_expires_at).toBeNull()
  })

  it('one API key: the sign-up admin key is it', async () => {
    const res = await withKey(ws.key, '/v1/api-keys', {
      method: 'POST',
      json: { name: 'agent', scopes: ['posts:read'] },
    })

    expect(res.status).toBe(402)
    expect(res.json<{ error: { code: string; details: Record<string, unknown> } }>().error).toMatchObject({
      code: 'plan_limit_exceeded',
      details: { limit: 'api_keys', max: 1, used: 1, plan: 'free' },
    })
  })

  it('3 webhooks: 6 parallel creates give exactly 3 webhooks', async () => {
    const results = await Promise.all(
      Array.from({ length: 6 }, (_, i) =>
        withKey(ws.key, '/v1/webhooks', {
          method: 'POST',
          json: { url: `${RECEIVER_INTERNAL}/free-${i}`, events: ['post.purged'] },
        }),
      ),
    )

    expect(counts(results)).toEqual({ 201: 3, 402: 3 })
    expect(results.filter((r) => r.status === 402).map(limitOf)).toEqual(['webhooks', 'webhooks', 'webhooks'])
    expect((await api(ws.key).get<{ data: unknown[] }>('/v1/webhooks')).data).toHaveLength(3)
  })

  it('2 alerts: 4 parallel creates give exactly 2', async () => {
    const results = await Promise.all(
      Array.from({ length: 4 }, (_, i) =>
        withKey(ws.key, '/v1/alerts', {
          method: 'POST',
          json: { name: `Free ${i}`, type: 'ntfy', config: { server_url: RECEIVER_INTERNAL, topic: `free${i}` } },
        }),
      ),
    )

    expect(counts(results)).toEqual({ 201: 2, 402: 2 })
    expect((await api(ws.key).get<{ data: unknown[] }>('/v1/alerts')).data).toHaveLength(2)
  })

  it('3 channels: 5 parallel connects give exactly 3', async () => {
    const results = await Promise.all(
      Array.from({ length: 5 }, (_, i) =>
        withKey(ws.key, '/v1/channels', { method: 'POST', json: { provider: 'custom', name: `Free channel ${i}` } }),
      ),
    )

    expect(counts(results)).toEqual({ 201: 3, 402: 2 })
    expect(results.filter((r) => r.status === 402).map(limitOf)).toEqual(['channels', 'channels'])
    expect((await api(ws.key).get<{ data: unknown[] }>('/v1/channels')).data).toHaveLength(3)
    expect((await usage()).channels).toBe(3)
  })

  it('files up to 5 MB', async () => {
    const big = new FormData()

    // A real PNG header (types are detected from content) padded past 5 MB.
    big.set('file', new Blob([Buffer.concat([png, Buffer.alloc(6 << 20)])], { type: 'image/png' }), 'big.png')
    const res = await withKey(ws.key, '/v1/media', { method: 'POST', form: big })

    expect(res.status).toBe(402)
    expect(limitOf(res)).toBe('max_upload_bytes')
    expect((await usage()).storage_bytes ?? 0).toBe(0)
  })

  it('no event export on Free (402), and the workspace export is data only', async () => {
    expect((await withKey(ws.key, '/v1/events/export')).status).toBe(402)

    const collection = await api(ws.key).collection({ name: unique('Free export') })
    const media = await api(ws.key).upload(png, 'avatar.png')

    await api(ws.key).createPost(collection.id, { content: 'with a picture', media: [media.id] })

    const { job, files } = await exportZip()

    expect(job.includes_media).toBe(false)
    expect([...files.keys()].filter((n) => n.startsWith('media/'))).toEqual([])
    const meta = JSON.parse(files.get('workspace.json')!.toString('utf8')) as { includes_media: boolean; note?: string }

    expect(meta.includes_media).toBe(false)
    expect(meta.note).toBeTruthy()
    expect(ndjson(files, 'media.ndjson')).toHaveLength(1)
    expect(ndjson<{ content: string }>(files, 'posts.ndjson').map((p) => p.content)).toContain('with a picture')
  })
})

describe('races', () => {
  it('10 parallel publishes of one post: one wins, the rest 409, one request to the automation', async () => {
    const owner = api(ws.key)
    const collection = await owner.collection({ name: unique('Race') })
    const [channel] = (await owner.get<{ data: { id: string }[] }>('/v1/channels')).data
    const post = await owner.createPost(collection.id, { content: 'contested', targets: [{ channel_id: channel!.id }] })
    const results = await Promise.all(
      Array.from({ length: 10 }, () => withKey(ws.key, `/v1/posts/${post.id}/publish`, { method: 'POST' })),
    )

    expect(counts(results)).toEqual({ 202: 1, 409: 9 })
    const requested = await waitFor(
      async () => {
        const events = (await owner.history(post.id)).filter((e) => e.type === 'post.publish_requested')

        return events.length > 0 ? events : undefined
      },
      { message: 'the automation was not asked' },
    )

    expect(requested).toHaveLength(1)
    expect((await owner.jobs({ kind: 'publish_target' })).filter((j) => j.payload.post_id === post.id)).toHaveLength(1)
  })

  it('parallel PATCH with the same expected_version: one wins, the others 409', async () => {
    const owner = api(ws.key)
    const collection = await owner.collection({ name: unique('Versions') })
    const post = await owner.createPost(collection.id, { content: 'v1' })
    const results = await Promise.all(
      Array.from({ length: 8 }, (_, i) =>
        withKey(ws.key, `/v1/posts/${post.id}`, {
          method: 'PATCH',
          json: { content: `writer ${i}`, expected_version: post.version },
        }),
      ),
    )

    expect(counts(results)).toEqual({ 200: 1, 409: 7 })
    const winner = results.find((r) => r.status === 200)!.json<{ content: string; version: number }>()
    const now = await owner.getPost(post.id)

    expect(now).toMatchObject({ content: winner.content, version: post.version + 1 })
  })

  it('parallel creates near the monthly quota stop exactly at 50', async () => {
    const collection = await api(ws.key).collection({ name: unique('Quota') })
    const used = (await usage()).posts_per_month ?? 0
    const filler = 48 - used

    expect(filler).toBeGreaterThan(0)

    for (let i = 0; i < filler; i++) {
      // withKey waits out 429s: this is ~50 requests on one key.
      expect(
        (
          await withKey(ws.key, '/v1/posts', {
            method: 'POST',
            json: { collection: collection.id, content: `filler ${i}` },
          })
        ).status,
      ).toBe(201)
    }

    expect((await usage()).posts_per_month).toBe(48)

    const results = await Promise.all(
      Array.from({ length: 6 }, (_, i) =>
        withKey(ws.key, '/v1/posts', { method: 'POST', json: { collection: collection.id, content: `last ${i}` } }),
      ),
    )

    expect(counts(results)).toEqual({ 201: 2, 402: 4 })
    expect(results.filter((r) => r.status === 402).map(limitOf)).toEqual(Array(4).fill('posts_per_month'))
    expect((await usage()).posts_per_month).toBe(50)
    expect(
      (await api(ws.key).listPosts({ collection_id: collection.id, limit: 100 })).filter((p) =>
        p.content.startsWith('last'),
      ),
    ).toHaveLength(2)
    // Duplicating counts too; a schedule that fetches posts from an endpoint would hit the same wall.
    const copy = await withKey(
      ws.key,
      `/v1/posts/${(await api(ws.key).listPosts({ collection_id: collection.id, limit: 1 }))[0]!.id}/duplicate`,
      { method: 'POST' },
    )

    expect(copy.status).toBe(402)
  })

  it('an Idempotency-Key replays per key: same body same answer, other body 422', async () => {
    await server('set-plan', ws.workspaceId, 'pro') // room for posts again
    const collection = await api(ws.key).collection({ name: unique('Idem cloud') })
    const key = `idem-${Date.now()}`
    const send = (content: string) =>
      withKey(ws.key, '/v1/posts', {
        method: 'POST',
        json: { collection: collection.id, content },
        headers: { 'Idempotency-Key': key },
      })

    const [first, ...replays] = await Promise.all([send('once'), send('once'), send('once')])
    const all = [first!, ...replays]
    const created = all.filter((r) => r.status === 201)

    // Concurrent duplicates: one runs, the others replay it or are told it is in progress.
    expect(created.length).toBeGreaterThanOrEqual(1)
    expect(all.every((r) => r.status === 201 || r.status === 409)).toBe(true)
    expect(new Set(created.map((r) => r.json<{ id: string }>().id)).size).toBe(1)
    const later = await send('once')

    expect(later.status).toBe(201)
    expect(later.headers['idempotent-replayed']).toBe('true')
    expect((await send('different')).status).toBe(422)
    expect(await api(ws.key).listPosts({ collection_id: collection.id })).toHaveLength(1)
    await server('set-plan', ws.workspaceId, 'free')
  })
})

describe('Pro and suspension', () => {
  it('Pro lifts the count limits and exports media files', async () => {
    await server('set-plan', ws.workspaceId, 'pro')
    const res = await withKey(ws.key, '/v1/webhooks', {
      method: 'POST',
      json: { url: `${RECEIVER_INTERNAL}/pro`, events: ['post.purged'] },
    })

    expect(res.status).toBe(201)
    expect(
      (await withKey(ws.key, '/v1/channels', { method: 'POST', json: { provider: 'custom', name: 'Pro channel' } }))
        .status,
    ).toBe(201)
    const { job, files } = await exportZip()

    expect(job.includes_media).toBe(true)
    expect([...files.keys()].filter((n) => n.startsWith('media/'))).toHaveLength(1)
    const hooks = JSON.parse(files.get('webhooks.json')!.toString('utf8')) as { secret: string }[]

    expect(hooks.length).toBe(4)
    expect(hooks.every((h) => h.secret === '[redacted]')).toBe(true)
  })

  it('an ended Pro period makes the workspace read-only (402 plan_expired) until Free is chosen', async () => {
    await server('set-plan', ws.workspaceId, 'pro', '--until', '2020-01-01')
    const write = await withKey(ws.key, '/v1/collections', { method: 'POST', json: { name: unique('Suspended') } })

    expect(write.status).toBe(402)
    expect(write.json<{ error: { code: string } }>().error.code).toBe('plan_expired')
    const [post] = await api(ws.key).listPosts({ limit: 1 })

    expect(
      (
        await withKey(ws.key, `/v1/posts/${post!.id}/schedule`, {
          method: 'POST',
          json: { publish_at: new Date(Date.now() + 3600_000).toISOString() },
        })
      ).status,
    ).toBe(402)
    expect((await withKey(ws.key, '/v1/posts')).status).toBe(200)
    expect((await withKey(ws.key, '/v1/workspace/usage')).json<{ suspended: boolean }>().suspended).toBe(true)

    const free = await withKey(ws.key, '/v1/workspace/plan', { method: 'POST', json: { plan: 'free' } })

    expect(free.status).toBe(200)
    expect((await withKey(ws.key, '/v1/collections', { method: 'POST', json: { name: unique('Back') } })).status).toBe(
      201,
    )
  })
})
