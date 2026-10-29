import { act, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { WelcomeGate } from '@/components/onboarding/welcome-redirect'
import { WelcomeWizard } from '@/components/onboarding/welcome-wizard'
import { api as apiClient } from '@/lib/api'
import { qk } from '@/lib/queries'

import { nav } from './navigation'
import { PLANS } from './plans-fixtures'
import { makeClient, mockApi, renderWithProviders, sessionFixture } from './utils'

const routes = {
  'GET /v1/providers': { data: [] },
  'GET /v1/channels': { data: [] },
  'POST /v1/session/onboarded': { status: 204 },
}

function render(extra: Record<string, unknown>, ui = <WelcomeWizard />) {
  // The session is in place before the first render, as the auth gate guarantees in the app.
  const client = makeClient()

  client.setQueryData(qk.setup, { initialized: true, mode: 'cloud', signup_open: true, setup_token_required: false })
  client.setQueryData(qk.session, {
    ...sessionFixture('cloud'),
    user: { id: 'usr_1', name: 'ana@example.com', email: 'ana@example.com', avatar_url: '' },
    accounts_enabled: true,
    role: 'owner',
    onboarded: false,
    ...extra,
  })

  return renderWithProviders(ui, { client })
}

describe('welcome wizard', () => {
  beforeEach(() => {
    nav.set('/welcome')
    sessionStorage.clear()
  })

  it('collects everything first and sets the workspace up only at the end', async () => {
    const api = mockApi({
      ...routes,
      'PATCH /v1/workspace': { body: { id: 'ws_1', name: 'Acme' } },
      'POST /v1/invitations': { status: 201, body: {} },
      'POST /v1/api-keys': { status: 201, body: { id: 'key_1', name: 'Claude Code', key: 'stk_agent_secret' } },
    })
    const user = userEvent.setup()

    render({})
    expect(await screen.findByRole('heading', { name: 'Welcome to Staqly' })).toBeInTheDocument()
    // An email address is never used as the greeting's name.
    expect(screen.getByLabelText('What should we call you?')).toHaveValue('')
    await user.type(screen.getByLabelText('What should we call you?'), 'Ana')
    await user.click(screen.getByLabelText(/Run a team/))
    await user.click(screen.getByRole('button', { name: /Continue/ }))

    expect(await screen.findByRole('heading', { name: 'Name your workspace' })).toBeInTheDocument()
    expect(screen.getByLabelText(/Workspace name/)).toHaveValue('Test WS')
    await user.clear(screen.getByLabelText(/Workspace name/))
    await user.type(screen.getByLabelText(/Workspace name/), 'Acme')
    await user.click(screen.getByRole('button', { name: /Continue/ }))

    expect(await screen.findByRole('heading', { name: 'Where do you post?' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /do it later/ }))

    // The team goal asks for people first; connecting an agent is its own step after it.
    expect(await screen.findByRole('heading', { name: 'Invite your team' })).toBeInTheDocument()
    await user.type(screen.getByLabelText('Email addresses'), 'sam@example.com')
    await user.click(screen.getByRole('button', { name: /Invite 1 when ready/ }))
    expect(await screen.findByRole('heading', { name: 'Connect an agent' })).toBeInTheDocument()
    // Nothing was written on the way.
    expect(api.calls.filter((c) => c.method !== 'GET')).toHaveLength(0)
    // The workspace can write already (no billing): the key is made here, on the explicit ask.
    await user.click(screen.getByRole('button', { name: /Make a key for Claude Code/ }))
    expect((await screen.findAllByText(/stk_agent_secret/)).length).toBeGreaterThan(0)
    expect(api.find('POST', '/v1/api-keys')).toHaveLength(1)
    await user.click(screen.getByRole('button', { name: /Continue/ }))

    expect(await screen.findByRole('heading', { name: 'You are all set' })).toBeInTheDocument()
    expect(api.find('PATCH', '/v1/workspace')[0].body).toMatchObject({ name: 'Acme' })
    expect(api.find('POST', '/v1/invitations')[0].body).toEqual({ email: 'sam@example.com', role: 'editor' })
    // Not a second key at the end, and the end doesn't show it again.
    expect(api.find('POST', '/v1/api-keys')).toHaveLength(1)
    expect(screen.getByText('1 invitation sent')).toBeInTheDocument()
    expect(screen.queryByText(/stk_agent_secret/)).not.toBeInTheDocument()
    // The last step ends the wizard itself: nothing left to skip.
    expect(screen.queryByRole('button', { name: 'Skip for now' })).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /Go to the calendar/ }))
    await waitFor(() => expect(api.find('POST', '/v1/session/onboarded')).toHaveLength(1))
    expect(api.find('POST', '/v1/session/onboarded')[0].body).toEqual({ name: 'Ana' })
    await waitFor(() => expect(nav.replace).toHaveBeenCalledWith('/schedule', undefined))
  })

  it('without a plan: the plan is the last step, with no way around it', async () => {
    const api = mockApi({
      ...routes,
      'GET /v1/billing': { enabled: true, subscription_status: null, suspended: true, trial: 'available' },
      'GET /v1/plans': { data: PLANS },
      'POST /v1/billing/checkout': { url: 'https://checkout.stripe.test/cs_1' },
    })
    const assign = vi.fn<(url: string) => void>()

    vi.stubGlobal('location', { ...window.location, assign })
    nav.set('/welcome', 'step=team')
    render({})
    const user = userEvent.setup()

    // Invite your team is the last step before the plan (a creator's agent step comes before it).
    expect(await screen.findByRole('heading', { name: 'Invite your team' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Skip' }))

    expect(await screen.findByRole('heading', { name: 'Choose a plan' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Skip for now' })).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: /Compare all plans/ })).toHaveAttribute('href', '/plans?from=welcome')
    await user.click(await screen.findByTestId('welcome-plan-starter'))
    await user.click(screen.getByTestId('start-trial'))
    await waitFor(() => expect(assign).toHaveBeenCalledWith('https://checkout.stripe.test/cs_1'))
    // Still leaving for Stripe: the button stays busy so a second click can't open another checkout.
    expect(screen.getByTestId('start-trial')).toBeDisabled()
    expect(api.find('POST', '/v1/billing/checkout')[0].body).toEqual({
      plan: 'starter',
      interval: 'month',
      return_to: 'welcome',
    })
    vi.unstubAllGlobals()
  })

  it('back from Stripe: waits for the plan, then sets the workspace up', async () => {
    let paid = false
    const api = mockApi({
      ...routes,
      'GET /v1/billing': () => ({ enabled: true, subscription_status: paid ? 'trialing' : null, suspended: !paid }),
      // Back from Stripe the wizard asks the server to read the subscription from Stripe (no webhook needed).
      'POST /v1/billing/sync': () => ({
        enabled: true,
        subscription_status: paid ? 'trialing' : null,
        suspended: !paid,
      }),
      'GET /v1/workspace/usage': { suspended: false },
      'PATCH /v1/workspace': { body: { id: 'ws_1', name: 'Acme' } },
    })

    sessionStorage.setItem('staqly:welcome', JSON.stringify({ name: 'Ana', workspaceName: 'Acme' }))
    nav.set('/welcome', 'step=done&checkout=success')
    render({})
    expect(await screen.findByRole('heading', { name: 'Setting up your workspace' })).toBeInTheDocument()
    expect(api.find('PATCH', '/v1/workspace')).toHaveLength(0)
    paid = true
    expect(await screen.findByRole('heading', { name: 'You are all set' })).toBeInTheDocument()
    expect(api.find('PATCH', '/v1/workspace')[0].body).toMatchObject({ name: 'Acme' })
    expect(api.find('POST', '/v1/billing/sync').length).toBeGreaterThan(0)
  })

  it('back from Stripe: a slow payment check is not asked again while it runs', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
    mockApi({
      ...routes,
      'GET /v1/billing': { enabled: true, subscription_status: null, suspended: true },
      'GET /v1/workspace/usage': { suspended: true },
    })
    // Stripe takes its time: the first check doesn't answer within the test.
    const sync = vi.spyOn(apiClient, 'syncBilling').mockReturnValue(new Promise(() => {}))

    sessionStorage.setItem('staqly:welcome', JSON.stringify({ name: 'Ana', workspaceName: 'Acme' }))
    nav.set('/welcome', 'step=done&checkout=success')
    render({})
    expect(await screen.findByRole('heading', { name: 'Setting up your workspace' })).toBeInTheDocument()
    act(() => vi.advanceTimersByTime(10_000))
    expect(sync).toHaveBeenCalledTimes(1)
    sync.mockRestore()
    vi.useRealTimers()
  })

  it('Not now on Connect an agent makes no key', async () => {
    const api = mockApi({ ...routes, 'POST /v1/api-keys': { status: 201, body: { id: 'k', name: 'x', key: 'stk_x' } } })
    const user = userEvent.setup()

    nav.set('/welcome', 'step=agent')
    render({})
    expect(await screen.findByRole('heading', { name: 'Connect an agent' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Not now' }))
    expect(await screen.findByRole('heading', { name: 'Invite your team' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Skip' }))
    expect(await screen.findByRole('heading', { name: 'You are all set' })).toBeInTheDocument()
    expect(api.find('POST', '/v1/api-keys')).toHaveLength(0)
  })

  it('an agent chosen before the payment gets its key on a step of its own right after it', async () => {
    const api = mockApi({
      ...routes,
      'POST /v1/api-keys': { status: 201, body: { id: 'key_1', name: 'Codex', key: 'stk_agent_secret' } },
    })
    const user = userEvent.setup()

    sessionStorage.setItem('staqly:welcome', JSON.stringify({ name: 'Ana', agent: 'codex' }))
    nav.set('/welcome', 'step=done&checkout=success')
    render({})
    expect(await screen.findByRole('heading', { name: 'Connect Codex' })).toBeInTheDocument()
    expect(screen.getAllByText(/stk_agent_secret/).length).toBeGreaterThan(0)
    expect(api.find('POST', '/v1/api-keys')[0].body).toMatchObject({ name: 'Codex' })
    await user.click(screen.getByRole('button', { name: /Continue/ }))
    expect(await screen.findByRole('heading', { name: 'You are all set' })).toBeInTheDocument()
    expect(screen.queryByText(/stk_agent_secret/)).not.toBeInTheDocument()
  })

  it('setting up again after a failure counts people already invited as invited', async () => {
    const api = mockApi({
      ...routes,
      'POST /v1/invitations': {
        status: 409,
        body: { error: { code: 'already_invited', message: 'ana@example.com already has an open invitation' } },
      },
    })

    sessionStorage.setItem(
      'staqly:welcome',
      JSON.stringify({ name: 'Ana', invites: { emails: ['ana@example.com'], role: 'editor' } }),
    )
    nav.set('/welcome', 'step=done')
    render({})
    expect(await screen.findByRole('heading', { name: 'You are all set' })).toBeInTheDocument()
    expect(api.find('POST', '/v1/invitations')).toHaveLength(1)
    expect(screen.getByText('1 invitation sent')).toBeInTheDocument()
    expect(screen.queryByText(/Not sent/)).not.toBeInTheDocument()
  })

  it('an invited member gets the short version: their name and their role', async () => {
    const api = mockApi(routes)
    const user = userEvent.setup()

    render({
      role: 'editor',
      user: { id: 'usr_2', name: 'Sam', email: 's@x.io', avatar_url: '' },
    })
    expect(await screen.findByText('Step 1 of 2')).toBeInTheDocument()
    expect(screen.getByLabelText('What should we call you?')).toHaveValue('Sam')
    expect(screen.queryByText('What brings you here?')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /Continue/ }))
    expect(await screen.findByRole('heading', { name: 'You joined Test WS' })).toBeInTheDocument()
    expect(screen.getByText('You are an editor')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /Go to the calendar/ }))
    await waitFor(() => expect(api.find('POST', '/v1/session/onboarded')).toHaveLength(1))
  })

  it('Skip for now ends it at once', async () => {
    const api = mockApi(routes)
    const user = userEvent.setup()

    render({})
    await user.click(await screen.findByRole('button', { name: 'Skip for now' }))
    await waitFor(() => expect(api.find('POST', '/v1/session/onboarded')).toHaveLength(1))
    await waitFor(() => expect(nav.replace).toHaveBeenCalledWith('/schedule', undefined))
  })
})

describe('welcome redirect', () => {
  it('sends someone who has not seen the welcome from the calendar to it, once', async () => {
    mockApi({})
    nav.reset()
    nav.set('/schedule')
    render(
      {},
      <WelcomeGate>
        <p>calendar</p>
      </WelcomeGate>,
    )
    await waitFor(() => expect(nav.replace).toHaveBeenCalledWith('/welcome', undefined))
    expect(screen.queryByText('calendar')).not.toBeInTheDocument()
  })

  it('leaves deep links alone', async () => {
    mockApi({})
    nav.reset()
    nav.set('/posts')
    render(
      {},
      <WelcomeGate>
        <p>calendar</p>
      </WelcomeGate>,
    )
    await new Promise((r) => setTimeout(r, 20))
    expect(nav.replace).not.toHaveBeenCalled()
  })

  it('leaves people who finished it alone', async () => {
    mockApi({})
    nav.reset()
    nav.set('/schedule')
    render(
      { onboarded: true },
      <WelcomeGate>
        <p>calendar</p>
      </WelcomeGate>,
    )
    await new Promise((r) => setTimeout(r, 20))
    expect(nav.replace).not.toHaveBeenCalled()
  })
})
