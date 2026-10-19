import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { ChannelsPage } from '@/components/channels/channels-page'

import { channel, provider } from './fixtures'
import { mockApi, renderWithProviders } from './utils'

const telegram = provider({
  id: 'telegram',
  name: 'Telegram',
  auth: 'token',
  credential_fields: [
    { key: 'bot_token', label: 'Bot token', kind: 'secret', required: true, help: 'From @BotFather' },
    { key: 'chat_id', label: 'Chat or channel', kind: 'text', required: true },
  ],
  settings_fields: [],
})
const soon = provider({ id: 'tiktok', name: 'TikTok', status: 'soon' })

describe('channels', () => {
  it('lists connected channels by platform with their state, and the empty state offers Add channel', async () => {
    mockApi({
      'GET /v1/channels': {
        data: [
          channel(),
          channel({
            id: 'ch_2',
            name: 'News bot',
            provider: 'telegram',
            status: 'needs_reconnect',
            last_error: 'Token revoked',
          }),
        ],
      },
      'GET /v1/providers': { data: [provider(), telegram, soon] },
    })
    renderWithProviders(<ChannelsPage />, { mode: 'local' })

    expect(await screen.findByRole('heading', { level: 1, name: 'Channels' })).toBeInTheDocument()
    const mastodon = await screen.findByTestId('channels-mastodon')

    expect(within(mastodon).getByText('Brand on Mastodon')).toBeInTheDocument()
    expect(within(mastodon).getByText('Connected')).toBeInTheDocument()
    const tg = screen.getByTestId('channels-telegram')

    expect(within(tg).getByText('Needs reconnect')).toBeInTheDocument()
    expect(within(tg).getByText('Token revoked')).toBeInTheDocument()
    expect(within(tg).getByRole('button', { name: 'Reconnect' })).toBeInTheDocument()
  })

  it('Test connection names the channel and says whether it works', async () => {
    let ok = true

    mockApi({
      'GET /v1/channels': { data: [channel()] },
      'GET /v1/providers': { data: [provider()] },
      'POST /v1/channels/ch_1/test': () =>
        ok ? { ok: true, profile: { handle: '@brand' } } : { ok: false, error: 'Token revoked' },
    })
    const user = userEvent.setup()

    renderWithProviders(<ChannelsPage />, { mode: 'local' })
    await user.click(await screen.findByRole('button', { name: 'Actions for Brand on Mastodon' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Test connection' }))
    expect(await screen.findByText('“Brand on Mastodon” works')).toBeInTheDocument()
    ok = false
    await user.click(screen.getByRole('button', { name: 'Actions for Brand on Mastodon' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Test connection' }))
    expect(await screen.findByText('“Brand on Mastodon” needs to be reconnected')).toBeInTheDocument()
    expect(screen.getByText('Token revoked')).toBeInTheDocument()
  })

  it('Add channel shows every platform (coming-soon ones greyed) and connects a token provider with its fields', async () => {
    const api = mockApi({
      'GET /v1/channels': { data: [] },
      'GET /v1/providers': { data: [provider(), telegram, soon] },
      'POST /v1/channels': channel({ id: 'ch_9', provider: 'telegram', name: 'News bot', handle: '@newsbot' }),
    })
    const user = userEvent.setup()

    renderWithProviders(<ChannelsPage />, { mode: 'local' })
    await user.click(await screen.findByTestId('add-channel'))
    const sheet = await screen.findByRole('dialog', { name: 'Add channel' })

    expect(within(sheet).getByRole('button', { name: 'TikTok, coming soon' })).toBeDisabled()
    await user.click(within(sheet).getByRole('button', { name: 'Telegram' }))
    expect(await screen.findByRole('dialog', { name: /Connect Telegram/ })).toBeInTheDocument()
    await user.type(screen.getByLabelText(/Bot token/), '123:abc')
    await user.type(screen.getByLabelText(/Chat or channel/), '@newsbot')
    await user.type(screen.getByLabelText(/^Name/), 'News bot')
    await user.click(screen.getByTestId('connect-channel'))
    await waitFor(() => expect(api.find('POST', '/v1/channels')).toHaveLength(1))
    expect(api.find('POST', '/v1/channels')[0].body).toEqual({
      provider: 'telegram',
      name: 'News bot',
      fields: { bot_token: '123:abc', chat_id: '@newsbot' },
    })
    expect(await screen.findAllByText(/is connected/)).not.toHaveLength(0)
  })

  it('your own automation: edit the endpoint URL, method and headers of a connected channel', async () => {
    const custom = provider({
      id: 'custom',
      name: 'Your own automation',
      auth: 'none',
      credential_fields: [
        { key: 'url', label: 'Endpoint URL', kind: 'url', required: false },
        {
          key: 'method',
          label: 'Method',
          kind: 'select',
          required: false,
          default: 'POST',
          options: ['POST', 'PUT', 'PATCH', 'GET'].map((m) => ({ value: m, label: m })),
        },
        { key: 'headers', label: 'Headers', kind: 'textarea', required: false },
      ],
      settings_fields: [],
    })
    const api = mockApi({
      'GET /v1/channels': { data: [channel({ id: 'ch_c', provider: 'custom', name: 'My n8n' })] },
      'GET /v1/providers': { data: [custom] },
      'PATCH /v1/channels/ch_c': channel({ id: 'ch_c', provider: 'custom', name: 'My n8n', handle: 'n8n.test' }),
    })
    const user = userEvent.setup()

    renderWithProviders(<ChannelsPage />, { mode: 'local' })
    await user.click(await screen.findByRole('button', { name: 'Actions for My n8n' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Edit connection' }))
    expect(await screen.findByRole('dialog', { name: /Edit My n8n/ })).toBeInTheDocument()
    expect(screen.getByRole('combobox', { name: 'Method' })).toHaveTextContent('POST')
    await user.type(screen.getByLabelText(/Endpoint URL/), 'https://n8n.test/webhook/staqly')
    await user.type(screen.getByLabelText(/Headers/), 'Authorization: Bearer t0k')
    await user.click(screen.getByTestId('connect-channel'))
    await waitFor(() => expect(api.find('PATCH', '/v1/channels/ch_c')).toHaveLength(1))
    // Editing a working channel only sends the fields; it does not touch its status.
    expect(api.find('PATCH', '/v1/channels/ch_c')[0].body).toEqual({
      fields: { url: 'https://n8n.test/webhook/staqly', method: 'POST', headers: 'Authorization: Bearer t0k' },
    })
    expect(await screen.findByText('“My n8n” was updated')).toBeInTheDocument()
  })

  it('an OAuth platform without a developer app asks for the app keys first in local mode', async () => {
    const api = mockApi({
      'GET /v1/channels': { data: [] },
      'GET /v1/providers': { data: [provider({ app_configured: false })] },
      'PUT /v1/providers/mastodon/app': provider({ app_configured: true }),
    })
    const user = userEvent.setup()

    renderWithProviders(<ChannelsPage />, { mode: 'local' })
    await user.click(await screen.findByTestId('add-channel'))
    await user.click(await screen.findByRole('button', { name: 'Mastodon' }))
    expect(await screen.findByText(/needs your own app first/)).toBeInTheDocument()
    await user.type(screen.getByLabelText(/Client ID/), 'id-1')
    await user.type(screen.getByLabelText(/Client secret/), 'secret-1')
    await user.click(screen.getByRole('button', { name: 'Save app' }))
    await waitFor(() => expect(api.find('PUT', '/v1/providers/mastodon/app')).toHaveLength(1))
    expect(api.find('PUT', '/v1/providers/mastodon/app')[0].body).toEqual({
      client_id: 'id-1',
      client_secret: 'secret-1',
    })
    expect(await screen.findByTestId('oauth-start')).toHaveTextContent('Connect with Mastodon')
  })
})
