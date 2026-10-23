import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { SystemPage } from '@/components/settings/system-page'

import { channel, provider } from './fixtures'
import { mockApi, renderWithProviders } from './utils'

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
