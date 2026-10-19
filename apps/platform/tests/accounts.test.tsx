import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { SignIn } from '@/components/auth/sign-in'
import { MembersPage } from '@/components/settings/members-page'
import { AppShell } from '@/components/shell/app-shell'
import { qk } from '@/lib/queries'

import { nav } from './navigation'
import { PLANS, UNLOCKED, usage } from './plans-fixtures'
import { makeClient, mockApi, renderWithProviders, sessionFixture } from './utils'

const ANA = { id: 'usr_1', name: 'Ana Owner', email: 'ana@example.com', avatar_url: '' }

function accountSession(role: string, accountOwner: boolean) {
  return {
    ...sessionFixture('cloud'),
    user: ANA,
    role,
    account_owner: accountOwner,
    accounts_enabled: true,
    workspaces: [
      { id: 'ws_1', name: 'Test WS', role, current: true },
      { id: 'ws_2', name: 'Clients', role: 'editor', current: false },
    ],
  }
}

function shell(session: unknown, plan = PLANS[0]) {
  const client = makeClient()

  client.setQueryData(qk.session, session)

  return { client, routes: { 'GET /v1/collections': { data: [] }, 'GET /v1/workspace/usage': usage(plan, {}) } }
}

describe('sign-in with Google', () => {
  it('offers only Continue with Google and explains an error in plain words', async () => {
    mockApi({
      'GET /v1/setup': {
        initialized: true,
        mode: 'cloud',
        signup_open: false,
        setup_token_required: true,
        google_sign_in: true,
      },
    })
    nav.set('/login', 'error=signup_closed&next=%2Fposts')
    renderWithProviders(<SignIn />)

    const button = await screen.findByTestId('google-sign-in')

    expect(button).toHaveAttribute('href', '/api/v1/auth/google/start?next=%2Fposts')
    expect(screen.getByRole('alert')).toHaveTextContent(/accepts new people by invitation/i)
    expect(screen.queryByLabelText(/api key/i)).not.toBeInTheDocument()

    await userEvent.setup().type(screen.getByLabelText(/setup token/i), 'tok')
    expect(button).toHaveAttribute('href', '/api/v1/auth/google/start?next=%2Fposts&setup_token=tok')
  })
})

describe('the signed-in person in the sidebar', () => {
  it('cloud: Google name and address, Sign out, and the workspace switcher', async () => {
    const { client, routes } = shell(accountSession('owner', true))
    const api = mockApi({ ...routes, 'DELETE /v1/session': { status: 204 } })
    const user = userEvent.setup()

    nav.set('/schedule')
    renderWithProviders(
      <AppShell>
        <div>page</div>
      </AppShell>,
      { client },
    )

    expect((await screen.findAllByTestId('user-name'))[0]).toHaveTextContent('Ana Owner')
    expect(screen.getAllByText('ana@example.com').length).toBeGreaterThan(0)

    await user.click(screen.getAllByTestId('workspace-menu-trigger')[0])
    let menu = await screen.findByRole('menu')

    expect(
      within(menu)
        .getAllByTestId('workspace-switch-item')
        .map((i) => i.textContent),
    ).toEqual(['Test WS', 'Clients'])
    expect(within(menu).getByRole('menuitem', { name: /new workspace/i })).toBeInTheDocument()
    expect(within(menu).queryByRole('menuitem', { name: /connect a device/i })).not.toBeInTheDocument()
    await user.keyboard('{Escape}')

    await user.click(screen.getAllByTestId('user-menu-trigger')[0])
    menu = await screen.findByRole('menu')
    await user.click(within(menu).getByRole('menuitem', { name: /sign out/i }))
    await waitFor(() => expect(api.find('DELETE', '/v1/session')).toHaveLength(1))
  })

  it('local: the computer user, no sign out', async () => {
    const { client, routes } = shell(
      { ...sessionFixture('local'), user: { id: '', name: 'piotr', email: '', avatar_url: '' } },
      UNLOCKED,
    )

    mockApi(routes)
    nav.set('/schedule')
    const user = userEvent.setup()

    renderWithProviders(
      <AppShell>
        <div>page</div>
      </AppShell>,
      { client },
    )

    expect((await screen.findAllByTestId('user-name'))[0]).toHaveTextContent('piotr')
    expect(screen.getAllByText('This computer').length).toBeGreaterThan(0)
    await user.click(screen.getAllByTestId('user-menu-trigger')[0])
    expect(
      within(await screen.findByRole('menu')).queryByRole('menuitem', { name: /sign out/i }),
    ).not.toBeInTheDocument()
  })
})

describe('members', () => {
  it('an admin changes roles up to admin and never touches the account owner', async () => {
    const client = makeClient()

    client.setQueryData(qk.session, accountSession('admin', false))
    const api = mockApi({
      'GET /v1/members': {
        data: [
          {
            user_id: 'usr_1',
            name: 'Ana Owner',
            email: 'ana@example.com',
            avatar_url: '',
            role: 'owner',
            account_owner: true,
            you: false,
            created_at: '2026-10-19T00:00:00Z',
          },
          {
            user_id: 'usr_2',
            name: 'Cleo',
            email: 'cleo@example.com',
            avatar_url: '',
            role: 'editor',
            account_owner: false,
            you: false,
            created_at: '2026-10-19T00:00:00Z',
          },
        ],
      },
      'GET /v1/invitations': { data: [] },
      'PATCH /v1/members/usr_2': {
        user_id: 'usr_2',
        name: 'Cleo',
        email: 'cleo@example.com',
        avatar_url: '',
        role: 'admin',
        account_owner: false,
        you: false,
        created_at: '2026-10-19T00:00:00Z',
      },
    })

    nav.set('/settings/members')
    const user = userEvent.setup()

    renderWithProviders(<MembersPage />, { client })

    const select = await screen.findByLabelText('Role of Cleo')

    expect(within(select).queryByRole('option', { name: 'Owner' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Role of Ana Owner')).not.toBeInTheDocument()
    await user.selectOptions(select, 'admin')
    await waitFor(() => expect(api.find('PATCH', '/v1/members/usr_2')).toHaveLength(1))
  })

  it('per user pricing: the seats billed and what one more person costs while the workspace pays', async () => {
    const client = makeClient()

    client.setQueryData(qk.session, accountSession('owner', true))
    mockApi({
      'GET /v1/members': {
        data: [
          {
            user_id: 'usr_1',
            name: 'Ana Owner',
            email: 'ana@example.com',
            avatar_url: '',
            role: 'owner',
            account_owner: true,
            you: true,
            created_at: '2026-10-19T00:00:00Z',
          },
        ],
      },
      'GET /v1/invitations': { data: [] },
      'GET /v1/billing': {
        enabled: true,
        plan: 'plus',
        subscription_status: 'active',
        plan_expires_at: null,
        suspended: false,
        has_customer: true,
        seats: 2,
      },
      'GET /v1/billing/details': {
        subscription: { status: 'active', amount: 2900, currency: 'usd', interval: 'month', quantity: 2 },
        payment_methods: [],
        invoices: [],
        has_more_invoices: false,
      },
      'GET /v1/workspace/usage': usage(PLANS.find((p) => p.id === 'plus') ?? PLANS[0], {}),
    })

    nav.set('/settings/members')
    const user = userEvent.setup()

    renderWithProviders(<MembersPage />, { client })
    expect(await screen.findByText(/2 seats billed, one per member/)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /invite people/i }))
    expect(await screen.findByTestId('invite-seat-price')).toHaveTextContent(
      'Each person you add is billed $29/month on Plus (prorated).',
    )
  })
})
