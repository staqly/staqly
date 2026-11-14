import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { adminApi, AdminApiError, errorText } from './api'

const json = (status: number, body?: unknown) =>
  new Response(body === undefined ? null : JSON.stringify(body), { status })
const err = (status: number, code: string) => json(status, { error: { code, message: code } })

let fetchMock: ReturnType<typeof vi.fn>

beforeEach(() => {
  fetchMock = vi.fn()
  vi.stubGlobal('fetch', fetchMock)
  vi.stubGlobal('window', { location: { pathname: '/', assign: vi.fn() } })
})

afterEach(async () => {
  // a 401 forgets the cached token
  fetchMock.mockResolvedValueOnce(err(401, 'unauthorized'))
  await adminApi.get('/v1/admin/x').catch(() => {})
  vi.unstubAllGlobals()
})

const headerOf = (call: unknown[]) => (call[1] as RequestInit).headers as Record<string, string>

describe('adminApi', () => {
  it('sends the CSRF token from /me on POST', async () => {
    fetchMock
      .mockResolvedValueOnce(json(200, { email: 'a@b.c', csrf_token: 'T1' }))
      .mockResolvedValueOnce(json(200, { ok: 1 }))

    await expect(adminApi.send('POST', '/v1/admin/x', { a: 1 })).resolves.toEqual({ ok: 1 })
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/admin/me')
    expect(fetchMock.mock.calls[1][0]).toBe('/api/v1/admin/x')
    expect(headerOf(fetchMock.mock.calls[1])['X-CSRF-Token']).toBe('T1')
    expect(headerOf(fetchMock.mock.calls[0])['X-CSRF-Token']).toBeUndefined()
  })

  it('refetches the token once on csrf_failed', async () => {
    fetchMock
      .mockResolvedValueOnce(json(200, { csrf_token: 'old' }))
      .mockResolvedValueOnce(err(403, 'csrf_failed'))
      .mockResolvedValueOnce(json(200, { csrf_token: 'new' }))
      .mockResolvedValueOnce(json(200, { ok: 2 }))

    await expect(adminApi.send('DELETE', '/v1/admin/x')).resolves.toEqual({ ok: 2 })
    expect(headerOf(fetchMock.mock.calls[3])['X-CSRF-Token']).toBe('new')
  })

  it('gives up after a second csrf_failed', async () => {
    fetchMock
      .mockResolvedValueOnce(json(200, { csrf_token: 'a' }))
      .mockResolvedValueOnce(err(403, 'csrf_failed'))
      .mockResolvedValueOnce(json(200, { csrf_token: 'b' }))
      .mockResolvedValueOnce(err(403, 'csrf_failed'))

    await expect(adminApi.send('POST', '/v1/admin/x')).rejects.toMatchObject({
      status: 403,
      code: 'csrf_failed',
    })
  })

  it('redirects to /sign-in on 401 and throws', async () => {
    fetchMock.mockResolvedValueOnce(err(401, 'unauthorized'))

    await expect(adminApi.get('/v1/admin/overview')).rejects.toMatchObject({
      status: 401,
      code: 'unauthorized',
    })
    expect(window.location.assign).toHaveBeenCalledWith('/sign-in')
  })

  it('does not redirect when already on /sign-in', async () => {
    vi.stubGlobal('window', { location: { pathname: '/sign-in', assign: vi.fn() } })
    fetchMock.mockResolvedValueOnce(err(401, 'unauthorized'))

    await expect(adminApi.get('/v1/admin/me')).rejects.toBeInstanceOf(AdminApiError)
    expect(window.location.assign).not.toHaveBeenCalled()
  })

  it('returns undefined on 204 and maps the error envelope', async () => {
    fetchMock
      .mockResolvedValueOnce(json(200, { csrf_token: 't' }))
      .mockResolvedValueOnce(new Response(null, { status: 204 }))
    await expect(adminApi.send('POST', '/v1/admin/x')).resolves.toBeUndefined()

    fetchMock.mockResolvedValueOnce(json(400, { error: { code: 'invalid_request', message: 'bad' } }))
    await expect(adminApi.get('/v1/admin/x')).rejects.toMatchObject({
      status: 400,
      code: 'invalid_request',
      message: 'bad',
    })
  })
})

describe('validation errors and sign-in', () => {
  it('keeps the 422 fields and prints them', async () => {
    fetchMock.mockResolvedValueOnce(
      json(422, { error: { code: 'validation_failed', message: 'plan: x', fields: { plan: 'unknown plan' } } }),
    )

    const e = await adminApi.get('/v1/admin/x').catch((x: unknown) => x)

    expect(e).toMatchObject({ status: 422, fields: { plan: 'unknown plan' } })
    expect(errorText(e as Error)).toBe('plan: unknown plan')
    expect(errorText(new Error('boom'))).toBe('boom')
  })

  it('anon posts without asking /me for a CSRF token', async () => {
    fetchMock.mockResolvedValueOnce(new Response(null, { status: 204 }))

    await adminApi.anon('/v1/admin/auth/code', { email: 'a@b.c' })
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/admin/auth/code')
    expect(headerOf(fetchMock.mock.calls[0])['X-CSRF-Token']).toBeUndefined()
  })
})
