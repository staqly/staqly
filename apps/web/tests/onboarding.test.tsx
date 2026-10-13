import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { RecoverForm } from '@/components/auth/recover-form'
import { SetupFlow } from '@/components/auth/setup-flow'

import { nav } from './navigation'
import { PLANS } from './plans-fixtures'
import { mockApi, renderWithProviders } from './utils'

const SETUP_RESULT = {
  workspace: {
    id: 'ws_1',
    name: 'Acme',
    plan: 'free',
    recovery_rotated_at: '2026-01-01T00:00:00Z',
    created_at: '2026-01-01T00:00:00Z',
  },
  api_key: {
    id: 'key_1',
    name: 'Default key',
    prefix: 'stk_abcd',
    scopes: ['items:read', 'items:write'],
    created_at: '2026-01-01T00:00:00Z',
    last_used_at: null,
    expires_at: null,
    revoked_at: null,
    key: 'stk_secret_api_key_value',
  },
  recovery_key: 'staqly-recovery-AAAAA-BBBBB-CCCCC',
  csrf_token: 'csrf-new',
}

describe('cloud signup (create your free workspace)', () => {
  it('shows the plans from /v1/plans and creates a workspace on the Free plan', async () => {
    const api = mockApi({
      'GET /v1/setup': { initialized: false, mode: 'cloud', signup_open: true, setup_token_required: false },
      'GET /v1/plans': { data: PLANS },
      'POST /v1/workspaces': { status: 201, body: SETUP_RESULT },
    })
    const user = userEvent.setup()

    renderWithProviders(<SetupFlow />)

    expect(await screen.findByRole('heading', { name: /create your free workspace/i })).toBeInTheDocument()
    expect(screen.getByText(/the workspace for everything your ai agents create/i)).toBeInTheDocument()

    // Plan cards rendered from the API.
    const free = await screen.findByTestId('plan-free')
    const indie = screen.getByTestId('plan-indie')
    const team = screen.getByTestId('plan-team')

    expect(within(free).getByText('$0')).toBeInTheDocument()
    expect(within(indie).getByText('$9')).toBeInTheDocument()
    expect(within(indie).getByText(/most popular/i)).toBeInTheDocument()
    expect(within(team).getByText('$29')).toBeInTheDocument()
    expect(within(team).getByText('Unlimited items')).toBeInTheDocument()
    expect(screen.getAllByText(/signed webhooks, full history and mcp/i).length).toBeGreaterThan(0)

    // Payments are not integrated: paid plans can't be bought here.
    for (const b of screen.getAllByRole('button', { name: /coming soon/i })) {
      expect(b).toBeDisabled()
    }

    expect(screen.queryByLabelText('Setup token')).not.toBeInTheDocument()

    await user.type(screen.getByLabelText('Workspace name'), 'Acme')
    await user.click(screen.getByRole('button', { name: /create free workspace/i }))

    expect(await screen.findByTestId('setup-secrets')).toBeInTheDocument()
    expect(api.find('POST', '/v1/workspaces')[0].body).toEqual({ name: 'Acme' })
    expect(screen.getByTestId('setup-plan')).toHaveTextContent(/free plan/i)
    expect(screen.getByTestId('secret-API key')).toHaveTextContent('stk_secret_api_key_value')
    expect(screen.getByTestId('secret-Recovery key')).toHaveTextContent('staqly-recovery-AAAAA-BBBBB-CCCCC')
    expect(screen.getByText(/permanently locked out/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /copy recovery key/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /download keys as .txt/i })).toBeInTheDocument()

    const open = screen.getByRole('button', { name: /open dashboard/i })

    expect(open).toBeDisabled()
    await user.click(open)
    expect(nav.replace).not.toHaveBeenCalled()

    await user.click(screen.getByRole('checkbox', { name: /i have saved my recovery key/i }))
    expect(open).toBeEnabled()
    await user.click(open)
    expect(nav.replace).toHaveBeenCalledWith('/inbox', undefined)
  })

  it('asks for the setup token only when the server requires one', async () => {
    const api = mockApi({
      'GET /v1/setup': { initialized: false, mode: 'cloud', signup_open: false, setup_token_required: true },
      'GET /v1/plans': { data: PLANS },
      'POST /v1/workspaces': { status: 201, body: SETUP_RESULT },
    })
    const user = userEvent.setup()

    renderWithProviders(<SetupFlow />)
    await user.type(await screen.findByLabelText('Workspace name'), 'Acme')
    await user.click(screen.getByRole('button', { name: /create free workspace/i }))
    expect(await screen.findByText('Setup token is required')).toBeInTheDocument()
    expect(api.find('POST', '/v1/workspaces')).toHaveLength(0)

    await user.type(screen.getByLabelText('Setup token'), 'dev-token')
    await user.click(screen.getByRole('button', { name: /create free workspace/i }))
    await screen.findByTestId('setup-secrets')
    expect(api.find('POST', '/v1/workspaces')[0].body).toEqual({ name: 'Acme', setup_token: 'dev-token' })
  })

  it('omits name and setup_token when not given', async () => {
    const api = mockApi({
      'GET /v1/setup': { initialized: false, mode: 'cloud', signup_open: true, setup_token_required: false },
      'GET /v1/plans': { data: PLANS },
      'POST /v1/workspaces': { status: 201, body: SETUP_RESULT },
    })
    const user = userEvent.setup()

    renderWithProviders(<SetupFlow />)
    await user.click(await screen.findByRole('button', { name: /create free workspace/i }))
    await screen.findByTestId('setup-secrets')
    expect(api.find('POST', '/v1/workspaces')[0].body).toEqual({})
  })

  it('explains a 429 from the per-IP signup rate limit', async () => {
    mockApi({
      'GET /v1/setup': { initialized: false, mode: 'cloud', signup_open: true, setup_token_required: false },
      'GET /v1/plans': { data: PLANS },
      'POST /v1/workspaces': { status: 429, body: { error: { code: 'rate_limited', message: 'too many requests' } } },
    })
    const user = userEvent.setup()

    renderWithProviders(<SetupFlow />)
    await user.click(await screen.findByRole('button', { name: /create free workspace/i }))
    expect(await screen.findByRole('alert')).toHaveTextContent(/too many workspaces were created from your network/i)
    expect(screen.queryByTestId('setup-secrets')).not.toBeInTheDocument()
  })

  it('shows a closed state when sign-ups are disabled', async () => {
    mockApi({
      'GET /v1/setup': { initialized: false, mode: 'cloud', signup_open: false, setup_token_required: false },
      'GET /v1/plans': { data: PLANS },
    })
    renderWithProviders(<SetupFlow />)
    expect(await screen.findByText(/sign-ups are closed on this server/i)).toBeInTheDocument()
    expect(screen.queryByLabelText('Workspace name')).not.toBeInTheDocument()
  })
})

describe('local mode', () => {
  it('skips onboarding and goes straight to the inbox', async () => {
    const api = mockApi({ 'GET /v1/setup': { initialized: true, mode: 'local', setup_token_required: false } })

    renderWithProviders(<SetupFlow />)
    await waitFor(() => expect(nav.replace).toHaveBeenCalledWith('/inbox', undefined))
    expect(screen.queryByLabelText('Workspace name')).not.toBeInTheDocument()
    expect(screen.queryByText(/recovery key/i)).not.toBeInTheDocument()
    expect(api.find('POST', '/v1/workspaces')).toHaveLength(0)
  })

  it('skips the recovery login page', async () => {
    mockApi({ 'GET /v1/setup': { initialized: true, mode: 'local', setup_token_required: false } })
    renderWithProviders(<RecoverForm />)
    await waitFor(() => expect(nav.replace).toHaveBeenCalledWith('/inbox', undefined))
  })
})

describe('recover access (cloud mode)', () => {
  it('exchanges the recovery key for a session', async () => {
    const api = mockApi({
      'GET /v1/setup': { initialized: true, mode: 'cloud', setup_token_required: true },
      'POST /v1/sessions/recover': { status: 201, body: { csrf_token: 'c' } },
    })

    nav.set('/login', 'next=/webhooks')
    const user = userEvent.setup()

    renderWithProviders(<RecoverForm />)
    await user.type(screen.getByLabelText('Recovery key'), 'staqly-recovery-XYZ')
    await user.click(screen.getByRole('button', { name: /sign in/i }))
    await waitFor(() => expect(nav.replace).toHaveBeenCalledWith('/webhooks', undefined))
    expect(api.find('POST', '/v1/sessions/recover')[0].body).toEqual({ recovery_key: 'staqly-recovery-XYZ' })
  })

  it('explains that each workspace is opened with its own recovery key', async () => {
    mockApi({ 'GET /v1/setup': { initialized: true, mode: 'cloud', signup_open: true, setup_token_required: false } })
    renderWithProviders(<RecoverForm />)
    expect(await screen.findByRole('heading', { name: /sign in to your workspace/i })).toBeInTheDocument()
    expect(screen.getByText(/each workspace has its own recovery key/i)).toBeInTheDocument()
    expect(await screen.findByRole('link', { name: /create one free/i })).toHaveAttribute('href', '/setup')
    // Cloud mode never bounces from /login to /setup ("initialized" is meaningless there).
    expect(nav.replace).not.toHaveBeenCalled()
  })

  it('shows an inline error for a wrong key', async () => {
    mockApi({
      'GET /v1/setup': { initialized: true, mode: 'cloud', setup_token_required: true },
      'POST /v1/sessions/recover': {
        status: 401,
        body: { error: { code: 'unauthorized', message: 'invalid recovery key' } },
      },
    })
    const user = userEvent.setup()

    renderWithProviders(<RecoverForm />)
    await user.type(screen.getByLabelText('Recovery key'), 'nope')
    await user.click(screen.getByRole('button', { name: /sign in/i }))
    expect(await screen.findByText(/not valid for this workspace/i)).toBeInTheDocument()
  })
})
