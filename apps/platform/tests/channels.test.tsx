import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { ConnectForm } from '@/components/channels/add-channel-sheet'
import { ChannelsPage } from '@/components/channels/channels-page'

import { channel, group, provider } from './fixtures'
import { nav } from './navigation'
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
  it('a connection that came back with an error shows a modal over Channels with the platform, details and Try again', async () => {
    const api = mockApi({
      'GET /v1/channels': { data: [] },
      'GET /v1/providers': { data: [provider()] },
      'GET /v1/channels/oauth/mastodon/start': { url: 'https://mastodon.example/authorize' },
    })
    const user = userEvent.setup()

    nav.set('/channels', 'connect_error=Mastodon refused the sign-in (invalid_grant)&provider=mastodon')
    renderWithProviders(<ChannelsPage />, { mode: 'local' })
    const dialog = await screen.findByRole('dialog', { name: "Mastodon wasn't connected" })

    expect(within(dialog).getByText('Mastodon refused the sign-in')).toBeInTheDocument()
    expect(within(dialog).getByText('invalid_grant')).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: 'Try again' }))
    await waitFor(() => expect(api.find('GET', '/v1/channels/oauth/mastodon/start')).toHaveLength(1))
    await user.click(within(dialog).getByRole('button', { name: 'Back to Channels' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

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

  it('an OAuth platform without its app keys is a disabled coming soon tile', async () => {
    mockApi({
      'GET /v1/channels': { data: [] },
      'GET /v1/providers': { data: [provider({ app_configured: false, status: 'soon' })] },
    })
    const user = userEvent.setup()

    renderWithProviders(<ChannelsPage />, { mode: 'cloud' })
    await user.click(await screen.findByTestId('add-channel'))
    expect(await screen.findByRole('button', { name: 'Mastodon, coming soon' })).toBeDisabled()
  })

  it('a row shows its last and next post and the 30-day count, or No posts yet', async () => {
    const hour = 3600_000

    mockApi({
      'GET /v1/channels': {
        data: [
          channel({
            activity: {
              last_published_at: new Date(Date.now() - 2 * hour).toISOString(),
              last_post_id: 'post_last',
              next_publish_at: new Date(Date.now() + 26 * hour).toISOString(),
              published_30d: 4,
            },
          }),
          channel({
            id: 'ch_2',
            name: 'News bot',
            provider: 'telegram',
            activity: { last_published_at: null, last_post_id: null, next_publish_at: null, published_30d: 0 },
          }),
        ],
      },
      'GET /v1/providers': { data: [provider(), telegram] },
      'GET /v1/channel-groups': { data: [group()] },
    })
    renderWithProviders(<ChannelsPage />, { mode: 'local' })
    const rows = await screen.findAllByTestId('channel-row')
    const brand = rows[0]!

    expect(within(brand).getByRole('link', { name: '2 hours ago' })).toHaveAttribute('href', '/posts/post_last')
    expect(within(brand).getByText(/^Next post /)).toBeInTheDocument()
    expect(within(brand).getByText('4 posts in 30 days')).toBeInTheDocument()
    expect(within(rows[1]!).getByText('No posts yet')).toBeInTheDocument()
    // One group (Default): no group sections, no group filter.
    expect(screen.queryByTestId('channel-group')).not.toBeInTheDocument()
    expect(screen.queryByRole('combobox', { name: 'Group filter' })).not.toBeInTheDocument()
  })

  it('with groups: sections per group, move a channel, create, rename and delete a group (Default stays)', async () => {
    const api = mockApi({
      'GET /v1/channels': {
        data: [channel(), channel({ id: 'ch_2', name: 'Client bot', provider: 'telegram', group_id: 'grp_a' })],
      },
      'GET /v1/providers': { data: [provider(), telegram] },
      'GET /v1/channel-groups': {
        data: [group({ channels: 1 }), group({ id: 'grp_a', name: 'Client A', default: false, channels: 1 })],
      },
      'PATCH /v1/channels/ch_1': channel({ group_id: 'grp_a' }),
      'POST /v1/channel-groups': group({ id: 'grp_b', name: 'Client B', default: false }),
      'PATCH /v1/channel-groups/grp_a': group({ id: 'grp_a', name: 'Client Alpha', default: false }),
      'DELETE /v1/channel-groups/grp_a': { status: 204 },
    })
    const user = userEvent.setup()

    renderWithProviders(<ChannelsPage />, { mode: 'local' })
    const sections = await screen.findAllByTestId('channel-group')

    expect(sections).toHaveLength(2)
    expect(within(sections[0]!).getByText('Brand on Mastodon')).toBeInTheDocument()
    expect(within(sections[1]!).getByText('Client bot')).toBeInTheDocument()

    // Folding a group hides its channels.
    await user.click(within(sections[1]!).getByRole('button', { name: /Client A/, expanded: true }))
    expect(within(sections[1]!).queryByText('Client bot')).not.toBeVisible()

    // Move to group from the row menu.
    await user.click(screen.getByRole('button', { name: 'Actions for Brand on Mastodon' }))
    // The submenu opens from the keyboard (jsdom has no hover).
    ;(await screen.findByRole('menuitem', { name: 'Move to group' })).focus()
    await user.keyboard('{ArrowRight}')
    ;(await screen.findByRole('menuitem', { name: 'Client A' })).focus()
    await user.keyboard('{Enter}')
    await waitFor(() => expect(api.find('PATCH', '/v1/channels/ch_1')).toHaveLength(1))
    expect(api.find('PATCH', '/v1/channels/ch_1')[0]!.body).toEqual({ group_id: 'grp_a' })

    // New group (a modal).
    await user.click(screen.getByRole('button', { name: 'New group' }))
    const dialog = await screen.findByRole('dialog', { name: 'New group' })

    await user.type(within(dialog).getByRole('textbox', { name: /Name/ }), 'Client B')
    await user.click(within(dialog).getByRole('button', { name: 'Create group' }))
    await waitFor(() => expect(api.find('POST', '/v1/channel-groups')).toHaveLength(1))
    expect(api.find('POST', '/v1/channel-groups')[0]!.body).toEqual({ name: 'Client B' })

    // Default has no Delete; another group asks first.
    await user.click(screen.getByRole('button', { name: 'Actions for the group Default' }))
    expect(screen.queryByRole('menuitem', { name: 'Delete group' })).not.toBeInTheDocument()
    await user.keyboard('{Escape}')
    await user.click(screen.getByRole('button', { name: 'Actions for the group Client A' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Delete group' }))
    const confirm = await screen.findByRole('alertdialog')

    expect(confirm).toHaveTextContent('Its 1 channel move to Default')
    await user.click(within(confirm).getByRole('button', { name: 'Delete group' }))
    await waitFor(() => expect(api.find('DELETE', '/v1/channel-groups/grp_a')).toHaveLength(1))
  })

  it('Connect with a platform shows a spinner and stays disabled until the browser leaves for the platform', async () => {
    const api = mockApi({ 'GET /v1/channels/oauth/mastodon/start': { url: 'https://mastodon.example/authorize' } })
    const user = userEvent.setup()

    renderWithProviders(<ConnectForm provider={provider()} reconnect={null} onDone={() => {}} />, { mode: 'local' })
    await user.click(screen.getByRole('button', { name: 'Connect with Mastodon' }))
    const button = await screen.findByRole('button', { name: 'Opening Mastodon...' })

    expect(button).toBeDisabled()
    // The request is done and the page is navigating away: still busy, so a second click can't start another.
    await waitFor(() => expect(api.find('GET', '/v1/channels/oauth/mastodon/start')).toHaveLength(1))
    await new Promise((r) => setTimeout(r, 50))
    expect(button).toBeDisabled()
    expect(button).toHaveTextContent('Opening Mastodon...')
  })
})
