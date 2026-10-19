// SDK behaviour against real servers, TypeScript and Python (sync + async)
// side by side: typed errors, retries with Retry-After, idempotency keys and
// replays, pagination, uploads and downloads, waiting for a post to be
// published (released by a result report, timing out, aborted), webhook
// signatures, timeouts and a server that dies mid-request.
import { createHash, randomBytes } from 'node:crypto'
import { createServer, type Socket } from 'node:net'

import {
  APIConnectionError,
  APIUserAbortError,
  AuthenticationError,
  ConflictError,
  type FetchLike,
  NotFoundError,
  PayloadTooLargeError,
  PermissionDeniedError,
  PlanLimitError,
  type Post,
  RateLimitError,
  Staqly,
  TimeoutError,
  UnsupportedMediaTypeError,
  ValidationError,
  verifyWebhook,
  WaitTimeoutError,
  WebhookVerificationError,
} from '@staqly/sdk'
import { afterAll, beforeAll, describe, expect, inject, it } from 'vitest'

import {
  b64,
  type CloudWorkspace,
  curl,
  operator,
  type PyClient,
  PythonSDK,
  receiver,
  sdk,
  signup,
  sleep,
  startServer,
  until,
} from './lib.js'

const local = inject('local')
const cloud = inject('cloud')
const limited = inject('limited')
const bin = inject('bin')
const PNG = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==',
  'base64',
)
const MODES = ['sync', 'async'] as const

let py: PythonSDK
let ws: CloudWorkspace

interface Target {
  name: 'local' | 'cloud'
  url: string
  key: string
  admin: string
}

const targets = (): Target[] => [
  { name: 'local', url: local.url, key: local.token, admin: local.token },
  { name: 'cloud', url: cloud.url, key: ws.agentKey, admin: ws.adminKey },
]
const pyc = (t: { url: string; key: string }, extra: Partial<PyClient> = {}): PyClient => ({
  base_url: t.url,
  api_key: t.key,
  ...extra,
})

async function errorOf(p: Promise<unknown>): Promise<Error & { status?: number; code?: string }> {
  try {
    await p
  } catch (err) {
    return err as Error & { status?: number; code?: string }
  }

  throw new Error('expected the call to fail')
}

/** Waits until the post's targets were handed to the automation. */
async function requested(c: Staqly, id: string): Promise<Post> {
  return until(async () => {
    const p = await c.posts.get(id)

    return p.targets.every((t) => t.status === 'publishing' && t.attempts > 0) ? p : undefined
  })
}

beforeAll(async () => {
  py = new PythonSDK()
  await py.start()
  ws = await signup(cloud.url)
})

afterAll(() => py?.stop())

describe.each(['local', 'cloud'] as const)('%s: typed errors are the same in every SDK', (name) => {
  let t: Target
  let c: Staqly
  let col: string
  let channel: string

  beforeAll(async () => {
    t = targets().find((x) => x.name === name)!
    c = sdk(t.url, t.key || null)
    col = (await c.collections.create({ name: `errors ${Date.now()}` })).id
    channel = (await c.channels.connect({ provider: 'custom', name: `errors channel ${Date.now()}` })).id
  })

  it('404 NotFoundError', async () => {
    const ts = await errorOf(c.posts.get('post_doesnotexist0000000'))

    expect(ts).toBeInstanceOf(NotFoundError)

    for (const mode of MODES) {
      const e = await py.fail(mode, pyc(t), 'posts.get', ['post_doesnotexist0000000'])

      expect([e.type, e.status, e.code]).toEqual(['NotFoundError', 404, ts.code])
    }
  })

  it('409 ConflictError: publishing twice, cancelling a draft and a stale expected_version', async () => {
    const post = await c.posts.create({ collection: col, content: 'publish twice', targets: [{ channel_id: channel }] })

    await c.posts.publish(post.id)
    const ts = await errorOf(c.posts.publish(post.id))

    expect(ts).toBeInstanceOf(ConflictError)
    const fresh = await c.posts.create({ collection: col, content: 'stale' })
    const stale = await errorOf(c.posts.update(fresh.id, { content: 'x', expected_version: 7 }))

    expect(stale).toBeInstanceOf(ConflictError)
    expect(await errorOf(c.posts.cancel(fresh.id))).toBeInstanceOf(ConflictError)

    for (const mode of MODES) {
      const e = await py.fail(mode, pyc(t), 'posts.publish', [post.id])

      expect([e.type, e.status, e.code]).toEqual(['ConflictError', 409, ts.code])
      const s = await py.fail(mode, pyc(t), 'posts.update', [fresh.id], { content: 'x', expected_version: 7 })

      expect([s.type, s.status, s.code]).toEqual(['ConflictError', 409, stale.code])
    }

    expect((await c.posts.get(fresh.id)).content).toBe('stale') // nothing applied
  })

  it('422 ValidationError: a time in the past, a scheduled post without a channel, an unknown status', async () => {
    const ts = await errorOf(
      c.posts.create({
        collection: col,
        content: 'x',
        targets: [{ channel_id: channel }],
        publish_at: '2020-01-01T00:00:00Z',
        status: 'scheduled',
      }),
    )

    expect(ts).toBeInstanceOf(ValidationError)
    expect(
      await errorOf(
        c.posts.create({
          collection: col,
          content: 'x',
          publish_at: new Date(Date.now() + 3600_000).toISOString(),
          status: 'scheduled',
        }),
      ),
    ).toBeInstanceOf(ValidationError)
    expect(await errorOf(c.posts.list({ status: 'approved' as 'draft' }))).toBeInstanceOf(ValidationError)

    for (const mode of MODES) {
      const e = await py.fail(mode, pyc(t), 'posts.create', [col], {
        content: 'x',
        targets: [{ channel_id: channel }],
        publish_at: '2020-01-01T00:00:00Z',
        status: 'scheduled',
      })

      expect([e.type, e.status, e.code]).toEqual(['ValidationError', 422, ts.code])
    }
  })

  it('415 UnsupportedMediaTypeError for HTML and executables', async () => {
    const html = Buffer.from('<html><script>alert(1)</script></html>')
    const ts = await errorOf(c.media.upload(html, { filename: 'x.html', contentType: 'text/html' }))

    expect(ts).toBeInstanceOf(UnsupportedMediaTypeError)

    for (const mode of MODES) {
      const e = await py.fail(mode, pyc(t), 'media.upload', [b64(html)], { filename: 'x.html' })

      expect([e.type, e.status]).toEqual(['UnsupportedMediaTypeError', 415])
    }
  })
})

describe('local: every SDK needs the access token', () => {
  it('401 AuthenticationError without it or with a wrong one, and it works with it', async () => {
    const wrong = `${local.token}x`

    expect(await errorOf(sdk(local.url, null).posts.list())).toBeInstanceOf(AuthenticationError)
    expect(await errorOf(sdk(local.url, wrong).posts.list())).toBeInstanceOf(AuthenticationError)
    expect((await sdk(local.url, local.token).posts.list()).data).toBeDefined()

    for (const mode of MODES) {
      expect((await py.fail(mode, pyc({ url: local.url, key: '' }), 'posts.list')).type).toBe('AuthenticationError')
      expect((await py.fail(mode, pyc({ url: local.url, key: wrong }), 'posts.list')).type).toBe('AuthenticationError')
    }
  })
})

describe('cloud: auth, scopes and plan limits', () => {
  it('401 AuthenticationError for a wrong key, in every SDK', async () => {
    const bad = { url: cloud.url, key: 'stk_wrongwrongwrongwrongwrong' }

    expect(await errorOf(sdk(cloud.url, bad.key).posts.list())).toBeInstanceOf(AuthenticationError)
    expect(await errorOf(sdk(cloud.url, null).posts.list())).toBeInstanceOf(AuthenticationError)

    for (const mode of MODES) {
      expect((await py.fail(mode, pyc(bad), 'posts.list')).type).toBe('AuthenticationError')
      expect((await py.fail(mode, pyc({ url: cloud.url, key: '' }), 'posts.list')).type).toBe('AuthenticationError')
    }
  })

  it('403 PermissionDeniedError for a missing scope, in every SDK: a writer may not publish', async () => {
    const admin = sdk(cloud.url, ws.adminKey)
    const writer = await admin.apiKeys.create({
      name: 'writer',
      scopes: ['posts:read', 'posts:write', 'collections:read', 'channels:read'],
    })
    const w = sdk(cloud.url, writer.key!)
    const col = await admin.collections.create({ name: `scopes ${Date.now()}` })
    const channel = await admin.channels.connect({ provider: 'custom', name: `scopes channel ${Date.now()}` })
    const post = await w.posts.create({
      collection: col.id,
      content: 'written, not published',
      targets: [{ channel_id: channel.id }],
    })
    const ts = await errorOf(w.posts.publish(post.id))

    expect(ts).toBeInstanceOf(PermissionDeniedError)
    expect(await errorOf(w.channels.connect({ provider: 'custom' }))).toBeInstanceOf(PermissionDeniedError)

    for (const mode of MODES) {
      const e = await py.fail(mode, pyc({ url: cloud.url, key: writer.key! }), 'posts.publish', [post.id])

      expect([e.type, e.status, e.code]).toEqual(['PermissionDeniedError', 403, ts.code])
    }

    expect((await admin.posts.get(post.id)).status).toBe('draft')
    await admin.apiKeys.revoke(writer.id)
  })

  it('402 PlanLimitError on the Starter trial (channels and webhooks) and its upload size limit', async () => {
    const free = await signup(cloud.url)
    const admin = sdk(cloud.url, free.adminKey)
    // The test server starts workspaces on Pro: this one goes on a week of Starter, like a new trial.
    const week = new Date(Date.now() + 7 * 864e5).toISOString().slice(0, 10)

    expect(await operator(bin, cloud, ['set-plan', free.workspaceId, 'starter', '--until', week])).toMatchObject({
      code: 0,
    })
    expect((await admin.workspace.usage()).plan.id).toBe('starter') // the trial
    const c = sdk(cloud.url, free.agentKey)
    const limits = (await c.workspace.usage()).plan.limits

    for (let i = 0; i < limits.channels; i++) {
      await c.channels.connect({ provider: 'custom', name: `free ${i}` })
    }

    const ch = (await errorOf(c.channels.connect({ provider: 'custom', name: 'one too many' }))) as PlanLimitError

    expect(ch).toBeInstanceOf(PlanLimitError)
    expect([ch.status, ch.limit, ch.max, ch.plan]).toEqual([402, 'channels', limits.channels, 'starter'])

    for (let i = 0; i < limits.webhooks; i++) {
      await c.webhooks.create({ url: 'https://example.com/hook' })
    }

    const ts = (await errorOf(c.webhooks.create({ url: 'https://example.com/hook' }))) as PlanLimitError

    expect([ts.status, ts.limit, ts.max, ts.plan]).toEqual([402, 'webhooks', limits.webhooks, 'starter'])

    for (const mode of MODES) {
      const e = await py.fail(mode, pyc({ url: cloud.url, key: free.agentKey }), 'webhooks.create', [
        'https://example.com/hook',
      ])

      expect([e.type, e.status, e.limit, e.max, e.plan]).toEqual([
        'PlanLimitError',
        402,
        'webhooks',
        limits.webhooks,
        'starter',
      ])
      const pch = await py.fail(mode, pyc({ url: cloud.url, key: free.agentKey }), 'channels.connect', ['custom'], {
        name: 'x',
      })

      expect([pch.type, pch.limit]).toEqual(['PlanLimitError', 'channels'])
    }

    // 26 MB is over Starter's 25 MB per file
    const big = Buffer.concat([Buffer.from('RIFF\x24\x00\x00\x00WAVEfmt ', 'latin1'), randomBytes(26 << 20)])
    const up = await errorOf(c.media.upload(big, { filename: 'big.wav' }))

    expect([PlanLimitError, PayloadTooLargeError].some((k) => up instanceof k)).toBe(true)

    for (const mode of MODES) {
      const e = await py.fail(mode, pyc({ url: cloud.url, key: free.agentKey }), 'media.upload', [b64(big)], {
        filename: 'big.wav',
      })

      expect(['PlanLimitError', 'PayloadTooLargeError']).toContain(e.type)
    }
  })
})

describe('429 and Retry-After (limited server: burst 3, then 1 request/s per key)', () => {
  let key: string

  beforeAll(async () => {
    key = (await signup(limited.url)).agentKey
    await sleep(4000) // the signup used the burst
  })

  it('TypeScript: RateLimitError without retries; waits Retry-After and succeeds with them', async () => {
    const noRetry = sdk(limited.url, key, { maxRetries: 0 })
    let err: unknown

    for (let i = 0; i < 10 && !err; i++) {
      err = await noRetry.posts.list().then(
        () => undefined,
        (e: unknown) => e,
      )
    }

    expect(err).toBeInstanceOf(RateLimitError)
    expect((err as RateLimitError).retryAfter).toBeGreaterThanOrEqual(1)
    const started = Date.now()
    const c = sdk(limited.url, key, { maxRetries: 3 })

    for (let i = 0; i < 4; i++) {
      await c.posts.list() // each one may wait for Retry-After first
    }

    expect(Date.now() - started).toBeGreaterThanOrEqual(2000)
  })

  it.each(MODES)('Python %s: RateLimitError.retry_after, then retried to success', async (mode) => {
    await sleep(4000)
    let err: Awaited<ReturnType<PythonSDK['fail']>> | undefined

    for (let i = 0; i < 10 && !err; i++) {
      err = await py
        .call(mode, pyc({ url: limited.url, key }, { max_retries: 0 }), 'posts.list')
        .then(() => undefined)
        .catch((e: { error: Awaited<ReturnType<PythonSDK['fail']>> }) => e.error)
    }

    expect(err?.type).toBe('RateLimitError')
    expect(err?.retry_after).toBeGreaterThanOrEqual(1)
    const started = Date.now()

    for (let i = 0; i < 4; i++) {
      await py.call(mode, pyc({ url: limited.url, key }, { max_retries: 3 }), 'posts.list')
    }

    expect(Date.now() - started).toBeGreaterThanOrEqual(2000)
  })

  it('a POST retried after 429 is applied exactly once (Idempotency-Key)', async () => {
    await sleep(4000)
    const c = sdk(limited.url, key, { maxRetries: 5 })
    const col = await c.collections.create({ name: `rl ${Date.now()}` })
    const ref = `rl-${Date.now()}`

    await Promise.all(
      Array.from({ length: 6 }, (_, i) =>
        c.posts.create({ collection: col.id, content: `burst ${i}`, external_ref: `${ref}-${i}` }),
      ),
    )
    await sleep(3000)
    const all = await c.posts.list({ collection_id: col.id, limit: 50 })

    expect(all.data).toHaveLength(6)
  })
})

describe.each(['local', 'cloud'] as const)('%s: idempotency keys', (name) => {
  let t: Target
  let col: string

  beforeAll(async () => {
    t = targets().find((x) => x.name === name)!
    col = (await sdk(t.url, t.key || null).collections.create({ name: `idem ${Date.now()}` })).id
  })

  it('TypeScript sends one key per unsafe JSON request, none on GET and multipart', async () => {
    const seen: { method: string; key: string | null }[] = []
    const record: FetchLike = (input, init) => {
      seen.push({ method: init?.method ?? 'GET', key: new Headers(init?.headers).get('idempotency-key') })

      return fetch(input, init)
    }
    const c = new Staqly({ baseUrl: t.url, apiKey: t.key || null, fetch: record })

    await c.posts.create({ collection: col, content: 'key' })
    await c.posts.list({ collection_id: col })
    await c.media.upload(PNG, { filename: 'a.png' })
    expect(seen[0]!.key).toMatch(/^sdk-[0-9a-f-]{36}$/)
    expect(seen[1]!.key).toBeNull()
    expect(seen[2]!.key).toBeNull()
  })

  it.each(MODES)('Python %s sends one key per unsafe JSON request, none on GET and multipart', async (mode) => {
    const client = pyc(t, { record: true, record_key: `${name}-${mode}` })

    await py.call(mode, client, 'posts.create', [col], { content: 'key' })
    await py.call(mode, client, 'posts.list', [], { collection_id: col })
    await py.call(mode, client, 'media.upload', [b64(PNG)], { filename: 'a.png' })
    const seen = await py.call<{ method: string; headers: Record<string, string> }[]>(mode, client, '_requests', [], {
      key: `${name}-${mode}`,
    })

    expect(seen[0]!.headers['idempotency-key']).toMatch(/^sdk-[0-9a-f-]{36}$/)
    expect(seen[1]!.headers['idempotency-key']).toBeUndefined()
    expect(seen[2]!.headers['idempotency-key']).toBeUndefined()
  })

  it('a create whose response was lost is retried and applied once: TypeScript', async () => {
    let dropped = false
    const lossy: FetchLike = async (input, init) => {
      const res = await fetch(input, init)

      if (!dropped && init?.method === 'POST') {
        dropped = true
        await res.arrayBuffer()

        throw new TypeError('fetch failed: socket hang up')
      }

      return res
    }
    const c = new Staqly({ baseUrl: t.url, apiKey: t.key || null, fetch: lossy })
    const ref = `lost-ts-${Date.now()}`
    const post = await c.posts.create({ collection: col, content: 'once', external_ref: ref })

    expect(dropped).toBe(true)
    const found = await sdk(t.url, t.key || null).posts.list({ collection_id: col, q: 'once' })

    expect(found.data.filter((p) => p.external_ref === ref).map((p) => p.id)).toEqual([post.id])
  })

  it.each(MODES)('a create whose response was lost is retried and applied once: Python %s', async (mode) => {
    const ref = `lost-py-${mode}-${Date.now()}`
    const post = await py.call<Post>(mode, pyc(t, { drop_first_response: true }), 'posts.create', [col], {
      content: 'once more',
      external_ref: ref,
    })
    const found = await sdk(t.url, t.key || null).posts.list({ collection_id: col, q: 'once more' })

    expect(found.data.filter((p) => p.external_ref === ref).map((p) => p.id)).toEqual([post.id])
  })

  it('without retries and without a key, a lost response is not replayed (TypeScript)', async () => {
    const seen: (string | null)[] = []
    const record: FetchLike = (input, init) => {
      seen.push(new Headers(init?.headers).get('idempotency-key'))

      return fetch(input, init)
    }
    const c = new Staqly({ baseUrl: t.url, apiKey: t.key || null, fetch: record, maxRetries: 0 })

    await c.posts.create({ collection: col, content: 'no key' })
    expect(seen).toEqual([null])
  })

  it('an explicit key replays across clients with the same principal (TS, curl, Python)', async () => {
    const key = `xc-${name}-${Date.now()}`
    const body = { collection: col, content: 'replayed, same body' }
    const c = sdk(t.url, t.key || null)
    const first = await c.posts.create(body, { idempotencyKey: key })
    const again = await c.posts.create(body, { idempotencyKey: key })

    expect(again.id).toBe(first.id)
    // curl with byte-identical JSON gets the stored response back
    const replay = await curl(`${t.url}/v1/posts`, { key: t.key, json: body, headers: { 'Idempotency-Key': key } })

    expect(replay.status).toBe(201)
    expect(replay.headers['idempotent-replayed']).toBe('true')
    expect(replay.json().id).toBe(first.id)

    // Python formats JSON differently (spaces after separators); the hash is over canonical JSON, so the
    // same body with the same key replays there too
    for (const mode of MODES) {
      const py1 = (await py.call(mode, pyc(t), 'posts.create', [col], {
        content: body.content,
        request_options: { idempotency_key: key },
      })) as { id: string }

      expect(py1.id).toBe(first.id)
    }

    // another body with the same key is refused, not applied
    const ts = await errorOf(c.posts.create({ collection: col, content: 'other' }, { idempotencyKey: key }))

    expect(ts).toBeInstanceOf(ValidationError)
    expect(ts.code).toBe('idempotency_mismatch')

    for (const mode of MODES) {
      const e = await py.fail(mode, pyc(t), 'posts.create', [col], {
        content: 'other',
        request_options: { idempotency_key: key },
      })

      expect([e.type, e.code]).toEqual(['ValidationError', 'idempotency_mismatch'])
    }
  })
})

describe.each(['local', 'cloud'] as const)('%s: pagination, uploads, downloads', (name) => {
  let t: Target
  let c: Staqly
  let col: string
  const made: string[] = []

  beforeAll(async () => {
    t = targets().find((x) => x.name === name)!
    c = sdk(t.url, t.key || null)
    col = (await c.collections.create({ name: `pages ${Date.now()}` })).id

    for (let i = 0; i < 7; i++) {
      made.push((await c.posts.create({ collection: col, content: `page post ${i}`, tags: ['pages'] })).id)
    }
  })

  it('iterators walk more than one page and agree (TS, Python sync and async)', async () => {
    const ts: string[] = []

    for await (const post of c.posts.iterate({ collection_id: col, tag: 'pages', limit: 2, sort: 'created_asc' })) {
      ts.push(post.id)
    }

    expect(ts).toEqual(made)

    for (const mode of MODES) {
      const posts = await py.call<Post[]>(mode, pyc(t), 'posts.iterate', [], {
        collection_id: col,
        tag: 'pages',
        limit: 2,
        sort: 'created_asc',
      })

      expect(posts.map((p) => p.id)).toEqual(ts) // same order too
    }

    const evTs: string[] = []

    for await (const e of c.events.iterate({ collection_id: col, type: 'post.created', limit: 3 })) {
      evTs.push(e.id)
    }

    expect(evTs).toHaveLength(7)

    for (const mode of MODES) {
      const evs = await py.call<{ id: string }[]>(mode, pyc(t), 'events.iterate', [], {
        collection_id: col,
        type: 'post.created',
        limit: 3,
      })

      expect(evs.map((e) => e.id)).toEqual(evTs)
    }
  })

  it('a large upload round-trips byte for byte through both SDKs, and a post carries it', async () => {
    const big = Buffer.concat([
      Buffer.from('RIFF\x24\x00\x00\x00WAVEfmt ', 'latin1'),
      randomBytes(name === 'local' ? 12 << 20 : 4 << 20),
    ])
    const sha = createHash('sha256').update(big).digest('hex')
    const media = await c.media.upload(big, { filename: 'big.wav' })

    expect([media.mime_type, media.size_bytes, media.sha256]).toEqual(['audio/wave', big.length, sha])

    for (const mode of MODES) {
      const m = await py.call<{ id: string; sha256: string; mime_type: string }>(
        mode,
        pyc(t),
        'media.upload',
        [b64(big)],
        { filename: 'big.wav' },
      )

      expect([m.mime_type, m.sha256]).toEqual(['audio/wave', sha])
      const back = await py.call<{ $bytes: string }>(mode, pyc(t), 'media.download', [media.id])

      expect(createHash('sha256').update(Buffer.from(back.$bytes, 'base64')).digest('hex')).toBe(sha)
    }

    expect(Buffer.from(await c.media.download(media.id)).equals(big)).toBe(true)
    const post = await c.posts.create({ collection: col, content: 'with a recording', media: [media.id] })

    expect(post.media_items.map((m) => m.id)).toEqual([media.id])
  })

  it('the stored type comes from the bytes, not the declared Content-Type', async () => {
    const ts = await c.media.upload(PNG, { filename: 'not-really.txt', contentType: 'text/plain' })

    expect(ts.mime_type).toBe('image/png')

    for (const mode of MODES) {
      const m = await py.call<{ mime_type: string }>(mode, pyc(t), 'media.upload', [b64(PNG)], {
        filename: 'not-really.txt',
        content_type: 'text/plain',
      })

      expect(m.mime_type).toBe('image/png')
    }
  })

  it('event exports stream the same events in TS and Python', async () => {
    const ts = []

    for await (const e of c.exports.events({ collection_id: col, type: 'post.created' })) {
      ts.push(e.id)
    }

    expect(ts.length).toBeGreaterThanOrEqual(7)

    for (const mode of MODES) {
      const evs = await py.call<{ id: string }[]>(mode, pyc(t), 'exports.events', [], {
        collection_id: col,
        type: 'post.created',
      })

      expect(evs.map((e) => e.id)).toEqual(ts)
    }

    const csv = await (await c.exports.eventsResponse({ format: 'csv', collection_id: col })).text()

    expect(csv.split('\n')[0]).toContain('created_at,id,type')
  })
})

describe.each(['local', 'cloud'] as const)('%s: waiting for a post to be published', (name) => {
  let t: Target
  let c: Staqly
  let col: string
  let channel: string

  beforeAll(async () => {
    t = targets().find((x) => x.name === name)!
    c = sdk(t.url, t.key || null)
    col = (await c.collections.create({ name: `wait ${Date.now()}` })).id
    channel = (await c.channels.connect({ provider: 'custom', name: `wait channel ${Date.now()}` })).id
  })

  const publishing = async () => {
    const post = await c.posts.create({ collection: col, content: 'wait for me', targets: [{ channel_id: channel }] })

    await c.posts.publish(post.id)

    return requested(c, post.id)
  }

  it('returns promptly when another client (curl) reports the result', async () => {
    const post = await publishing()

    setTimeout(() => {
      void curl(`${t.url}/v1/posts/${post.id}/targets/${channel}/result`, {
        key: t.key,
        json: { status: 'published', url: 'https://example.com/via-curl' },
      })
    }, 800)
    const started = Date.now()
    const done = await c.posts.waitForPublished(post.id, { timeout: 20_000, pollInterval: 200 })

    expect([done.status, done.targets[0]!.external_url]).toEqual(['published', 'https://example.com/via-curl'])
    expect(Date.now() - started).toBeLessThan(5000)
    // A failure settles the wait too: the status says what happened.
    const failing = await publishing()

    setTimeout(() => {
      void curl(`${t.url}/v1/posts/${failing.id}/targets/${channel}/result`, {
        key: t.key,
        json: { status: 'failed', error: 'no' },
      })
    }, 500)

    for (const mode of MODES) {
      const r = await py.call<{ ok: boolean; result: Post }>(mode, pyc(t), '_wait_timed', [failing.id], {
        timeout: 20,
        poll_interval: 0.2,
      })

      expect([r.ok, r.result.status, r.result.targets[0]!.error]).toEqual([true, 'failed', 'no'])
    }
  })

  it('times out with WaitTimeoutError (TS, Python sync and async)', async () => {
    const post = await publishing()
    const started = Date.now()
    const err = await errorOf(c.posts.waitForPublished(post.id, { timeout: 2000, pollInterval: 200 }))

    expect(err).toBeInstanceOf(WaitTimeoutError)
    expect(Date.now() - started).toBeLessThan(6000)

    for (const mode of MODES) {
      const r = await py.call<{ ok: boolean; error: { type: string }; elapsed: number }>(
        mode,
        pyc(t),
        '_wait_timed',
        [post.id],
        { timeout: 2, poll_interval: 0.2 },
      )

      expect(r.ok).toBe(false)
      expect(r.error.type).toBe('WaitTimeoutError')
      expect(r.elapsed).toBeLessThan(6)
    }
  })

  it('stops on abort (TS AbortSignal, Python task cancellation)', async () => {
    const post = await publishing()
    const ctrl = new AbortController()

    setTimeout(() => ctrl.abort(), 500)
    const started = Date.now()
    const err = await errorOf(c.posts.waitForPublished(post.id, { timeout: 60_000, signal: ctrl.signal }))

    expect(err).toBeInstanceOf(APIUserAbortError)
    expect(Date.now() - started).toBeLessThan(3000)
    const r = await py.call<{ cancelled: boolean; elapsed: number }>('async', pyc(t), '_cancel_wait', [post.id], {
      after: 0.5,
    })

    expect(r.cancelled).toBe(true)
    expect(r.elapsed).toBeLessThan(3)
  })
})

describe.each(['local', 'cloud'] as const)('%s: webhook signatures of real deliveries', (name) => {
  it('TS verifyWebhook and Python verify_webhook accept every delivery and reject tampering', async () => {
    const t = targets().find((x) => x.name === name)!
    const c = sdk(t.url, t.key || null)
    const rx = await receiver()

    try {
      const col = await c.collections.create({ name: `hooks ${Date.now()}` })
      const channel = await c.channels.connect({ provider: 'custom', name: `hooks channel ${Date.now()}` })
      const hook = await c.webhooks.create({
        url: rx.url,
        events: ['post.created', 'post.publish_requested'],
        collection_ids: [col.id],
      })
      const post = await c.posts.create({
        collection: col.id,
        content: 'signed\nünïcödé ✓',
        targets: [{ channel_id: channel.id }],
      })

      await c.posts.publish(post.id)
      await until(() => rx.received.length >= 2)

      for (const d of rx.received) {
        const ev = await verifyWebhook(hook.secret, d.body, d.headers)

        expect(ev.data.post).toBeDefined()

        for (const mode of ['sync'] as const) {
          const pe = await py.call<{ type: string }>(mode, pyc(t), '_verify_webhook', [], {
            secret: hook.secret,
            body: d.body,
            headers: d.headers,
          })

          expect(pe.type).toBe(ev.type)
        }

        const tampered = d.body.replace('signed', 'signeD')

        expect(await errorOf(verifyWebhook(hook.secret, tampered, d.headers))).toBeInstanceOf(WebhookVerificationError)
        expect(
          (
            await py.fail('sync', pyc(t), '_verify_webhook', [], {
              secret: hook.secret,
              body: tampered,
              headers: d.headers,
            })
          ).type,
        ).toBe('WebhookVerificationError')
      }

      // deliveries run in parallel: they may arrive in any order (created_at orders them)
      const got = rx.received.map((d) => JSON.parse(d.body) as { type: string; created_at: string })

      expect(got.toSorted((a, b) => a.created_at.localeCompare(b.created_at)).map((e) => e.type)).toEqual([
        'post.created',
        'post.publish_requested',
      ])
      // a replayed delivery is refused once it is older than the tolerance
      const old = rx.received[0]!

      expect(
        await errorOf(
          verifyWebhook(hook.secret, old.body, old.headers, { tolerance: 1, now: Date.now() / 1000 + 600 }),
        ),
      ).toBeInstanceOf(WebhookVerificationError)
    } finally {
      rx.close()
    }
  })
})

describe('timeouts and network failures', () => {
  it('a server that never answers: TimeoutError, no hang (TS, Python sync and async)', async () => {
    const sockets: Socket[] = []
    const hole = createServer((s) => sockets.push(s))

    await new Promise<void>((r) => hole.listen(0, '127.0.0.1', r))
    const url = `http://127.0.0.1:${(hole.address() as { port: number }).port}`

    try {
      let started = Date.now()

      expect(await errorOf(sdk(url, null, { timeout: 500, maxRetries: 0 }).posts.list())).toBeInstanceOf(TimeoutError)
      expect(Date.now() - started).toBeLessThan(2000)
      started = Date.now()
      expect(await errorOf(sdk(url, null, { timeout: 400, maxRetries: 1 }).posts.list())).toBeInstanceOf(TimeoutError)
      expect(Date.now() - started).toBeLessThan(3000)
      // a POST is retried too: it carries an Idempotency-Key
      expect(
        await errorOf(sdk(url, null, { timeout: 300, maxRetries: 1 }).posts.create({ collection: 'x', content: 'x' })),
      ).toBeInstanceOf(TimeoutError)

      for (const mode of MODES) {
        const e = await py.fail(mode, { base_url: url, api_key: '', timeout: 0.5, max_retries: 0 }, 'posts.list')

        expect(e.type).toBe('TimeoutError')
        expect(e.mro).toContain('APIConnectionError')
      }
    } finally {
      sockets.forEach((s) => s.destroy())
      hole.close()
    }
  })

  it('connection refused: APIConnectionError after the retries', async () => {
    const url = 'http://127.0.0.1:9' // discard port, nothing listens
    const started = Date.now()
    const err = await errorOf(sdk(url, null, { maxRetries: 2 }).posts.list())

    expect(err).toBeInstanceOf(APIConnectionError)
    expect(err).not.toBeInstanceOf(TimeoutError)
    expect(Date.now() - started).toBeLessThan(5000)

    for (const mode of MODES) {
      expect((await py.fail(mode, { base_url: url, api_key: '', max_retries: 1 }, 'posts.list')).type).toBe(
        'APIConnectionError',
      )
    }
  })

  it('the server is killed while every SDK waits for a post: all fail fast with APIConnectionError', async () => {
    const port = Number(process.env.XC_CRASH_PORT ?? 25433)
    const server = await startServer(bin, { mode: 'local', port, dashboardPort: port + 5000 })

    try {
      const c = sdk(server.url, server.token, { maxRetries: 2 })
      const col = await c.collections.create({ name: 'crash' })
      const channel = await c.channels.connect({ provider: 'custom', name: 'crash channel' })
      const post = await c.posts.create({
        collection: col.id,
        content: 'publishing forever',
        targets: [{ channel_id: channel.id }],
      })

      await c.posts.publish(post.id)
      const started = Date.now()
      const waits = [
        errorOf(c.posts.waitForPublished(post.id, { timeout: 60_000, pollInterval: 300 })),
        ...MODES.map((mode) =>
          py.call<{ ok: boolean; error: { type: string; mro: string[] } }>(
            mode,
            { base_url: server.url, api_key: server.token, max_retries: 2 },
            '_wait_timed',
            [post.id],
            { timeout: 60, poll_interval: 0.3 },
          ),
        ),
      ] as const

      await sleep(1000)
      server.proc.kill('SIGKILL')
      const [ts, ...pys] = await Promise.all(waits)

      expect(ts).toBeInstanceOf(APIConnectionError)

      for (const r of pys) {
        expect(r.ok).toBe(false)
        expect(r.error.mro).toContain('APIConnectionError')
      }

      expect(Date.now() - started).toBeLessThan(15_000)
    } finally {
      await server.stop('SIGKILL')
    }
  })
})
