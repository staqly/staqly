import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { SystemPage } from '@/components/settings/system-page'
import { api, ApiError, type ProviderCheck } from '@/lib/api'
import { qk } from '@/lib/queries'

import { provider } from './fixtures'
import { makeClient, mockApi, renderWithProviders, sessionFixture } from './utils'

const routes = {
  'GET /readyz': { status: 'ok', checks: { database: 'ok', migrations: 'ok', workers: 'ok' } },
  'GET /health': { status: 'ok', version: 'dev' },
  'GET /v1/jobs/stats': { queued: 0, running: 0, failed: 0, done_today: 0 },
  'GET /v1/jobs': { data: [] },
  'GET /v1/providers': {
    data: [
      provider({ id: 'x', name: 'X' }),
      provider({ id: 'instagram', name: 'Instagram' }),
      provider({ id: 'linkedin', name: 'LinkedIn', app_configured: false, status: 'soon' }),
      provider({ id: 'custom', name: 'Your own automation', auth: 'none' }),
    ],
  },
  'POST /v1/providers/x/check': {
    provider: 'x',
    ok: true,
    checks: [
      { key: 'keys', label: 'App keys', ok: true, detail: 'Set' },
      { key: 'platform', label: 'X', ok: true, detail: 'X accepts the app keys' },
      { key: 'return_address', label: 'Return address', ok: true, detail: 'https://app.example/channels/callback/x' },
    ],
  },
  'POST /v1/providers/instagram/check': {
    provider: 'instagram',
    ok: false,
    checks: [
      { key: 'keys', label: 'App keys', ok: true, detail: 'Set' },
      {
        key: 'media',
        label: 'Media storage',
        ok: false,
        detail:
          "Instagram downloads pictures and videos from a link, and media links point at maxiofs:8080, which the internet can't reach",
      },
    ],
  },
}

describe('system page', () => {
  afterEach(() => vi.restoreAllMocks())

  it.each(['editor', 'viewer'])('shows the platforms read-only to %s', async (role) => {
    mockApi(routes)
    const client = makeClient()

    client.setQueryData(qk.session, { ...sessionFixture('cloud'), role })
    renderWithProviders(<SystemPage />, { client })
    expect(await within(await screen.findByTestId('platforms-health')).findByText('Instagram')).toBeInTheDocument()
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
    failed = false
    await user.click(screen.getByRole('button', { name: 'Try again' }))
    expect(await screen.findByTestId('platforms-health')).toBeInTheDocument()
  })

  it('waits out a rate limit once in Test all and keeps the other tests off until the batch ends', async () => {
    mockApi(routes)
    const user = userEvent.setup()
    const ready = routes['POST /v1/providers/x/check'] as ProviderCheck
    let finish!: () => void
    const pending = new Promise<ProviderCheck>((resolve) => {
      finish = () => resolve({ ...ready, provider: 'instagram' })
    })
    const check = vi
      .spyOn(api, 'checkProvider')
      .mockResolvedValueOnce(ready)
      .mockRejectedValueOnce(new ApiError(429, { message: 'Rate limited' }, undefined, 0.001))
      .mockReturnValueOnce(pending)

    renderWithProviders(<SystemPage />, { mode: 'cloud' })
    await user.click(await screen.findByRole('button', { name: 'Test all' }))
    await waitFor(() => expect(check).toHaveBeenCalledTimes(3))
    expect(screen.getByRole('button', { name: 'Test X' })).toBeDisabled()
    finish()
    await waitFor(() => expect(screen.getAllByText('Ready')).toHaveLength(2))
    expect(check.mock.calls.map(([id]) => id)).toEqual(['x', 'instagram', 'instagram'])
  })

  it('in the cloud: health and each platform set up on the server with a test, no queue or monitoring', async () => {
    mockApi(routes)
    const user = userEvent.setup()

    renderWithProviders(<SystemPage />, { mode: 'cloud' })
    const list = await screen.findByTestId('platforms-health')

    // Only platforms connected through their sign-in and set up here: not LinkedIn (no keys), not your own automation.
    expect(within(list).getByText('Instagram')).toBeInTheDocument()
    expect(within(list).queryByText('LinkedIn')).not.toBeInTheDocument()
    expect(within(list).queryByText('Your own automation')).not.toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Queue' })).not.toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Monitoring' })).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Test all' }))
    expect(await within(list).findByText('X accepts the app keys')).toBeInTheDocument()
    expect(within(list).getByText('https://app.example/channels/callback/x')).toBeInTheDocument()
    expect(await within(list).findByText(/which the internet can't reach/)).toBeInTheDocument()
    expect(within(list).getByText('Needs attention')).toBeInTheDocument()
  })

  it('locally: the queue and monitoring too', async () => {
    mockApi(routes)
    renderWithProviders(<SystemPage />, { mode: 'local' })

    expect(await screen.findByRole('region', { name: 'Queue' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'Monitoring' })).toBeInTheDocument()
  })
})
