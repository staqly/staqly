import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api, ApiError, errorMessage, getCsrfToken, onUnauthorized, setCsrfToken } from '@/lib/api'

import { mockApi, sessionFixture } from './utils'

const WORKSPACE = sessionFixture('cloud').workspace

function refused(status: number, code: string, message: string, extra: Record<string, unknown> = {}) {
  return { status, body: { error: { code, message, ...extra } } }
}

/** What the call threw, as the ApiError every caller gets. */
async function failure(call: Promise<unknown>): Promise<ApiError> {
  const err = await call.then(
    () => undefined,
    (e: unknown) => e,
  )

  expect(err).toBeInstanceOf(ApiError)

  return err as ApiError
}

beforeEach(() => setCsrfToken('csrf-test'))

afterEach(() => {
  onUnauthorized(null)
  setCsrfToken(null)
})

describe('the CSRF token', () => {
  it('goes with every write and with no read', async () => {
    const mocked = mockApi({ 'GET /v1/workspace': WORKSPACE, 'PATCH /v1/workspace': WORKSPACE })

    await api.workspace()
    await api.renameWorkspace('Acme')

    expect(mocked.find('GET', '/v1/workspace')[0]!.headers).not.toHaveProperty('X-CSRF-Token')
    expect(mocked.find('PATCH', '/v1/workspace')[0]!.headers).toMatchObject({ 'X-CSRF-Token': 'csrf-test' })
    expect(mocked.find('PATCH', '/v1/workspace')[0]!.body).toEqual({ name: 'Acme' })
  })

  it('is read from the session before the first write, once for writes started together', async () => {
    setCsrfToken(null)
    const mocked = mockApi({
      'GET /v1/session': { ...sessionFixture('cloud'), csrf_token: 'from-session' },
      'PATCH /v1/workspace': WORKSPACE,
    })

    await Promise.all([api.renameWorkspace('One'), api.renameWorkspace('Two')])

    expect(mocked.find('GET', '/v1/session')).toHaveLength(1)
    expect(mocked.find('PATCH', '/v1/workspace').map((c) => c.headers['X-CSRF-Token'])).toEqual([
      'from-session',
      'from-session',
    ])
  })

  it('a local install has none: nothing is sent and the session is not asked for one', async () => {
    setCsrfToken('')
    const mocked = mockApi({ 'PATCH /v1/workspace': WORKSPACE })

    await api.renameWorkspace('Acme')

    expect(mocked.find('PATCH', '/v1/workspace')[0]!.headers).not.toHaveProperty('X-CSRF-Token')
    expect(mocked.find('GET', '/v1/session')).toHaveLength(0)
  })

  it('a token the server refuses (signed in again in another tab) is read again and the write repeated', async () => {
    setCsrfToken('stale')
    const mocked = mockApi({
      'GET /v1/session': { ...sessionFixture('cloud'), csrf_token: 'fresh' },
      'PATCH /v1/workspace': (call: { headers: Record<string, string> }) =>
        call.headers['X-CSRF-Token'] === 'fresh' ? WORKSPACE : refused(403, 'csrf_failed', 'CSRF token mismatch'),
    })

    await expect(api.renameWorkspace('Acme')).resolves.toMatchObject({ name: WORKSPACE.name })
    expect(mocked.find('PATCH', '/v1/workspace').map((c) => c.headers['X-CSRF-Token'])).toEqual(['stale', 'fresh'])
  })

  it('a token refused twice fails: the write is repeated once, never in a loop', async () => {
    const mocked = mockApi({
      'GET /v1/session': sessionFixture('cloud'),
      'PATCH /v1/workspace': refused(403, 'csrf_failed', 'CSRF token mismatch'),
    })

    const err = await failure(api.renameWorkspace('Acme'))

    expect(err.code).toBe('csrf_failed')
    expect(mocked.find('PATCH', '/v1/workspace')).toHaveLength(2)
  })

  it('any other 403 (a role that may not do it) is not repeated', async () => {
    const mocked = mockApi({ 'PATCH /v1/workspace': refused(403, 'forbidden', 'missing scope admin') })

    const err = await failure(api.renameWorkspace('Acme'))

    expect(err.status).toBe(403)
    expect(mocked.find('PATCH', '/v1/workspace')).toHaveLength(1)
  })
})

describe('a session that ran out (401)', () => {
  it('tells the app, which signs in again, and forgets the CSRF token of the old session', async () => {
    const signIn = vi.fn<(err: ApiError) => void>()

    onUnauthorized(signIn)
    mockApi({ 'GET /v1/workspace': refused(401, 'unauthorized', 'sign in first') })

    const err = await failure(api.workspace())

    expect(err.isUnauthorized).toBe(true)
    expect(signIn).toHaveBeenCalledTimes(1)
    expect(signIn).toHaveBeenCalledWith(err)
    expect(getCsrfToken()).toBeNull()
  })

  it('a public call (an invitation or preview link opened signed out) fails without sending anyone to sign-in', async () => {
    const signIn = vi.fn<(err: ApiError) => void>()

    onUnauthorized(signIn)
    mockApi({ 'GET /v1/invitations/lookup': refused(401, 'invalid_link', 'this link is not valid') })

    const err = await failure(api.lookupInvitation('tok'))

    expect(err.code).toBe('invalid_link')
    expect(signIn).not.toHaveBeenCalled()
    expect(getCsrfToken()).toBe('csrf-test')
  })

  it('other refusals leave the session alone', async () => {
    const signIn = vi.fn<(err: ApiError) => void>()

    onUnauthorized(signIn)
    mockApi({ 'GET /v1/workspace': refused(403, 'forbidden', 'missing scope admin') })

    await failure(api.workspace())

    expect(signIn).not.toHaveBeenCalled()
    expect(getCsrfToken()).toBe('csrf-test')
  })
})

describe('refusals the app treats in their own way', () => {
  it('402 is a plan limit and carries which limit, how much and on which plan', async () => {
    mockApi({
      'PATCH /v1/workspace': refused(402, 'plan_limit_exceeded', 'plan limit reached', {
        details: { limit: 'channels', max: 3, used: 3, plan: 'starter' },
      }),
    })

    const err = await failure(api.renameWorkspace('Acme'))

    expect(err.isPlanLimit).toBe(true)
    expect(err.planLimit).toEqual({ limit: 'channels', max: 3, used: 3, plan: 'starter' })
    expect(err.isRateLimited).toBe(false)
  })

  it('409 is a conflict with its code and details; 422 carries the fields', async () => {
    mockApi({
      'PATCH /v1/workspace': refused(409, 'post_changed', 'the post was changed', { details: { version: 4 } }),
      'GET /v1/workspace': refused(422, 'validation_failed', 'invalid input', { fields: { name: 'is required' } }),
    })

    const conflict = await failure(api.renameWorkspace('Acme'))

    expect(conflict.isConflict).toBe(true)
    expect(conflict.code).toBe('post_changed')
    expect(conflict.details).toEqual({ version: 4 })
    expect(conflict.planLimit).toBeUndefined()

    const invalid = await failure(api.workspace())

    expect(invalid.fields).toEqual({ name: 'is required' })
    expect(errorMessage(invalid)).toBe('Invalid input: name is required')
  })

  it('429 says how long to wait when the server names it, and "in a moment" when it does not', async () => {
    let retryAfter: string | undefined = '12'

    mockApi({
      'GET /v1/workspace': () => ({
        ...refused(429, 'rate_limited', 'too many requests'),
        headers: retryAfter ? { 'Retry-After': retryAfter } : undefined,
      }),
    })

    const timed = await failure(api.workspace())

    expect(timed.isRateLimited).toBe(true)
    expect(timed.isPlanLimit).toBe(false)
    expect(timed.retryAfter).toBe(12)
    expect(errorMessage(timed)).toBe('Too many requests. Try again in 12s.')

    retryAfter = undefined
    expect(errorMessage(await failure(api.workspace()))).toBe('Too many requests. Try again in a moment.')
  })
})

describe('answers that are not our JSON', () => {
  it('an empty 204 is no value', async () => {
    mockApi({ 'DELETE /v1/members/usr_1': { status: 204 } })

    await expect(api.removeMember('usr_1')).resolves.toBeUndefined()
  })

  it("a proxy's short text is the message; an error without a body names its status", async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response('upload too large for this server', { status: 413 })),
    )

    const text = await failure(api.workspace())

    expect(text.message).toBe('upload too large for this server')
    expect(text.code).toBe('http_413')

    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response(null, { status: 500 })),
    )

    expect((await failure(api.workspace())).message).toBe('Request failed (500)')
  })

  it('a request that never reaches the server says so, as status 0', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => {
        throw new TypeError('Failed to fetch')
      }),
    )

    const err = await failure(api.renameWorkspace('Acme'))

    expect(err.status).toBe(0)
    expect(err.code).toBe('network_error')
    expect(err.message).toBe('Cannot reach the staqly API. Is the server running?')
  })
})
