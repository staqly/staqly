import { describe, expect, it } from 'vitest'

import {
  APIError,
  InternalServerError,
  PermissionDeniedError,
  PlanLimitError,
  RateLimitError,
  type Staqly,
} from '../src/index.js'
import { apiError, client, json, post } from './helpers.js'

interface HeaderCase {
  name: string
  call: (c: Staqly) => Promise<unknown>
  reply?: Response
  auth: boolean
  idempotent: boolean
  jsonBody: boolean
}

const empty = () => new Response(null, { status: 204 })

const headerCases: HeaderCase[] = [
  { name: 'posts.get', call: (c) => c.posts.get('pst_1'), auth: true, idempotent: false, jsonBody: false },
  {
    name: 'posts.create',
    call: (c) => c.posts.create({ title: 'Post', collection: 'col_1', content: 't' }),
    auth: true,
    idempotent: true,
    jsonBody: true,
  },
  {
    name: 'posts.update',
    call: (c) => c.posts.update('pst_1', { content: 'x' }),
    auth: true,
    idempotent: true,
    jsonBody: true,
  },
  {
    name: 'posts.delete',
    call: (c) => c.posts.delete('pst_1'),
    reply: empty(),
    auth: true,
    idempotent: true,
    jsonBody: false,
  },
  {
    name: 'posts.publish',
    call: (c) => c.posts.publish('pst_1'),
    auth: true,
    idempotent: true,
    jsonBody: false,
  },
  {
    name: 'providers.setApp (PUT)',
    call: (c) => c.providers.setApp('mastodon', { client_id: 'a', client_secret: 'b' }),
    reply: empty(),
    auth: true,
    idempotent: true,
    jsonBody: true,
  },
  {
    name: 'billing.checkout',
    call: (c) => c.billing.checkout({ plan: 'starter' }),
    reply: json({ url: 'https://x' }),
    auth: true,
    idempotent: true,
    jsonBody: true,
  },
  {
    name: 'workspace.delete',
    call: (c) => c.workspace.delete('Acme'),
    reply: empty(),
    auth: true,
    idempotent: true,
    jsonBody: true,
  },
  {
    name: 'workspaces.create',
    call: (c) => c.workspaces.create({ name: 'Acme' }),
    reply: json({}, 201),
    auth: false,
    idempotent: true,
    jsonBody: true,
  },
  {
    name: 'sessions.recover',
    call: (c) => c.sessions.recover('rk_1'),
    reply: json({}, 201),
    auth: false,
    idempotent: true,
    jsonBody: true,
  },
  {
    name: 'sessions.signInWithApiKey',
    call: (c) => c.sessions.signInWithApiKey('stk_admin'),
    reply: json({}, 201),
    auth: false,
    idempotent: true,
    jsonBody: true,
  },
  { name: 'setup', call: (c) => c.setup(), reply: json({}), auth: false, idempotent: false, jsonBody: false },
  { name: 'whoami', call: (c) => c.whoami(), reply: json({}), auth: true, idempotent: false, jsonBody: false },
]

describe('request headers', () => {
  for (const tc of headerCases) {
    it(tc.name, async () => {
      const { c, calls } = client(tc.reply ?? json(post()))

      await tc.call(c)
      const h = calls[0]!.headers

      expect(h.get('authorization')).toBe(tc.auth ? 'Bearer stk_test' : null)
      expect(h.get('accept')).toBe('application/json')
      expect(h.get('user-agent')).toMatch(/^staqly-sdk-ts\//)
      expect(h.get('idempotency-key') !== null).toBe(tc.idempotent)
      expect(h.get('content-type')).toBe(tc.jsonBody ? 'application/json' : null)
    })
  }

  it('multipart uploads carry no Idempotency-Key', async () => {
    const { c, calls } = client(json({ id: 'med_1' }, 201))

    await c.media.upload(new Uint8Array([1]))
    expect(calls[0]!.headers.get('idempotency-key')).toBeNull()
    expect(calls[0]!.headers.get('authorization')).toBe('Bearer stk_test')
  })

  it('per-request headers override defaults', async () => {
    const { c, calls } = client(json(post()))

    await c.posts.get('pst_1', { headers: { 'X-Trace': 't1' } })
    expect(calls[0]!.headers.get('x-trace')).toBe('t1')
  })
})

describe('request shape gaps', () => {
  it('posts.search sends q with the other filters', async () => {
    const { c, calls } = client(json({ data: [] }))

    await c.posts.search('launch', { status: 'draft', limit: 5 })
    expect(Object.fromEntries(calls[0]!.url.searchParams)).toEqual({ q: 'launch', status: 'draft', limit: '5' })
  })

  it('empty and undefined query values are dropped', async () => {
    const { c, calls } = client(json({ data: [] }))

    await c.posts.list({ status: undefined, q: '', limit: 0 })
    expect(Object.fromEntries(calls[0]!.url.searchParams)).toEqual({ limit: '0' })
  })

  it('posts.schedule accepts a Date', async () => {
    const { c, calls } = client(json(post({ status: 'scheduled' })))

    await c.posts.schedule('pst_1', new Date('2026-11-03T09:00:00Z'))
    expect(calls[0]!.path).toBe('/v1/posts/pst_1/schedule')
    expect(calls[0]!.body).toEqual({ publish_at: '2026-11-03T09:00:00.000Z' })
  })

  it('collections.resolveId sends no request for an ID and resolves a name', async () => {
    const { c, calls } = client(json({ id: 'col_9', name: 'News' }))

    expect(await c.collections.resolveId('col_1')).toBe('col_1')
    expect(calls).toHaveLength(0)
    expect(await c.collections.resolveId('News')).toBe('col_9')
    expect(calls.map((x) => x.path)).toEqual(['/v1/collections/News'])
  })

  it('collections.delete sends the confirm name as a query param', async () => {
    const { c, calls } = client(empty())

    await c.collections.delete('col_1', 'My posts')
    expect(calls[0]!.method).toBe('DELETE')
    expect(calls[0]!.url.searchParams.get('confirm')).toBe('My posts')
  })

  it('path segments are URL-encoded', async () => {
    const { c, calls } = client(json(post()))

    await c.posts.retryTarget('pst/1', 'ch 1')
    expect(calls[0]!.url.pathname).toBe('/v1/posts/pst%2F1/targets/ch%201/retry')
  })

  it('exports.workspaceExportResponse returns the raw response', async () => {
    const { c, calls } = client(
      new Response(new Uint8Array([0x50, 0x4b]), { headers: { 'Content-Type': 'application/zip' } }),
    )
    const res = await c.exports.workspaceExportResponse('exp_1')

    expect(calls[0]!.method).toBe('GET')
    expect(calls[0]!.path).toBe('/v1/workspace/exports/exp_1/download')
    expect(res.headers.get('content-type')).toBe('application/zip')
    expect(new Uint8Array(await res.arrayBuffer())).toEqual(new Uint8Array([0x50, 0x4b]))
  })

  it('exports.eventsResponse passes filters and format', async () => {
    const { c, calls } = client(new Response('a,b\n'))

    await c.exports.eventsResponse({ format: 'csv', type_prefix: 'post.' })
    expect(calls[0]!.path).toBe('/v1/events/export')
    expect(Object.fromEntries(calls[0]!.url.searchParams)).toEqual({ format: 'csv', type_prefix: 'post.' })
  })

  it('alerts.deliveries sends limit', async () => {
    const { c, calls } = client(json({ data: [] }))

    await c.alerts.deliveries('nch_1', { limit: 3 })
    expect(calls[0]!.url.searchParams.get('limit')).toBe('3')
  })

  it('webhooks.deliveries.list sends filters', async () => {
    const { c, calls } = client(json({ data: [] }))

    await c.webhooks.deliveries.list({ webhook_id: 'wh_1', status: 'failed' })
    expect(Object.fromEntries(calls[0]!.url.searchParams)).toEqual({ webhook_id: 'wh_1', status: 'failed' })
  })
})

describe('error mapping gaps', () => {
  it('402 without the plan_limit code still maps to PlanLimitError', async () => {
    const { c } = client(apiError(402, 'payment_required', 'pay'))
    const err = await c.posts.create({ title: 'Post', collection: 'col_1', content: 't' }).catch((e: unknown) => e)

    expect(err).toBeInstanceOf(PlanLimitError)
  })

  it('plan_limit_exceeded code wins over the status', async () => {
    const { c } = client(apiError(403, 'plan_limit_exceeded', 'limit'))
    const err = await c.posts.list().catch((e: unknown) => e)

    expect(err).toBeInstanceOf(PlanLimitError)
  })

  it('403 forbidden is not flagged as not_local', async () => {
    const { c } = client(apiError(403, 'forbidden', 'nope'))
    const err = (await c.posts.list().catch((e) => e)) as PermissionDeniedError

    expect(err).toBeInstanceOf(PermissionDeniedError)
    expect(err.isNotLocal).toBe(false)
  })

  it('5xx maps to InternalServerError once retries are exhausted', async () => {
    const { c, calls } = client(apiError(503, 'unavailable', 'down'))
    const err = await c.posts.get('pst_1', { maxRetries: 0 }).catch((e: unknown) => e)

    expect(err).toBeInstanceOf(InternalServerError)
    expect(calls).toHaveLength(1)
  })

  it('a JSON body without an error object becomes http_error', async () => {
    const { c } = client(json({ unexpected: true }, 418))
    const err = (await c.posts.get('pst_1').catch((e) => e)) as APIError

    expect(err.constructor).toBe(APIError)
    expect(err.code).toBe('http_error')
    expect(err.message).toContain('HTTP 418')
  })

  it('429 Retry-After as an HTTP date', async () => {
    const when = new Date(Date.now() + 60_000).toUTCString()
    const { c } = client(new Response('', { status: 429, headers: { 'Retry-After': when } }))
    const err = (await c.posts.list({}, { maxRetries: 0 }).catch((e) => e)) as RateLimitError

    expect(err).toBeInstanceOf(RateLimitError)
    expect(err.retryAfter).toBeGreaterThan(50)
    expect(err.retryAfter).toBeLessThanOrEqual(60)
  })
})
