import { readFileSync } from 'node:fs'
import { inspect } from 'node:util'

import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  APIConnectionError,
  APIError,
  APIUserAbortError,
  AuthenticationError,
  BadRequestError,
  ConflictError,
  GoneError,
  InternalServerError,
  NotFoundError,
  PayloadTooLargeError,
  PermissionDeniedError,
  PlanLimitError,
  RateLimitError,
  Staqly,
  StaqlyError,
  TimeoutError,
  UnsupportedMediaTypeError,
  ValidationError,
  VERSION,
} from '../src/index.js'
import { apiError, client, json, mockFetch } from './helpers.js'

afterEach(() => {
  vi.unstubAllEnvs()
})

describe('configuration', () => {
  it('defaults to the local server without credentials', async () => {
    vi.stubEnv('STAQLY_API_URL', '')
    vi.stubEnv('STAQLY_API_KEY', '')
    const m = mockFetch(json({ data: [] }))
    const c = new Staqly({ fetch: m.fetch })

    expect(c.baseUrl).toBe('http://localhost:3333')
    await c.collections.list()
    expect(m.calls[0]!.url.toString()).toBe('http://localhost:3333/v1/collections')
    expect(m.calls[0]!.headers.get('authorization')).toBeNull()
    expect(m.calls[0]!.headers.get('user-agent')).toBe(`staqly-sdk-ts/${VERSION}`)
  })

  it('reads STAQLY_API_URL and STAQLY_API_KEY', async () => {
    vi.stubEnv('STAQLY_API_URL', 'https://staqly.example.com/')
    vi.stubEnv('STAQLY_API_KEY', 'stk_env')
    const m = mockFetch(json({ data: [] }))
    const c = new Staqly({ fetch: m.fetch })

    await c.plans.list()
    expect(m.calls[0]!.url.toString()).toBe('https://staqly.example.com/v1/plans')
    expect(m.calls[0]!.headers.get('authorization')).toBe('Bearer stk_env')
  })

  it('apiKey: null ignores the environment', async () => {
    vi.stubEnv('STAQLY_API_KEY', 'stk_env')
    const m = mockFetch(json({ data: [] }))

    await new Staqly({ apiKey: null, fetch: m.fetch, baseUrl: 'http://x' }).plans.list()
    expect(m.calls[0]!.headers.get('authorization')).toBeNull()
  })

  it('drops trailing slashes from baseUrl', () => {
    expect(new Staqly({ baseUrl: 'http://x/api///' }).baseUrl).toBe('http://x/api')
    expect(new Staqly({ baseUrl: '/'.repeat(100_000) + 'a' }).baseUrl).toBe('/'.repeat(100_000) + 'a')
  })

  it('sends custom headers and user agent suffix', async () => {
    const m = mockFetch(json({}))
    const c = new Staqly({
      baseUrl: 'http://x',
      fetch: m.fetch,
      defaultHeaders: { 'X-Trace': '1' },
      userAgent: 'bot/2',
    })

    await c.workspace.get({ headers: { 'X-Req': '2' } })
    expect(m.calls[0]!.headers.get('x-trace')).toBe('1')
    expect(m.calls[0]!.headers.get('x-req')).toBe('2')
    expect(m.calls[0]!.headers.get('user-agent')).toBe(`staqly-sdk-ts/${VERSION} bot/2`)
  })

  it('VERSION matches package.json', () => {
    const pkg = JSON.parse(readFileSync(new URL('../package.json', import.meta.url), 'utf8'))

    expect(VERSION).toBe(pkg.version)
  })

  it('health and workspace signup are sent without credentials', async () => {
    const { c, calls } = client(json({ status: 'ok' }))

    await c.health()
    await c.workspaces.create({ name: 'x' })
    expect(calls.map((x) => x.headers.get('authorization'))).toEqual([null, null])
    expect(calls[1]!.body).toEqual({ name: 'x' })
  })

  it('public endpoints (setup, live, recover, API key sign-in, redeem) are sent without credentials', async () => {
    const { c, calls } = client(json({ status: 'ok' }))

    await c.setup()
    await c.live()
    await c.sessions.recover('rk_1')
    await c.sessions.signInWithApiKey('stk_admin')
    await c.deviceLinks.redeem('tok')
    expect(calls.map((x) => x.headers.get('authorization'))).toEqual([null, null, null, null, null])
  })

  it('openapi returns the YAML text', async () => {
    const { c, calls } = client(new Response('openapi: 3.2.0\n', { headers: { 'Content-Type': 'application/yaml' } }))

    expect(await c.openapi()).toBe('openapi: 3.2.0\n')
    expect(calls[0]!.path).toBe('/openapi.yaml')
    expect(calls[0]!.headers.get('accept')).toBe('application/yaml')
    expect(calls[0]!.headers.get('authorization')).toBeNull()
  })

  it('metrics sends the metrics token, never the API key', async () => {
    const { c, calls } = client(new Response('staqly_up 1\n', { headers: { 'Content-Type': 'text/plain' } }))

    expect(await c.metrics()).toBe('staqly_up 1\n')
    await c.metrics({ token: 'mt_secret' })
    expect(calls.map((x) => x.path)).toEqual(['/metrics', '/metrics'])
    expect(calls.map((x) => x.headers.get('authorization'))).toEqual([null, 'Bearer mt_secret'])
  })
})

describe('error mapping', () => {
  const cases: [number, string, new (...a: never[]) => APIError][] = [
    [400, 'bad_request', BadRequestError],
    [401, 'unauthorized', AuthenticationError],
    [403, 'forbidden', PermissionDeniedError],
    [404, 'not_found', NotFoundError],
    [409, 'conflict', ConflictError],
    [410, 'gone', GoneError],
    [413, 'too_large', PayloadTooLargeError],
    [415, 'unsupported_media_type', UnsupportedMediaTypeError],
    [422, 'validation_failed', ValidationError],
    [418, 'teapot', APIError],
  ]

  for (const [status, code, cls] of cases) {
    it(`${status} → ${cls.name}`, async () => {
      const { c } = client(apiError(status, code, 'boom'))
      const err = await c.posts.get('pst_1').catch((e: unknown) => e)

      expect(err).toBeInstanceOf(cls)
      expect(err).toBeInstanceOf(APIError)
      expect(err).toBeInstanceOf(StaqlyError)
      expect(err).toMatchObject({ status, code, message: `${status} ${code}: boom` })
    })
  }

  it('422 carries fields', async () => {
    const { c } = client(apiError(422, 'validation_failed', 'name: is required', { fields: { name: 'is required' } }))
    const err = (await c.collections.create({ name: '' }).catch((e) => e)) as ValidationError

    expect(err.fields).toEqual({ name: 'is required' })
  })

  it('402 → PlanLimitError with details', async () => {
    const details = { limit: 'webhooks', max: 3, used: 3, plan: 'free' }
    const { c } = client(apiError(402, 'plan_limit_exceeded', 'upgrade', { details }))
    const err = (await c.webhooks.create({ url: 'https://x' }).catch((e) => e)) as PlanLimitError

    expect(err).toBeInstanceOf(PlanLimitError)
    expect([err.limit, err.max, err.used, err.plan]).toEqual(['webhooks', 3, 3, 'free'])
    expect(err.details).toEqual(details)
  })

  it('403 not_local is flagged', async () => {
    const { c } = client(apiError(403, 'not_local', 'local mode only accepts connections from this machine'))
    const err = (await c.posts.list().catch((e) => e)) as PermissionDeniedError

    expect(err.isNotLocal).toBe(true)
  })

  it('429 exposes retryAfter and request id; non-JSON bodies survive', async () => {
    const res = new Response('slow down', { status: 429, headers: { 'Retry-After': '120', 'X-Request-Id': 'req_1' } })
    const { c } = client(res)
    const err = (await c.posts.list().catch((e) => e)) as RateLimitError

    expect(err).toBeInstanceOf(RateLimitError)
    expect(err.retryAfter).toBe(120)
    expect(err.requestId).toBe('req_1')
    expect(err.code).toBe('http_error')
    expect(err.body).toBe('slow down')
  })

  it('network failure → APIConnectionError', async () => {
    const c = new Staqly({
      baseUrl: 'http://x',
      maxRetries: 0,
      fetch: async () => {
        throw new TypeError('fetch failed')
      },
    })

    await expect(c.posts.list()).rejects.toBeInstanceOf(APIConnectionError)
  })
})

describe('retries', () => {
  it('retries 5xx with the same Idempotency-Key, then succeeds', async () => {
    const { c, calls } = client(
      apiError(503, 'unavailable'),
      new Response(JSON.stringify({ error: { code: 'internal', message: 'x' } }), {
        status: 500,
        headers: { 'Retry-After': '0' },
      }),
      json({ id: 'col_1' }, 201),
    )
    const out = await c.collections.create({ name: 'a' })

    expect(out).toEqual({ id: 'col_1' })
    expect(calls).toHaveLength(3)
    const keys = calls.map((x) => x.headers.get('idempotency-key'))

    expect(keys[0]).toMatch(/^sdk-/)
    expect(new Set(keys).size).toBe(1)
  })

  it('uses a caller-provided Idempotency-Key', async () => {
    const { c, calls } = client(json({}, 201))

    await c.posts.create({ title: 'Post', collection: 'Posts', content: 'x' }, { idempotencyKey: 'job-42' })
    expect(calls[0]!.headers.get('idempotency-key')).toBe('job-42')
  })

  it('GET requests carry no Idempotency-Key', async () => {
    const { c, calls } = client(json({ data: [] }))

    await c.posts.list()
    expect(calls[0]!.headers.get('idempotency-key')).toBeNull()
  })

  it('gives up after maxRetries and throws the last error', async () => {
    const { c, calls } = client(new Response('{}', { status: 502, headers: { 'Retry-After': '0' } }))

    await expect(c.posts.list()).rejects.toBeInstanceOf(InternalServerError)
    expect(calls).toHaveLength(3)
  })

  it('respects maxRetries per request', async () => {
    const { c, calls } = client(new Response('{}', { status: 502, headers: { 'Retry-After': '0' } }))

    await expect(c.posts.list({}, { maxRetries: 0 })).rejects.toBeInstanceOf(InternalServerError)
    expect(calls).toHaveLength(1)
  })

  it('does not retry 4xx', async () => {
    const { c, calls } = client(apiError(409, 'conflict'))

    await expect(c.posts.publish('pst_1')).rejects.toBeInstanceOf(ConflictError)
    expect(calls).toHaveLength(1)
  })

  it('retries 409 idempotency_in_progress', async () => {
    const { c, calls } = client(
      new Response(JSON.stringify({ error: { code: 'idempotency_in_progress', message: 'x' } }), {
        status: 409,
        headers: { 'Retry-After': '0' },
      }),
      json({ id: 'pst_1' }),
    )

    await c.posts.publish('pst_1')
    expect(calls).toHaveLength(2)
  })

  it('honours Retry-After on 429', async () => {
    const { c, calls } = client(
      new Response('{}', { status: 429, headers: { 'Retry-After': '0' } }),
      json({ data: [] }),
    )

    await c.posts.list()
    expect(calls).toHaveLength(2)
  })

  it('does not wait for a Retry-After longer than 60s', async () => {
    const { c, calls } = client(new Response('{}', { status: 429, headers: { 'Retry-After': '3600' } }))

    await expect(c.posts.list()).rejects.toBeInstanceOf(RateLimitError)
    expect(calls).toHaveLength(1)
  })

  it('retries network errors with backoff', async () => {
    let n = 0
    const c = new Staqly({
      baseUrl: 'http://x',
      maxRetries: 1,
      fetch: async () => {
        if (n++ === 0) {
          throw new TypeError('fetch failed')
        }

        return json({ data: [] })
      },
    })

    await c.plans.list()
    expect(n).toBe(2)
  })

  it('multipart uploads are not retried on 5xx (no server-side idempotency)', async () => {
    const { c, calls } = client(new Response('{}', { status: 503, headers: { 'Retry-After': '0' } }))

    await expect(c.media.upload(new Uint8Array([1, 2, 3]))).rejects.toBeInstanceOf(InternalServerError)
    expect(calls).toHaveLength(1)
    expect(calls[0]!.headers.get('idempotency-key')).toBeNull()
  })

  it('multipart uploads are retried on 429', async () => {
    const { c, calls } = client(new Response('{}', { status: 429, headers: { 'Retry-After': '0' } }), json({}, 201))

    await c.media.upload(new Uint8Array([1]))
    expect(calls).toHaveLength(2)
  })

  it('no automatic Idempotency-Key when retries are disabled', async () => {
    const m = mockFetch(json({}, 201))

    await new Staqly({ baseUrl: 'http://x', fetch: m.fetch, maxRetries: 0 }).collections.create({ name: 'a' })
    expect(m.calls[0]!.headers.get('idempotency-key')).toBeNull()
  })
})

describe('timeouts and aborts', () => {
  const hanging = (_: unknown, init?: RequestInit) =>
    new Promise<Response>((_resolve, reject) => {
      init?.signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')))
    })

  it('throws TimeoutError after the timeout', async () => {
    const c = new Staqly({ baseUrl: 'http://x', fetch: hanging, timeout: 20, maxRetries: 0 })
    const err = await c.posts.list().catch((e) => e)

    expect(err).toBeInstanceOf(TimeoutError)
    expect(err).toBeInstanceOf(APIConnectionError)
  })

  it('retries timeouts', async () => {
    let n = 0
    const c = new Staqly({
      baseUrl: 'http://x',
      timeout: 20,
      maxRetries: 1,
      fetch: (u, init) => (n++ === 0 ? hanging(u, init) : Promise.resolve(json({ data: [] }))),
    })

    await c.plans.list()
    expect(n).toBe(2)
  })

  it('throws APIUserAbortError when the caller aborts', async () => {
    const c = new Staqly({ baseUrl: 'http://x', fetch: hanging, maxRetries: 0 })
    const ctrl = new AbortController()
    const p = c.posts.list({}, { signal: ctrl.signal })

    ctrl.abort()
    await expect(p).rejects.toBeInstanceOf(APIUserAbortError)
  })
})

describe('streamed (raw) responses', () => {
  // A body that delivers one chunk, then waits until the request signal aborts.
  function streamingFetch() {
    return (_: unknown, init?: RequestInit) => {
      const body = new ReadableStream<Uint8Array>({
        start(controller) {
          controller.enqueue(new Uint8Array([1, 2, 3]))
          init?.signal?.addEventListener('abort', () => controller.error(new DOMException('aborted', 'AbortError')))
        },
      })

      return Promise.resolve(new Response(body, { status: 200, headers: { 'Content-Type': 'video/mp4' } }))
    }
  }

  it('stays abortable via opts.signal until the body is consumed', async () => {
    const c = new Staqly({ baseUrl: 'http://x', fetch: streamingFetch(), maxRetries: 0 })
    const ctrl = new AbortController()
    const res = await c.media.downloadResponse('med_1', { signal: ctrl.signal })

    expect(res.status).toBe(200)
    expect(res.headers.get('content-type')).toBe('video/mp4')
    const reader = res.body!.getReader()

    expect((await reader.read()).value).toEqual(new Uint8Array([1, 2, 3]))
    const pending = reader.read()

    ctrl.abort()
    await expect(pending).rejects.toThrow(/abort/i)
  })

  it('releases the abort listener once the body has been read', async () => {
    const ctrl = new AbortController()
    const remove = vi.spyOn(ctrl.signal, 'removeEventListener')
    const c = new Staqly({
      baseUrl: 'http://x',
      maxRetries: 0,
      fetch: () => Promise.resolve(new Response('hello', { status: 200 })),
    })
    const res = await c.media.downloadResponse('med_1', { signal: ctrl.signal })

    expect(remove).not.toHaveBeenCalled()
    expect(await res.text()).toBe('hello')
    expect(remove).toHaveBeenCalledWith('abort', expect.any(Function))
  })
})

describe('API key handling', () => {
  const secret = 'stk_super_secret_value'

  it('is not enumerable, inspected or serialized', () => {
    const c = new Staqly({ baseUrl: 'http://x', apiKey: secret, fetch: mockFetch(json({})).fetch })

    expect(c.http.apiKey).toBe(secret)
    expect(Object.keys(c.http)).not.toContain('apiKey')

    for (const obj of [c, c.http]) {
      expect(inspect(obj, { depth: 5 })).not.toContain(secret)
      expect(JSON.stringify(obj)).not.toContain(secret)
    }

    expect(inspect(c.http)).toContain("apiKey: '***'")
  })

  it('is still sent as a bearer token', async () => {
    const m = mockFetch(json({ data: [] }))

    await new Staqly({ baseUrl: 'http://x', apiKey: secret, fetch: m.fetch }).plans.list()
    expect(m.calls[0]!.headers.get('authorization')).toBe(`Bearer ${secret}`)
  })
})

describe('node helpers', () => {
  it('saveResponse streams a body to disk', async () => {
    const { saveResponse, fileFromPath } = await import('../src/node.js')
    const { mkdtempSync, readFileSync, rmSync } = await import('node:fs')
    const { join } = await import('node:path')
    const { tmpdir } = await import('node:os')
    const dir = mkdtempSync(join(tmpdir(), 'staqly-sdk-'))

    try {
      const path = join(dir, 'out.bin')

      expect(await saveResponse(new Response(new Uint8Array([1, 2, 3])), path)).toBe(3)
      expect([...readFileSync(path)]).toEqual([1, 2, 3])
      const f = await fileFromPath(path, { type: 'application/octet-stream' })

      expect(f.name).toBe('out.bin')
      expect(f.size).toBe(3)
    } finally {
      rmSync(dir, { recursive: true })
    }
  })
})
