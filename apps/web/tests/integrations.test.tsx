import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { ApiKeysManager } from '@/components/integrations/api-keys'
import { ApiDocsPage, ClaudeCodePage, CodexPage, McpPage, OpenCodePage } from '@/components/integrations/pages'
import { SettingsPage } from '@/components/settings/settings-page'
import { qk } from '@/lib/queries'
import { rememberApiKey } from '@/lib/session-key'

import { nav } from './navigation'
import { mockApi, renderWithProviders } from './utils'

const KEY = {
  id: 'key_1',
  name: 'Default key',
  prefix: 'stk_abcd',
  scopes: ['items:read'],
  created_at: '2026-01-01T00:00:00Z',
  last_used_at: null,
  expires_at: null,
  revoked_at: null,
}
const SCOPES = [
  'collections:read',
  'collections:write',
  'items:read',
  'items:write',
  'items:review',
  'actions:execute',
  'webhooks:manage',
  'admin',
]

describe('API keys (cloud mode)', () => {
  it('creates a key with the chosen scopes and shows it once', async () => {
    rememberApiKey(null)
    const api = mockApi({
      'GET /v1/api-keys': { data: [KEY], available_scopes: SCOPES },
      'POST /v1/api-keys': {
        status: 201,
        body: {
          ...KEY,
          id: 'key_2',
          name: 'Reviewer bot',
          scopes: ['collections:read', 'items:read', 'items:review', 'actions:execute', 'webhooks:manage'],
          key: 'stk_brand_new_secret',
        },
      },
    })

    nav.set('/integrations/api-keys')
    const user = userEvent.setup()

    renderWithProviders(<ApiKeysManager />, { mode: 'cloud' })
    expect(await screen.findByText('stk_abcd…')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /create api key/i }))
    const dialog = await screen.findByRole('dialog')

    await user.type(within(dialog).getByLabelText('Name'), 'Reviewer bot')
    await user.click(within(dialog).getByRole('button', { name: /reviewer agent/i }))
    await user.click(within(dialog).getByRole('checkbox', { name: 'webhooks:manage' }))
    await user.click(within(dialog).getByRole('button', { name: /create key/i }))

    await waitFor(() => expect(api.find('POST', '/v1/api-keys')).toHaveLength(1))
    expect(api.find('POST', '/v1/api-keys')[0].body).toEqual({
      name: 'Reviewer bot',
      scopes: ['collections:read', 'items:read', 'items:review', 'actions:execute', 'webhooks:manage'],
    })
    const reveal = await screen.findByRole('dialog', { name: /your new api key/i })

    expect(within(reveal).getByTestId('secret-New API key')).toHaveTextContent('stk_brand_new_secret')
    expect(within(reveal).getByText(/shown only once/i)).toBeInTheDocument()
    await user.click(within(reveal).getByRole('button', { name: /i've copied it/i }))
    await waitFor(() => expect(screen.queryByText('stk_brand_new_secret')).not.toBeInTheDocument())
  })
})

describe('local mode integrations', () => {
  it('hides API key management and explains cloud mode', async () => {
    const api = mockApi({})

    renderWithProviders(<ApiKeysManager />, { mode: 'local' })
    expect(screen.getByTestId('cloud-mode-card')).toHaveTextContent('STAQLY_MODE=cloud')
    expect(screen.queryByRole('button', { name: /create api key/i })).not.toBeInTheDocument()
    expect(api.find('GET', '/v1/api-keys')).toHaveLength(0)
  })

  it('shows the API URL the API reports (PUBLIC_API_URL at runtime, not a build-time value)', async () => {
    mockApi({})
    const { client } = renderWithProviders(<ClaudeCodePage />, { mode: 'local' })

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

  it('shows keyless Claude Code and Codex commands', () => {
    mockApi({})
    renderWithProviders(
      <>
        <ClaudeCodePage />
        <CodexPage />
      </>,
      { mode: 'local' },
    )
    expect(screen.getByText('claude mcp add --transport http staqly http://localhost:3333/mcp')).toBeInTheDocument()
    expect(screen.getByText('claude mcp add staqly -- docker exec -i staqly-api mcp-stdio')).toBeInTheDocument()
    expect(screen.getByText('codex mcp add staqly --url http://localhost:3333/mcp')).toBeInTheDocument()
    expect(screen.getByText('codex mcp add staqly -- docker exec -i staqly-api mcp-stdio')).toBeInTheDocument()
    // Outside the "Cloud mode" explainer nothing mentions a key.
    const outside = [...document.querySelectorAll('pre')].filter((b) => !b.closest('[data-testid=cloud-mode-card]'))

    for (const b of outside) {
      expect(b.textContent).not.toMatch(/Bearer|API_KEY/)
    }
  })

  it('curl examples have no Authorization header', async () => {
    mockApi({ 'GET /v1/collections': { data: [] } })
    renderWithProviders(<ApiDocsPage />, { mode: 'local' })
    const blocks = document.querySelectorAll('pre')

    expect(blocks.length).toBeGreaterThan(5)
    const outside = [...blocks].filter((b) => !b.closest('[data-testid=cloud-mode-card]'))

    for (const b of outside) {
      expect(b.textContent).not.toContain('Authorization')
    }
  })
})

describe('cloud mode integrations', () => {
  it('prefills snippets with the key created in this tab, never the recovery key', () => {
    rememberApiKey({ name: 'Reviewer bot', key: 'stk_in_memory' })
    mockApi({})
    renderWithProviders(<ClaudeCodePage />, { mode: 'cloud' })
    expect(
      screen.getByText(
        'claude mcp add --transport http staqly http://localhost:3333/mcp --header "Authorization: Bearer stk_in_memory"',
      ),
    ).toBeInTheDocument()
    rememberApiKey(null)
  })
})

describe('settings', () => {
  it('local mode has no sessions, recovery key or logout', async () => {
    mockApi({})
    renderWithProviders(<SettingsPage />, { mode: 'local' })
    expect(screen.getByText('Backup & restore')).toBeInTheDocument()
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

    expect(done).toBeDisabled()
    await user.click(within(dialog).getByRole('checkbox'))
    expect(done).toBeEnabled()
  })
})

describe('OpenCode page', () => {
  it('local: keyless opencode.json, stdio variant and the verification command', () => {
    mockApi({})
    renderWithProviders(<OpenCodePage />, { mode: 'local' })
    expect(screen.getByRole('heading', { name: 'OpenCode' })).toBeInTheDocument()
    const blocks = [...document.querySelectorAll('pre')].filter((b) => !b.closest('[data-testid=cloud-mode-card]'))
    const configs = blocks.map((b) => b.textContent ?? '').filter((t) => t.includes('"$schema"'))

    expect(configs).toHaveLength(2)
    expect(JSON.parse(configs[0])).toEqual({
      $schema: 'https://opencode.ai/config.json',
      mcp: { staqly: { type: 'remote', url: 'http://localhost:3333/mcp', enabled: true } },
    })
    expect(JSON.parse(configs[1])).toEqual({
      $schema: 'https://opencode.ai/config.json',
      mcp: { staqly: { type: 'local', command: ['docker', 'exec', '-i', 'staqly-api', 'mcp-stdio'], enabled: true } },
    })
    expect(screen.getByText('opencode mcp list')).toBeInTheDocument()
    expect(screen.getByText('✓ staqly connected')).toBeInTheDocument()
    blocks.forEach((b) => expect(b.textContent).not.toMatch(/Bearer|API_KEY/))
  })

  it('cloud: bearer header from {env:STAQLY_API_KEY}', () => {
    rememberApiKey(null)
    mockApi({})
    renderWithProviders(<OpenCodePage />, { mode: 'cloud' })
    const config = [...document.querySelectorAll('pre')]
      .map((b) => b.textContent ?? '')
      .find((t) => t.includes('"remote"'))!

    expect(JSON.parse(config).mcp.staqly).toEqual({
      type: 'remote',
      url: 'http://localhost:3333/mcp',
      headers: { Authorization: 'Bearer {env:STAQLY_API_KEY}' },
      enabled: true,
    })
    expect(screen.getByText('export STAQLY_API_KEY=<YOUR_API_KEY>')).toBeInTheDocument()
  })

  it('MCP page lists all 19 tools including rules and watchers', () => {
    mockApi({})
    renderWithProviders(<McpPage />, { mode: 'local' })
    const rows = screen.getAllByRole('row').slice(1)

    expect(rows).toHaveLength(19)

    for (const t of [
      'list_rules',
      'create_rule',
      'list_watcher_presets',
      'create_watcher_from_preset',
      'list_watchers',
    ]) {
      expect(screen.getByText(t)).toBeInTheDocument()
    }

    expect(screen.getByRole('link', { name: /opencode/i })).toHaveAttribute('href', '/integrations/opencode')
  })
})
