import { onlineManager } from '@tanstack/react-query'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it } from 'vitest'

import { ConnectDevice } from '@/components/devices/connect-device'
import { ConnectDeviceDialog, QR_WARNING } from '@/components/devices/connect-device-dialog'
import { SettingsPage } from '@/components/settings/settings-page'
import { AppShell } from '@/components/shell/app-shell'
import { AuthGate } from '@/components/shell/auth-gate'
import { getCsrfToken } from '@/lib/api'
import { captureLinkToken } from '@/lib/local-token'

import { nav } from './navigation'
import { PLANS, UNLOCKED, usage } from './plans-fixtures'
import { mockApi, renderWithProviders, sessionFixture } from './utils'

const LINK = {
  url: 'http://192.168.1.20:8080/connect#tok_abc',
  expires_at: new Date(Date.now() + 5 * 60_000).toISOString(),
  reachable: true,
}

function shellRoutes(plan = PLANS[0]) {
  return {
    'GET /v1/collections': { data: [] },
    'GET /v1/workspace/usage': usage(plan, { posts_per_month: 12 }),
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
  it('cloud without a person: workspace name, plan badge, Plan & usage and only Log out', async () => {
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

    expect(within(trigger).getByTestId('plan-badge')).toHaveTextContent('Starter')
    // Plan & usage lives in the workspace menu, not in the sidebar footer.
    expect(screen.queryByTestId('sidebar-plan-link')).not.toBeInTheDocument()

    let menu = await openWorkspaceMenu(user)

    expect(within(menu).getByRole('menuitem', { name: /plan & usage/i })).toBeInTheDocument()
    await user.keyboard('{Escape}')
    menu = await openWorkspaceMenu(user)

    // A session without a person (an API key on a development server) can only log out: no API-key workspace
    // creation and no "sign in to another workspace" in the dashboard.
    expect(within(menu).queryByRole('menuitem', { name: /create another workspace/i })).not.toBeInTheDocument()
    expect(within(menu).queryByRole('menuitem', { name: /sign in to another workspace/i })).not.toBeInTheDocument()
    await user.click(within(menu).getByRole('menuitem', { name: /log out/i }))
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

  it('puts the typed network address in the code when the link points at localhost', async () => {
    localStorage.removeItem('staqly.lan-host')
    mockApi({
      'POST /v1/device-links': {
        status: 201,
        body: {
          ...LINK,
          url: 'http://localhost:8080/connect#tok_abc',
          reachable: false,
          hint: 'Open the dashboard at its address.',
        },
      },
    })
    const user = userEvent.setup()

    renderWithProviders(<ConnectDeviceDialog open onOpenChange={() => {}} />, { mode: 'local' })
    await user.type(await screen.findByTestId('lan-host'), '192.168.1.20')

    expect(screen.getByTestId('device-url')).toHaveTextContent('http://192.168.1.20:8080/connect#tok_abc')
    expect(screen.getByTestId('unreachable-hint')).toHaveTextContent(/same Wi-Fi/)
    expect(localStorage.getItem('staqly.lan-host')).toBe('192.168.1.20')
  })

  it('shows the server hint when the dashboard only listens on this computer', async () => {
    mockApi({
      'POST /v1/device-links': {
        status: 201,
        body: {
          ...LINK,
          url: 'http://localhost:8080/connect#tok_abc',
          reachable: false,
          hint: 'The dashboard only listens on this computer. Restart it with `staqly serve --expose` to open it from a phone.',
        },
      },
    })
    renderWithProviders(<ConnectDeviceDialog open onOpenChange={() => {}} />, { mode: 'local' })
    const hint = await screen.findByTestId('unreachable-hint')

    expect(hint).toHaveTextContent(/can't reach this computer/i)
    expect(within(hint).getByText('staqly serve --expose').tagName).toBe('CODE')
    expect(screen.queryByTestId('lan-host')).not.toBeInTheDocument()
    expect(screen.getByText(QR_WARNING)).toBeInTheDocument()
  })
})

describe('/connect page', () => {
  afterEach(() => {
    window.history.replaceState(null, '', '/')
  })

  it('redeems the token from the URL fragment, removes it and opens the calendar', async () => {
    window.history.replaceState(null, '', '/connect#tok_abc')
    const api = mockApi({ 'POST /v1/device-links/redeem': { status: 201, body: { csrf_token: 'csrf-phone' } } })

    renderWithProviders(<ConnectDevice />)
    await waitFor(() => expect(nav.replace).toHaveBeenCalledWith('/schedule', undefined))
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
  const setup = { initialized: true, mode: 'local', signup_open: false, setup_token_required: false }
  const TOKEN = 'stk_local_abcdefghijklmnopqrstuvwxyz234567abcdefghijklmnopqr'

  /** A local server that wants the token: 401 until the browser signs in with it. */
  function localServer() {
    let signedIn = false

    return mockApi({
      'GET /v1/setup': setup,
      'GET /v1/session': () =>
        signedIn
          ? { ...sessionFixture('local'), csrf_token: 'csrf-local' }
          : { status: 401, body: { error: { code: 'unauthorized', message: 'local mode needs the access token' } } },
      'POST /v1/sessions/api-key': (c: { body: unknown }) => {
        if ((c.body as { api_key?: string }).api_key !== TOKEN) {
          return { status: 401, body: { error: { code: 'unauthorized', message: 'not the access token' } } }
        }

        signedIn = true

        return { status: 201, body: { csrf_token: 'csrf-local' } }
      },
    })
  }

  it('signs a local browser in with the pasted access token', async () => {
    const api = localServer()
    const user = userEvent.setup()

    renderWithProviders(
      <AuthGate>
        <div>dashboard</div>
      </AuthGate>,
    )
    expect(await screen.findByTestId('local-sign-in')).toBeInTheDocument()
    expect(screen.getAllByText('staqly serve token').length).toBeGreaterThan(0)
    expect(screen.getAllByText('docker exec staqly server token').length).toBeGreaterThan(0)
    expect(screen.queryByText('dashboard')).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Sign in' }))
    expect(await screen.findByText('Paste the access token.')).toBeInTheDocument()
    await user.type(screen.getByLabelText(/access token/i), 'stk_local_wrong')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))
    expect(await screen.findByText(/not the access token of this computer/i)).toBeInTheDocument()

    await user.clear(screen.getByLabelText(/access token/i))
    await user.type(screen.getByLabelText(/access token/i), TOKEN)
    await user.click(screen.getByRole('button', { name: 'Sign in' }))
    expect(await screen.findByText('dashboard')).toBeInTheDocument()
    expect(api.find('POST', '/v1/sessions/api-key').at(-1)?.body).toEqual({ api_key: TOKEN })
    expect(getCsrfToken()).toBe('csrf-local')
  })

  it('signs in from the #token= link and takes the token out of the address bar', async () => {
    const api = localServer()

    window.history.replaceState(null, '', `/schedule?view=week#token=${TOKEN}`)
    expect(captureLinkToken()).toBe(TOKEN)
    expect(window.location.hash).toBe('')
    expect(window.location.pathname + window.location.search).toBe('/schedule?view=week')

    renderWithProviders(
      <AuthGate>
        <div>dashboard</div>
      </AuthGate>,
    )
    expect(await screen.findByText('dashboard')).toBeInTheDocument()
    expect(api.find('POST', '/v1/sessions/api-key')).toHaveLength(1)
    expect(api.find('POST', '/v1/sessions/api-key')[0]?.body).toEqual({ api_key: TOKEN })
  })

  it('a link with a wrong token shows the sign-in with a plain reason', async () => {
    localServer()
    window.history.replaceState(null, '', '/schedule#token=stk_local_nottherightone')
    captureLinkToken()

    renderWithProviders(
      <AuthGate>
        <div>dashboard</div>
      </AuthGate>,
    )
    expect(await screen.findByTestId('local-sign-in')).toBeInTheDocument()
    expect(screen.getByText(/not the access token of this computer/i)).toBeInTheDocument()
  })

  it('says the server is not answering when a proxy in front of it gives up', async () => {
    mockApi({ 'GET /v1/setup': setup, 'GET /v1/session': { status: 502, body: { error: { code: 'bad_gateway' } } } })

    renderWithProviders(
      <AuthGate>
        <div>dashboard</div>
      </AuthGate>,
    )
    expect(await screen.findByRole('heading', { name: "Can't reach staqly" })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Try again' })).toBeInTheDocument()
  })

  it('says the browser is offline, and opens once it is back online', async () => {
    mockApi({ 'GET /v1/setup': setup, 'GET /v1/session': { ...sessionFixture('local'), csrf_token: 'csrf-local' } })
    onlineManager.setOnline(false)

    try {
      renderWithProviders(
        <AuthGate>
          <div>dashboard</div>
        </AuthGate>,
      )
      expect(await screen.findByRole('heading', { name: "You're offline" })).toBeInTheDocument()
      onlineManager.setOnline(true)
      expect(await screen.findByText('dashboard')).toBeInTheDocument()
    } finally {
      onlineManager.setOnline(true)
    }
  })

  it('leaves connect-a-device links to their page', () => {
    window.history.replaceState(null, '', '/connect#tok_device')
    expect(captureLinkToken()).toBeNull()
    expect(window.location.hash).toBe('#tok_device')
    window.history.replaceState(null, '', '/schedule#token=tok_device')
    expect(captureLinkToken()).toBeNull()
    window.history.replaceState(null, '', '/')
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

  it('leaves ⌘K to a rich text editor (the post editor adds a link with it)', async () => {
    mockApi(shellRoutes())
    nav.set('/inbox')
    const user = userEvent.setup()

    renderWithProviders(
      <AppShell>
        <div role="textbox" aria-label="editor" contentEditable suppressContentEditableWarning tabIndex={0} />
      </AppShell>,
      { mode: 'local' },
    )
    const editor = screen.getByLabelText('editor')

    // jsdom has no isContentEditable: what browsers derive from the attribute.
    Object.defineProperty(editor, 'isContentEditable', { value: true })
    await user.click(editor)
    await user.keyboard('{Control>}k{/Control}')
    await new Promise((r) => setTimeout(r, 50))
    expect(screen.queryByRole('dialog', { name: 'Command palette' })).not.toBeInTheDocument()
  })
})
