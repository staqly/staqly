import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { ApiKeysManager, SCOPE_INFO, SCOPE_PRESETS } from '@/components/integrations/api-keys'
import { IntegrationsPage } from '@/components/integrations/pages'
import { SettingsPage } from '@/components/settings/settings-page'
import { ALL_SCOPES } from '@/lib/api'
import { qk } from '@/lib/queries'
import { rememberApiKey } from '@/lib/session-key'

import { nav } from './navigation'
import { mockApi, renderWithProviders } from './utils'

const KEY = {
  id: 'key_1',
  name: 'Default key',
  prefix: 'stk_abcd',
  scopes: ['posts:read'],
  created_at: '2026-01-01T00:00:00Z',
  last_used_at: null,
  expires_at: null,
  revoked_at: null,
}
const SCOPES = [
  'collections:read',
  'collections:write',
  'collections:delete',
  'posts:read',
  'posts:write',
  'posts:delete',
  'posts:publish',
  'channels:read',
  'channels:manage',
  'media:read',
  'media:write',
  'webhooks:manage',
  'events:read',
  'admin',
]

describe('API keys (cloud mode)', () => {
  it('keeps the delete scopes out of every preset but Full access', () => {
    for (const scope of ['posts:delete', 'collections:delete'] as const) {
      expect(ALL_SCOPES).toContain(scope)
      expect(SCOPE_INFO[scope]).toMatch(/delete/i)
      const withScope = SCOPE_PRESETS.filter((p) => p.scopes.includes(scope)).map((p) => p.id)

      expect(withScope).toEqual(['full'])
    }
  })

  it('offers the delete scopes as their own checkboxes, unticked by the Content agent preset', async () => {
    rememberApiKey(null)
    const api = mockApi({
      'GET /v1/api-keys': { data: [KEY], available_scopes: SCOPES },
      'POST /v1/api-keys': { status: 201, body: { ...KEY, id: 'key_3', name: 'Cleaner', key: 'stk_cleaner_secret' } },
    })

    nav.set('/integrations/api-keys')
    const user = userEvent.setup()

    renderWithProviders(<ApiKeysManager />, { mode: 'cloud' })
    await user.click(await screen.findByRole('button', { name: /create api key/i }))
    const dialog = await screen.findByRole('dialog')

    await user.type(within(dialog).getByLabelText('Name*'), 'Cleaner')
    // Scopes are a multiselect grouped by what they touch.
    await user.click(within(dialog).getByRole('button', { name: 'Scopes' }))
    expect(await screen.findByRole('group', { name: 'Posts' })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: /^posts:write/ })).toHaveAttribute('data-checked', 'true')
    expect(screen.getByRole('option', { name: /^posts:delete/ })).toHaveAttribute('data-checked', 'false')
    expect(screen.getByRole('option', { name: /^collections:delete/ })).toHaveAttribute('data-checked', 'false')
    await user.click(screen.getByRole('option', { name: /^posts:delete/ }))
    await user.keyboard('{Escape}')
    await user.click(within(dialog).getByRole('button', { name: /create key/i }))

    await waitFor(() => expect(api.find('POST', '/v1/api-keys')).toHaveLength(1))
    expect(api.find('POST', '/v1/api-keys')[0].body).toEqual({
      // Every key expires; the default is a month.
      expires_at: expect.stringMatching(/^\d{4}-\d{2}-\d{2}T/),
      name: 'Cleaner',
      scopes: [
        'collections:read',
        'posts:read',
        'posts:write',
        'posts:publish',
        'media:read',
        'media:write',
        'posts:delete',
      ],
    })
  })

  it('creates a key with the chosen scopes and shows it once', async () => {
    rememberApiKey(null)
    const api = mockApi({
      'GET /v1/api-keys': { data: [KEY], available_scopes: SCOPES },
      'POST /v1/api-keys': {
        status: 201,
        body: {
          ...KEY,
          id: 'key_2',
          name: 'Publisher bot',
          scopes: ['posts:read', 'posts:publish', 'webhooks:manage'],
          key: 'stk_brand_new_secret',
        },
      },
    })

    nav.set('/integrations/api-keys')
    const user = userEvent.setup()

    renderWithProviders(<ApiKeysManager />, { mode: 'cloud' })
    expect(await screen.findByText('stk_abcd...')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /create api key/i }))
    const dialog = await screen.findByRole('dialog')

    await user.type(within(dialog).getByLabelText('Name*'), 'Publisher bot')
    await user.click(within(dialog).getByRole('button', { name: /publisher automation/i }))
    await user.click(within(dialog).getByRole('button', { name: 'Scopes' }))
    await user.click(await screen.findByRole('option', { name: /^webhooks:manage/ }))
    await user.keyboard('{Escape}')
    await user.click(within(dialog).getByRole('button', { name: /create key/i }))

    await waitFor(() => expect(api.find('POST', '/v1/api-keys')).toHaveLength(1))
    const body = api.find('POST', '/v1/api-keys')[0].body as { expires_at: string }

    expect(body).toEqual({
      name: 'Publisher bot',
      scopes: ['posts:read', 'posts:publish', 'webhooks:manage'],
      expires_at: expect.any(String),
    })
    // A month ahead by default (never "never").
    expect(new Date(body.expires_at).getTime() - Date.now()).toBeGreaterThan(29 * 24 * 3600_000)
    const reveal = await screen.findByRole('dialog', { name: /your new api key/i })

    expect(within(reveal).getByTestId('secret-New API key')).toHaveTextContent('stk_brand_new_secret')
    expect(within(reveal).getByText(/shown only once/i)).toBeInTheDocument()
    await user.click(within(reveal).getByRole('button', { name: /i've copied it/i }))
    await waitFor(() => expect(screen.queryByText('stk_brand_new_secret')).not.toBeInTheDocument())
  })
})

const outsideCloudCard = () =>
  [...document.querySelectorAll('pre')].filter((b) => !b.closest('[data-testid=cloud-mode-card]'))

function renderTab(tab: string, mode: 'local' | 'cloud') {
  mockApi({ 'GET /v1/events': { data: [] }, 'GET /v1/agents': { data: [] } })
  nav.set('/integrations', `tab=${tab}`)

  return renderWithProviders(<IntegrationsPage />, { mode })
}

describe('local mode integrations', () => {
  it('hides API key management and explains cloud mode', async () => {
    const api = mockApi({})

    renderWithProviders(<ApiKeysManager />, { mode: 'local' })
    expect(screen.getByTestId('cloud-mode-card')).toHaveTextContent('STAQLY_MODE=cloud')
    expect(screen.queryByRole('button', { name: /create api key/i })).not.toBeInTheDocument()
    expect(api.find('GET', '/v1/api-keys')).toHaveLength(0)
  })

  it('shows the API URL the API reports (PUBLIC_API_URL at runtime, not a build-time value)', async () => {
    const { client } = renderTab('claude-code', 'local')

    expect(screen.getByText('claude mcp add --transport http staqly http://localhost:3333/mcp')).toBeInTheDocument()
    client.setQueryData(qk.setup, {
      initialized: true,
      mode: 'local',
      signup_open: false,
      setup_token_required: false,
      public_api_url: 'https://staqly.example.com/',
    })
    expect(
      await screen.findByText('claude mcp add --transport http staqly https://staqly.example.com/mcp'),
    ).toBeInTheDocument()
  })

  it.each([
    ['claude-code', 'claude mcp add --transport http staqly http://localhost:3333/mcp', '/agents/claude-code'],
    ['codex', 'codex mcp add staqly --url http://localhost:3333/mcp', '/agents/codex'],
    ['api', 'http://localhost:3333/mcp', '/api'],
  ])('%s tab: keyless snippet and a link to the full guide', (tab, snippet, docs) => {
    renderTab(tab, 'local')
    expect(screen.getByText(snippet)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /full guide/i })).toHaveAttribute('href', `https://docs.staqly.ai${docs}`)

    for (const b of outsideCloudCard()) {
      expect(b.textContent).not.toMatch(/Bearer|API_KEY|Authorization/)
    }
  })

  it('opencode tab: keyless opencode.json', () => {
    renderTab('opencode', 'local')
    const config = outsideCloudCard().find((b) => b.textContent?.includes('"$schema"'))!

    expect(JSON.parse(config.textContent!)).toEqual({
      $schema: 'https://opencode.ai/config.json',
      mcp: {
        servers: {
          staqly: {
            type: 'remote',
            url: 'http://localhost:3333/mcp',
            codemode: false,
            timeout: { execution: 200000 },
          },
        },
      },
    })
  })

  it('switching tabs puts the tab in the URL; unknown tabs fall back to Claude Code', async () => {
    const user = userEvent.setup()

    renderTab('nope', 'local')
    expect(screen.getByRole('tab', { name: 'Claude Code' })).toHaveAttribute('aria-selected', 'true')
    await user.click(screen.getByRole('tab', { name: 'n8n' }))
    expect(nav.replace).toHaveBeenCalledWith('/integrations?tab=n8n', { scroll: false })
  })

  it('keeps connected agents on their own Agents & activity tab', () => {
    renderTab('claude-code', 'local')
    expect(screen.queryByTestId('agents')).not.toBeInTheDocument()
    expect(screen.getByTestId('agents-tab')).toHaveAttribute('href', '/integrations?tab=claude-code&view=agents')
    expect(screen.getByRole('link', { name: 'Setup' })).toHaveAttribute('aria-current', 'page')
  })
})

describe('connection options (local install)', () => {
  it('offers STDIO for Docker next to HTTP, and never shows cloud addresses or keys on a local install', async () => {
    const user = userEvent.setup()

    renderTab('claude-code', 'local')
    expect(screen.queryByRole('tab', { name: 'staqly serve' })).not.toBeInTheDocument()
    expect(screen.getByText('claude mcp add --transport http staqly http://localhost:3333/mcp')).toBeInTheDocument()
    // The transport is a choice (radios): tabs would point at tab panels that do not exist (axe aria-valid-attr-value).
    expect(screen.getByRole('radio', { name: 'HTTP' })).toHaveAttribute('aria-checked', 'true')
    expect(screen.queryByRole('tab', { name: 'HTTP' })).not.toBeInTheDocument()
    await user.click(screen.getByRole('radio', { name: 'STDIO (Docker)' }))
    expect(screen.getByText('claude mcp add staqly -- docker exec -i staqly mcp-stdio')).toBeInTheDocument()

    // A local install connects only to itself: no "Cloud instance" choice, no app.staqly.ai, no keys.
    expect(screen.queryByRole('tab', { name: 'Cloud instance' })).not.toBeInTheDocument()
    expect(screen.queryByText(/app\.staqly\.ai/)).not.toBeInTheDocument()
    expect(screen.queryByText(/Authorization: Bearer/)).not.toBeInTheDocument()

    const bar = screen.getByTestId('connection-bar')

    expect(bar).toHaveTextContent('Agents reach this workspace at')
    expect(within(bar).getByRole('button', { name: /test connection/i })).toBeInTheDocument()
  })
})

describe('cloud mode integrations', () => {
  it('prefills snippets with the key created in this tab, never the recovery key', () => {
    rememberApiKey({ name: 'Reviewer bot', key: 'stk_in_memory' })
    renderTab('claude-code', 'cloud')
    expect(
      screen.getByText(
        'claude mcp add --transport http staqly http://localhost:3333/mcp --header "Authorization: Bearer stk_in_memory"',
      ),
    ).toBeInTheDocument()
    rememberApiKey(null)
  })

  it('opencode tab: bearer header from {env:STAQLY_API_KEY}', () => {
    rememberApiKey(null)
    renderTab('opencode', 'cloud')
    const config = [...document.querySelectorAll('pre')]
      .map((b) => b.textContent ?? '')
      .find((t) => t.includes('"remote"'))!

    expect(JSON.parse(config).mcp.servers.staqly).toEqual({
      type: 'remote',
      url: 'http://localhost:3333/mcp',
      headers: { Authorization: 'Bearer {env:STAQLY_API_KEY}' },
      codemode: false,
      timeout: { execution: 200000 },
    })
    expect(screen.getByText('export STAQLY_API_KEY=<YOUR_API_KEY>')).toBeInTheDocument()
  })
})

describe('settings', () => {
  it('local mode has no sessions, recovery key or logout', async () => {
    mockApi({})
    renderWithProviders(<SettingsPage />, { mode: 'local' })
    expect(screen.getByRole('link', { name: 'Backups' })).toHaveAttribute('href', '/settings/backups')
    expect(screen.queryByText('Active sessions')).not.toBeInTheDocument()
    expect(screen.queryByText(/rotate recovery key/i)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /log out/i })).not.toBeInTheDocument()
  })

  it('cloud mode lists sessions and can rotate the recovery key', async () => {
    mockApi({
      'GET /v1/sessions': {
        data: [
          {
            id: 's1',
            user_agent: 'Mozilla/5.0 (Macintosh; Intel Mac OS X) Chrome/140',
            created_at: '2026-01-01T00:00:00Z',
            last_seen_at: '2026-01-01T00:00:00Z',
            expires_at: '2026-02-01T00:00:00Z',
            current: true,
          },
        ],
      },
      'POST /v1/recovery-key/rotate': { recovery_key: 'staqly-recovery-NEW' },
    })
    const user = userEvent.setup()

    renderWithProviders(<SettingsPage />, { mode: 'cloud' })
    expect(await screen.findByText('Chrome on macOS')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /rotate recovery key/i }))
    await user.click(await screen.findByRole('button', { name: /^rotate key$/i }))
    const dialog = await screen.findByRole('dialog', { name: /your new recovery key/i })

    expect(within(dialog).getByText('staqly-recovery-NEW')).toBeInTheDocument()
    const done = within(dialog).getByRole('button', { name: 'Done' })

    await user.click(done)
    // Still open: the checkbox says why.
    expect(within(dialog).getByRole('alert')).toHaveTextContent('Confirm that you saved the new recovery key')
    expect(within(dialog).getByRole('checkbox')).toHaveAttribute('aria-invalid', 'true')
    expect(within(dialog).getByRole('checkbox')).toHaveAttribute('aria-required', 'true')
    await user.click(within(dialog).getByRole('checkbox'))
    expect(within(dialog).queryByRole('alert')).not.toBeInTheDocument()
    await user.click(done)
    await waitFor(() =>
      expect(screen.queryByRole('dialog', { name: /your new recovery key/i })).not.toBeInTheDocument(),
    )
  })
})
