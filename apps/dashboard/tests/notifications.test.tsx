import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'

import { SCOPE_INFO, SCOPE_PRESETS } from '@/components/integrations/api-keys'
import { ChannelFormSheet } from '@/components/notifications/channel-form-sheet'
import { ChannelsPage } from '@/components/notifications/channels-page'
import { PlanUsagePage } from '@/components/settings/plan-usage-page'
import { ALL_SCOPES, type NotificationChannel } from '@/lib/api'
import {
  channelApiFieldMap,
  channelHealth,
  channelToValues,
  defaultChannelValues,
  isMasked,
  maskedHint,
  storedSecrets,
  validateChannel,
  valuesToChannelInput,
} from '@/lib/channels'

import { collection } from './fixtures'
import { nav } from './navigation'
import { PLANS, usage } from './plans-fixtures'
import { mockApi, renderWithProviders } from './utils'

function channel(over: Partial<NotificationChannel> = {}): NotificationChannel {
  return {
    id: 'nch_1',
    name: 'My phone',
    type: 'ntfy',
    enabled: true,
    config: { server_url: 'https://ntfy.sh', topic: 'staqly-abc123', priority: 4 },
    events: ['item.created', 'item.escalated'],
    collection_ids: [],
    digest_window_seconds: 0,
    fail_count: 0,
    last_error: '',
    disabled_reason: '',
    last_sent_at: null,
    interactive: false,
    created_at: '2026-10-01T00:00:00Z',
    updated_at: '2026-10-01T00:00:00Z',
    ...over,
  }
}

const SLACK = channel({
  id: 'nch_slack',
  name: 'Team Slack',
  type: 'slack',
  config: {
    webhook_url: 'https://hooks.slack.com/••••',
    signing_secret: '••••0123',
    approver_user_ids: ['U012ABC'],
  },
  interactive: true,
  integration_url: 'http://localhost:3333/v1/integrations/slack/interactions',
  digest_window_seconds: 300,
})

const DISABLED_TG = channel({
  id: 'nch_tg',
  name: 'Ops Telegram',
  type: 'telegram',
  enabled: false,
  fail_count: 10,
  disabled_reason: 'Disabled after 10 consecutive failed sends',
  last_error: 'telegram: Unauthorized (401)',
  config: { bot_token: '••••saw1', chat_id: '-1001234567890', mode: 'polling' },
  interactive: true,
})

const LIST = { data: [channel(), SLACK, DISABLED_TG] }

function routes(extra: Record<string, unknown> = {}) {
  return {
    'GET /v1/notification-channels': LIST,
    'GET /v1/collections': { data: [collection(), collection({ id: 'col_2', name: 'Research' })] },
    'GET /v1/workspace/usage': usage(PLANS[0], { notification_channels: 3 }),
    ...extra,
  }
}

async function openMenu(user: ReturnType<typeof userEvent.setup>, name: string) {
  await user.click(await screen.findByRole('button', { name: `${name} options` }))

  return screen.findByRole('menu')
}

describe('notification channel helpers', () => {
  it('recognises masked secrets and describes them without revealing anything', () => {
    expect(isMasked('••••abcd')).toBe(true)
    expect(isMasked('https://hooks.slack.com/••••')).toBe(true)
    expect(isMasked('plain')).toBe(false)
    expect(maskedHint('••••abcd')).toBe('ends in abcd')
    expect(maskedHint('https://hooks.slack.com/••••')).toBe('hooks.slack.com')
    expect(maskedHint('••••')).toBe('')
    expect(storedSecrets(SLACK)).toEqual(['webhook_url', 'signing_secret'])
  })

  it('never prefills secrets when editing', () => {
    const v = channelToValues(SLACK)

    expect(v.webhook_url).toBe('')
    expect(v.signing_secret).toBe('')
    expect(v.delivery).toBe('webhook')
    expect(v.approver_user_ids).toEqual(['U012ABC'])
    expect(channelToValues(DISABLED_TG).bot_token).toBe('')
  })

  it('validates like the server', () => {
    const tg = { ...defaultChannelValues('telegram'), bot_token: 'nope', chat_id: 'me' }

    expect(validateChannel('telegram', tg)).toMatchObject({
      bot_token: expect.any(String),
      chat_id: expect.any(String),
    })
    const ok = { ...tg, bot_token: '123456:ABCDEFGHIJKLMNOPQRSTUVWXYZ', chat_id: '-100123', approver_user_ids: ['42'] }

    expect(validateChannel('telegram', ok)).toEqual({})
    expect(validateChannel('telegram', { ...ok, approver_user_ids: ['U12'] }).approver_user_ids).toMatch(/numeric/)
    // A stored token does not have to be re-entered.
    expect(validateChannel('telegram', { ...ok, bot_token: '' }, ['bot_token'])).toEqual({})

    const slack = { ...defaultChannelValues('slack'), webhook_url: 'ftp://x' }

    expect(validateChannel('slack', slack).webhook_url).toMatch(/http/)
    expect(validateChannel('slack', { ...slack, delivery: 'bot' as const })).toMatchObject({
      bot_token: expect.any(String),
      channel: expect.any(String),
    })
    expect(
      validateChannel('slack', { ...slack, webhook_url: 'https://hooks.slack.com/x', signing_secret: 'short' })
        .signing_secret,
    ).toBe('Looks too short')

    const discord = {
      ...defaultChannelValues('discord'),
      delivery: 'bot' as const,
      bot_token: 't',
      channel: 'abc',
      public_key: 'xyz',
    }

    expect(validateChannel('discord', discord)).toMatchObject({
      channel: expect.any(String),
      public_key: expect.any(String),
    })

    expect(validateChannel('ntfy', { ...defaultChannelValues('ntfy'), topic: 'has space' }).topic).toBeTruthy()
    expect(validateChannel('ntfy', { ...defaultChannelValues('ntfy'), topic: 'staqly-x' })).toEqual({})

    const email = {
      ...defaultChannelValues('email'),
      host: 'smtp.example.com',
      from: 'staqly <bot@example.com>',
      to: ['me@example.com'],
    }

    expect(validateChannel('email', email)).toEqual({})
    expect(validateChannel('email', { ...email, port: '99999', to: [] })).toMatchObject({
      port: expect.any(String),
      to: expect.any(String),
    })
    expect(validateChannel('email', { ...email, to: ['Me <me@example.com>'] }).to).toMatch(/Invalid address/)
  })

  it('builds request bodies: unchanged secrets are left out, removed ones are null', () => {
    const create = valuesToChannelInput('telegram', {
      ...defaultChannelValues('telegram'),
      name: 'Phone',
      bot_token: ' 123456:ABCDEFGHIJKLMNOPQRSTUVWXYZ ',
      chat_id: '42',
    })

    expect(create).toEqual({
      name: 'Phone',
      type: 'telegram',
      enabled: true,
      config: { bot_token: '123456:ABCDEFGHIJKLMNOPQRSTUVWXYZ', chat_id: '42', mode: 'polling' },
      events: ['item.created'],
      collection_ids: [],
      digest_window_seconds: 0,
    })

    const stored = storedSecrets(SLACK)
    const v = channelToValues(SLACK)
    const keep = valuesToChannelInput('slack', v, { editing: true, stored })

    expect(keep.type).toBeUndefined()
    expect(keep.config).not.toHaveProperty('webhook_url')
    expect(keep.config).not.toHaveProperty('signing_secret')
    expect(keep.config?.channel).toBeNull()

    const replaced = valuesToChannelInput(
      'slack',
      { ...v, webhook_url: 'https://hooks.slack.com/new' },
      { editing: true, stored, secrets: { webhook_url: 'set', signing_secret: 'remove' } },
    )

    expect(replaced.config?.webhook_url).toBe('https://hooks.slack.com/new')
    expect(replaced.config?.signing_secret).toBeNull()

    const toBot = valuesToChannelInput(
      'slack',
      { ...v, delivery: 'bot', bot_token: 'xoxb-1', channel: 'C01' },
      { editing: true, stored },
    )

    expect(toBot.config).toMatchObject({ webhook_url: null, bot_token: 'xoxb-1', channel: 'C01' })
  })

  it('maps 422 field names onto the fields the form shows for the type', () => {
    const telegram = channelApiFieldMap('telegram', 'webhook')

    expect(telegram('name')).toBe('name')
    expect(telegram('events')).toBe('events')
    expect(telegram('config.chat_id')).toBe('chat_id')
    expect(telegram('config.webhook_url')).toBeNull()
    expect(telegram('type')).toBeNull()
    expect(telegram('enabled')).toBeNull()
    expect(channelApiFieldMap('slack', 'webhook')('config.webhook_url')).toBe('webhook_url')
    expect(channelApiFieldMap('slack', 'webhook')('config.channel')).toBeNull()
    expect(channelApiFieldMap('discord', 'bot')('config.public_key')).toBe('public_key')
    expect(channelApiFieldMap('discord', 'bot')('config.signing_secret')).toBeNull()
  })

  it('offers the channels:manage API key scope, only in the Full access preset', () => {
    expect(ALL_SCOPES).toContain('channels:manage')
    expect(SCOPE_INFO['channels:manage']).toMatch(/notification channels/i)
    const withScope = SCOPE_PRESETS.filter((p) => p.scopes.includes('channels:manage')).map((p) => p.id)

    expect(withScope).toEqual(['full'])
  })

  it('derives channel health', () => {
    expect(channelHealth(channel())).toBe('active')
    expect(channelHealth(DISABLED_TG)).toBe('disabled')
    expect(channelHealth(channel({ enabled: false }))).toBe('paused')
    expect(channelHealth(channel({ fail_count: 2 }))).toBe('failing')
  })
})

describe('notification channels page', () => {
  beforeEach(() => nav.set('/settings/notifications'))

  it('lists channels with type, status, events and the plan usage', async () => {
    mockApi(routes())
    renderWithProviders(<ChannelsPage />, { mode: 'cloud' })
    const cards = await screen.findAllByTestId('channel-card')

    expect(cards).toHaveLength(3)
    expect(within(cards[0]).getByRole('img', { name: 'ntfy' })).toBeInTheDocument()
    expect(within(cards[0]).getByText('Active')).toBeInTheDocument()
    expect(within(cards[0]).getByText(/ntfy\.sh\/staqly-abc123/)).toBeInTheDocument()
    expect(within(cards[1]).getByRole('img', { name: 'Slack' })).toBeInTheDocument()
    expect(within(cards[1]).getByText('Buttons in chat')).toBeInTheDocument()
    expect(within(cards[1]).getByText(/Digest every 5 min/)).toBeInTheDocument()
    expect(within(cards[0]).getByText('item.escalated')).toBeInTheDocument()
    expect(await screen.findByTestId('usage-chip-notification_channels')).toHaveTextContent('3 / 2 channels')
  })

  it('shows why a channel was auto-disabled and re-enables it', async () => {
    const api = mockApi(
      routes({ 'PATCH /v1/notification-channels/nch_tg': { ...DISABLED_TG, enabled: true, disabled_reason: '' } }),
    )
    const user = userEvent.setup()

    renderWithProviders(<ChannelsPage />, { mode: 'local' })
    const card = await screen.findByRole('article', { name: 'Ops Telegram' })

    expect(within(card).getByText('Disabled after failures')).toBeInTheDocument()
    expect(within(card).getByText('Disabled after 10 consecutive failed sends')).toBeInTheDocument()
    expect(within(card).getByText(/Unauthorized \(401\)/)).toBeInTheDocument()
    await user.click(within(card).getByRole('button', { name: /re-enable/i }))
    await waitFor(() => expect(api.find('PATCH', '/v1/notification-channels/nch_tg')).toHaveLength(1))
    expect(api.find('PATCH', '/v1/notification-channels/nch_tg')[0].body).toEqual({ enabled: true })
    expect(await screen.findByText('Channel re-enabled')).toBeInTheDocument()
  })

  it('sends a test message and reports success or the delivery error', async () => {
    const api = mockApi(
      routes({
        'POST /v1/notification-channels/nch_1/test': { ok: true },
        'POST /v1/notification-channels/nch_slack/test': { ok: false, error: 'slack: 404 no_service' },
      }),
    )
    const user = userEvent.setup()

    renderWithProviders(<ChannelsPage />, { mode: 'local' })
    const phone = await screen.findByRole('article', { name: 'My phone' })

    await user.click(within(phone).getByRole('button', { name: /send test/i }))
    expect(await screen.findByText('Test message sent')).toBeInTheDocument()
    const slack = screen.getByRole('article', { name: 'Team Slack' })

    await user.click(within(slack).getByRole('button', { name: /send test/i }))
    expect(await screen.findByText('Test message failed')).toBeInTheDocument()
    expect(screen.getByText('slack: 404 no_service')).toBeInTheDocument()
    expect(api.find('POST', '/v1/notification-channels/nch_slack/test')).toHaveLength(1)
  })

  it('opens the deliveries drawer', async () => {
    mockApi(
      routes({
        'GET /v1/notification-channels/nch_1/deliveries': {
          data: [
            {
              id: 'ntf_1',
              channel_id: 'nch_1',
              event_type: 'item.created',
              status: 'failed',
              attempts: 3,
              last_error: 'ntfy: 500',
              item_id: 'itm_1',
              created_at: '2026-10-01T00:00:00Z',
            },
          ],
        },
      }),
    )
    const user = userEvent.setup()

    renderWithProviders(<ChannelsPage />, { mode: 'local' })
    const phone = await screen.findByRole('article', { name: 'My phone' })

    await user.click(within(phone).getByRole('button', { name: /deliveries/i }))
    const list = await screen.findByTestId('channel-deliveries')

    expect(within(list).getByText('item.created')).toBeInTheDocument()
    expect(within(list).getByText('failed')).toBeInTheDocument()
    expect(within(list).getByText('ntfy: 500')).toBeInTheDocument()
    expect(within(list).getByRole('link', { name: 'Open item' })).toHaveAttribute('href', '/items/itm_1')
  })

  it('edits a channel: secrets are masked, never prefilled, and only sent when replaced', async () => {
    const api = mockApi(routes({ 'PATCH /v1/notification-channels/nch_slack': SLACK }))
    const user = userEvent.setup()

    renderWithProviders(<ChannelsPage />, { mode: 'local' })
    const menu = await openMenu(user, 'Team Slack')

    await user.click(within(menu).getByRole('menuitem', { name: /edit/i }))
    const form = await screen.findByTestId('channel-form')

    expect(within(form).getByTestId('secret-webhook_url')).toHaveTextContent('•••• set')
    expect(within(form).getByTestId('secret-webhook_url')).toHaveTextContent('hooks.slack.com')
    expect(within(form).getByTestId('secret-signing_secret')).toHaveTextContent('ends in 0123')

    // No input anywhere carries a stored (masked) value.
    for (const input of within(form).getAllByRole('textbox')) {
      expect((input as HTMLInputElement).value).not.toContain('••••')
    }

    expect(within(form).getByText(SLACK.integration_url!)).toBeInTheDocument()

    // Save without touching secrets: they are not sent.
    await user.click(within(form).getByRole('button', { name: /save channel/i }))
    await waitFor(() => expect(api.find('PATCH', '/v1/notification-channels/nch_slack')).toHaveLength(1))
    const body = api.find('PATCH', '/v1/notification-channels/nch_slack')[0].body as {
      config: Record<string, unknown>
    }

    expect(body.config).not.toHaveProperty('webhook_url')
    expect(body.config).not.toHaveProperty('signing_secret')
    expect(body.config.approver_user_ids).toEqual(['U012ABC'])
  })

  it('replaces and removes secrets on edit', async () => {
    const api = mockApi(routes({ 'PATCH /v1/notification-channels/nch_slack': SLACK }))
    const user = userEvent.setup()

    renderWithProviders(<ChannelFormSheet open onOpenChange={() => {}} channel={SLACK} />, { mode: 'local' })
    const form = await screen.findByTestId('channel-form')

    await user.click(within(form).getByRole('button', { name: 'Replace Webhook URL' }))
    const input = within(form).getByLabelText('Webhook URL*') as HTMLInputElement

    expect(input.value).toBe('')
    await user.type(input, 'https://hooks.slack.com/services/NEW')
    await user.click(within(form).getByRole('button', { name: 'Remove Signing secret' }))
    expect(within(form).getByText('Will be removed when you save.')).toBeInTheDocument()
    await user.click(within(form).getByRole('button', { name: /save channel/i }))
    await waitFor(() => expect(api.find('PATCH', '/v1/notification-channels/nch_slack')).toHaveLength(1))
    const body = api.find('PATCH', '/v1/notification-channels/nch_slack')[0].body as {
      config: Record<string, unknown>
    }

    expect(body.config.webhook_url).toBe('https://hooks.slack.com/services/NEW')
    expect(body.config.signing_secret).toBeNull()
  })

  it('creates a Telegram channel with type-specific fields and setup help', async () => {
    const api = mockApi(
      routes({ 'POST /v1/notification-channels': { status: 201, body: channel({ type: 'telegram' }) } }),
    )
    const user = userEvent.setup()

    renderWithProviders(<ChannelFormSheet open onOpenChange={() => {}} />, { mode: 'local' })
    const form = await screen.findByTestId('channel-form')

    expect(within(form).getByTestId('setup-guide')).toHaveTextContent(/@BotFather/)
    expect(within(form).getByTestId('setup-guide')).toHaveTextContent('/chatid')

    // Client-side validation first.
    await user.click(within(form).getByRole('button', { name: /create channel/i }))
    expect(await within(form).findByText(/Paste the bot token from @BotFather/)).toBeInTheDocument()
    expect(api.find('POST', '/v1/notification-channels')).toHaveLength(0)

    await user.type(within(form).getByLabelText('Bot token*'), '123456:ABCDEFGHIJKLMNOPQRSTUVWXYZ')
    await user.type(within(form).getByLabelText('Chat ID*'), '123456789')
    // Events and collections use the same multi-select as webhooks.
    await user.click(within(form).getByRole('button', { name: 'Events' }))
    await user.click(await screen.findByRole('option', { name: /item\.approved/ }))
    await user.keyboard('{Escape}')
    await user.click(within(form).getByRole('button', { name: 'Collections' }))
    await user.click(await screen.findByRole('option', { name: 'Research' }))
    await user.keyboard('{Escape}')
    await user.selectOptions(within(form).getByLabelText('Delivery'), '300')
    await user.click(within(form).getByRole('button', { name: /create channel/i }))
    await waitFor(() => expect(api.find('POST', '/v1/notification-channels')).toHaveLength(1))
    expect(api.find('POST', '/v1/notification-channels')[0].body).toEqual({
      name: 'Telegram',
      type: 'telegram',
      enabled: true,
      config: { bot_token: '123456:ABCDEFGHIJKLMNOPQRSTUVWXYZ', chat_id: '123456789', mode: 'polling' },
      events: ['item.created', 'item.approved'],
      collection_ids: ['col_2'],
      digest_window_seconds: 300,
    })
  })

  it('switches type forms: ntfy and email fields', async () => {
    const api = mockApi(routes({ 'POST /v1/notification-channels': { status: 201, body: channel() } }))
    const user = userEvent.setup()

    renderWithProviders(<ChannelFormSheet open onOpenChange={() => {}} />, { mode: 'local' })
    const form = await screen.findByTestId('channel-form')

    await user.click(within(form).getByRole('radio', { name: /email/i }))
    expect(within(form).getByLabelText('SMTP host*')).toBeInTheDocument()
    expect(within(form).getByLabelText('Security')).toHaveValue('starttls')
    expect(within(form).queryByLabelText('Bot token*')).not.toBeInTheDocument()

    await user.click(within(form).getByRole('radio', { name: /ntfy/i }))
    expect(within(form).getByLabelText('Server')).toHaveValue('https://ntfy.sh')
    await user.click(within(form).getByRole('button', { name: /random/i }))
    expect((within(form).getByLabelText('Topic*') as HTMLInputElement).value).toMatch(/^staqly-[a-z0-9]{6,}$/)
    await user.click(within(form).getByRole('button', { name: /create channel/i }))
    await waitFor(() => expect(api.find('POST', '/v1/notification-channels')).toHaveLength(1))
    const body = api.find('POST', '/v1/notification-channels')[0].body as {
      name: string
      type: string
      config: Record<string, unknown>
    }

    expect(body.type).toBe('ntfy')
    expect(body.name).toBe('ntfy')
    expect(body.config.server_url).toBe('https://ntfy.sh')
  })

  it('explains the Free plan channel limit (402)', async () => {
    mockApi(
      routes({
        'POST /v1/notification-channels': {
          status: 402,
          body: {
            error: {
              code: 'plan_limit_exceeded',
              message: 'The Free plan allows 2 notification_channels; upgrade the workspace plan to continue.',
              details: { limit: 'notification_channels', max: 2, used: 2, plan: 'free' },
            },
          },
        },
      }),
    )
    const user = userEvent.setup()

    renderWithProviders(<ChannelFormSheet open onOpenChange={() => {}} />, { mode: 'cloud' })
    const form = await screen.findByTestId('channel-form')

    await user.click(within(form).getByRole('radio', { name: /ntfy/i }))
    await user.type(within(form).getByLabelText('Topic*'), 'staqly-topic')
    await user.click(within(form).getByRole('button', { name: /create channel/i }))
    expect(await screen.findByText('Plan limit reached')).toBeInTheDocument()
    expect(screen.getByText('Your Free plan includes 2 notification channels (2 used).')).toBeInTheDocument()
  })

  it('shows a notification channels meter on Plan & usage', async () => {
    mockApi({
      'GET /v1/workspace/usage': usage(PLANS[0], { notification_channels: 2 }),
      'GET /v1/plans': { data: PLANS },
    })
    renderWithProviders(<PlanUsagePage />, { mode: 'cloud' })
    const meter = await screen.findByTestId('meter-Notification channels')

    expect(meter).toHaveAttribute('data-level', 'full')
    expect(meter).toHaveTextContent('2/ 2')
  })
})
