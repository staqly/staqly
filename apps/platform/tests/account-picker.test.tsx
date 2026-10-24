import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it } from 'vitest'

import { OAuthCallback } from '@/components/channels/oauth-callback'
import { AddChannelPage } from '@/components/connect/add-channel-page'

import { provider } from './fixtures'
import { nav } from './navigation'
import { mockApi, renderWithProviders } from './utils'

const facebook = provider({ id: 'facebook', name: 'Facebook Page', auth: 'oauth2' })
const pending = {
  id: 'pc_1',
  provider: 'facebook',
  expires_at: '2026-10-23T10:10:00Z',
  accounts: [
    { id: '101', name: 'Jane Doe Bakery', handle: '@janedoebakery' },
    { id: '102', name: 'Jane Doe Studio', handle: '@janedoestudio' },
  ],
}

afterEach(() => {
  nav.reset()
  window.history.replaceState(null, '', '/')
})

describe('picking accounts after a sign-in', () => {
  it('lists the accounts, ticks none, and connects the picked ones as channels', async () => {
    nav.set('/channels/callback/facebook', 'pending=pc_1')
    const api = mockApi({
      'GET /v1/providers': { data: [facebook] },
      'GET /v1/pending-connections/pc_1': pending,
      'POST /v1/pending-connections/pc_1': { status: 201, body: { data: [{ id: 'ch_9', provider: 'facebook' }] } },
    })
    const user = userEvent.setup()

    renderWithProviders(<OAuthCallback />)
    expect(
      await screen.findByRole('heading', { name: 'Pick the Facebook Page accounts to connect' }),
    ).toBeInTheDocument()
    const connect = screen.getByTestId('accounts-connect')

    expect(connect).toBeDisabled()
    await user.click(screen.getByRole('checkbox', { name: /Jane Doe Studio/ }))
    expect(connect).toHaveTextContent('Connect account')
    await user.click(connect)
    await waitFor(() => expect(api.find('POST', '/v1/pending-connections/pc_1')).toHaveLength(1))
    expect(api.find('POST', '/v1/pending-connections/pc_1')[0].body).toEqual({ account_ids: ['102'] })
    await waitFor(() => expect(nav.replace).toHaveBeenCalledWith('/channels?connected=ch_9', undefined))
  })

  it('a sign-in that is no longer waiting lands on Channels with the reason', async () => {
    nav.set('/channels/callback/facebook', 'pending=pc_old')
    mockApi({
      'GET /v1/providers': { data: [facebook] },
      'GET /v1/pending-connections/pc_old': {
        status: 404,
        body: { error: { code: 'not_found', message: 'this sign-in is not waiting any more' } },
      },
    })

    renderWithProviders(<OAuthCallback />)
    await waitFor(() => expect(nav.replace).toHaveBeenCalledWith(expect.stringContaining('connect_error='), undefined))
  })

  it('on a connect link page: the customer picks, every account is connected', async () => {
    window.history.replaceState(null, '', '/add-channel?pending=pc_2&workspace=Acme+Studio#tok_abc')
    const api = mockApi({
      'GET /v1/connect-links/tok_abc': {
        workspace: 'Acme Studio',
        note: '',
        providers: [facebook],
        expires_at: '2026-10-27T10:00:00Z',
        uses_left: 2,
      },
      'GET /v1/connect-links/tok_abc/pending/pc_2': { ...pending, id: 'pc_2' },
      'POST /v1/connect-links/tok_abc/pending/pc_2': {
        status: 201,
        body: {
          data: [
            { name: 'Jane Doe Bakery', handle: '@janedoebakery', provider: 'facebook' },
            { name: 'Jane Doe Studio', handle: '@janedoestudio', provider: 'facebook' },
          ],
        },
      },
    })
    const user = userEvent.setup()

    renderWithProviders(<AddChannelPage />)
    await user.click(await screen.findByRole('checkbox', { name: /Jane Doe Bakery/ }))
    await user.click(screen.getByRole('checkbox', { name: /Jane Doe Studio/ }))
    await user.click(screen.getByRole('button', { name: /Connect 2 accounts/ }))
    expect(await screen.findByTestId('connected')).toHaveTextContent(
      'Connected @janedoebakery, @janedoestudio to Acme Studio',
    )
    expect(api.find('POST', '/v1/connect-links/tok_abc/pending/pc_2')[0].body).toEqual({
      account_ids: ['101', '102'],
    })
  })

  it('on a connect link page: a sign-in with no accounts says so and goes back to the platforms', async () => {
    window.history.replaceState(null, '', '/add-channel?pending=pc_3#tok_abc')
    mockApi({
      'GET /v1/connect-links/tok_abc': {
        workspace: 'Acme Studio',
        note: '',
        providers: [facebook],
        expires_at: '2026-10-27T10:00:00Z',
        uses_left: 2,
      },
      'GET /v1/connect-links/tok_abc/pending/pc_3': { ...pending, id: 'pc_3', accounts: [] },
    })
    const user = userEvent.setup()

    renderWithProviders(<AddChannelPage />)
    expect(await screen.findByText(/no Facebook Page accounts to post as/)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(window.location.search).toBe('')
    expect(window.location.hash).toBe('#tok_abc')
    expect(await screen.findByRole('button', { name: /Facebook Page/ })).toBeInTheDocument()
  })
})
