import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it } from 'vitest'

import { ConnectLinkButton } from '@/components/channels/connect-link-dialog'
import { AddChannelPage } from '@/components/connect/add-channel-page'

import { provider } from './fixtures'
import { mockApi, renderWithProviders } from './utils'

const telegram = provider({
  id: 'telegram',
  name: 'Telegram',
  auth: 'token',
  credential_fields: [{ key: 'bot_token', label: 'Bot token', type: 'secret', required: true } as never],
})
const link = {
  id: 'chl_1',
  note: 'For ACME',
  providers: ['telegram'],
  group_id: '',
  max_uses: 1,
  uses: 0,
  status: 'open',
  created_by: 'Ana',
  created_at: '2026-10-20T10:00:00Z',
  expires_at: '2026-10-27T10:00:00Z',
}

afterEach(() => {
  window.history.replaceState(null, '', '/')
})

describe('connect links: the team', () => {
  it('makes a link with a note, platforms and expiry, shows it once with a copy icon, and revokes open ones', async () => {
    const api = mockApi({
      'GET /v1/providers': { data: [telegram, provider({ id: 'custom', name: 'Your own automation', auth: 'none' })] },
      'GET /v1/channel-groups': {
        data: [{ id: 'grp_d', name: 'Default', default: true, color: '', position: 0, channels: 0 }],
      },
      'GET /v1/channel-links': { data: [link] },
      'POST /v1/channel-links': {
        status: 201,
        body: { ...link, id: 'chl_2', url: 'http://localhost:8080/add-channel#tok' },
      },
      'DELETE /v1/channel-links/chl_1': { status: 204 },
    })
    const user = userEvent.setup()

    renderWithProviders(<ConnectLinkButton />, { mode: 'cloud' })
    await user.click(screen.getByRole('button', { name: 'Invite someone to connect a channel' }))
    await user.type(await screen.findByLabelText('Note for them'), 'For ACME')
    await user.click(screen.getByRole('button', { name: 'Make link' }))
    await waitFor(() => expect(api.find('POST', '/v1/channel-links')).toHaveLength(1))
    expect(api.find('POST', '/v1/channel-links')[0].body).toMatchObject({
      note: 'For ACME',
      expires_in_hours: 168,
      max_uses: 1,
    })
    expect(await screen.findByTestId('connect-link-url')).toHaveValue('http://localhost:8080/add-channel#tok')
    expect(screen.getByRole('button', { name: 'Copy link' })).toBeInTheDocument()
    // The open links, with Revoke.
    await user.click(screen.getByRole('button', { name: 'Revoke the link "For ACME"' }))
    await waitFor(() => expect(api.find('DELETE', '/v1/channel-links/chl_1')).toHaveLength(1))
  })
})

describe('connect links: the page someone else opens', () => {
  it('shows the workspace, the note and the allowed platforms, and connects a token platform', async () => {
    window.location.hash = '#tok_abc'
    const api = mockApi({
      'GET /v1/connect-links/tok_abc': {
        workspace: 'Acme Studio',
        note: 'For ACME',
        providers: [telegram],
        expires_at: link.expires_at,
        uses_left: 1,
      },
      'POST /v1/connect-links/tok_abc/channels': {
        status: 201,
        body: { name: 'ACME news', handle: '@acme', provider: 'telegram' },
      },
    })
    const user = userEvent.setup()

    renderWithProviders(<AddChannelPage />)
    expect(await screen.findByRole('heading', { name: /Connect an account to Acme Studio/ })).toBeInTheDocument()
    expect(screen.getByText('For ACME')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Telegram' }))
    await user.type(await screen.findByLabelText(/Bot token/), '123:abc')
    await user.click(screen.getByTestId('link-connect'))
    expect(await screen.findByTestId('connected')).toHaveTextContent('Connected @acme to Acme Studio')
    expect(api.find('POST', '/v1/connect-links/tok_abc/channels')[0].body).toEqual({
      provider: 'telegram',
      fields: { bot_token: '123:abc' },
    })
  })

  it('a closed link says why', async () => {
    window.location.hash = '#tok_old'
    mockApi({
      'GET /v1/connect-links/tok_old': {
        status: 410,
        body: { error: { code: 'link_expired', message: 'this link expired; ask for a new one' } },
      },
    })

    renderWithProviders(<AddChannelPage />)
    expect(await screen.findByRole('heading', { name: "This link can't be used" })).toBeInTheDocument()
    expect(screen.getByText(/expired/i)).toBeInTheDocument()
  })

  it('back from the platform: connected', async () => {
    window.history.replaceState(null, '', '/add-channel?connected=%40acme&workspace=Acme+Studio#tok_abc')
    mockApi({})

    renderWithProviders(<AddChannelPage />)
    expect(await screen.findByTestId('connected')).toHaveTextContent('Connected @acme to Acme Studio')
  })
})
