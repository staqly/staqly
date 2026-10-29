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

function accountSession(role: string) {
  return {
    ...sessionFixture('cloud'),
    user: ANA,
    role,
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
    const { client, routes } = shell(accountSession('owner'))
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

    // The workspace menu is the workspaces; the workspace's pages are in the person's menu.
    expect(within(menu).queryByRole('menuitem', { name: /plan & usage|members|settings/i })).not.toBeInTheDocument()

    // A choice: the open workspace is the checked one.
    expect(
      within(menu)
        .getAllByRole('menuitemradio')
        .map((i) => i.textContent),
    ).toEqual(['TTest WS', 'CClients'])
    expect(within(menu).getByRole('menuitemradio', { name: 'Test WS' })).toHaveAttribute('aria-checked', 'true')
    expect(within(menu).getByRole('menuitemradio', { name: 'Clients' })).toHaveAttribute('aria-checked', 'false')
    expect(within(menu).queryByRole('menuitem', { name: /all \d+ workspaces/i })).not.toBeInTheDocument()
    expect(within(menu).getByRole('menuitem', { name: /new workspace/i })).toBeInTheDocument()
    expect(within(menu).queryByRole('menuitem', { name: /connect a device/i })).not.toBeInTheDocument()
    await user.keyboard('{Escape}')

    await user.click(screen.getAllByTestId('user-menu-trigger')[0])
    menu = await screen.findByRole('menu')
    expect(within(menu).getByTestId('user-role')).toHaveTextContent('Owner')
    // Members and the person's own account, then the workspace's settings; Plan & usage is in the workspace menu.
    const items = within(menu)
      .getAllByRole('menuitem')
      .map((i) => i.textContent?.trim())

    expect(items).toEqual(expect.arrayContaining(['Members', 'Account settings', 'Workspace settings', 'Sign out']))
    expect(items).not.toContain('Plan & usage')
    expect(within(menu).queryByRole('menuitem', { name: 'Billing' })).not.toBeInTheDocument()
    await user.click(within(menu).getByRole('menuitem', { name: /sign out/i }))
    await waitFor(() => expect(api.find('DELETE', '/v1/session')).toHaveLength(1))
  })

  it('cloud: many workspaces: the menu lists the open one and a few, the rest are found by search', async () => {
    const workspaces = Array.from({ length: 20 }, (_, i) => ({
      id: `ws_${i}`,
      name: `Client ${String(i).padStart(2, '0')}`,
      role: 'editor',
      current: i === 15,
    }))
    const { client, routes } = shell({ ...accountSession('owner'), workspaces })
    const api = mockApi({ ...routes, 'POST /v1/session/workspace': { status: 204 } })
    const user = userEvent.setup()

    nav.set('/schedule')
    renderWithProviders(
      <AppShell>
        <div>page</div>
      </AppShell>,
      { client },
    )

    await user.click((await screen.findAllByTestId('workspace-menu-trigger'))[0])
    const menu = await screen.findByRole('menu')
    const radios = within(menu).getAllByRole('menuitemradio')

    expect(radios).toHaveLength(5)
    expect(radios[0]).toHaveAccessibleName('Client 15')
    await user.click(within(menu).getByRole('menuitem', { name: 'All 20 workspaces...' }))
    const dialog = await screen.findByRole('dialog', { name: 'Switch workspace' })

    await user.type(within(dialog).getByRole('combobox'), 'client 07')
    expect(
      within(dialog)
        .getAllByRole('option')
        .map((o) => o.textContent),
    ).toEqual(['CClient 07'])
    await user.click(within(dialog).getByRole('option'))
    await waitFor(() =>
      expect(api.find('POST', '/v1/session/workspace').map((c) => c.body)).toEqual([{ workspace_id: 'ws_7' }]),
    )
  })

  it('local: the computer user, who signed in with the access token and can sign out', async () => {
    const { client, routes } = shell(
      { ...sessionFixture('local'), user: { id: '', name: 'jane', email: '', avatar_url: '' } },
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

    expect((await screen.findAllByTestId('user-name'))[0]).toHaveTextContent('jane')
    expect(screen.getAllByText('This computer').length).toBeGreaterThan(0)
    await user.click(screen.getAllByTestId('user-menu-trigger')[0])
    const menu = await screen.findByRole('menu')

    expect(within(menu).getByRole('menuitem', { name: /sign out/i })).toBeInTheDocument()
    // No welcome on a local install, so no way back into it either.
    expect(within(menu).queryByTestId('menu-getting-started')).not.toBeInTheDocument()
  })
})

const member = (id: string, name: string, role: string, you: boolean) => ({
  user_id: id,
  name,
  email: `${id}@example.com`,
  avatar_url: '',
  role,
  you,
  created_at: '2026-10-19T00:00:00Z',
})

describe('members', () => {
  it('an admin changes roles up to admin and never touches an owner', async () => {
    const client = makeClient()

    client.setQueryData(qk.session, accountSession('admin'))
    const api = mockApi({
      'GET /v1/members': {
        data: [
          {
            user_id: 'usr_1',
            name: 'Ana Owner',
            email: 'ana@example.com',
            avatar_url: '',
            role: 'owner',
            you: false,
            created_at: '2026-10-19T00:00:00Z',
          },
          {
            user_id: 'usr_2',
            name: 'Cleo',
            email: 'cleo@example.com',
            avatar_url: '',
            role: 'editor',
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

    client.setQueryData(qk.session, accountSession('owner'))
    mockApi({
      'GET /v1/members': {
        data: [
          {
            user_id: 'usr_1',
            name: 'Ana Owner',
            email: 'ana@example.com',
            avatar_url: '',
            role: 'owner',
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
  it('the only owner keeps the role and cannot leave: alone they delete the workspace, with others they hand it over first', async () => {
    const client = makeClient()

    client.setQueryData(qk.session, accountSession('owner'))
    mockApi({
      'GET /v1/members': { data: [member('usr_1', 'Ana Owner', 'owner', true)] },
      'GET /v1/invitations': { data: [] },
    })
    nav.set('/settings/members')

    const { unmount } = renderWithProviders(<MembersPage />, { client })

    expect(await screen.findByRole('link', { name: 'Delete workspace' })).toHaveAttribute('href', '/settings/data')
    expect(screen.queryByLabelText('Role of Ana Owner')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Leave' })).not.toBeInTheDocument()
    unmount()

    const other = makeClient()

    other.setQueryData(qk.session, accountSession('owner'))
    mockApi({
      'GET /v1/members': {
        data: [member('usr_1', 'Ana Owner', 'owner', true), member('usr_2', 'Cleo', 'editor', false)],
      },
      'GET /v1/invitations': { data: [] },
    })
    renderWithProviders(<MembersPage />, { client: other })
    expect(await screen.findByTestId('sole-owner-note')).toHaveTextContent('Make someone else an owner to leave')
    expect(screen.queryByLabelText('Role of Ana Owner')).not.toBeInTheDocument()
    expect(screen.getByLabelText('Role of Cleo')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Leave' })).not.toBeInTheDocument()
  })
})
