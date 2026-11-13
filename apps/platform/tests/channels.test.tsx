import { cleanup, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { ConnectForm } from '@/components/channels/add-channel-sheet'
import { ChannelsPage } from '@/components/channels/channels-page'
import { peekOAuthReturn, setOAuthReturn } from '@/lib/welcome'

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
          channel({ id: 'ch_3', name: 'Alerts bot', provider: 'telegram' }),
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
    // What needs reconnecting comes first: its platform above Mastodon, the channel above its platform's others.
    expect(tg.compareDocumentPosition(mastodon) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(within(tg).getAllByTestId('channel-row')[0]).toHaveTextContent('News bot')
  })

  it('a sign-in that runs out within two weeks says Renew by and renews in one click; one further away only in the menu', async () => {
    const day = 86_400_000
    const api = mockApi({
      'GET /v1/channels': {
        data: [
          channel({ renew_by: new Date(Date.now() + 5 * day).toISOString() }),
          channel({ id: 'ch_2', name: 'Later', renew_by: new Date(Date.now() + 40 * day).toISOString() }),
        ],
      },
      'GET /v1/providers': { data: [provider()] },
      'GET /v1/channels/oauth/mastodon/start': { url: 'https://mastodon.example/authorize' },
    })
    const user = userEvent.setup()

    renderWithProviders(<ChannelsPage />, { mode: 'local' })
    const [soonRow, laterRow] = await screen.findAllByTestId('channel-row')

    expect(within(soonRow!).getByTestId('channel-renew-by')).toHaveTextContent(/^Renew by /)
    expect(within(laterRow!).queryByTestId('channel-renew-by')).not.toBeInTheDocument()
    expect(within(laterRow!).queryByRole('button', { name: 'Renew' })).not.toBeInTheDocument()
    await user.click(within(laterRow!).getByRole('button', { name: 'Actions for Later' }))
    expect(await screen.findByRole('menuitem', { name: 'Renew sign-in' })).toBeInTheDocument()
    await user.keyboard('{Escape}')
    // Straight to the platform: no drawer, nothing to type.
    await user.click(within(soonRow!).getByRole('button', { name: 'Renew' }))
    await waitFor(() => expect(api.find('GET', '/v1/channels/oauth/mastodon/start')).toHaveLength(1))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    // The Renew by line does the same (on phones it is the only button), and the "?" beside it says why.
    await user.click(within(soonRow!).getByTestId('channel-renew-by'))
    await waitFor(() => expect(api.find('GET', '/v1/channels/oauth/mastodon/start')).toHaveLength(2))
    await user.click(within(soonRow!).getByRole('button', { name: /^Why renew / }))
    expect(await screen.findByText(/ends this sign-in on that day/)).toBeInTheDocument()
  })

  it('a renewal started under a group filter comes back to that group', async () => {
    mockApi({
      'GET /v1/channels': {
        data: [channel({ group_id: 'grp_2', renew_by: new Date(Date.now() + 86_400_000).toISOString() })],
      },
      'GET /v1/channel-groups': { data: [group(), group({ id: 'grp_2', name: 'Clients', default: false })] },
      'GET /v1/providers': { data: [provider()] },
      'GET /v1/channels/oauth/mastodon/start': { url: 'https://mastodon.example/authorize' },
    })
    const user = userEvent.setup()

    nav.set('/channels', 'group=grp_2')
    renderWithProviders(<ChannelsPage />, { mode: 'local' })
    await user.click(await screen.findByRole('button', { name: 'Renew' }))
    await waitFor(() => expect(peekOAuthReturn()).toBe('/channels?group=grp_2'))
  })

  it('why a channel needs reconnecting is a short note read with its status, the whole text on hover or with More', async () => {
    const why =
      'Telegram: this channel was connected through @staqly_bot, but this server now posts as @staqly_dev_bot: add @staqly_dev_bot to the chat and connect it again'
    // jsdom has no layout: the two lines hide part of the text.
    const height = vi.spyOn(Element.prototype, 'scrollHeight', 'get').mockReturnValue(80)
    const observed: (() => void)[] = []

    vi.stubGlobal(
      'ResizeObserver',
      class {
        constructor(private cb: () => void) {}
        observe() {
          observed.push(this.cb)
          this.cb()
        }
        disconnect() {}
      },
    )
    mockApi({
      'GET /v1/channels': {
        data: [channel({ id: 'ch_2', name: 'News', provider: 'telegram', status: 'needs_reconnect', last_error: why })],
      },
      'GET /v1/providers': { data: [provider(), telegram] },
    })
    const user = userEvent.setup()

    renderWithProviders(<ChannelsPage />, { mode: 'local' })
    const note = await screen.findByTestId('channel-error')

    expect(note).toHaveAttribute('title', why)
    expect(within(note).getByText(why)).toHaveClass('line-clamp-2')
    // A screen reader reads the reason with the status.
    expect(screen.getByText('Needs reconnect')).toHaveAccessibleDescription(why)
    const more = within(note).getByRole('button', { name: 'More' })

    expect(more).toHaveAttribute('aria-expanded', 'false')
    await user.click(more)
    expect(within(note).getByRole('button', { name: 'Less' })).toHaveAttribute('aria-expanded', 'true')
    expect(within(note).getByText(why)).not.toHaveClass('line-clamp-2')
    // The old long red line is gone.
    expect(note.closest('li')?.querySelector('.text-destructive')).toBeNull()
    expect(observed.length).toBeGreaterThan(0)
    height.mockRestore()
    vi.unstubAllGlobals()
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

    expect(within(sheet).getByRole('button', { name: 'TikTok, soon' })).toBeDisabled()
    await user.click(within(sheet).getByRole('button', { name: 'Telegram' }))
    expect(await screen.findByRole('dialog', { name: /Connect Telegram/ })).toBeInTheDocument()
    await user.type(screen.getByLabelText(/Bot token/), '123:abc')
    await user.type(screen.getByLabelText(/Chat or channel/), '@newsbot')
    // The platform names the account: no Name field.
    expect(screen.queryByLabelText(/^Name/)).not.toBeInTheDocument()
    await user.click(screen.getByTestId('connect-channel'))
    await waitFor(() => expect(api.find('POST', '/v1/channels')).toHaveLength(1))
    expect(api.find('POST', '/v1/channels')[0].body).toEqual({
      provider: 'telegram',
      fields: { bot_token: '123:abc', chat_id: '@newsbot' },
    })
    expect(await screen.findAllByText(/is connected/)).not.toHaveLength(0)
  })

  it('Moltbook registers a new agent and shows its claim link, or takes the key of an agent you have', async () => {
    const moltbook = provider({
      id: 'moltbook',
      name: 'Moltbook',
      auth: 'token',
      credential_fields: [{ key: 'api_key', label: 'API key', kind: 'secret', required: true }],
      settings_fields: [],
    })
    const api = mockApi({
      'GET /v1/channels': { data: [] },
      'GET /v1/providers': { data: [moltbook] },
      'POST /v1/channels/moltbook/register': {
        channel: channel({ id: 'ch_7', provider: 'moltbook', name: 'newbot', handle: 'u/newbot' }),
        claim_url: 'https://www.moltbook.com/claim/moltbook_claim_x',
      },
    })
    const user = userEvent.setup()

    renderWithProviders(<ChannelsPage />, { mode: 'local' })
    await user.click(await screen.findByTestId('add-channel'))
    await user.click(
      within(await screen.findByRole('dialog', { name: 'Add channel' })).getByRole('button', { name: 'Moltbook' }),
    )

    // An agent you have: its key, and back to registering.
    await user.click(await screen.findByRole('button', { name: /Paste its API key/ }))
    expect(await screen.findByLabelText(/API key/)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /Register a new one/ }))

    await user.click(await screen.findByTestId('moltbook-register'))
    expect(await screen.findByText('Agent name is required')).toBeInTheDocument()
    await user.type(screen.getByLabelText(/Agent name/), 'newbot')
    await user.type(screen.getByLabelText(/Description/), 'Posts for us')
    await user.click(screen.getByTestId('moltbook-register'))
    await waitFor(() => expect(api.find('POST', '/v1/channels/moltbook/register')).toHaveLength(1))
    expect(api.find('POST', '/v1/channels/moltbook/register')[0].body).toEqual({
      name: 'newbot',
      description: 'Posts for us',
    })
    const claim = await screen.findByTestId('moltbook-claim')

    expect(within(claim).getByRole('link', { name: /Open claim page/ })).toHaveAttribute(
      'href',
      'https://www.moltbook.com/claim/moltbook_claim_x',
    )
  })

  it('Discord through the bot offers a channel webhook instead, for someone who may not add the bot', async () => {
    const discord = provider({ id: 'discord', name: 'Discord' })
    const webhook = provider({
      id: 'discord_webhook',
      name: 'Discord webhook',
      auth: 'token',
      credential_fields: [{ key: 'webhook_url', label: 'Webhook URL', kind: 'secret', required: true }],
      settings_fields: [],
    })

    mockApi({ 'GET /v1/channels': { data: [] }, 'GET /v1/providers': { data: [discord, webhook] } })
    const user = userEvent.setup()

    renderWithProviders(<ChannelsPage />, { mode: 'local' })
    await user.click(await screen.findByTestId('add-channel'))
    const sheet = await screen.findByRole('dialog', { name: 'Add channel' })

    // The bot works, so the grid shows Discord alone.
    expect(within(sheet).queryByRole('button', { name: 'Discord webhook' })).not.toBeInTheDocument()
    await user.click(within(sheet).getByRole('button', { name: 'Discord' }))
    await user.click(await screen.findByRole('button', { name: /Use a channel webhook instead/ }))
    expect(await screen.findByLabelText(/Webhook URL/)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /Add the bot to a whole Discord server instead/ }))
    expect(await screen.findByTestId('oauth-start')).toBeInTheDocument()
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
    expect(await screen.findByRole('button', { name: 'Mastodon, soon' })).toBeDisabled()
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

  it('with groups: a pane of groups, one group open, move a channel, create, edit and delete a group (Default stays)', async () => {
    const api = mockApi({
      'GET /v1/channels': {
        data: [channel(), channel({ id: 'ch_2', name: 'Client bot', provider: 'telegram', group_id: 'grp_a' })],
      },
      'GET /v1/providers': { data: [provider(), telegram] },
      'GET /v1/channel-groups': {
        data: [
          group({ channels: 1 }),
          group({ id: 'grp_a', name: 'Client A', default: false, icon: 'rocket', channels: 1 }),
        ],
      },
      'PATCH /v1/channels/ch_1': channel({ group_id: 'grp_a' }),
      'POST /v1/channel-groups': group({ id: 'grp_b', name: 'Client B', default: false }),
      'PATCH /v1/channel-groups/grp_a': group({ id: 'grp_a', name: 'Client Alpha', default: false }),
      'DELETE /v1/channel-groups/grp_a': { status: 204 },
    })
    const user = userEvent.setup()

    nav.set('/channels', 'group=grp_a')
    renderWithProviders(<ChannelsPage />, { mode: 'local' })
    const pane = await screen.findByTestId('groups-pane')

    // The pane: each group with its count; the open group is marked and its channels show.
    expect(within(pane).queryByRole('link', { name: /All channels/ })).not.toBeInTheDocument()
    expect(within(pane).getByRole('link', { name: /Default\s*1/ })).not.toHaveAttribute('aria-current')
    expect(within(pane).getByRole('link', { name: /Client A\s*1/ })).toHaveAttribute('aria-current', 'page')
    expect(await screen.findByText('Client bot')).toBeInTheDocument()
    expect(screen.queryByText('Brand on Mastodon')).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 1, name: /Client A/ })).toBeInTheDocument()

    nav.set('/channels')
    cleanup()
    renderWithProviders(<ChannelsPage />, { mode: 'local' })
    await screen.findByText('Brand on Mastodon')

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
    await user.click(within(screen.getByTestId('groups-pane')).getByRole('button', { name: 'New group' }))
    const dialog = await screen.findByRole('dialog', { name: 'New group' })

    await user.type(within(dialog).getByRole('textbox', { name: /Name/ }), 'Client B')
    await user.click(within(dialog).getByRole('button', { name: 'Create group' }))
    await waitFor(() => expect(api.find('POST', '/v1/channel-groups')).toHaveLength(1))
    expect(api.find('POST', '/v1/channel-groups')[0]!.body).toEqual({ name: 'Client B' })

    // Edit: an icon picked from the set goes with the name.
    await user.click(screen.getByRole('button', { name: 'Actions for the group Client A' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Edit' }))
    const edit = await screen.findByRole('dialog', { name: 'Edit group' })

    await user.click(within(edit).getByRole('button', { name: 'Icon: rocket' }))
    await user.click(await screen.findByRole('button', { name: 'star' }))
    await user.click(within(edit).getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(api.find('PATCH', '/v1/channel-groups/grp_a')).toHaveLength(1))
    expect(api.find('PATCH', '/v1/channel-groups/grp_a')[0]!.body).toEqual({ name: 'Client A', icon: 'star' })

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

  it('a connection started from Channels forgets a welcome return left by an abandoned earlier one', async () => {
    const api = mockApi({ 'GET /v1/channels/oauth/mastodon/start': { url: 'https://mastodon.example/authorize' } })
    const user = userEvent.setup()

    setOAuthReturn('/welcome?step=done')
    renderWithProviders(<ConnectForm provider={provider()} reconnect={null} onDone={() => {}} />, { mode: 'local' })
    await user.click(screen.getByRole('button', { name: 'Connect with Mastodon' }))
    await waitFor(() => expect(api.find('GET', '/v1/channels/oauth/mastodon/start')).toHaveLength(1))
    await waitFor(() => expect(peekOAuthReturn()).toBeNull())
  })
})

async function openMenu(user: ReturnType<typeof userEvent.setup>, name: string) {
  await user.click(await screen.findByRole('button', { name: `Actions for ${name}` }))

  return screen.findByRole('menu')
}

describe('channel actions', () => {
  const day = 86_400_000

  it('Remove asks first and says what happens to scheduled posts; Cancel keeps the channel', async () => {
    const api = mockApi({
      'GET /v1/channels': { data: [channel()] },
      'GET /v1/providers': { data: [provider()] },
      'DELETE /v1/channels/ch_1': { status: 204 },
    })
    const user = userEvent.setup()

    renderWithProviders(<ChannelsPage />, { mode: 'local' })
    await user.click(within(await openMenu(user, 'Brand on Mastodon')).getByRole('menuitem', { name: 'Remove' }))

    const confirm = await screen.findByRole('alertdialog')

    expect(confirm).toHaveTextContent('Remove Brand on Mastodon?')
    expect(confirm).toHaveTextContent('scheduled posts lose this channel')
    await user.click(within(confirm).getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument())
    expect(api.find('DELETE', '/v1/channels/ch_1')).toHaveLength(0)
    expect(screen.getByTestId('channel-row')).toBeInTheDocument()

    await user.click(within(await openMenu(user, 'Brand on Mastodon')).getByRole('menuitem', { name: 'Remove' }))
    await user.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Remove' }))
    expect(await screen.findByText('“Brand on Mastodon” was removed')).toBeInTheDocument()
    expect(api.find('DELETE', '/v1/channels/ch_1')).toHaveLength(1)
    await waitFor(() => expect(screen.queryByTestId('channel-row')).not.toBeInTheDocument())
  })

  it('a removal the server refuses is said and the channel stays', async () => {
    mockApi({
      'GET /v1/channels': { data: [channel()] },
      'GET /v1/providers': { data: [provider()] },
      'DELETE /v1/channels/ch_1': {
        status: 403,
        body: { error: { code: 'forbidden', message: 'missing scope channels:manage' } },
      },
    })
    const user = userEvent.setup()

    renderWithProviders(<ChannelsPage />, { mode: 'cloud' })
    await user.click(within(await openMenu(user, 'Brand on Mastodon')).getByRole('menuitem', { name: 'Remove' }))
    await user.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Remove' }))
    expect(await screen.findByText('Missing scope channels:manage')).toBeInTheDocument()
    expect(screen.getByTestId('channel-row')).toHaveTextContent('Brand on Mastodon')
  })

  it('Disable stops a channel and Enable brings it back; one that needs reconnecting offers neither', async () => {
    let status: 'connected' | 'disabled' = 'connected'
    const api = mockApi({
      'GET /v1/channels': {
        data: [
          channel(),
          channel({ id: 'ch_2', name: 'Broken', status: 'needs_reconnect', last_error: 'token expired' }),
        ],
      },
      'GET /v1/providers': { data: [provider()] },
      'PATCH /v1/channels/ch_1': ({ body }: { body: { status: 'connected' | 'disabled' } }) => {
        status = body.status

        return channel({ status })
      },
    })
    const user = userEvent.setup()

    renderWithProviders(<ChannelsPage />, { mode: 'local' })
    await user.click(within(await openMenu(user, 'Brand on Mastodon')).getByRole('menuitem', { name: 'Disable' }))
    expect(await screen.findByText('“Brand on Mastodon” is disabled')).toBeInTheDocument()
    expect(api.find('PATCH', '/v1/channels/ch_1')[0]!.body).toEqual({ status: 'disabled' })

    await user.click(within(await openMenu(user, 'Brand on Mastodon')).getByRole('menuitem', { name: 'Enable' }))
    expect(await screen.findByText('“Brand on Mastodon” is enabled again')).toBeInTheDocument()
    expect(api.find('PATCH', '/v1/channels/ch_1')[1]!.body).toEqual({ status: 'connected' })

    const broken = await openMenu(user, 'Broken')

    expect(within(broken).queryByRole('menuitem', { name: /Disable|Enable/ })).not.toBeInTheDocument()
    expect(within(broken).getByRole('menuitem', { name: 'Reconnect' })).toBeInTheDocument()
  })

  it('only a working channel asks for a renewal: a broken or disabled one says nothing about Renew by', async () => {
    const renewBy = new Date(Date.now() + 3 * day).toISOString()

    mockApi({
      'GET /v1/channels': {
        data: [
          channel({ id: 'ch_off', name: 'Paused', status: 'disabled', renew_by: renewBy }),
          channel({ id: 'ch_bad', name: 'Broken', status: 'needs_reconnect', renew_by: renewBy }),
          channel({ id: 'ch_ok', name: 'Working', renew_by: renewBy }),
        ],
      },
      'GET /v1/providers': { data: [provider()] },
    })
    const user = userEvent.setup()

    renderWithProviders(<ChannelsPage />, { mode: 'local' })

    const rows = await screen.findAllByTestId('channel-row')
    const row = (name: string) => rows.find((r) => r.textContent?.includes(name))!

    expect(within(row('Working')).getByTestId('channel-renew-by')).toBeInTheDocument()
    expect(within(row('Paused')).queryByTestId('channel-renew-by')).not.toBeInTheDocument()
    expect(within(row('Broken')).queryByTestId('channel-renew-by')).not.toBeInTheDocument()
    expect(within(row('Broken')).queryByRole('button', { name: 'Renew' })).not.toBeInTheDocument()
    // The broken one is asked to reconnect instead, and comes first.
    expect(rows[0]).toHaveTextContent('Broken')
    expect(within(row('Broken')).getByRole('button', { name: 'Reconnect' })).toBeInTheDocument()
    expect(within(await openMenu(user, 'Paused')).queryByRole('menuitem', { name: 'Renew sign-in' })).toBeNull()
  })

  it('a channel over the plan limit is refused in plain words, with the way to the plans', async () => {
    mockApi({
      'GET /v1/channels': { data: [channel()] },
      'GET /v1/providers': { data: [telegram] },
      'POST /v1/channels': {
        status: 402,
        body: {
          error: {
            code: 'plan_limit_exceeded',
            message: 'plan limit reached',
            details: { limit: 'channels', max: 5, used: 5, plan: 'starter' },
          },
        },
      },
    })
    const user = userEvent.setup()

    renderWithProviders(<ChannelsPage />, { mode: 'cloud' })
    await user.click(await screen.findByTestId('add-channel'))
    await user.click(await screen.findByRole('button', { name: 'Telegram' }))
    await user.type(await screen.findByLabelText(/Bot token/), '123:abc')
    await user.type(screen.getByLabelText(/Chat or channel/), '@brand')
    await user.click(screen.getByRole('button', { name: /^Connect/ }))

    expect(await screen.findByText('Plan limit reached')).toBeInTheDocument()
    expect(screen.getByText('Your Starter plan includes 5 channels (5 used).')).toBeInTheDocument()
  })
})
