import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { SystemPage } from '@/components/settings/system-page'
import { api, ApiError } from '@/lib/api'
import { qk } from '@/lib/queries'

import { channel, provider } from './fixtures'
import { makeClient, mockApi, renderWithProviders, sessionFixture } from './utils'

const routes = {
  'GET /readyz': { status: 'ok', checks: { database: 'ok', migrations: 'ok', workers: 'ok' } },
  'GET /health': { status: 'ok', version: 'dev' },
  'GET /v1/jobs/stats': { queued: 0, running: 0, failed: 0, done_today: 0 },
  'GET /v1/jobs': { data: [] },
  'GET /v1/providers': {
    data: [
      provider(),
      provider({ id: 'instagram', name: 'Instagram' }),
      provider({ id: 'tiktok', name: 'TikTok', status: 'soon' }),
    ],
  },
  'GET /v1/channels': {
    data: [
      channel(),
      channel({
        id: 'ch_2',
        name: 'Brand on Instagram',
        provider: 'instagram',
        status: 'needs_reconnect',
        last_error: 'Token revoked',
      }),
    ],
  },
  'POST /v1/channels/ch_1/test': { ok: true, profile: { handle: '@brand' } },
  'POST /v1/channels/ch_2/test': { ok: false, error: 'Instagram no longer accepts the sign-in' },
}

describe('system page', () => {
  afterEach(() => vi.restoreAllMocks())

  it.each(['editor', 'viewer'])('keeps channel diagnostics read-only for %s', async (role) => {
    mockApi(routes)
    const client = makeClient()

    client.setQueryData(qk.session, { ...sessionFixture('cloud'), role })
    renderWithProviders(<SystemPage />, { client })
    expect(await screen.findByText('Token revoked')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^Test/ })).not.toBeInTheDocument()
  })

  it('shows a retry when platforms cannot be loaded', async () => {
    const user = userEvent.setup()
    let failed = true

    mockApi({
      ...routes,
      'GET /v1/providers': () =>
        failed ? { status: 503, body: { error: { message: 'Unavailable' } } } : routes['GET /v1/providers'],
    })
    renderWithProviders(<SystemPage />, { mode: 'cloud' })
    expect(await screen.findByText('Could not load platforms')).toBeInTheDocument()
    expect(screen.queryByText(/No platform has its app keys/)).not.toBeInTheDocument()
    failed = false
    await user.click(screen.getByRole('button', { name: 'Try again' }))
    expect(await screen.findByRole('list', { name: 'Platforms ready to connect' })).toBeInTheDocument()
  })

  it('retries a rate-limited batch test and disables other tests until the batch ends', async () => {
    mockApi(routes)
    const user = userEvent.setup()
    let finish!: () => void
    const pending = new Promise<{ ok: boolean; profile: { handle: string } }>((resolve) => {
      finish = () => resolve({ ok: true, profile: { handle: '@second' } })
    })
    const check = vi
      .spyOn(api, 'testChannel')
      .mockResolvedValueOnce({ ok: true, profile: { handle: '@first' } })
      .mockRejectedValueOnce(new ApiError(429, { message: 'Rate limited' }, undefined, 0.001))
      .mockReturnValueOnce(pending)

    renderWithProviders(<SystemPage />, { mode: 'cloud' })
    await user.click(await screen.findByRole('button', { name: 'Test all' }))
    await waitFor(() => expect(check).toHaveBeenCalledTimes(3))
    expect(screen.getByRole('button', { name: 'Test Brand on Instagram' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Test Brand on Mastodon' })).toBeDisabled()
    finish()
    expect(await screen.findByText('Works, signed in as @second')).toBeInTheDocument()
    expect(check.mock.calls.map(([id]) => id)).toEqual(['ch_1', 'ch_2', 'ch_2'])
  })

  it('in the cloud: health and channels with a test each, no queue or monitoring', async () => {
    mockApi(routes)
    const user = userEvent.setup()

    renderWithProviders(<SystemPage />, { mode: 'cloud' })
    const card = await screen.findByTestId('channels-health')
    const ready = await within(card).findByRole('list', { name: 'Platforms ready to connect' })

    expect(await within(ready).findByText('Instagram')).toBeInTheDocument()
    expect(within(ready).queryByText('TikTok')).not.toBeInTheDocument() // not set up on this server
    expect(await within(card).findByText('Token revoked')).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Queue' })).not.toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Monitoring' })).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Test all' }))
    expect(await within(card).findByText('Works, signed in as @brand')).toBeInTheDocument()
    expect(await within(card).findByText('Instagram no longer accepts the sign-in')).toBeInTheDocument()
  })

  it('locally: the queue and monitoring too', async () => {
    mockApi(routes)
    renderWithProviders(<SystemPage />, { mode: 'local' })

    expect(await screen.findByRole('region', { name: 'Queue' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'Monitoring' })).toBeInTheDocument()
  })
})
