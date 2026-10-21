import { useQuery } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { AcceptInvitation } from '@/components/auth/accept-invitation'
import { SchedulesSection } from '@/components/collections/schedules-section'
import { Providers } from '@/components/providers'
import { QueueCard } from '@/components/settings/queue-card'
import { api, ApiError, READ_TIMEOUT_MS } from '@/lib/api'
import { useSession } from '@/lib/queries'

import { collection } from './fixtures'
import { nav } from './navigation'
import { mockApi, renderWithProviders, sessionFixture } from './utils'

const UNAUTHORIZED = { status: 401, body: { error: { code: 'unauthorized', message: 'Sign in first' } } }

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('reads that fail', () => {
  it('a read that hangs fails as unreachable after the time limit; writes have no limit', async () => {
    const signals: (AbortSignal | null | undefined)[] = []

    vi.stubGlobal(
      'fetch',
      vi.fn(
        (_url: string, init?: RequestInit) =>
          new Promise<Response>((_, reject) => {
            signals.push(init?.signal)
            init?.signal?.addEventListener('abort', () => reject(init.signal!.reason))
          }),
      ),
    )
    const timeout = vi.spyOn(AbortSignal, 'timeout').mockImplementation(() => {
      const c = new AbortController()

      queueMicrotask(() => c.abort(new DOMException('timed out', 'TimeoutError')))

      return c.signal
    })

    const err = await api.workspace().catch((e: unknown) => e)

    expect(timeout).toHaveBeenCalledWith(READ_TIMEOUT_MS)
    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).status).toBe(0)
    expect((err as ApiError).message).toMatch(/did not answer in time/)

    // A write carries only the caller's signal (none here): it is never cut off.
    void api.renameWorkspace('x').catch(() => undefined)
    await vi.waitFor(() => expect(signals).toHaveLength(2))
    expect(signals[1]).toBeUndefined()
    timeout.mockRestore()
  })

  it('a caller that cancels still gets an AbortError', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(
        (_url: string, init?: RequestInit) =>
          new Promise<Response>((_, reject) => {
            init?.signal?.addEventListener('abort', () => reject(init.signal!.reason))
          }),
      ),
    )
    const c = new AbortController()
    const pending = api.posts({}, c.signal).catch((e: unknown) => e)

    c.abort()
    const err = await pending

    // Not an ApiError: TanStack Query treats it as a cancel. (A DOMException, of jsdom's other realm here.)
    expect(err).not.toBeInstanceOf(ApiError)
    expect((err as DOMException).name).toBe('AbortError')
  })

  it('schedules that cannot load say so with Try again, not "No schedule yet"', async () => {
    mockApi({
      'GET /v1/schedules': { status: 500, body: { error: { code: 'internal', message: 'boom' } } },
      'GET /v1/channels': { data: [] },
    })

    renderWithProviders(<SchedulesSection collection={collection()} />, { mode: 'local' })

    expect(await screen.findByText('Could not load the schedules')).toBeInTheDocument()
    expect(screen.queryByText(/No schedule yet/)).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Try again/ })).toBeInTheDocument()
  })

  it('a queue that cannot load says so, and Try again loads it', async () => {
    let fail = true

    mockApi({
      'GET /v1/jobs/stats': { queued: 0, running: 0, failed: 0, done_24h: 0 },
      'GET /v1/jobs': () =>
        fail ? { status: 503, body: { error: { code: 'unavailable', message: 'down' } } } : { data: [] },
    })
    const user = userEvent.setup()

    renderWithProviders(<QueueCard />, { mode: 'local' })

    expect(await screen.findByText('Could not load the queue')).toBeInTheDocument()
    expect(screen.queryByText(/Nothing in the queue/)).not.toBeInTheDocument()
    fail = false
    await user.click(screen.getByRole('button', { name: /Try again/ }))
    expect(await screen.findByText(/Nothing in the queue/)).toBeInTheDocument()
  })
})

describe('a 401 in local mode', () => {
  it('asks the session again, so the access-token sign-in shows at once (no loop when the session itself fails)', async () => {
    let signedIn = true
    const mocked = mockApi({
      'GET /v1/setup': { initialized: true, mode: 'local', signup_open: false, setup_token_required: false },
      'GET /v1/session': () => (signedIn ? sessionFixture('local') : UNAUTHORIZED),
      'GET /v1/workspace': () => UNAUTHORIZED,
    })

    function Probe() {
      const session = useSession()
      const ws = useQuery({ queryKey: ['workspace'], queryFn: api.workspace, enabled: !!session.data && !signedIn })

      return <p>{session.error ? 'signed out' : ws.isError ? 'workspace failed' : 'ok'}</p>
    }

    const { rerender } = render(
      <Providers>
        <Probe />
      </Providers>,
    )

    expect(await screen.findByText('ok')).toBeInTheDocument()
    signedIn = false
    rerender(
      <Providers>
        <Probe key="again" />
      </Providers>,
    )

    expect(await screen.findByText('signed out')).toBeInTheDocument()
    // Once at start, once after the workspace's 401; the session's own 401 does not ask again.
    await new Promise((r) => setTimeout(r, 50))
    expect(mocked.find('GET', '/v1/session')).toHaveLength(2)
    expect(nav.replace).not.toHaveBeenCalled()
  })
})

describe('an invitation link while the server is down', () => {
  it('says the check failed (with Try again), not that the link is invalid', async () => {
    mockApi({
      'GET /v1/setup': { initialized: true, mode: 'cloud', signup_open: true, setup_token_required: false },
      'GET /v1/invitations/lookup': { status: 502, body: 'Bad gateway' },
    })

    nav.set('/invite', 'token=staqly-invite-abc')
    renderWithProviders(<AcceptInvitation />)

    expect(await screen.findByText('Could not check the invitation')).toBeInTheDocument()
    expect(screen.queryByText(/not valid/)).not.toBeInTheDocument()
  })

  it('a link the server rejects is still called invalid', async () => {
    mockApi({
      'GET /v1/setup': { initialized: true, mode: 'cloud', signup_open: true, setup_token_required: false },
      'GET /v1/invitations/lookup': { status: 404, body: { error: { code: 'not_found', message: 'Not found' } } },
    })

    nav.set('/invite', 'token=nope')
    renderWithProviders(<AcceptInvitation />)

    expect(await screen.findByText(/This invitation link is not valid/)).toBeInTheDocument()
  })
})

function SessionProbe() {
  useSession()

  return <p>probe</p>
}

describe('a 401 in the cloud', () => {
  it('sends to sign-in with the page and its query, so sign-in comes back to exactly that', async () => {
    mockApi({
      'GET /v1/setup': { initialized: true, mode: 'cloud', signup_open: true, setup_token_required: false },
      'GET /v1/session': UNAUTHORIZED,
    })
    nav.reset()
    nav.set('/integrations')
    window.history.replaceState(null, '', '/integrations?tab=codex')

    render(
      <Providers>
        <SessionProbe />
      </Providers>,
    )
    await vi.waitFor(() =>
      expect(nav.replace).toHaveBeenCalledWith('/login?next=%2Fintegrations%3Ftab%3Dcodex', undefined),
    )
    window.history.replaceState(null, '', '/')
  })
})
