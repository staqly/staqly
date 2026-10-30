// The same operations through every client — TypeScript SDK, Python SDK
// (sync and async), the staqly CLI, curl and MCP — must leave identical
// persisted state and emit identical events and webhooks. Then workflows that
// hand one post from client to client, and clients racing on the same posts.
import { ConflictError, type Event, type Post, type Staqly, verifyWebhook } from '@staqly/sdk'
import { afterAll, beforeAll, describe, expect, inject, it } from 'vitest'

import {
  type CloudWorkspace,
  cliRunner,
  curl,
  mcp,
  MCPToolError,
  normalize,
  PyCallError,
  type PyClient,
  PythonSDK,
  receiver,
  sdk,
  signup,
  until,
} from './lib.js'

const local = inject('local')
const cloud = inject('cloud')
const bin = inject('bin')

let py: PythonSDK
let ws: CloudWorkspace

beforeAll(async () => {
  py = new PythonSDK()
  await py.start()
  ws = await signup(cloud.url)
})

afterAll(() => py?.stop())

const CLIENTS = ['ts', 'py', 'pyasync', 'cli', 'curl', 'mcp'] as const

type ClientName = (typeof CLIENTS)[number]

const INPUT = {
  title: 'Same operation',
  content: 'Same operation\nBody text: ünïcödé ✓',
  metadata: { n: 1, nested: { a: [1, 2], ok: true } },
  source: 'xc',
  external_ref: 'same-ref',
  tags: ['xc', 'same'],
}
const RESULT = { url: 'https://example.com/p/1', external_id: 'p-42' }

/** What must match between clients: the type, the data and the principal. */
const strip = (e: Event) => normalize({ type: e.type, data: e.data, actor_type: e.actor.type, actor: e.actor.id })

/** Waits until the post's targets were handed to the automation. */
async function requested(c: Staqly, id: string): Promise<Post> {
  return until(async () => {
    const p = await c.posts.get(id)

    return p.targets.every((t) => t.status === 'publishing' && t.attempts > 0) ? p : undefined
  })
}

describe.each(['local', 'cloud'] as const)('%s: one operation, six clients', (name) => {
  let url: string
  let key: string
  let c: Staqly
  let col: string
  let channel: string
  let secret: string
  const ids = {} as Record<ClientName, string>
  let rx: Awaited<ReturnType<typeof receiver>>

  beforeAll(async () => {
    url = name === 'local' ? local.url : cloud.url
    key = name === 'local' ? local.token : ws.agentKey
    c = sdk(url, key || null)
    col = (await c.collections.create({ name: `same op ${Date.now()}` })).id
    channel = (await c.channels.connect({ provider: 'custom', name: `same op channel ${Date.now()}` })).id
    rx = await receiver()
    secret = (
      await c.webhooks.create({
        url: rx.url,
        events: ['post.created', 'post.published', 'post.target_published'],
        collection_ids: [col],
      })
    ).secret
    const cli = cliRunner(bin, url, key)
    const pyc: PyClient = { base_url: url, api_key: key }
    const targets = [{ channel_id: channel }]
    const steps: Record<ClientName, () => Promise<string>> = {
      ts: async () => {
        const created = await c.posts.create({ collection: col, ...INPUT, targets })

        await c.posts.publish(created.id)
        await requested(c, created.id)
        await c.posts.reportResult(created.id, channel, { status: 'published', ...RESULT })

        return created.id
      },
      py: () => pySteps('sync'),
      pyasync: () => pySteps('async'),
      cli: async () => {
        const created = await cli([
          'posts',
          'create',
          col,
          '--title',
          INPUT.title,
          '--content',
          INPUT.content,
          '--metadata',
          JSON.stringify(INPUT.metadata),
          '--source',
          INPUT.source,
          '--external-ref',
          INPUT.external_ref,
          '--tag',
          INPUT.tags.join(','),
          '--channel',
          channel,
          '--json',
        ])

        expect(created).toMatchObject({ code: 0 })
        const id = created.json<Post>().id

        expect((await cli(['posts', 'publish', id])).code).toBe(0)
        await requested(c, id)
        const res = await cli([
          'posts',
          'result',
          id,
          channel,
          '--status',
          'published',
          '--external-id',
          RESULT.external_id,
          '--url',
          RESULT.url,
        ])

        expect(res).toMatchObject({ code: 0 })

        return id
      },
      curl: async () => {
        const created = await curl(`${url}/v1/posts`, { key, json: { collection: col, ...INPUT, targets } })

        expect(created.status).toBe(201)
        const id = created.json<Post>().id

        expect((await curl(`${url}/v1/posts/${id}/publish`, { key, method: 'POST' })).status).toBe(202)
        await requested(c, id)
        expect(
          (
            await curl(`${url}/v1/posts/${id}/targets/${channel}/result`, {
              key,
              json: { status: 'published', ...RESULT },
            })
          ).status,
        ).toBe(200)

        return id
      },
      mcp: async () => {
        const { source: _source, ...input } = INPUT // create_post has no source argument
        const created = await mcp<Post>(url, key, 'create_post', { collection: col, ...input, targets })

        await mcp(url, key, 'publish_post', { post_id: created.id })
        await requested(c, created.id)
        await mcp(url, key, 'report_publish_result', {
          post_id: created.id,
          channel_id: channel,
          status: 'published',
          ...RESULT,
        })

        return created.id
      },
    }

    async function pySteps(mode: 'sync' | 'async'): Promise<string> {
      const created = await py.call<Post>(mode, pyc, 'posts.create', [col], { ...INPUT, targets })

      await py.call(mode, pyc, 'posts.publish', [created.id])
      await requested(c, created.id)
      await py.call(mode, pyc, 'posts.report_result', [created.id, channel], { status: 'published', ...RESULT })

      return created.id
    }

    for (const client of CLIENTS) {
      ids[client] = await steps[client]()
    }

    await until(() => rx.received.length >= CLIENTS.length * 3)
  })

  afterAll(() => rx?.close())

  it('persisted posts are identical (apart from IDs and times)', async () => {
    const posts = await Promise.all(CLIENTS.map((cl) => c.posts.get(ids[cl])))
    const ref = normalize(posts[0])

    expect(posts[0]).toMatchObject({
      status: 'published',
      version: 1,
      content: INPUT.content,
      metadata: INPUT.metadata,
      source: 'xc',
      external_ref: 'same-ref',
      tags: ['xc', 'same'],
    })
    expect(posts[0]!.targets[0]).toMatchObject({
      channel_id: channel,
      status: 'published',
      external_url: RESULT.url,
      external_id: RESULT.external_id,
      attempts: 1,
    })
    expect(posts[0]!.published_at).toBeTruthy()

    for (const [i, one] of posts.entries()) {
      expect({ client: CLIENTS[i], post: normalize(one) }).toEqual({ client: CLIENTS[i], post: ref })
    }

    // and they read back the same through every client
    const viaCurl = (await curl(`${url}/v1/posts/${ids.mcp}`, { key })).json()
    const viaPy = await py.call('sync', { base_url: url, api_key: key }, 'posts.get', [ids.cli])
    const viaMcp = await mcp(url, key, 'get_post', { post_id: ids.py })
    const viaCli = (await cliRunner(bin, url, key)(['posts', 'get', ids.ts, '--json'])).json()

    for (const other of [viaCurl, viaPy, viaMcp, viaCli]) {
      expect(normalize(other)).toEqual(ref)
    }
  })

  it('history events are identical; the actor is the same principal; clients differ only by agent name', async () => {
    const histories = await Promise.all(CLIENTS.map((cl) => c.posts.history(ids[cl])))
    const types = histories.map((h) => h.data.map((e) => e.type))

    for (const t of types) {
      expect(t).toEqual(['post.published', 'post.target_published', 'post.publish_requested', 'post.created'])
    }

    const ref = histories[0]!.data.map(strip)

    for (const [i, h] of histories.entries()) {
      expect({ client: CLIENTS[i], events: h.data.map(strip) }).toEqual({ client: CLIENTS[i], events: ref })
    }

    // every client is told apart by its agent name (User-Agent / MCP client), nothing else
    const agents = Object.fromEntries(CLIENTS.map((cl, i) => [cl, histories[i]!.data[3]!.actor.agent]))

    expect(new Set(Object.values(agents)).size).toBeGreaterThanOrEqual(4)
    console.log(`${name} actor.agent per client: ${JSON.stringify(agents)}`)
  })

  it('webhook deliveries are identical, verify in TS and Python, and match the stored events', async () => {
    const byPost = new Map<string, { type: string; data: unknown }[]>()
    const stored = new Map<string, Event>()

    for (const cl of CLIENTS) {
      for (const e of (await c.posts.history(ids[cl])).data) {
        stored.set(e.id, e)
      }
    }

    for (const d of rx.received) {
      const ev = await verifyWebhook(secret, d.body, d.headers)
      const pyEv = await py.call<{ type: string }>('sync', { base_url: url, api_key: key }, '_verify_webhook', [], {
        secret,
        body: d.body,
        headers: d.headers,
      })

      expect(pyEv.type).toBe(ev.type)
      // the delivery carries the stored event: same ID, type, actor and data
      const ref = stored.get(ev.id)!

      expect([ev.type, ev.data, ev.created_at]).toEqual([ref.type, ref.data, ref.created_at])
      expect(ev.actor).toMatchObject({ type: ref.actor.type, id: ref.actor.id })
      expect(d.headers['x-staqly-event-id']).toBe(ev.id)
      const postId = (ev.data as { post: { id: string } }).post.id

      byPost.set(postId, [...(byPost.get(postId) ?? []), { type: ev.type, data: normalize(ev.data) }])
    }

    const per = CLIENTS.map((cl) => byPost.get(ids[cl])!.toSorted((a, b) => a.type.localeCompare(b.type)))

    for (const [i, events] of per.entries()) {
      expect({ client: CLIENTS[i], types: events.map((e) => e.type) }).toEqual({
        client: CLIENTS[i],
        types: ['post.created', 'post.published', 'post.target_published'],
      })
      expect({ client: CLIENTS[i], events }).toEqual({ client: CLIENTS[i], events: per[0] })
    }
  })
})

describe.each(['local', 'cloud'] as const)('%s: workflows across clients', (name) => {
  let url: string
  let key: string
  let c: Staqly
  let cli: ReturnType<typeof cliRunner>
  let pyc: PyClient

  beforeAll(() => {
    url = name === 'local' ? local.url : cloud.url
    key = name === 'local' ? local.token : ws.agentKey
    c = sdk(url, key || null)
    cli = cliRunner(bin, url, key)
    pyc = { base_url: url, api_key: key }
  })

  it('CLI connects a channel → Python writes → curl schedules (stale version 409) → TS waits → MCP reports a failure → CLI retries → Python reports success', async () => {
    const colName = `Handoff ${name} ${Date.now()}`

    expect((await cli(['collections', 'create', colName])).code).toBe(0)
    const connected = await cli([
      'channels',
      'connect',
      'custom',
      '--name',
      `Handoff pipeline ${name} ${Date.now()}`,
      '--json',
    ])

    expect(connected).toMatchObject({ code: 0 })
    const channel = connected.json<{ id: string; name: string }>()
    const post = await py.call<Post>('sync', pyc, 'posts.create', [colName], {
      title: 'Post',
      content: 'Launch post\nv1',
      targets: [{ channel_id: channel.id }],
    })
    // A schedule based on an old version is refused; the right one makes it scheduled.
    const stale = await curl(`${url}/v1/posts/${post.id}`, {
      method: 'PATCH',
      key,
      json: { content: 'edited', expected_version: 7 },
    })

    expect(stale.status).toBe(409)
    const at = new Date(Date.now() + 3600_000).toISOString()
    const scheduled = await curl(`${url}/v1/posts/${post.id}/schedule`, { key, json: { publish_at: at } })

    expect(scheduled.status).toBe(200)
    expect(scheduled.json<Post>().status).toBe('scheduled')
    // Publish now instead of waiting an hour: the TS SDK waits while the others act.
    await mcp(url, key, 'publish_post', { post_id: post.id })
    await requested(c, post.id)
    const waiting = c.posts.waitForPublished(post.id, { timeout: 20_000, pollInterval: 300 })

    await mcp(url, key, 'report_publish_result', {
      post_id: post.id,
      channel_id: channel.id,
      status: 'failed',
      error: 'mock publisher down',
    })
    const failed = await waiting

    expect(failed.status).toBe('failed')
    expect(failed.targets[0]).toMatchObject({ status: 'failed', error: 'mock publisher down' })
    const events = (await c.events.list({ post_id: post.id })).data.toReversed()

    expect(events.map((e) => e.type)).toEqual([
      'post.created',
      'post.scheduled',
      'post.publish_requested',
      'post.target_failed',
      'post.failed',
    ])
    // the same principal everywhere; only the agent name tells the clients apart
    expect({ ...events[1]!.actor, agent: undefined }).toEqual({ ...events[0]!.actor, agent: undefined })
    // The CLI retries the channel; Python reports success this time.
    const retried = await cli(['posts', 'retry', post.id, channel.name, '--json'])

    expect(retried).toMatchObject({ code: 0 })
    expect(retried.json<Post>().status).toBe('publishing')
    await requested(c, post.id)
    const done = await py.call<Post>('async', pyc, 'posts.report_result', [post.id, channel.id], {
      status: 'published',
      url: 'https://x/1',
    })

    expect(done.status).toBe('published')
    const listed = await cli(['posts', 'list', '--collection', colName, '--status', 'published', '--json'])

    expect(listed.code).toBe(0)
    expect(listed.json<{ data: Post[] }>().data.map((p) => p.id)).toEqual([post.id])
    expect((await c.workspace.usage()).usage.posts_per_month).toBeGreaterThanOrEqual(1)
  })

  it('TS duplicates a published post → MCP edits the copy → curl tags it → CLI publishes with --wait while Python reports', async () => {
    const col = (await c.collections.create({ name: `Rewrite ${name} ${Date.now()}` })).id
    const channel = await c.channels.connect({ provider: 'custom', name: `Rewrite channel ${name} ${Date.now()}` })
    const original = await c.posts.create({
      title: 'Post',
      collection: col,
      content: 'Draft\nv1',
      targets: [{ channel_id: channel.id }],
    })

    await c.posts.publish(original.id)
    await requested(c, original.id)
    await c.posts.reportResult(original.id, channel.id, { status: 'published', url: 'https://x/orig' })
    // A published post cannot be edited; its copy can.
    expect(await c.posts.update(original.id, { content: 'x' }).catch((e: Error) => e)).toBeInstanceOf(ConflictError)
    const copy = await c.posts.duplicate(original.id)

    expect([copy.status, copy.content, copy.targets[0]!.channel_id]).toEqual(['draft', 'Draft\nv1', channel.id])
    const rewritten = await mcp<Post>(url, key, 'update_post', {
      post_id: copy.id,
      content: 'Draft\nv2 with a call to action',
      expected_version: copy.version,
    })

    expect(rewritten.content).toBe('Draft\nv2 with a call to action')
    const tagged = await curl(`${url}/v1/posts/${copy.id}`, {
      method: 'PATCH',
      key,
      json: { tags: ['rewritten'], expected_version: rewritten.version },
    })

    expect(tagged.status).toBe(200)
    // The CLI publishes and waits (exit 0 published); Python plays the automation in the meantime.
    setTimeout(async () => {
      await requested(c, copy.id)
      await py.call('sync', pyc, 'posts.report_result', [copy.id, channel.id], {
        status: 'published',
        url: 'https://x/copy',
      })
    }, 500)
    const published = await cli(['posts', 'publish', copy.id, '--wait', '--timeout', '30s', '--json'])

    expect(published).toMatchObject({ code: 0 })
    expect(published.json<Post>()).toMatchObject({ status: 'published', tags: ['rewritten'] })
    const types = (await c.posts.history(copy.id)).data.map((e) => e.type).toReversed()

    expect(types).toEqual([
      'post.created',
      'post.updated',
      'post.updated',
      'post.publish_requested',
      'post.target_published',
      'post.published',
    ])
  })
})

type Outcome = { client: string; ok: boolean; status?: string }

const settle = async (client: string, p: Promise<unknown>): Promise<Outcome> => {
  try {
    const r = (await p) as { status?: number | string; json?: () => Post }

    if (typeof r?.status === 'number') {
      return { client, ok: r.status < 300, status: r.status < 300 ? r.json!().status : String(r.status) }
    }

    return { client, ok: true, status: (r as Post).status }
  } catch (err) {
    const conflict =
      err instanceof ConflictError ||
      (err instanceof PyCallError && err.error.status === 409) ||
      (err instanceof MCPToolError && /conflict|cannot|version|being published|modified/i.test(err.text))

    if (!conflict) {
      throw err
    }

    return { client, ok: false }
  }
}

describe.each(['local', 'cloud'] as const)('%s: clients racing on the same post', (name) => {
  let url: string
  let key: string
  let c: Staqly
  let col: string
  let channel: string
  let pyc: PyClient

  beforeAll(async () => {
    url = name === 'local' ? local.url : cloud.url
    key = name === 'local' ? local.token : ws.agentKey
    c = sdk(url, key || null)
    pyc = { base_url: url, api_key: key }
    col = (await c.collections.create({ name: `race ${Date.now()}` })).id
    channel = (await c.channels.connect({ provider: 'custom', name: `race channel ${Date.now()}` })).id
  })

  it('twelve publishes at once (TS, Python, curl, MCP): exactly one starts the run', async () => {
    for (let round = 0; round < 3; round++) {
      const post = await c.posts.create({
        title: 'Post',
        collection: col,
        content: `race ${round}`,
        targets: [{ channel_id: channel }],
      })
      const outcomes = await Promise.all([
        settle('ts-1', c.posts.publish(post.id)),
        settle('ts-2', sdk(url, key || null).posts.publish(post.id)),
        settle('ts-3', c.posts.publish(post.id)),
        settle('py-sync', py.call('sync', pyc, 'posts.publish', [post.id])),
        settle('py-async', py.call('async', pyc, 'posts.publish', [post.id])),
        settle('py-async-2', py.call('async', pyc, 'posts.publish', [post.id])),
        settle('curl-1', curl(`${url}/v1/posts/${post.id}/publish`, { key, method: 'POST' })),
        settle('curl-2', curl(`${url}/v1/posts/${post.id}/publish`, { key, method: 'POST' })),
        settle('curl-3', curl(`${url}/v1/posts/${post.id}/publish`, { key, method: 'POST' })),
        settle('mcp-1', mcp(url, key, 'publish_post', { post_id: post.id })),
        settle('mcp-2', mcp(url, key, 'publish_post', { post_id: post.id })),
        settle('mcp-3', mcp(url, key, 'publish_post', { post_id: post.id })),
      ])
      const winners = outcomes.filter((o) => o.ok)

      expect({ winners: winners.length, outcomes }).toMatchObject({ winners: 1 })
      await requested(c, post.id)
      const requests = (await c.posts.history(post.id)).data.filter((e) => e.type === 'post.publish_requested')

      expect(requests).toHaveLength(1)
      expect(
        (await c.jobs.list({ kind: 'publish_target' })).data.filter((j) => j.payload?.post_id === post.id),
      ).toHaveLength(1)
    }
  })

  it('concurrent metadata patches from every client: no lost update; one winner among equal expected_versions', async () => {
    const post = await c.posts.create({ title: 'Post', collection: col, content: 'patch me', metadata: { base: true } })
    const patches = [
      c.posts.update(post.id, { metadata: { base: true, ts: 1 } }),
      py.call('sync', pyc, 'posts.update', [post.id], { metadata: { base: true, py: 1 } }),
      py.call('async', pyc, 'posts.update', [post.id], { metadata: { base: true, pyasync: 1 } }),
      curl(`${url}/v1/posts/${post.id}`, { method: 'PATCH', key, json: { metadata: { base: true, curl: 1 } } }),
      mcp(url, key, 'update_post', { post_id: post.id, metadata: { base: true, mcp: 1 } }),
    ]

    await Promise.all(patches)
    const after = await c.posts.get(post.id)

    expect(after.version).toBe(1 + patches.length)
    expect(after.metadata).toMatchObject({ base: true })

    const guarded = await Promise.all([
      settle('ts', c.posts.update(post.id, { content: 'ts', expected_version: after.version })),
      settle('py', py.call('sync', pyc, 'posts.update', [post.id], { content: 'py', expected_version: after.version })),
      settle(
        'pyasync',
        py.call('async', pyc, 'posts.update', [post.id], { content: 'pyasync', expected_version: after.version }),
      ),
      settle(
        'curl',
        curl(`${url}/v1/posts/${post.id}`, {
          method: 'PATCH',
          key,
          json: { content: 'curl', expected_version: after.version },
        }),
      ),
      settle(
        'mcp',
        mcp(url, key, 'update_post', { post_id: post.id, content: 'mcp', expected_version: after.version }),
      ),
    ])
    const winners = guarded.filter((o) => o.ok)

    expect({ winners: winners.length, guarded }).toMatchObject({ winners: 1 })
    expect((await c.posts.get(post.id)).content).toBe(winners[0]!.client)
  })

  it('one Idempotency-Key repeated at once from every client makes one post', async () => {
    const k = `race-${name}-${Date.now()}`
    const body = { title: 'Post', collection: col, content: 'only one of me' }
    const results = await Promise.allSettled([
      c.posts.create(body, { idempotencyKey: k }),
      py.call<Post>('sync', pyc, 'posts.create', [col], {
        title: 'Post',
        content: body.content,
        request_options: { idempotency_key: k },
      }),
      py.call<Post>('async', pyc, 'posts.create', [col], {
        title: 'Post',
        content: body.content,
        request_options: { idempotency_key: k },
      }),
      curl(`${url}/v1/posts`, { key, json: body, headers: { 'Idempotency-Key': k } }).then((r) => {
        if (r.status === 409) {
          throw new ConflictError(409, { code: 'conflict', message: 'in progress' }, new Headers(), null)
        }

        return r.json<Post>()
      }),
    ])
    const ok = results.filter((r): r is PromiseFulfilledResult<Post> => r.status === 'fulfilled').map((r) => r.value.id)

    // Concurrent duplicates: one runs, the others replay it or are told it is in progress (409).
    expect(ok.length).toBeGreaterThanOrEqual(1)
    expect(new Set(ok).size).toBe(1)
    expect((await c.posts.list({ collection_id: col, q: 'only one of me' })).data).toHaveLength(1)
  })
})
