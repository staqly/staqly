import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'

import { WelcomeGate } from '@/components/onboarding/welcome-redirect'
import { WelcomeWizard } from '@/components/onboarding/welcome-wizard'
import { qk } from '@/lib/queries'

import { nav } from './navigation'
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
    account_owner: true,
    role: 'owner',
    onboarded: false,
    ...extra,
  })

  return renderWithProviders(ui, { client })
}

describe('welcome wizard', () => {
  beforeEach(() => nav.set('/welcome'))

  it('walks a new owner through name, goal, workspace, channel and the next step, then finishes', async () => {
    const api = mockApi(routes)
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
    await user.click(screen.getByRole('button', { name: /Continue/ }))

    expect(await screen.findByRole('heading', { name: 'Connect your first channel' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /do it later/ }))

    // The team goal asks for people first; connecting an agent is its own step after it. Both can be skipped.
    expect(await screen.findByRole('heading', { name: 'Invite your team' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Skip' }))
    expect(await screen.findByRole('heading', { name: 'Connect an agent' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Skip' }))

    expect(await screen.findByRole('heading', { name: 'You are all set' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /Go to the calendar/ }))
    await waitFor(() => expect(api.find('POST', '/v1/session/onboarded')).toHaveLength(1))
    expect(api.find('POST', '/v1/session/onboarded')[0].body).toEqual({ name: 'Ana' })
    await waitFor(() => expect(nav.replace).toHaveBeenCalledWith('/schedule', undefined))
  })

  it('an invited member gets the short version: their name and their role', async () => {
    const api = mockApi(routes)
    const user = userEvent.setup()

    render({
      account_owner: false,
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

  it('comes back to the channel step after an OAuth platform', async () => {
    mockApi(routes)
    nav.set('/welcome', 'step=channel&error=access%20denied')
    render({})
    expect(await screen.findByRole('heading', { name: 'Connect your first channel' })).toBeInTheDocument()
    expect(screen.getByRole('alert')).toHaveTextContent('access denied')
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
