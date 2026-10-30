// Hostile and sloppy input over raw HTTP (local mode): broken or trailing
// JSON, oversized bodies, wrong content types, unknown fields and wrong
// types, huge and exotic content, guessed or malicious IDs, tampered
// pagination cursors and search syntax. Then Idempotency-Key replays and
// races: parallel publishes of one post, parallel edits with
// expected_version and parallel creates.
import { beforeAll, describe, expect, it } from 'vitest'

import { api, type Channel, type Collection, type Post, unique } from '../lib/clients.js'
import { type HttpInit, type HttpResponse } from '../lib/http.js'

const agent = api()
let collection: Collection
let channel: Channel

// Raw answers, with the access token (local mode).
function raw(path: string, init: HttpInit = {}): Promise<HttpResponse> {
  return agent.raw(path, init)
}

function errCode(res: HttpResponse): string {
  return res.json<{ error: { code: string } }>().error.code
}

function counts(responses: HttpResponse[]): Record<number, number> {
  const out: Record<number, number> = {}

  for (const r of responses) {
    out[r.status] = (out[r.status] ?? 0) + 1
  }

  return out
}

const send = (body: string | Buffer, contentType = 'application/json', path = '/v1/posts') =>
  raw(path, { method: 'POST', body, headers: { 'Content-Type': contentType } })

const create = (json: Record<string, unknown>, headers: Record<string, string> = {}) =>
  raw('/v1/posts', { method: 'POST', json: { collection: collection.id, title: 'Post', ...json }, headers })

beforeAll(async () => {
  collection = await agent.collection({ name: unique('Input') })
  channel = await agent.customChannel(unique('Input channel'))
})

describe('malformed bodies', () => {
  it('broken JSON, trailing data and a second value are 400 bad_request', async () => {
    for (const body of [
      '{"content":',
      '{content: "x"}',
      '',
      'null x',
      '{"content":"a"} trailing',
      '{"content":"a"}{"content":"b"}',
    ]) {
      const res = await send(body)

      expect(res.status).toBe(400)
      expect(errCode(res)).toBe('bad_request')
    }

    expect(await agent.listPosts({ collection_id: collection.id })).toHaveLength(0)
  })

  it('unknown fields and wrong types are 400, not silently dropped', async () => {
    for (const json of [
      { collection: collection.id, content: 'x', contnet: 'typo' },
      { collection: collection.id, content: 123 },
      { collection: collection.id, content: 'x', tags: 'not-a-list' },
      { collection: collection.id, targets: [{ channel: channel.id }] },
    ]) {
      expect((await raw('/v1/posts', { method: 'POST', json })).status).toBe(400)
    }

    expect((await raw('/v1/posts', { method: 'POST', json: ['content'] })).status).toBe(400)
  })

  it('a missing collection, a post without a time but scheduled, or with unknown media and channels are 404/422', async () => {
    expect((await raw('/v1/posts', { method: 'POST', json: { content: 'homeless' } })).status).toBe(422)
    expect(
      (await raw('/v1/posts', { method: 'POST', json: { collection: 'col_nope', title: 'Post', content: 'x' } }))
        .status,
    ).toBe(404)
    expect((await create({ content: 'x', status: 'scheduled' })).status).toBe(422)
    expect((await create({ content: 'x', status: 'published' })).status).toBe(422)
    expect((await create({ content: 'x', media: ['med_nope'] })).status).toBe(422)
    expect((await create({ content: 'x', targets: [{ channel_id: 'ch_nope' }] })).status).toBe(422)
    expect(
      (await create({ content: 'x', targets: [{ channel_id: channel.id }, { channel_id: channel.id }] })).status,
    ).toBe(422)
    expect((await create({ content: 'x', publish_now: true })).status).toBe(422) // nowhere to publish
    expect((await create({ content: 'x', metadata: ['not', 'an', 'object'] })).status).toBe(422)
  })

  it('bodies over 2 MiB are 413 too_large; content over the limit is 422', async () => {
    const huge = await send(JSON.stringify({ collection: collection.id, content: 'a'.repeat(3 << 20) }))

    expect(huge.status).toBe(413)
    expect(errCode(huge)).toBe('too_large')
    const big = await create({ content: 'a'.repeat(60_000) })

    expect(big.status).toBe(422)
    expect(big.json<{ error: { fields: Record<string, string> } }>().error.fields.content).toMatch(/at most/)
  })

  it('other content types are 415 with the JSON error body', async () => {
    for (const ct of ['text/plain', 'application/x-www-form-urlencoded', 'application/xml', '']) {
      const res = await send('{"name":"ct"}', ct, '/v1/collections')

      expect(res.status).toBe(415)
      expect(errCode(res)).toBe('unsupported_media_type')
    }

    // Parameters and case do not matter.
    expect(
      (await send(JSON.stringify({ name: unique('ct') }), 'Application/JSON; charset=utf-8', '/v1/collections')).status,
    ).toBe(201)
  })
})

describe('content and tags', () => {
  it('keeps exotic Unicode intact and finds it by a prefix', async () => {
    const content = 'Ünïcødé 🚀 עברית العربية 中文 ﷽ é ‍👩‍💻\n<script>alert(1)</script>'
    const post = await agent.createPost(collection.id, { content })
    const back = await agent.getPost(post.id)

    expect(back.content).toBe(content)
    expect((await agent.listPosts({ q: 'Ünïcødé' })).map((p) => p.id)).toContain(post.id)
    expect((await agent.listPosts({ q: 'alert', collection_id: collection.id })).map((p) => p.id)).toContain(post.id)
  })

  it('tags: trimmed, deduplicated, at most 20 of at most 40 characters; a tag filter finds the post', async () => {
    const post = await agent.createPost(collection.id, { content: 'tagged', tags: [' launch ', 'launch', 'sale', ''] })

    expect(post.tags).toEqual(['launch', 'sale'])
    expect((await agent.listPosts({ tag: 'launch', collection_id: collection.id })).map((p) => p.id)).toEqual([post.id])
    expect((await create({ content: 'x', tags: ['x'.repeat(41)] })).status).toBe(422)
    expect((await create({ content: 'x', tags: Array.from({ length: 21 }, (_, i) => `t${i}`) })).status).toBe(422)
    const tags = await agent.get<{ data: { name: string; count: number }[] }>('/v1/tags')

    expect(tags.data.find((t) => t.name === 'launch')?.count).toBeGreaterThanOrEqual(1)
  })

  it('a NUL byte or invalid UTF-8 never becomes a 500', async () => {
    expect((await create({ content: 'nul\u0000byte' })).status).toBeLessThan(500)
    const invalid = await send(
      Buffer.concat([
        Buffer.from(`{"collection":"${collection.id}","content":"bad `),
        Buffer.from([0xff, 0xfe]),
        Buffer.from('"}'),
      ]),
    )

    expect(invalid.status).toBeLessThan(500)
  })
})

describe('IDs, cursors and queries', () => {
  it('guessed, malformed and malicious IDs are 404', async () => {
    for (const id of [
      'post_doesnotexist00000000',
      'not-an-id',
      "post_' OR '1'='1",
      'post_%00',
      '..%2F..%2Fetc%2Fpasswd',
      'x'.repeat(500),
    ]) {
      expect((await raw(`/v1/posts/${id}`)).status).toBe(404)
      expect((await raw(`/v1/posts/${id}/publish`, { method: 'POST' })).status).toBe(404)
      expect(
        (await raw(`/v1/posts/${id}/targets/${channel.id}/result`, { method: 'POST', json: { status: 'published' } }))
          .status,
      ).toBe(404)
    }

    expect((await raw('/v1/media/..%2F..%2Fworkspace.db')).status).toBe(404)
    expect((await raw('/v1/collections/col_nope/posts')).status).toBe(404)
    expect((await raw('/v1/channels/ch_nope')).status).toBe(404)
    expect((await raw('/v1/schedules/sch_nope')).status).toBe(404)
    expect((await raw('/v1/nope')).status).toBe(404)
  })

  it('tampered cursors are 422 or stay inside the workspace, never 500', async () => {
    const b64 = (v: unknown) => Buffer.from(JSON.stringify(v)).toString('base64url')

    for (const cursor of ['zzzz', '!!!', b64({ v: 1 }), b64({ v: { nested: true }, id: 'post_x' }), b64([1, 2])]) {
      expect((await raw(`/v1/posts?cursor=${encodeURIComponent(cursor)}`)).status).toBe(422)
    }

    expect((await raw(`/v1/posts?cursor=${b64({ v: "' OR 1=1 --", id: "' OR 1=1 --" })}`)).status).toBeLessThan(500)

    for (const cursor of ['zzzz', b64({ v: 1 })]) {
      expect((await raw(`/v1/events?cursor=${cursor}`)).status).toBeLessThan(500)
      expect((await raw(`/v1/jobs?cursor=${cursor}`)).status).toBeLessThan(500)
    }
  })

  it('pagination walks every post once, in both orders', async () => {
    const col = await agent.collection({ name: unique('Pages') })
    const ids = new Set<string>()

    for (let i = 0; i < 7; i++) {
      ids.add((await agent.createPost(col.id, { content: `page ${i}` })).id)
    }

    for (const sort of ['created_asc', 'created_desc', 'publish_asc']) {
      const seen: string[] = []
      let cursor: string | undefined

      do {
        const qs = new URLSearchParams({ collection_id: col.id, limit: '3', sort, ...(cursor ? { cursor } : {}) })
        const page = (await raw(`/v1/posts?${qs}`)).json<{ data: Post[]; next_cursor?: string }>()

        seen.push(...page.data.map((p) => p.id))
        cursor = page.next_cursor
      } while (cursor)

      expect(seen.toSorted()).toEqual([...ids].toSorted())
    }
  })

  it('bad limits clamp, bad filters are 422, FTS syntax is never a 500', async () => {
    expect((await raw('/v1/posts?limit=-5')).status).toBe(200)
    expect((await raw('/v1/posts?limit=abc')).status).toBe(200)
    expect((await raw('/v1/posts?limit=100000')).json<{ data: unknown[] }>().data.length).toBeLessThanOrEqual(200)
    expect((await raw('/v1/posts?status=nope')).status).toBe(422)
    expect((await raw('/v1/posts?sort=nope')).status).toBe(422)
    expect((await raw('/v1/posts?publish_after=yesterday')).status).toBe(422)
    expect((await raw('/v1/posts?publish_after=2026-01-01&publish_before=2026-02-01')).status).toBe(200)

    for (const q of ['"unbalanced', '*', 'NEAR(', 'a OR', '-', '"', 'content:x', ')))', '^', 'AND AND']) {
      expect((await raw(`/v1/posts?q=${encodeURIComponent(q)}`)).status).toBeLessThan(500)
    }
  })
})

describe('Idempotency-Key', () => {
  it('replays the first answer for the same request, refuses a different one', async () => {
    const key = `idem-${Date.now()}`
    const first = await create({ content: 'only once' }, { 'Idempotency-Key': key })

    expect(first.status).toBe(201)
    expect(first.headers['idempotent-replayed']).toBeUndefined()

    const again = await create({ content: 'only once' }, { 'Idempotency-Key': key })

    expect(again.status).toBe(201)
    expect(again.headers['idempotent-replayed']).toBe('true')
    expect(again.json()).toEqual(first.json())

    const changed = await create({ content: 'something else' }, { 'Idempotency-Key': key })

    expect(changed.status).toBe(422)
    expect(errCode(changed)).toBe('idempotency_mismatch')
    expect(
      (await raw('/v1/collections', { method: 'POST', json: { name: 'x' }, headers: { 'Idempotency-Key': key } }))
        .status,
    ).toBe(422)
    expect(
      (await agent.listPosts({ collection_id: collection.id })).filter((p) => p.content === 'only once'),
    ).toHaveLength(1)
  })

  it('replays an error answer too, and a publish is not started twice', async () => {
    const post = await agent.createPost(collection.id, {
      content: 'publish once',
      targets: [{ channel_id: channel.id }],
    })
    const key = `idem-publish-${Date.now()}`
    const publish = () => raw(`/v1/posts/${post.id}/publish`, { method: 'POST', headers: { 'Idempotency-Key': key } })
    const a = await publish()
    const b = await publish()

    expect([a.status, b.status]).toEqual([202, 202])
    expect(b.headers['idempotent-replayed']).toBe('true')
    // Without the key, the same click again is a conflict (the post is being published).
    expect((await raw(`/v1/posts/${post.id}/publish`, { method: 'POST' })).status).toBe(409)
    expect(
      (await agent.history(post.id)).filter((e) => e.type === 'post.publish_requested').length,
    ).toBeLessThanOrEqual(1)

    const badKey = `idem-bad-${Date.now()}`
    const bad = () => create({ content: 'x'.repeat(60_000) }, { 'Idempotency-Key': badKey })

    expect((await bad()).status).toBe(422)
    expect((await bad()).headers['idempotent-replayed']).toBe('true')
  })

  it('refuses keys over 200 characters and ignores keys on GET', async () => {
    expect((await create({ content: 'x' }, { 'Idempotency-Key': 'k'.repeat(201) })).status).toBe(422)
    expect((await raw('/v1/posts', { headers: { 'Idempotency-Key': 'get' } })).status).toBe(200)
  })

  it('without a key, duplicate submissions create duplicates (by design)', async () => {
    const col = await agent.collection({ name: unique('Dupes') })

    await Promise.all([1, 2].map(() => agent.createPost(col.id, { content: 'same' })))
    expect(await agent.listPosts({ collection_id: col.id })).toHaveLength(2)
  })
})

describe('races', () => {
  it('20 parallel publishes of one post: exactly one starts, the automation is asked once per channel', async () => {
    const post = await agent.createPost(collection.id, { content: 'contested', targets: [{ channel_id: channel.id }] })
    const results = await Promise.all(
      Array.from({ length: 20 }, () => raw(`/v1/posts/${post.id}/publish`, { method: 'POST' })),
    )

    expect(counts(results)).toEqual({ 202: 1, 409: 19 })
    const jobs = await agent.jobs({ kind: 'publish_target' })

    expect(jobs.filter((j) => j.payload.post_id === post.id)).toHaveLength(1)
  })

  it('schedule and cancel racing: the post ends in one consistent state', async () => {
    const post = await agent.createPost(collection.id, { content: 'flip flop', targets: [{ channel_id: channel.id }] })
    const at = new Date(Date.now() + 3600_000).toISOString()
    const results = await Promise.all(
      Array.from({ length: 10 }, (_, i) =>
        i % 2
          ? raw(`/v1/posts/${post.id}/cancel`, { method: 'POST' })
          : raw(`/v1/posts/${post.id}/schedule`, { method: 'POST', json: { publish_at: at } }),
      ),
    )

    expect(results.every((r) => r.status === 200 || r.status === 409)).toBe(true)
    const final = await agent.getPost(post.id)
    const jobs = (await agent.jobs({ kind: 'publish_post', status: 'queued' })).filter(
      (j) => j.payload.post_id === post.id,
    )

    expect(jobs.length).toBe(final.status === 'scheduled' ? 1 : 0)
  })

  it('parallel edits with the same expected_version: one wins; without it, all apply in order', async () => {
    const post = await agent.createPost(collection.id, { content: 'v1' })
    const guarded = await Promise.all(
      Array.from({ length: 10 }, (_, i) =>
        raw(`/v1/posts/${post.id}`, { method: 'PATCH', json: { content: `guarded ${i}`, expected_version: 1 } }),
      ),
    )

    expect(counts(guarded)).toEqual({ 200: 1, 409: 9 })

    const unguarded = await Promise.all(
      Array.from({ length: 10 }, (_, i) =>
        raw(`/v1/posts/${post.id}`, { method: 'PATCH', json: { content: `free ${i}` } }),
      ),
    )

    expect(counts(unguarded)).toEqual({ 200: 10 })
    expect((await agent.getPost(post.id)).version).toBe(12)
  })

  it('50 parallel creates all land, with unique IDs', async () => {
    const col = await agent.collection({ name: unique('Burst') })
    const created = await Promise.all(
      Array.from({ length: 50 }, (_, i) =>
        raw('/v1/posts', { method: 'POST', json: { collection: col.id, title: 'Post', content: `b${i}` } }),
      ),
    )

    expect(counts(created)).toEqual({ 201: 50 })
    expect(new Set(created.map((r) => r.json<{ id: string }>().id)).size).toBe(50)
    expect(await agent.listPosts({ collection_id: col.id, limit: 100 })).toHaveLength(50)
  })
})
