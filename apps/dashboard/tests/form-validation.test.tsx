import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { CreateKeyDialog } from '@/components/integrations/api-keys'
import { EditItemDialog } from '@/components/items/item-actions'
import { ItemEditForm } from '@/components/items/item-edit-form'
import { ChannelFormSheet } from '@/components/notifications/channel-form-sheet'
import { SettingsPage } from '@/components/settings/settings-page'
import { WatcherSheet } from '@/components/watchers/watcher-sheet'
import { WebhookFormSheet } from '@/components/webhooks/webhook-form-sheet'
import type { Watcher } from '@/lib/api'
import { ApiError } from '@/lib/api'
import { apiFieldErrors, fieldMessage } from '@/lib/form-errors'

import { collection, item } from './fixtures'
import { PLANS, usage } from './plans-fixtures'
import { mockApi, renderWithProviders } from './utils'

/**
 * One pattern for every form: a required field has a red * after its label and aria-required; an
 * invalid field gets aria-invalid (red border), data-invalid on its Field (red label) and the message
 * right under it; errors appear on submit and clear once the field is fixed.
 */
function expectInvalid(control: HTMLElement, message: string | RegExp) {
  expect(control).toHaveAttribute('aria-invalid', 'true')

  const field = control.closest<HTMLElement>('[data-slot=field], [data-invalid]')

  expect(field).toHaveAttribute('data-invalid', 'true')
  expect(within(field!).getByText(message)).toHaveAttribute('data-slot', 'field-error')
}

function expectRequired(label: HTMLElement, control: HTMLElement) {
  expect(label.querySelector('[data-slot=required-mark]')).toHaveTextContent('*')
  expect(control).toHaveAttribute('aria-required', 'true')
}

const unprocessable = (fields: Record<string, string>) => ({
  status: 422,
  body: { error: { code: 'validation_failed', message: 'validation failed', fields } },
})

const COLLECTIONS = { data: [collection(), collection({ id: 'col_2', name: 'Research' })] }

describe('form error helpers', () => {
  it('turns API messages into sentences', () => {
    expect(fieldMessage('is required')).toBe('This field is required')
    expect(fieldMessage('must be an absolute http(s) URL')).toBe('Must be an absolute http(s) URL')
    expect(fieldMessage('')).toBe('Invalid value')
  })

  it('maps API fields onto form fields and reports what found no field', () => {
    const err = new ApiError(422, {
      code: 'validation_failed',
      message: 'x',
      fields: { 'config.chat_id': 'bad', type: 'no' },
    })
    const { errors, complete } = apiFieldErrors(err, (k) => (k === 'config.chat_id' ? 'chat_id' : null))

    expect(errors).toEqual({ chat_id: 'Bad' })
    expect(complete).toBe(false)
    expect(apiFieldErrors(new Error('x'), () => 'a')).toEqual({ errors: {}, complete: false })
  })
})

describe('item edit form', () => {
  it('requires a title and, for text items, content', async () => {
    const user = userEvent.setup()
    const api = mockApi({ 'GET /v1/collections': COLLECTIONS })

    renderWithProviders(<ItemEditForm item={item({ type: 'text', content: 'Hello', media: undefined })} />)
    const title = screen.getByLabelText('Title*')
    const content = screen.getByLabelText('Content*')

    await user.clear(title)
    await user.clear(content)
    await user.click(screen.getByRole('button', { name: /save changes/i }))
    expectInvalid(title, 'Title is required')
    expectInvalid(content, 'Content is required')
    expect(api.find('PATCH', '/v1/items/itm_1')).toHaveLength(0)
  })

  it('flags an invalid URL and metadata on save, and server errors on selects', async () => {
    const user = userEvent.setup()
    const api = mockApi({
      'GET /v1/collections': COLLECTIONS,
      'PATCH /v1/items/itm_1': unprocessable({ collection_id: 'unknown collection' }),
    })

    renderWithProviders(<ItemEditForm item={item({ type: 'link', url: 'https://a.test', media: undefined })} />)
    const url = screen.getByLabelText('URL*')
    const metadata = screen.getByLabelText('Metadata')

    // Title and URL are required for a link: marked with an asterisk.
    expect(screen.getByLabelText('Title*')).toHaveAttribute('aria-required', 'true')
    expect(url).toHaveAttribute('aria-required', 'true')
    await user.clear(url)
    await user.type(url, 'nope')
    await user.clear(metadata)
    await user.type(metadata, '[[1]')
    await user.click(screen.getByRole('button', { name: /save changes/i }))
    expectInvalid(url, 'Must be an absolute http(s) URL')
    expectInvalid(metadata, 'Metadata must be a JSON object')
    expect(api.find('PATCH', '/v1/items/itm_1')).toHaveLength(0)

    await user.clear(url)
    await user.type(url, 'https://b.test')
    expect(url).toHaveAttribute('aria-invalid', 'false')
    await user.clear(metadata)
    await user.click(screen.getByRole('button', { name: /save changes/i }))
    await waitFor(() => expect(api.find('PATCH', '/v1/items/itm_1')).toHaveLength(1))
    expectInvalid(screen.getByRole('combobox', { name: 'Collection' }), 'Unknown collection')
  })
})

describe('edit item dialog', () => {
  it('requires a valid URL for links and valid JSON for JSON items', async () => {
    const user = userEvent.setup()
    const api = mockApi({ 'PATCH /v1/items/itm_1': item({ type: 'link', url: 'https://b.test', media: undefined }) })
    const { unmount } = renderWithProviders(
      <EditItemDialog
        item={item({ type: 'link', url: 'https://a.test', media: undefined })}
        open
        onOpenChange={() => {}}
      />,
    )
    const url = screen.getByLabelText('URL*')

    expectRequired(screen.getByText('URL', { selector: 'label' }), url)
    expectRequired(screen.getByText('Title', { selector: 'label' }), screen.getByLabelText('Title*'))
    await user.clear(url)
    await user.click(screen.getByRole('button', { name: 'Save changes' }))
    expectInvalid(url, 'URL is required')
    await user.type(url, 'a.test')
    expectInvalid(url, 'Enter an absolute http(s) URL')
    expect(api.find('PATCH', '/v1/items/itm_1')).toHaveLength(0)
    await user.clear(url)
    await user.type(url, 'https://b.test')
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Save changes' }))
    await waitFor(() => expect(api.find('PATCH', '/v1/items/itm_1')).toHaveLength(1))
    expect(api.find('PATCH', '/v1/items/itm_1')[0].body).toEqual({ url: 'https://b.test', expected_version: 1 })
    unmount()

    renderWithProviders(
      <EditItemDialog
        item={item({ type: 'json', content: '{"a":1}', media: undefined })}
        open
        onOpenChange={() => {}}
      />,
    )
    const json = screen.getByLabelText('JSON*')

    await user.type(json, '{{')
    await user.click(screen.getByRole('button', { name: 'Save changes' }))
    expectInvalid(json, 'Content must be valid JSON')
  })
})

const WATCHER_ROUTES = {
  'GET /v1/collections': COLLECTIONS,
  'GET /v1/workspace/usage': usage(PLANS[0], { watchers: 0 }),
  'GET /v1/plans': { data: PLANS },
}

describe('watcher forms', () => {
  it('add: requires URL and name, then shows a server error under the interval', async () => {
    const user = userEvent.setup()
    const api = mockApi({ ...WATCHER_ROUTES, 'POST /v1/watchers': unprocessable({ every: 'must be at most 168h' }) })

    renderWithProviders(<WatcherSheet open onOpenChange={() => {}} state={{ mode: 'add' }} />, {
      mode: 'local',
    })
    const sheet = await screen.findByRole('dialog')
    const url = within(sheet).getByLabelText('URL*')
    const name = within(sheet).getByLabelText('Name*')

    expectRequired(within(sheet).getByText('URL', { selector: 'label' }), url)
    expectRequired(within(sheet).getByText('Name', { selector: 'label' }), name)
    await user.click(within(sheet).getByRole('button', { name: /start watching/i }))
    expectInvalid(url, 'Enter an absolute http(s) URL')
    expectInvalid(name, 'Give the watcher a name')
    expect(api.find('POST', '/v1/watchers')).toHaveLength(0)

    await user.type(url, 'https://example.com/feed.xml')
    await user.type(name, 'Blog')
    expect(within(sheet).queryByRole('alert')).not.toBeInTheDocument()
    await user.click(within(sheet).getByRole('button', { name: /start watching/i }))
    await waitFor(() => expect(api.find('POST', '/v1/watchers')).toHaveLength(1))
    expectInvalid(await within(sheet).findByLabelText('Check'), 'Must be at most 168h')
  })

  it('edit: an emptied name and a bad URL are flagged on save', async () => {
    const user = userEvent.setup()
    const w: Watcher = {
      id: 'wat_1',
      name: 'HN',
      kind: 'rss',
      url: 'https://hnrss.org/frontpage',
      collection_id: 'col_1',
      keywords: [],
      exclude_keywords: [],
      every: '30m0s',
      every_seconds: 1800,
      status: 'active',
      last_error: '',
      last_run_at: null,
      next_run_at: null,
      items_created: 0,
      fail_count: 0,
      created_at: '2026-10-01T00:00:00Z',
      updated_at: '2026-10-01T00:00:00Z',
    } as unknown as Watcher
    const api = mockApi(WATCHER_ROUTES)

    renderWithProviders(<WatcherSheet open onOpenChange={() => {}} state={{ mode: 'edit', watcher: w }} />, {
      mode: 'local',
    })
    const sheet = await screen.findByRole('dialog')
    const name = within(sheet).getByLabelText('Name*')
    const url = within(sheet).getByLabelText('URL*')

    expectRequired(within(sheet).getByText('Name', { selector: 'label' }), name)
    await user.clear(name)
    await user.clear(url)
    await user.type(url, 'ftp://x')
    await user.click(within(sheet).getByRole('button', { name: /save watcher/i }))
    expectInvalid(name, 'Give the watcher a name')
    expectInvalid(url, 'Enter an absolute http(s) URL')
    expect(api.find('PATCH', '/v1/watchers/wat_1')).toHaveLength(0)
  })
})

describe('webhook form', () => {
  it('flags URL, events and limits on submit and clears each once fixed', async () => {
    const user = userEvent.setup()
    const api = mockApi({
      'GET /v1/collections': COLLECTIONS,
      'POST /v1/webhooks': unprocessable({ url: 'must not contain credentials' }),
    })

    renderWithProviders(<WebhookFormSheet open onOpenChange={() => {}} />)
    const dialog = await screen.findByRole('dialog')
    const url = within(dialog).getByLabelText('Endpoint URL*')
    const timeout = within(dialog).getByLabelText('Timeout (seconds)*')

    expectRequired(within(dialog).getByText('Endpoint URL', { selector: 'label' }), url)
    expectRequired(within(dialog).getByText('Timeout (seconds)', { selector: 'label' }), timeout)
    const events = within(dialog).getByRole('button', { name: 'Events' })
    const toggleEvent = async (name: RegExp) => {
      await user.click(events)
      await user.click(await screen.findByRole('option', { name }))
      await user.keyboard('{Escape}')
    }

    await toggleEvent(/item\.approved/)
    await toggleEvent(/item\.rejected/)
    await user.clear(timeout)
    await user.type(timeout, '0')
    await user.click(within(dialog).getByRole('button', { name: /create webhook/i }))
    expectInvalid(url, 'Endpoint URL is required')
    expectInvalid(timeout, 'Between 1 and 60 seconds')
    expect(within(dialog).getByText('Subscribe to at least one event (use * for all)')).toHaveAttribute(
      'data-slot',
      'field-error',
    )
    expect(events).toHaveAttribute('aria-invalid', 'true')
    expect(api.find('POST', '/v1/webhooks')).toHaveLength(0)

    await user.type(url, 'https://user:pw@hooks.test/x')
    await toggleEvent(/item\.approved/)
    await user.type(timeout, '5')
    expect(within(dialog).queryByRole('alert')).not.toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: /create webhook/i }))
    await waitFor(() => expect(api.find('POST', '/v1/webhooks')).toHaveLength(1))
    expectInvalid(url, 'Must not contain credentials')
    await user.type(url, 'x')
    expect(url).toHaveAttribute('aria-invalid', 'false')
  })
})

const channelRoutes = (extra: Record<string, unknown> = {}) => ({
  'GET /v1/notification-channels': { data: [] },
  'GET /v1/collections': COLLECTIONS,
  'GET /v1/workspace/usage': usage(PLANS[0], { notification_channels: 0 }),
  ...extra,
})

describe('notification channel form', () => {
  it('telegram: required connection fields, then a server error on the chat ID', async () => {
    const user = userEvent.setup()
    const api = mockApi(
      channelRoutes({
        'POST /v1/notification-channels': unprocessable({
          'config.chat_id': 'must be a numeric chat ID or @channelname',
        }),
      }),
    )

    renderWithProviders(<ChannelFormSheet open onOpenChange={() => {}} />, { mode: 'local' })
    const form = await screen.findByTestId('channel-form')
    const name = within(form).getByLabelText('Name*')
    const token = within(form).getByLabelText('Bot token*')
    const chat = within(form).getByLabelText('Chat ID*')

    expectRequired(within(form).getByText('Name', { selector: 'label' }), name)
    expectRequired(within(form).getByText('Bot token', { selector: 'label' }), token)
    expectRequired(within(form).getByText('Chat ID', { selector: 'label' }), chat)
    await user.clear(name)
    await user.click(within(form).getByRole('button', { name: /create channel/i }))
    expectInvalid(name, 'Give the channel a name')
    expectInvalid(token, /Paste the bot token from @BotFather/)
    expectInvalid(chat, /A numeric chat ID/)

    await user.type(name, 'Phone')
    await user.type(token, '123456:ABCDEFGHIJKLMNOPQRSTUVWXYZ')
    await user.type(chat, '123')
    expect(within(form).queryByRole('alert')).not.toBeInTheDocument()
    await user.click(within(form).getByRole('button', { name: /create channel/i }))
    await waitFor(() => expect(api.find('POST', '/v1/notification-channels')).toHaveLength(1))
    expectInvalid(chat, 'Must be a numeric chat ID or @channelname')
  })

  it('shows errors only after submit, then follows every change', async () => {
    const user = userEvent.setup()
    const api = mockApi(channelRoutes())

    renderWithProviders(<ChannelFormSheet open onOpenChange={() => {}} />, { mode: 'local' })
    const form = await screen.findByTestId('channel-form')
    const chat = within(form).getByLabelText('Chat ID*')

    await user.type(chat, 'not a chat')
    expect(chat).toHaveAttribute('aria-invalid', 'false')
    expect(within(form).queryByText(/A numeric chat ID/)).not.toBeInTheDocument()

    await user.click(within(form).getByRole('button', { name: /create channel/i }))
    expectInvalid(chat, /A numeric chat ID/)
    await waitFor(() => expect(within(form).getByLabelText('Bot token*')).toHaveFocus())
    expect(api.find('POST', '/v1/notification-channels')).toHaveLength(0)

    await user.clear(chat)
    await user.type(chat, '123')
    expect(chat).toHaveAttribute('aria-invalid', 'false')
    await user.type(chat, 'x')
    expectInvalid(chat, /A numeric chat ID/)
  })

  it('puts 422 field errors under their fields; editing one clears only its own', async () => {
    const user = userEvent.setup()
    const api = mockApi(
      channelRoutes({
        'POST /v1/notification-channels': unprocessable({
          name: 'is already used by another channel',
          'config.chat_id': 'chat not found',
        }),
      }),
    )

    renderWithProviders(<ChannelFormSheet open onOpenChange={() => {}} />, { mode: 'local' })
    const form = await screen.findByTestId('channel-form')
    const name = within(form).getByLabelText('Name*')
    const chat = within(form).getByLabelText('Chat ID*')

    await user.type(within(form).getByLabelText('Bot token*'), '123456:ABCDEFGHIJKLMNOPQRSTUVWXYZ')
    await user.type(chat, '123')
    await user.click(within(form).getByRole('button', { name: /create channel/i }))
    await waitFor(() => expect(api.find('POST', '/v1/notification-channels')).toHaveLength(1))
    expectInvalid(name, 'This field is already used by another channel')
    expectInvalid(chat, 'Chat not found')
    await waitFor(() => expect(name).toHaveFocus())
    // Every field error found its field: nothing is toasted.
    expect(screen.queryByText('Could not create the channel')).not.toBeInTheDocument()

    await user.type(chat, '4')
    expect(chat).toHaveAttribute('aria-invalid', 'false')
    expect(within(form).queryByText('Chat not found')).not.toBeInTheDocument()
    expectInvalid(name, 'This field is already used by another channel')
  })

  it('toasts a 422 field the form does not show', async () => {
    const user = userEvent.setup()
    const api = mockApi(
      channelRoutes({
        'POST /v1/notification-channels': unprocessable({
          'config.chat_id': 'chat not found',
          'config.webhook_url': 'must not be set for telegram',
        }),
      }),
    )

    renderWithProviders(<ChannelFormSheet open onOpenChange={() => {}} />, { mode: 'local' })
    const form = await screen.findByTestId('channel-form')
    const chat = within(form).getByLabelText('Chat ID*')

    await user.type(within(form).getByLabelText('Bot token*'), '123456:ABCDEFGHIJKLMNOPQRSTUVWXYZ')
    await user.type(chat, '123')
    await user.click(within(form).getByRole('button', { name: /create channel/i }))
    await waitFor(() => expect(api.find('POST', '/v1/notification-channels')).toHaveLength(1))
    expectInvalid(chat, 'Chat not found')
    expect(await screen.findByText('Could not create the channel')).toBeInTheDocument()
  })

  it('ntfy, email, slack and discord mark their required fields', async () => {
    const user = userEvent.setup()

    mockApi(channelRoutes())
    renderWithProviders(<ChannelFormSheet open onOpenChange={() => {}} />, { mode: 'local' })
    const form = await screen.findByTestId('channel-form')
    const submit = () => user.click(within(form).getByRole('button', { name: /create channel/i }))

    await user.click(within(form).getByRole('radio', { name: /ntfy/i }))
    const topic = within(form).getByLabelText('Topic*')

    expectRequired(within(form).getByText('Topic', { selector: 'label' }), topic)
    await submit()
    expectInvalid(topic, '1–64 letters, digits, - or _')

    await user.click(within(form).getByRole('radio', { name: /email/i }))
    const host = within(form).getByLabelText('SMTP host*')
    const from = within(form).getByLabelText('From*')
    const to = within(form).getByLabelText('Recipients')

    expectRequired(within(form).getByText('SMTP host', { selector: 'label' }), host)
    expectRequired(within(form).getByText('To', { selector: 'label' }), to)
    await submit()
    expectInvalid(host, 'Enter the SMTP host, e.g. smtp.example.com')
    expectInvalid(from, 'An address, e.g. staqly <bot@example.com>')
    expect(to).toHaveAttribute('aria-invalid', 'true')
    expect(within(form).getByText('Add at least one recipient')).toHaveAttribute('data-slot', 'field-error')

    await user.click(within(form).getByRole('radio', { name: /slack/i }))
    expectRequired(
      within(form).getByText('Webhook URL', { selector: 'label' }),
      within(form).getByLabelText('Webhook URL*'),
    )
    await submit()
    expectInvalid(within(form).getByLabelText('Webhook URL*'), 'Paste the webhook URL')

    await user.click(within(form).getByRole('radio', { name: /discord/i }))
    await submit()
    expectInvalid(within(form).getByLabelText('Webhook URL*'), 'Paste the webhook URL')
  })
})

describe('API key form', () => {
  it('requires a name and a scope, and clears each error once fixed', async () => {
    const user = userEvent.setup()
    const api = mockApi({ 'POST /v1/api-keys': unprocessable({ expires_at: 'must be RFC3339 or YYYY-MM-DD' }) })

    renderWithProviders(<CreateKeyDialog open onOpenChange={() => {}} availableScopes={[]} onCreated={() => {}} />, {
      mode: 'cloud',
    })
    const dialog = await screen.findByRole('dialog')
    const name = within(dialog).getByLabelText('Name*')

    expectRequired(within(dialog).getByText('Name', { selector: 'label' }), name)
    // Untick the preset's scopes, one after another.
    await within(dialog)
      .getAllByRole('checkbox', { checked: true })
      .reduce((prev, box) => prev.then(() => user.click(box)), Promise.resolve())

    await user.click(within(dialog).getByRole('button', { name: /create key/i }))
    expectInvalid(name, 'Give the key a name so you know where it is used')
    expect(within(dialog).getByText('Select at least one scope')).toHaveAttribute('data-slot', 'field-error')
    expect(within(dialog).getByRole('checkbox', { name: 'items:read' })).toHaveAttribute('aria-invalid', 'true')
    expect(api.find('POST', '/v1/api-keys')).toHaveLength(0)

    await user.type(name, 'Bot')
    await user.click(within(dialog).getByRole('checkbox', { name: 'items:read' }))
    expect(within(dialog).queryByRole('alert')).not.toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: /create key/i }))
    await waitFor(() => expect(api.find('POST', '/v1/api-keys')).toHaveLength(1))
    expectInvalid(within(dialog).getByLabelText(/^expires/i), 'Must be RFC3339 or YYYY-MM-DD')
  })

  it('shows a server error on a field that passes the client checks', async () => {
    const user = userEvent.setup()
    const api = mockApi({ 'POST /v1/api-keys': unprocessable({ name: 'is already taken' }) })

    renderWithProviders(<CreateKeyDialog open onOpenChange={() => {}} availableScopes={[]} onCreated={() => {}} />, {
      mode: 'cloud',
    })
    const dialog = await screen.findByRole('dialog')
    const name = within(dialog).getByLabelText('Name*')

    await user.type(name, 'Bot')
    await user.click(within(dialog).getByRole('button', { name: /create key/i }))
    await waitFor(() => expect(api.find('POST', '/v1/api-keys')).toHaveLength(1))
    await waitFor(() => expectInvalid(name, 'This field is already taken'))
  })
})

describe('workspace settings', () => {
  it('requires a name and shows a server error under it', async () => {
    const user = userEvent.setup()
    const api = mockApi({ 'PATCH /v1/workspace': unprocessable({ name: 'is too long' }) })

    renderWithProviders(<SettingsPage />, { mode: 'local' })
    const name = screen.getByLabelText('Name*')

    expectRequired(screen.getByText('Name', { selector: 'label' }), name)
    await user.clear(name)
    await user.click(screen.getByRole('button', { name: 'Save' }))
    expectInvalid(name, 'Name is required')
    expect(api.find('PATCH', '/v1/workspace')).toHaveLength(0)

    await user.type(name, 'Acme')
    expect(name).toHaveAttribute('aria-invalid', 'false')
    await user.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(api.find('PATCH', '/v1/workspace')).toHaveLength(1))
    await waitFor(() => expectInvalid(name, 'This field is too long'))
  })
})
