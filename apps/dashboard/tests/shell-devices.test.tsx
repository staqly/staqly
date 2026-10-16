import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it } from 'vitest'

import { ConnectDevice } from '@/components/devices/connect-device'
import { ConnectDeviceDialog, QR_WARNING } from '@/components/devices/connect-device-dialog'
import { SettingsPage } from '@/components/settings/settings-page'
import { AppShell } from '@/components/shell/app-shell'
import { AuthGate } from '@/components/shell/auth-gate'
import { ApiError, getCsrfToken } from '@/lib/api'
import { qk } from '@/lib/queries'

import { nav } from './navigation'
import { PLANS, UNLOCKED, usage } from './plans-fixtures'
import { makeClient, mockApi, renderWithProviders } from './utils'

const LINK = {
  url: 'http://192.168.1.20:8080/connect#tok_abc',
  expires_at: new Date(Date.now() + 5 * 60_000).toISOString(),
  reachable: true,
}

function shellRoutes(plan = PLANS[0]) {
  return {
    'GET /v1/collections': { data: [] },
    'GET /v1/workspace/usage': usage(plan, { decisions_per_month: 12 }),
    'DELETE /v1/session': { status: 204 },
    'POST /v1/device-links': { status: 201, body: LINK },
  }
}

async function openWorkspaceMenu(user: ReturnType<typeof userEvent.setup>) {
  const triggers = await screen.findAllByTestId('workspace-menu-trigger')

  await user.click(triggers[0])

  return screen.findByRole('menu')
}

describe('workspace menu', () => {
  it('cloud: shows workspace name, plan badge and the multi-workspace actions', async () => {
    const api = mockApi(shellRoutes())

    nav.set('/inbox')
    const user = userEvent.setup()

    renderWithProviders(
      <AppShell>
        <div>page</div>
      </AppShell>,
      { mode: 'cloud' },
    )
    expect(screen.getByTestId('workspace-name')).toHaveTextContent('Test WS')
    const trigger = screen.getAllByTestId('workspace-menu-trigger')[0]

    expect(within(trigger).getByTestId('plan-badge')).toHaveTextContent('Free')
    // Sidebar footer links to Plan & usage with this month's item count.
    expect(await screen.findByText(/12 \/ 50 approvals this month/)).toBeInTheDocument()
    expect(screen.getByTestId('sidebar-plan-link')).toHaveAttribute('href', '/settings/plan')

    let menu = await openWorkspaceMenu(user)

    await user.click(within(menu).getByRole('menuitem', { name: /create another workspace/i }))
    expect(nav.push).toHaveBeenCalledWith('/setup')

    menu = await openWorkspaceMenu(user)
    await user.click(within(menu).getByRole('menuitem', { name: /sign in to another workspace/i }))
    await waitFor(() => expect(nav.replace).toHaveBeenCalledWith('/login', undefined))
    expect(api.find('DELETE', '/v1/session')).toHaveLength(1)
  })

  it('local: no workspace switching, but Plan & usage and Connect a device', async () => {
    mockApi(shellRoutes(UNLOCKED))
    nav.set('/inbox')
    const user = userEvent.setup()

    renderWithProviders(
      <AppShell>
        <div>page</div>
      </AppShell>,
      { mode: 'local' },
    )
    const menu = await openWorkspaceMenu(user)

    expect(within(menu).queryByText(/create another workspace/i)).not.toBeInTheDocument()
    expect(within(menu).queryByText(/sign in to another workspace/i)).not.toBeInTheDocument()
    expect(within(menu).queryByText(/log out/i)).not.toBeInTheDocument()
    expect(within(menu).getByRole('menuitem', { name: /plan & usage/i })).toBeInTheDocument()
    await user.click(within(menu).getByRole('menuitem', { name: /connect a device/i }))
    expect(await screen.findByRole('dialog', { name: /connect a device/i })).toBeInTheDocument()
    expect(await screen.findByTestId('device-url')).toHaveTextContent(LINK.url)
  })
})

describe('connect a device dialog', () => {
  it('renders the QR, the link with a copy button, a countdown and the warning', async () => {
    const api = mockApi({ 'POST /v1/device-links': { status: 201, body: LINK } })
    const user = userEvent.setup()

    renderWithProviders(<ConnectDeviceDialog open onOpenChange={() => {}} />, { mode: 'local' })
    expect(await screen.findByTitle('Device link QR code')).toBeInTheDocument()
    expect(screen.getByTestId('device-url')).toHaveTextContent(LINK.url)
    expect(screen.getByRole('button', { name: /copy device link/i })).toBeInTheDocument()
    expect(screen.getByTestId('device-countdown').textContent).toMatch(/^[45]:\d\d$/)
    expect(screen.getByText(QR_WARNING)).toBeInTheDocument()
    expect(screen.queryByTestId('unreachable-hint')).not.toBeInTheDocument()
    expect(api.find('POST', '/v1/device-links')).toHaveLength(1)

    await user.click(screen.getByRole('button', { name: /generate new code/i }))
    await waitFor(() => expect(api.find('POST', '/v1/device-links')).toHaveLength(2))
  })

  it('shows the server hint when the link is not reachable from other devices', async () => {
    mockApi({
      'POST /v1/device-links': {
        status: 201,
        body: {
          ...LINK,
          url: 'http://localhost:8080/connect#tok_abc',
          reachable: false,
          hint: "Phones can't reach this address yet: add PUBLIC_DASHBOARD_URL=http://<this computer's LAN IP>:8080 to .env and run `docker compose up -d`.",
        },
      },
    })
    renderWithProviders(<ConnectDeviceDialog open onOpenChange={() => {}} />, { mode: 'local' })
    const hint = await screen.findByTestId('unreachable-hint')

    expect(hint).toHaveTextContent(/can't reach this computer/i)
    expect(hint).toHaveTextContent(
      "Phones can't reach this address yet: add PUBLIC_DASHBOARD_URL=http://<this computer's LAN IP>:8080 to .env and run docker compose up -d.",
    )
    expect(within(hint).getByText('docker compose up -d').tagName).toBe('CODE')
    expect(screen.getByText(QR_WARNING)).toBeInTheDocument()
  })
})

describe('/connect page', () => {
  afterEach(() => {
    window.history.replaceState(null, '', '/')
  })

  it('redeems the token from the URL fragment, removes it and opens the inbox', async () => {
    window.history.replaceState(null, '', '/connect#tok_abc')
    const api = mockApi({ 'POST /v1/device-links/redeem': { status: 201, body: { csrf_token: 'csrf-phone' } } })

    renderWithProviders(<ConnectDevice />)
    await waitFor(() => expect(nav.replace).toHaveBeenCalledWith('/inbox', undefined))
    expect(api.find('POST', '/v1/device-links/redeem')[0].body).toEqual({ token: 'tok_abc' })
    // A browser that is already signed in still passes the CSRF check with its old session.
    expect(api.find('POST', '/v1/device-links/redeem')[0].headers['X-CSRF-Token']).toBe('csrf-test')
    expect(window.location.hash).toBe('')
    expect(getCsrfToken()).toBe('csrf-phone')
  })

  it('explains an expired link', async () => {
    window.history.replaceState(null, '', '/connect#tok_old')
    mockApi({
      'POST /v1/device-links/redeem': {
        status: 410,
        body: { error: { code: 'link_expired', message: 'link expired' } },
      },
    })
    renderWithProviders(<ConnectDevice />)
    expect(await screen.findByRole('heading', { name: /this link expired/i })).toBeInTheDocument()
    expect(screen.getByText(/generate a new qr on your computer/i)).toBeInTheDocument()
    expect(nav.replace).not.toHaveBeenCalled()
  })

  it('explains a link that was already used or is invalid', async () => {
    window.history.replaceState(null, '', '/connect#tok_used')
    mockApi({
      'POST /v1/device-links/redeem': { status: 410, body: { error: { code: 'link_used', message: 'used' } } },
    })
    renderWithProviders(<ConnectDevice />)
    expect(await screen.findByRole('heading', { name: /already used/i })).toBeInTheDocument()
  })
})

describe('devices in local mode', () => {
  it('lists sessions with friendly device names and can revoke another device', async () => {
    const api = mockApi({
      'GET /v1/sessions': {
        data: [
          {
            id: 's_phone',
            user_agent:
              'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1',
            created_at: '2026-10-01T00:00:00Z',
            last_seen_at: '2026-10-01T00:00:00Z',
            expires_at: '2026-11-01T00:00:00Z',
            current: false,
          },
          {
            id: 's_me',
            user_agent: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0) AppleWebKit/537.36 Chrome/140 Safari/537.36',
            created_at: '2026-10-01T00:00:00Z',
            last_seen_at: '2026-10-01T00:00:00Z',
            expires_at: '2026-11-01T00:00:00Z',
            current: true,
          },
        ],
      },
      'DELETE /v1/sessions/s_phone': { status: 204 },
    })
    const user = userEvent.setup()

    renderWithProviders(<SettingsPage />, { mode: 'local' })
    const devices = screen.getByRole('region', { name: 'Devices' })

    expect(await within(devices).findByText('Safari on iPhone')).toBeInTheDocument()
    expect(within(devices).getByText('Chrome on macOS')).toBeInTheDocument()
    expect(within(devices).getByText('This device')).toBeInTheDocument()
    expect(within(devices).getByRole('button', { name: /connect a device/i })).toBeInTheDocument()
    // Only other devices can be revoked.
    expect(within(devices).getAllByRole('button', { name: /^revoke/i })).toHaveLength(1)
    await user.click(within(devices).getByRole('button', { name: /revoke safari on iphone/i }))
    await user.click(await screen.findByRole('button', { name: /revoke access/i }))
    await waitFor(() => expect(api.find('DELETE', '/v1/sessions/s_phone')).toHaveLength(1))
  })
})

describe('auth gate', () => {
  it('shows the connect instructions for a device that is not connected (403 not_local)', async () => {
    mockApi({
      'GET /v1/setup': { initialized: true, mode: 'local', signup_open: false, setup_token_required: false },
      'GET /v1/session': { status: 403, body: { error: { code: 'not_local', message: 'connect this device first' } } },
    })
    const client = makeClient()

    renderWithProviders(
      <AuthGate>
        <div>dashboard</div>
      </AuthGate>,
      { client },
    )
    expect(await screen.findByTestId('not-connected')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: /sign in this browser/i })).toBeInTheDocument()
    expect(screen.getAllByText('staqly serve connect-link').length).toBeGreaterThan(0)
    expect(screen.getAllByText('docker exec staqly server connect-link').length).toBeGreaterThan(0)
    expect(screen.queryByText('dashboard')).not.toBeInTheDocument()
    expect(client.getQueryState(qk.session)?.error).toBeInstanceOf(ApiError)
  })
})

describe('command palette', () => {
  it('loads on the first ⌘K, not with the shell', async () => {
    mockApi(shellRoutes())
    nav.set('/inbox')
    const user = userEvent.setup()

    renderWithProviders(
      <AppShell>
        <div>page</div>
      </AppShell>,
      { mode: 'local' },
    )
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(document.querySelector('[cmdk-root]')).toBeNull()
    await user.keyboard('{Control>}k{/Control}')
    expect(await screen.findByRole('dialog', { name: 'Command palette' })).toBeInTheDocument()
    expect(await screen.findByPlaceholderText('Type a command or search...')).toBeInTheDocument()
    await user.keyboard('{Control>}k{/Control}')
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Command palette' })).not.toBeInTheDocument())
  })
})
