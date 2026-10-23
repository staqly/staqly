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
    const list = await screen.findByTestId('platforms-health')

    expect(within(list).getByText('Instagram')).toBeInTheDocument()
    expect(within(list).getAllByText('Set up on this server')).toHaveLength(2) // not checked for them
    expect(
      within(screen.getByRole('region', { name: 'Platforms' })).queryByRole('button', { name: 'Check now' }),
    ).not.toBeInTheDocument()
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

  it('checks when the page opens, waits out a rate limit once and keeps Check now off until the batch ends', async () => {
    mockApi(routes)
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
    await waitFor(() => expect(check).toHaveBeenCalledTimes(3))
    expect(
      within(screen.getByRole('region', { name: 'Platforms' })).getByRole('button', { name: /Checking/ }),
    ).toBeDisabled()
    finish()
    await waitFor(() => expect(screen.getAllByText('Ready to connect and post')).toHaveLength(2))
    expect(check.mock.calls.map(([id]) => id)).toEqual(['x', 'instagram', 'instagram'])
  })

  it('in the cloud: health and one line per platform set up on the server, no queue or monitoring', async () => {
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

    // Checked on opening: a ready platform says so in one line, a failing one gives the first thing to fix only.
    expect(await within(list).findByText('Ready to connect and post')).toBeInTheDocument()
    expect(await within(list).findByText(/which the internet can't reach/)).toBeInTheDocument()
    expect(within(list).getByText('needs attention')).toBeInTheDocument()
    expect(within(list).queryByText('X accepts the app keys')).not.toBeInTheDocument()
    expect(within(list).queryByText('https://app.example/channels/callback/x')).not.toBeInTheDocument()

    await user.click(
      within(screen.getByRole('region', { name: 'Platforms' })).getByRole('button', { name: 'Check now' }),
    )
    expect(
      await within(screen.getByRole('region', { name: 'Platforms' })).findByRole('button', { name: 'Check now' }),
    ).toBeEnabled()
  })

  it('locally: the queue and monitoring too', async () => {
    mockApi(routes)
    renderWithProviders(<SystemPage />, { mode: 'local' })

    expect(await screen.findByRole('region', { name: 'Queue' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'Monitoring' })).toBeInTheDocument()
  })
})
