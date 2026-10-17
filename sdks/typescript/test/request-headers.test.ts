import { describe, expect, it } from 'vitest'

import {
  APIError,
  InternalServerError,
  PermissionDeniedError,
  PlanLimitError,
  RateLimitError,
  type Staqly,
} from '../src/index.js'
import { apiError, client, item, json } from './helpers.js'

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
  { name: 'items.get', call: (c) => c.items.get('itm_1'), auth: true, idempotent: false, jsonBody: false },
  {
    name: 'items.create',
    call: (c) => c.items.create('col_1', { title: 't' }),
    auth: true,
    idempotent: true,
    jsonBody: true,
  },
  {
    name: 'items.update',
    call: (c) => c.items.update('itm_1', { title: 'x' }),
    auth: true,
    idempotent: true,
    jsonBody: true,
  },
  {
    name: 'items.delete',
    call: (c) => c.items.delete('itm_1'),
    reply: empty(),
    auth: true,
    idempotent: true,
    jsonBody: false,
  },
  {
    name: 'items.approve',
    call: (c) => c.items.approve('itm_1'),
    auth: true,
    idempotent: true,
    jsonBody: true,
  },
  {
    name: 'billing.checkout',
    call: (c) => c.billing.checkout(),
    reply: json({ url: 'https://x' }),
    auth: true,
    idempotent: true,
    jsonBody: false,
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
  {
    name: 'decisionLinks.decide (not retried, so no key)',
    call: (c) => c.decisionLinks.decide('tok'),
    reply: json({ status: 'approved' }),
    auth: false,
    idempotent: false,
    jsonBody: true,
  },
  { name: 'setup', call: (c) => c.setup(), reply: json({}), auth: false, idempotent: false, jsonBody: false },
  { name: 'whoami', call: (c) => c.whoami(), reply: json({}), auth: true, idempotent: false, jsonBody: false },
]

describe('request headers', () => {
  for (const tc of headerCases) {
    it(tc.name, async () => {
      const { c, calls } = client(tc.reply ?? json(item()))

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
    const { c, calls } = client(json(item()))

    await c.items.get('itm_1', { headers: { 'X-Trace': 't1' } })
    expect(calls[0]!.headers.get('x-trace')).toBe('t1')
  })
})

describe('request shape gaps', () => {
  it('items.search sends q with the other filters', async () => {
    const { c, calls } = client(json({ data: [] }))

    await c.items.search('launch', { status: 'pending', limit: 5 })
    expect(Object.fromEntries(calls[0]!.url.searchParams)).toEqual({ q: 'launch', status: 'pending', limit: '5' })
  })

  it('empty and undefined query values are dropped', async () => {
    const { c, calls } = client(json({ data: [] }))

    await c.items.list({ status: undefined, q: '', limit: 0 })
    expect(Object.fromEntries(calls[0]!.url.searchParams)).toEqual({ limit: '0' })
  })

  it('items.wait sends timeout and outlasts it on the HTTP side', async () => {
    const { c, calls } = client(json({ decided: true, item: item({ status: 'approved' }) }))
    const res = await c.items.wait('itm_1', { timeout: 45 })

    expect(calls[0]!.path).toBe('/v1/items/itm_1/wait')
    expect(calls[0]!.url.searchParams.get('timeout')).toBe('45')
    expect(res.decided).toBe(true)
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
    const { c, calls } = client(json(item()))

    await c.items.action('itm/1', 're gen')
    expect(calls[0]!.url.pathname).toBe('/v1/items/itm%2F1/actions/re%20gen')
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

    await c.exports.eventsResponse({ format: 'csv', type_prefix: 'item.' })
    expect(calls[0]!.path).toBe('/v1/events/export')
    expect(Object.fromEntries(calls[0]!.url.searchParams)).toEqual({ format: 'csv', type_prefix: 'item.' })
  })

  it('notificationChannels.deliveries sends limit', async () => {
    const { c, calls } = client(json({ data: [] }))

    await c.notificationChannels.deliveries('nch_1', { limit: 3 })
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
    const err = await c.items.create('col_1', { title: 't' }).catch((e: unknown) => e)

    expect(err).toBeInstanceOf(PlanLimitError)
  })

  it('plan_limit_exceeded code wins over the status', async () => {
    const { c } = client(apiError(403, 'plan_limit_exceeded', 'limit'))
    const err = await c.items.list().catch((e: unknown) => e)

    expect(err).toBeInstanceOf(PlanLimitError)
  })

  it('403 forbidden is not flagged as not_local', async () => {
    const { c } = client(apiError(403, 'forbidden', 'nope'))
    const err = (await c.items.list().catch((e) => e)) as PermissionDeniedError

    expect(err).toBeInstanceOf(PermissionDeniedError)
    expect(err.isNotLocal).toBe(false)
  })

  it('5xx maps to InternalServerError once retries are exhausted', async () => {
    const { c, calls } = client(apiError(503, 'unavailable', 'down'))
    const err = await c.items.get('itm_1', { maxRetries: 0 }).catch((e: unknown) => e)

    expect(err).toBeInstanceOf(InternalServerError)
    expect(calls).toHaveLength(1)
  })

  it('a JSON body without an error object becomes http_error', async () => {
    const { c } = client(json({ unexpected: true }, 418))
    const err = (await c.items.get('itm_1').catch((e) => e)) as APIError

    expect(err.constructor).toBe(APIError)
    expect(err.code).toBe('http_error')
    expect(err.message).toContain('HTTP 418')
  })

  it('429 Retry-After as an HTTP date', async () => {
    const when = new Date(Date.now() + 60_000).toUTCString()
    const { c } = client(new Response('', { status: 429, headers: { 'Retry-After': when } }))
    const err = (await c.items.list({}, { maxRetries: 0 }).catch((e) => e)) as RateLimitError

    expect(err).toBeInstanceOf(RateLimitError)
    expect(err.retryAfter).toBeGreaterThan(50)
    expect(err.retryAfter).toBeLessThanOrEqual(60)
  })
})
