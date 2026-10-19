import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { SetupFlow } from '@/components/auth/setup-flow'
import { SignIn } from '@/components/auth/sign-in'

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
    name: 'Admin key',
    prefix: 'stk_abcd',
    scopes: ['admin'],
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
    const pro = screen.getByTestId('plan-pro')

    expect(within(free).getByText('$0')).toBeInTheDocument()
    expect(within(pro).getByText('$30')).toBeInTheDocument()
    expect(within(pro).getByText(/most popular/i)).toBeInTheDocument()
    expect(within(pro).getByText('Unlimited posts')).toBeInTheDocument()
    expect(within(free).getByText('50 posts / month')).toBeInTheDocument()
    expect(screen.getAllByText(/signed webhooks, full history and mcp/i).length).toBeGreaterThan(0)

    // Payments are not integrated: paid plans can't be bought here.
    for (const b of screen.getAllByRole('button', { name: /coming soon/i })) {
      expect(b).toBeDisabled()
    }

    expect(screen.queryByLabelText('Setup token*')).not.toBeInTheDocument()

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

    const ack = screen.getByRole('checkbox', { name: /i have saved my api key and recovery key/i })

    expect(ack).toHaveAttribute('aria-required', 'true')
    await user.click(open)
    expect(nav.replace).not.toHaveBeenCalled()
    expect(screen.getByText('Confirm that you saved your API key and recovery key')).toHaveAttribute(
      'data-slot',
      'field-error',
    )
    expect(ack).toHaveAttribute('aria-invalid', 'true')

    await user.click(ack)
    expect(screen.queryByText('Confirm that you saved your API key and recovery key')).not.toBeInTheDocument()
    await user.click(open)
    expect(nav.replace).toHaveBeenCalledWith('/schedule', undefined)
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
    expect(await screen.findByText('Setup token is required')).toHaveAttribute('data-slot', 'field-error')
    expect(screen.getByLabelText('Setup token*')).toHaveAttribute('aria-invalid', 'true')
    expect(screen.getByLabelText('Setup token*')).toHaveAttribute('aria-required', 'true')
    expect(screen.getByLabelText('Setup token*').closest('[data-slot=field]')).toHaveAttribute('data-invalid', 'true')
    expect(api.find('POST', '/v1/workspaces')).toHaveLength(0)

    await user.type(screen.getByLabelText('Setup token*'), 'dev-token')
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

  it('toasts a 403 when the server asks for no setup token (no hidden field error)', async () => {
    mockApi({
      'GET /v1/setup': { initialized: false, mode: 'cloud', signup_open: true, setup_token_required: false },
      'GET /v1/plans': { data: PLANS },
      'POST /v1/workspaces': { status: 403, body: { error: { code: 'forbidden', message: 'sign-ups are closed' } } },
    })
    const user = userEvent.setup()

    renderWithProviders(<SetupFlow />)
    await user.click(await screen.findByRole('button', { name: /create free workspace/i }))
    expect(await screen.findByText(/sign-ups are closed/i)).toBeInTheDocument()
    expect(screen.queryByLabelText('Setup token*')).not.toBeInTheDocument()
    expect(screen.getByLabelText('Workspace name')).toHaveAttribute('aria-invalid', 'false')
    expect(screen.queryByTestId('setup-secrets')).not.toBeInTheDocument()
  })

  it('shows a 403 under the setup token when the server asks for one', async () => {
    mockApi({
      'GET /v1/setup': { initialized: false, mode: 'cloud', signup_open: false, setup_token_required: true },
      'GET /v1/plans': { data: PLANS },
      'POST /v1/workspaces': { status: 403, body: { error: { code: 'forbidden', message: 'invalid setup token' } } },
    })
    const user = userEvent.setup()

    renderWithProviders(<SetupFlow />)
    await user.type(await screen.findByLabelText('Setup token*'), 'wrong')
    await user.click(screen.getByRole('button', { name: /create free workspace/i }))
    expect(await screen.findByText('invalid setup token')).toHaveAttribute('data-slot', 'field-error')
    expect(screen.getByLabelText('Setup token*')).toHaveAttribute('aria-invalid', 'true')
  })

  it('with Stripe configured, paid plans point to upgrading after sign-up instead of "coming soon"', async () => {
    const api = mockApi({
      'GET /v1/setup': {
        initialized: false,
        mode: 'cloud',
        signup_open: true,
        setup_token_required: false,
        billing_enabled: true,
      },
      'GET /v1/plans': { data: PLANS },
      'POST /v1/workspaces': { status: 201, body: SETUP_RESULT },
    })
    const user = userEvent.setup()

    renderWithProviders(<SetupFlow />)
    await screen.findByTestId('plan-pro')
    expect(screen.queryByRole('button', { name: /coming soon/i })).not.toBeInTheDocument()
    expect(screen.queryByText(/paid plans are coming soon/i)).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /upgrade after sign-up/i }))
    expect(screen.getByLabelText('Workspace name')).toHaveFocus()

    await user.click(screen.getByRole('button', { name: /create free workspace/i }))
    await screen.findByTestId('setup-secrets')
    expect(api.find('POST', '/v1/workspaces')).toHaveLength(1)
    expect(screen.getByTestId('setup-plan')).toHaveTextContent(/upgrade any time under settings/i)
    expect(screen.getByTestId('setup-plan')).not.toHaveTextContent(/coming soon/i)
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
  it('skips onboarding and goes straight to the schedule', async () => {
    const api = mockApi({ 'GET /v1/setup': { initialized: true, mode: 'local', setup_token_required: false } })

    renderWithProviders(<SetupFlow />)
    await waitFor(() => expect(nav.replace).toHaveBeenCalledWith('/schedule', undefined))
    expect(screen.queryByLabelText('Workspace name')).not.toBeInTheDocument()
    expect(screen.queryByText(/recovery key/i)).not.toBeInTheDocument()
    expect(api.find('POST', '/v1/workspaces')).toHaveLength(0)
  })

  it('skips the recovery login page', async () => {
    mockApi({ 'GET /v1/setup': { initialized: true, mode: 'local', setup_token_required: false } })
    renderWithProviders(<SignIn />)
    await waitFor(() => expect(nav.replace).toHaveBeenCalledWith('/schedule', undefined))
  })
})

const RECOVER_RESULT = {
  csrf_token: 'c',
  api_key: { ...SETUP_RESULT.api_key, id: 'key_2', name: 'Admin key (recovered)', key: 'stk_new_admin_key' },
  recovery_key: 'staqly-recovery-NEXTT-KEYYY',
}

describe('sign in with an API key (cloud mode)', () => {
  it('exchanges an admin API key for a session', async () => {
    const api = mockApi({
      'GET /v1/setup': { initialized: true, mode: 'cloud', setup_token_required: true },
      'POST /v1/sessions/api-key': { status: 201, body: { csrf_token: 'c' } },
    })

    nav.set('/login', 'next=/webhooks')
    const user = userEvent.setup()

    renderWithProviders(<SignIn />)
    expect(screen.getByRole('heading', { name: /sign in with your api key/i })).toBeInTheDocument()
    await user.type(screen.getByLabelText('API key*'), ' stk_admin ')
    await user.click(screen.getByRole('button', { name: /sign in/i }))
    await waitFor(() => expect(nav.replace).toHaveBeenCalledWith('/webhooks', undefined))
    expect(api.find('POST', '/v1/sessions/api-key')[0].body).toEqual({ api_key: 'stk_admin' })
  })

  it('offers sign-up and never bounces from /login to /setup', async () => {
    mockApi({ 'GET /v1/setup': { initialized: true, mode: 'cloud', signup_open: true, setup_token_required: false } })
    renderWithProviders(<SignIn />)
    expect(await screen.findByRole('link', { name: /create one free/i })).toHaveAttribute('href', '/setup')
    // Cloud mode never bounces from /login to /setup ("initialized" is meaningless there).
    expect(nav.replace).not.toHaveBeenCalled()
  })

  it('requires the key: asterisk, aria-required and an error under the field', async () => {
    const api = mockApi({ 'GET /v1/setup': { initialized: true, mode: 'cloud', setup_token_required: true } })
    const user = userEvent.setup()

    renderWithProviders(<SignIn />)
    const key = screen.getByLabelText('API key*')

    expect(key).toHaveAttribute('aria-required', 'true')
    expect(key).toHaveAttribute('type', 'password')
    await user.click(screen.getByRole('button', { name: /sign in/i }))
    expect(await screen.findByRole('alert')).toHaveAttribute('data-slot', 'field-error')
    expect(key).toHaveAttribute('aria-invalid', 'true')
    expect(api.find('POST', '/v1/sessions/api-key')).toHaveLength(0)
  })

  it.each([
    [401, 'unauthorized', /not valid/i],
    [403, 'forbidden', /only keys with the admin scope sign in/i],
  ])('explains a %i under the field', async (status, code, text) => {
    mockApi({
      'GET /v1/setup': { initialized: true, mode: 'cloud', setup_token_required: true },
      'POST /v1/sessions/api-key': { status, body: { error: { code, message: 'nope' } } },
    })
    const user = userEvent.setup()

    renderWithProviders(<SignIn />)
    await user.type(screen.getByLabelText('API key*'), 'stk_agent')
    await user.click(screen.getByRole('button', { name: /sign in/i }))
    expect(await screen.findByText(text)).toBeInTheDocument()
    expect(nav.replace).not.toHaveBeenCalled()
  })
})

async function openRecovery() {
  const user = userEvent.setup()

  renderWithProviders(<SignIn />)
  await user.click(screen.getByRole('button', { name: /lost your key/i }))
  expect(screen.getByRole('heading', { name: /recover your workspace/i })).toBeInTheDocument()

  return user
}

describe('recover access (cloud mode)', () => {
  it('exchanges the recovery key for a session and shows the new keys once before opening the dashboard', async () => {
    const api = mockApi({
      'GET /v1/setup': { initialized: true, mode: 'cloud', setup_token_required: true },
      'POST /v1/sessions/recover': { status: 201, body: RECOVER_RESULT },
    })

    nav.set('/login', 'next=/webhooks')
    const user = await openRecovery()

    expect(screen.getByText(/a recovery key works once/i)).toBeInTheDocument()
    await user.type(screen.getByLabelText('Recovery key*'), 'staqly-recovery-XYZ')
    await user.click(screen.getByRole('button', { name: /recover access/i }))
    expect(await screen.findByTestId('recovered-secrets')).toBeInTheDocument()
    expect(api.find('POST', '/v1/sessions/recover')[0].body).toEqual({ recovery_key: 'staqly-recovery-XYZ' })
    expect(screen.getByTestId('secret-API key')).toHaveTextContent('stk_new_admin_key')
    expect(screen.getByTestId('secret-Recovery key')).toHaveTextContent('staqly-recovery-NEXTT-KEYYY')
    expect(screen.getByText(/no longer works/i)).toBeInTheDocument()

    const open = screen.getByRole('button', { name: /open dashboard/i })

    await user.click(open)
    expect(nav.replace).not.toHaveBeenCalled()
    expect(screen.getByText('Confirm that you saved your new keys')).toHaveAttribute('data-slot', 'field-error')
    await user.click(screen.getByRole('checkbox', { name: /i have saved my new api key and recovery key/i }))
    await user.click(open)
    expect(nav.replace).toHaveBeenCalledWith('/webhooks', undefined)
  })

  it('goes back to the API key form', async () => {
    mockApi({ 'GET /v1/setup': { initialized: true, mode: 'cloud', setup_token_required: true } })
    const user = await openRecovery()

    await user.click(screen.getByRole('button', { name: /sign in with your api key/i }))
    expect(screen.getByLabelText('API key*')).toBeInTheDocument()
  })

  it('requires the recovery key: asterisk, aria-required and an error under the field', async () => {
    const api = mockApi({ 'GET /v1/setup': { initialized: true, mode: 'cloud', setup_token_required: true } })
    const user = await openRecovery()
    const key = screen.getByLabelText('Recovery key*')

    expect(key).toHaveAttribute('aria-required', 'true')
    await user.click(screen.getByRole('button', { name: /recover access/i }))
    expect(await screen.findByRole('alert')).toHaveAttribute('data-slot', 'field-error')
    expect(key).toHaveAttribute('aria-invalid', 'true')
    expect(key.closest('[data-slot=field]')).toHaveAttribute('data-invalid', 'true')
    expect(api.find('POST', '/v1/sessions/recover')).toHaveLength(0)
    await user.type(key, 'staqly-recovery-X')
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('shows an inline error for a wrong or already used key', async () => {
    mockApi({
      'GET /v1/setup': { initialized: true, mode: 'cloud', setup_token_required: true },
      'POST /v1/sessions/recover': {
        status: 401,
        body: { error: { code: 'unauthorized', message: 'invalid recovery key' } },
      },
    })
    const user = await openRecovery()

    await user.type(screen.getByLabelText('Recovery key*'), 'nope')
    await user.click(screen.getByRole('button', { name: /recover access/i }))
    expect(await screen.findByText(/each recovery key works only once/i)).toBeInTheDocument()
  })
})
