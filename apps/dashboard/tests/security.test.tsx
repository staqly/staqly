import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import ErrorPage from '@/app/error'
import { SignIn } from '@/components/auth/sign-in'
import { ToggleChip } from '@/components/common/toggle-chip'
import { Inbox, isShortcutExempt } from '@/components/inbox/inbox'
import { ApiKeysManager } from '@/components/integrations/api-keys'
import { ItemPreview, ItemThumb } from '@/components/items/item-preview'
import { AppShell } from '@/components/shell/app-shell'
import { qk } from '@/lib/queries'
import { rememberApiKey, rememberedApiKey } from '@/lib/session-key'

import { APPROVE, REJECT, collection, item } from './fixtures'
import { nav } from './navigation'
import { PLANS, usage } from './plans-fixtures'
import { findActiveRow, getActiveRow, mockApi, renderWithProviders } from './utils'

const SECRET = { id: 'key_1', name: 'Default key', key: 'stk_secret' }

async function signIn(next: string) {
  nav.set('/login', new URLSearchParams({ next }).toString())
  const user = userEvent.setup()
  const r = renderWithProviders(<SignIn />)

  await user.type(screen.getByLabelText('API key*'), 'stk_admin')
  await user.click(screen.getByRole('button', { name: /sign in/i }))
  await waitFor(() => expect(nav.replace).toHaveBeenCalled())

  return r
}

describe('API key sign-in redirect (open redirect)', () => {
  const routes = {
    'GET /v1/setup': { initialized: true, mode: 'cloud', setup_token_required: true },
    'POST /v1/sessions/api-key': { status: 201, body: { csrf_token: 'c' } },
  }

  it.each(['/\\evil.example', '/\t/evil.example', '//evil.example', 'https://evil.example', '/login'])(
    'falls back to /inbox for next=%j',
    async (next) => {
      mockApi(routes)
      await signIn(next)
      expect(nav.replace).toHaveBeenCalledWith('/inbox', undefined)
    },
  )

  it('keeps a same-origin next with query and hash', async () => {
    mockApi(routes)
    await signIn('/items/itm_1?tab=events#v2')
    expect(nav.replace).toHaveBeenCalledWith('/items/itm_1?tab=events#v2', undefined)
  })

  it('sends the key once when Enter is pressed again while it is checked', async () => {
    const api = mockApi(routes)
    const mocked = globalThis.fetch
    const { promise: held, resolve: release } = Promise.withResolvers<void>()

    // The sign-in request stays open until released, so the form is still checking on the second Enter.
    vi.stubGlobal('fetch', async (input: RequestInfo | URL, init?: RequestInit) => {
      const res = mocked(input, init)

      if (String(input).endsWith('/v1/sessions/api-key')) {
        await held
      }

      return res
    })
    nav.set('/login')
    const user = userEvent.setup()

    renderWithProviders(<SignIn />)
    await user.type(screen.getByLabelText('API key*'), 'stk_admin{Enter}')
    await waitFor(() => expect(screen.getByRole('button', { name: /checking/i })).toBeDisabled())
    await user.keyboard('{Enter}')
    release()
    await waitFor(() => expect(nav.replace).toHaveBeenCalled())
    expect(api.find('POST', '/v1/sessions/api-key')).toHaveLength(1)
  })

  it('forgets the remembered API key and every cached query of the previous session', async () => {
    mockApi(routes)
    rememberApiKey(SECRET)
    nav.set('/login')
    const user = userEvent.setup()
    const { client } = renderWithProviders(<SignIn />)

    client.setQueryData(qk.apiKeys, { data: [{ id: 'key_old' }] })
    await user.type(screen.getByLabelText('API key*'), 'stk_admin')
    await user.click(screen.getByRole('button', { name: /sign in/i }))
    await waitFor(() => expect(nav.replace).toHaveBeenCalledWith('/inbox', undefined))
    expect(rememberedApiKey()).toBeNull()
    expect(client.getQueryData(qk.apiKeys)).toBeUndefined()
  })
})

const apiKey = (id: string, name: string) => ({
  id,
  name,
  prefix: `stk_${id}`,
  scopes: ['items:read'],
  created_at: '2026-01-01T00:00:00Z',
  last_used_at: null,
  expires_at: null,
  revoked_at: null,
})

describe('remembered API key lifecycle', () => {
  beforeEach(() => rememberApiKey(null))

  it('is cleared on sign-out', async () => {
    mockApi({
      'GET /v1/collections': { data: [] },
      'GET /v1/workspace/usage': usage(PLANS[0]),
      'DELETE /v1/session': { status: 204 },
    })
    rememberApiKey(SECRET)
    nav.set('/inbox')
    const user = userEvent.setup()

    renderWithProviders(
      <AppShell>
        <div>page</div>
      </AppShell>,
      { mode: 'cloud' },
    )
    await user.click((await screen.findAllByTestId('workspace-menu-trigger'))[0])
    const menu = await screen.findByRole('menu')

    await user.click(within(menu).getByRole('menuitem', { name: /sign in to another workspace/i }))
    await waitFor(() => expect(nav.replace).toHaveBeenCalledWith('/login', undefined))
    expect(rememberedApiKey()).toBeNull()
  })

  it('is cleared when that key is revoked, but not when another one is', async () => {
    const api = mockApi({
      'GET /v1/api-keys': {
        data: [apiKey('key_1', 'Default key'), apiKey('key_2', 'Other key')],
        available_scopes: [],
      },
      'DELETE /v1/api-keys/key_1': { status: 204 },
      'DELETE /v1/api-keys/key_2': { status: 204 },
    })

    rememberApiKey(SECRET)
    nav.set('/integrations/api-keys')
    const user = userEvent.setup()

    renderWithProviders(<ApiKeysManager />, { mode: 'cloud' })

    async function revoke(name: string) {
      await user.click(await screen.findByRole('button', { name: `Options for ${name}` }))
      await user.click(await screen.findByRole('menuitem', { name: /revoke/i }))
      const dialog = await screen.findByRole('alertdialog')

      await user.click(within(dialog).getByRole('button', { name: /revoke/i }))
    }

    await revoke('Other key')
    await waitFor(() => expect(api.find('DELETE', '/v1/api-keys/key_2')).toHaveLength(1))
    expect(rememberedApiKey()).toEqual(SECRET)

    await revoke('Default key')
    await waitFor(() => expect(api.find('DELETE', '/v1/api-keys/key_1')).toHaveLength(1))
    await waitFor(() => expect(rememberedApiKey()).toBeNull())
  })
})

const swatch = () => screen.getByRole('button').querySelector('span')!

describe('untrusted URLs and colors', () => {
  it('renders a javascript: link item as text, never as a link', () => {
    renderWithProviders(
      <ItemPreview
        item={item({ type: 'link', media: null, title: 'Click me', url: 'javascript:alert(document.cookie)' })}
      />,
    )
    const card = screen.getByTestId('preview-link')

    expect(within(card).queryAllByRole('link')).toHaveLength(0)
    expect(within(card).getByText('javascript:alert(document.cookie)')).toBeInTheDocument()
  })

  it('does not link link-items whose content is a data: URL', () => {
    renderWithProviders(
      <ItemPreview item={item({ type: 'link', media: null, url: '', content: 'data:text/html,<script>1</script>' })} />,
    )
    expect(within(screen.getByTestId('preview-link')).queryAllByRole('link')).toHaveLength(0)
  })

  it('does not link a non-http file URL', () => {
    renderWithProviders(<ItemPreview item={item({ type: 'file', media: null, url: 'javascript:alert(1)' })} />)
    expect(within(screen.getByTestId('preview-file')).queryAllByRole('link')).toHaveLength(0)
  })

  it('never uses non-http external media as a source', () => {
    renderWithProviders(<ItemPreview item={item({ media: null, url: 'javascript:alert(1)' })} />)
    expect(screen.queryByTestId('preview-image')).not.toBeInTheDocument()
    expect(screen.getByText('No image attached')).toBeInTheDocument()
  })

  it('external videos wait for a click and never autoplay', async () => {
    const user = userEvent.setup()

    renderWithProviders(
      <ItemPreview item={item({ type: 'video', media: null, url: 'https://cdn.example.com/v.mp4' })} />,
    )
    const box = screen.getByTestId('preview-video')

    expect(box.querySelector('video')).toBeNull()
    expect(within(box).getByText('cdn.example.com')).toBeInTheDocument()
    await user.click(within(box).getByRole('button', { name: /load video/i }))
    // Still nothing plays until the play tile is clicked.
    expect(box.querySelector('video')).toBeNull()
    await user.click(within(box).getByRole('button', { name: /^play/i }))
    const video = (await screen.findByRole('dialog')).querySelector('video')!

    expect(video).toHaveAttribute('src', 'https://cdn.example.com/v.mp4')
  })

  it('list thumbnails never load external media', () => {
    const { container } = renderWithProviders(
      <ItemThumb item={item({ media: null, url: 'https://cdn.example.com/a.png' })} />,
    )

    expect(container.querySelector('img')).toBeNull()
  })

  it('only puts valid collection colors into style', () => {
    const { rerender } = render(
      <ToggleChip pressed={false} color="red; background-image: url(https://evil.example/x)" onClick={() => {}}>
        Bad
      </ToggleChip>,
    )

    expect(swatch().getAttribute('style') ?? '').not.toContain('evil')
    rerender(
      <ToggleChip pressed={false} color="#22c55e" onClick={() => {}}>
        Good
      </ToggleChip>,
    )
    expect(swatch().style.background).toBe('rgb(34, 197, 94)')
  })
})

const INBOX_ITEMS = [item({ id: 'itm_1', title: 'Avatar one' }), item({ id: 'itm_2', title: 'Avatar two', version: 3 })]

function setupInbox() {
  return mockApi({
    'GET /v1/items': { data: INBOX_ITEMS },
    'GET /v1/collections': { data: [collection({ status_counts: { pending: 2 }, item_count: 2 })] },
    'GET /v1/items/*': (c: { path: string }) =>
      c.path.endsWith('/actions') ? { data: [APPROVE, REJECT] } : INBOX_ITEMS.find((i) => c.path.endsWith(i.id)),
    'GET /v1/events': { data: [] },
    'POST /v1/items/itm_1/actions/approve': item({ id: 'itm_1', status: 'approved', version: 2 }),
  })
}

async function inboxReady() {
  // The active row (the first one to start with) carries Reject and Approve.
  const preview = await findActiveRow()

  await within(preview).findByRole('button', { name: /^approve/i })

  return preview
}

describe('inbox keyboard guards', () => {
  beforeEach(() => nav.set('/inbox'))

  it('classifies buttons, links, role=button, fields and contenteditable as exempt', () => {
    document.body.innerHTML = `
      <button id="b"><span id="inner">x</span></button><a id="a" href="/x">x</a><div id="rb" role="button">x</div>
      <input id="i" /><textarea id="t"></textarea><select id="s"></select>
      <div id="ce" contenteditable="true"><p id="cep">x</p></div><div id="plain">x</div><div role="option" id="opt">x</div>`

    for (const id of ['b', 'inner', 'a', 'rb', 'i', 't', 's', 'ce', 'cep']) {
      expect({ id, exempt: isShortcutExempt(document.getElementById(id)) }).toEqual({ id, exempt: true })
    }

    expect(isShortcutExempt(document.getElementById('plain'))).toBe(false)
    expect(isShortcutExempt(document.getElementById('opt'))).toBe(false)
    expect(isShortcutExempt(null)).toBe(false)
  })

  it('ignores shortcuts typed while a button has focus', async () => {
    const api = setupInbox()

    renderWithProviders(<Inbox />, { mode: 'local' })
    const preview = await inboxReady()
    const approveButton = within(preview).getByRole('button', { name: /^approve/i })

    fireEvent.keyDown(approveButton, { key: 'A', shiftKey: true })
    fireEvent.keyDown(approveButton, { key: 'j' })
    await new Promise((r) => setTimeout(r, 50))
    expect(api.find('POST', '/v1/items/itm_1/actions/approve')).toHaveLength(0)
    expect(getActiveRow(/Avatar one/i)).toBeInTheDocument()
  })

  it('keeps shortcuts on a focused step tab, except Enter and Space', () => {
    document.body.innerHTML = `<div role="tablist"><button id="tab" role="tab">Approved</button></div>`
    const tab = document.getElementById('tab')

    expect(isShortcutExempt(tab, 'j')).toBe(false)
    expect(isShortcutExempt(tab, 'A')).toBe(false)
    expect(isShortcutExempt(tab, 'Enter')).toBe(true)
    expect(isShortcutExempt(tab, ' ')).toBe(true)
    expect(isShortcutExempt(tab)).toBe(true)
  })

  it('ignores auto-repeated decision keys', async () => {
    const api = setupInbox()

    renderWithProviders(<Inbox />, { mode: 'local' })
    await inboxReady()
    fireEvent.keyDown(window, { key: 'A', shiftKey: true, repeat: true })
    // Without Shift a key never decides anything.
    fireEvent.keyDown(window, { key: 'a' })
    await new Promise((r) => setTimeout(r, 50))
    expect(api.find('POST', '/v1/items/itm_1/actions/approve')).toHaveLength(0)
    fireEvent.keyDown(window, { key: 'A', shiftKey: true })
    await waitFor(() => expect(api.find('POST', '/v1/items/itm_1/actions/approve')).toHaveLength(1))
  })
})

describe('error boundary', () => {
  it('renders a branded screen whose Try again retries', async () => {
    const retry = vi.fn<() => void>()
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {})
    const user = userEvent.setup()

    render(<ErrorPage error={Object.assign(new Error('boom'), { digest: 'dg_123' })} retry={retry} />)
    expect(screen.getByRole('alert')).toHaveTextContent(/something went wrong/i)
    expect(screen.getByText(/dg_123/)).toBeInTheDocument()
    expect(screen.queryByText('boom')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /try again/i }))
    expect(retry).toHaveBeenCalledOnce()
    expect(screen.getByRole('link', { name: /back to inbox/i })).toHaveAttribute('href', '/inbox')
    spy.mockRestore()
  })
})
