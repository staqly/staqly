import { renderHook, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'

import { ApiKeysManager } from '@/components/integrations/api-keys'
import type { APIKey } from '@/lib/api'
import { rememberApiKey, useRememberedApiKey } from '@/lib/session-key'

import { nav } from './navigation'
import { mockApi, renderWithProviders } from './utils'

function key(over: Partial<APIKey> = {}): APIKey {
  return {
    id: 'key_1',
    name: 'Publisher bot',
    prefix: 'stk_abcd',
    scopes: ['posts:read', 'posts:publish'],
    created_at: '2026-10-01T00:00:00Z',
    last_used_at: null,
    expires_at: null,
    revoked_at: null,
    ...over,
  }
}

const remembered = () => renderHook(() => useRememberedApiKey()).result.current

/** The row menu of a key, opened. */
async function menu(user: ReturnType<typeof userEvent.setup>, name: string) {
  await user.click(await screen.findByRole('button', { name: `Options for ${name}` }))

  return screen.findByRole('menu')
}

beforeEach(() => {
  nav.set('/integrations/api-keys')
  rememberApiKey(null)
})

describe('API keys', () => {
  it('lists working keys first, each with its state; a revoked key has nothing left to do', async () => {
    mockApi({
      'GET /v1/api-keys': {
        data: [
          key({
            id: 'key_gone',
            name: 'Old bot',
            revoked_at: '2026-11-01T00:00:00Z',
            created_at: '2026-11-05T00:00:00Z',
          }),
          key({ id: 'key_late', name: 'Lapsed bot', expires_at: '2026-01-01T00:00:00Z' }),
          key({ created_at: '2026-10-20T00:00:00Z' }),
        ],
        available_scopes: [],
      },
    })

    renderWithProviders(<ApiKeysManager />, { role: 'admin' })

    const rows = await screen.findAllByTestId('api-key-row')

    expect(rows.map((r) => r.textContent)).toEqual([
      expect.stringMatching(/Publisher bot.*Active/),
      expect.stringMatching(/Lapsed bot.*Expired/),
      expect.stringMatching(/Old bot.*Revoked/),
    ])
    expect(rows[0]).toHaveTextContent('Never')
    expect(rows[0]).toHaveTextContent('posts:publish')
    expect(within(rows[0]!).getByRole('button', { name: 'Options for Publisher bot' })).toBeInTheDocument()
    expect(within(rows[2]!).queryByRole('button')).not.toBeInTheDocument()
  })

  it('revokes after a confirmation that says access ends at once, and forgets the key this tab remembered', async () => {
    const api = mockApi({
      'GET /v1/api-keys': { data: [key()], available_scopes: [] },
      'DELETE /v1/api-keys/key_1': { status: 204 },
    })
    const user = userEvent.setup()

    rememberApiKey({ id: 'key_1', name: 'Publisher bot', key: 'stk_secret' })
    renderWithProviders(<ApiKeysManager />, { role: 'admin' })
    await user.click(within(await menu(user, 'Publisher bot')).getByRole('menuitem', { name: 'Revoke' }))

    const confirm = await screen.findByRole('alertdialog')

    expect(confirm).toHaveTextContent('Revoke “Publisher bot”?')
    expect(confirm).toHaveTextContent('Any agent or automation using this key loses access immediately.')
    expect(api.find('DELETE', '/v1/api-keys/key_1')).toHaveLength(0)
    await user.click(within(confirm).getByRole('button', { name: 'Revoke key' }))

    expect(await screen.findByText('API key revoked')).toBeInTheDocument()
    expect(api.find('DELETE', '/v1/api-keys/key_1')).toHaveLength(1)
    expect(remembered()).toBeNull()
  })

  it('Cancel revokes nothing, and a refusal by the server is said', async () => {
    const api = mockApi({
      'GET /v1/api-keys': { data: [key()], available_scopes: [] },
      'DELETE /v1/api-keys/key_1': {
        status: 403,
        body: { error: { code: 'forbidden', message: 'this key belongs to someone else' } },
      },
    })
    const user = userEvent.setup()

    renderWithProviders(<ApiKeysManager />, { role: 'operator' })
    await user.click(within(await menu(user, 'Publisher bot')).getByRole('menuitem', { name: 'Revoke' }))
    await user.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument())
    expect(api.find('DELETE', '/v1/api-keys/key_1')).toHaveLength(0)

    await user.click(within(await menu(user, 'Publisher bot')).getByRole('menuitem', { name: 'Revoke' }))
    await user.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Revoke key' }))
    expect(await screen.findByText('This key belongs to someone else')).toBeInTheDocument()
    expect(screen.getByTestId('api-key-row')).toHaveTextContent('Active')
  })

  it('rotates after a confirmation: the new secret is shown once and replaces the one this tab remembered', async () => {
    const api = mockApi({
      'GET /v1/api-keys': { data: [key()], available_scopes: [] },
      'POST /v1/api-keys/key_1/rotate': key({ id: 'key_2', prefix: 'stk_wxyz', key: 'stk_rotated_secret' }),
    })
    const user = userEvent.setup()

    rememberApiKey({ id: 'key_1', name: 'Publisher bot', key: 'stk_old_secret' })
    renderWithProviders(<ApiKeysManager />, { role: 'admin' })
    await user.click(within(await menu(user, 'Publisher bot')).getByRole('menuitem', { name: 'Rotate' }))

    const confirm = await screen.findByRole('alertdialog')

    expect(confirm).toHaveTextContent('Rotate “Publisher bot”?')
    expect(confirm).toHaveTextContent('The current key stops working immediately')
    await user.click(within(confirm).getByRole('button', { name: 'Rotate key' }))

    const reveal = await screen.findByRole('dialog', { name: 'Rotated API key' })

    expect(within(reveal).getByTestId('secret-New API key')).toHaveTextContent('stk_rotated_secret')
    expect(reveal).toHaveTextContent('shown only once')
    expect(api.find('POST', '/v1/api-keys/key_1/rotate')).toHaveLength(1)
    expect(remembered()).toEqual({ id: 'key_2', name: 'Publisher bot', key: 'stk_rotated_secret' })

    await user.click(within(reveal).getByRole('button', { name: "I've copied it" }))
    await waitFor(() => expect(screen.queryByText('stk_rotated_secret')).not.toBeInTheDocument())
  })

  it('a key over the plan limit is refused in plain words, with the way to the plans', async () => {
    mockApi({
      'GET /v1/api-keys': { data: [], available_scopes: ['posts:read'] },
      'POST /v1/api-keys': {
        status: 402,
        body: {
          error: {
            code: 'plan_limit_exceeded',
            message: 'plan limit reached',
            details: { limit: 'api_keys', max: 2, used: 2, plan: 'starter' },
          },
        },
      },
    })
    const user = userEvent.setup()

    renderWithProviders(<ApiKeysManager />, { role: 'admin' })
    expect(await screen.findByText('No API keys')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /create api key/i }))

    const dialog = await screen.findByRole('dialog')

    await user.type(within(dialog).getByLabelText('Name*'), 'Third bot')
    await user.click(within(dialog).getByRole('button', { name: /create key/i }))

    expect(await screen.findByText('Plan limit reached')).toBeInTheDocument()
    expect(screen.getByText('Your Starter plan includes 2 API keys (2 used).')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'View plans' })).toBeInTheDocument()
  })
})
