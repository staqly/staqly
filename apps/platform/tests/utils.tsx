import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import type { ReactElement } from 'react'
import { vi } from 'vitest'

import { Toaster } from '@/components/ui/sonner'
import { TooltipProvider } from '@/components/ui/tooltip'
import { setCsrfToken } from '@/lib/api'
import { qk } from '@/lib/queries'

export interface Call {
  method: string
  path: string
  query: URLSearchParams
  body: unknown
  headers: Record<string, string>
}

type Reply = { status?: number; body?: unknown } | undefined
type Handler = (call: Call) => Reply | unknown

function filterPosts(out: unknown, query: URLSearchParams): unknown {
  const page = out as { data?: { status?: string }[] } | undefined

  if (!Array.isArray(page?.data)) {
    return out
  }

  const status = query.get('status')
  const hidePast = query.get('hide_past') === 'true'

  return {
    ...page,
    data: page.data.filter((p) => (status ? p.status === status : !(hidePast && p.status === 'published'))),
  }
}

/**
 * Minimal fetch mock: routes are "METHOD /v1/path" (exact) or functions.
 * Unmatched requests fail loudly with 501 so tests notice them.
 */
export function mockApi(routes: Record<string, Handler | unknown>) {
  const calls: Call[] = []
  const fetchMock = vi.fn<typeof fetch>(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(input), 'http://localhost')
    const method = (init?.method ?? 'GET').toUpperCase()
    const path = url.pathname.replace(/^\/api/, '')
    let body: unknown = undefined

    if (typeof init?.body === 'string') {
      try {
        body = JSON.parse(init.body)
      } catch {
        body = init.body
      }
    } else if (init?.body instanceof FormData) {
      body = init.body
    }

    const call: Call = {
      method,
      path,
      query: url.searchParams,
      body,
      headers: (init?.headers ?? {}) as Record<string, string>,
    }

    calls.push(call)
    const key = `${method} ${path}`
    let route = routes[key]

    if (route === undefined) {
      // Pattern routes: "GET /v1/items/*"
      for (const [k, v] of Object.entries(routes)) {
        if (k.endsWith('*') && key.startsWith(k.slice(0, -1))) {
          route = v
          break
        }
      }
    }

    if (route === undefined) {
      return new Response(JSON.stringify({ error: { code: 'not_mocked', message: `Unmocked ${key}` } }), {
        status: 501,
        headers: { 'Content-Type': 'application/json' },
      })
    }

    let out: unknown

    try {
      out = typeof route === 'function' ? (route as Handler)(call) : route
    } catch (e) {
      console.error('mock handler failed for', key, e)

      throw e
    }

    // A fixed list of posts answers like the server: status and hide_past filter it (the posts list asks for its
    // upcoming and its published posts separately).
    if (key === 'GET /v1/posts' && typeof route !== 'function') {
      out = filterPosts(out, call.query)
    }

    // A reply is { status: number, body }; anything else is a 200 JSON body.
    const isReply = !!out && typeof out === 'object' && typeof (out as { status?: unknown }).status === 'number'

    if (!isReply) {
      out = { status: 200, body: out }
    }

    const r = out as { status?: number; body?: unknown }
    const status = r.status ?? 200

    return new Response(status === 204 ? null : JSON.stringify(r.body ?? {}), {
      status,
      headers: { 'Content-Type': 'application/json' },
    })
  })

  vi.stubGlobal('fetch', fetchMock)

  return {
    calls,
    fetchMock,
    find: (method: string, path: string) => calls.filter((c) => c.method === method && c.path === path),
  }
}

export function makeClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, staleTime: Infinity, refetchOnWindowFocus: false },
      mutations: { retry: false },
    },
  })
}

export function renderWithProviders(ui: ReactElement, opts: { client?: QueryClient; mode?: 'local' | 'cloud' } = {}) {
  const client = opts.client ?? makeClient()

  setCsrfToken(opts.mode === 'local' ? '' : 'csrf-test')

  if (opts.mode) {
    client.setQueryData(qk.setup, {
      initialized: true,
      mode: opts.mode,
      signup_open: opts.mode === 'cloud',
      setup_token_required: opts.mode === 'cloud',
    })
    client.setQueryData(qk.session, sessionFixture(opts.mode))
  }

  const result = render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        {ui}
        <Toaster duration={Number.POSITIVE_INFINITY} />
      </TooltipProvider>
    </QueryClientProvider>,
  )

  return { ...result, client }
}

export function sessionFixture(mode: 'local' | 'cloud') {
  return {
    mode,
    csrf_token: mode === 'local' ? '' : 'csrf-test',
    workspace: {
      id: 'ws_1',
      name: 'Test WS',
      plan: mode === 'local' ? 'unlocked' : 'starter',
      recovery_rotated_at: '2026-01-01T00:00:00Z',
      created_at: '2026-01-01T00:00:00Z',
    },
    principal: {
      type: mode === 'local' ? 'local' : 'session',
      id: 's1',
      name: mode === 'local' ? 'Local' : 'Dashboard',
      // A dashboard session (and local mode) holds every scope.
      scopes: [
        'collections:read',
        'collections:write',
        'collections:delete',
        'posts:read',
        'posts:write',
        'posts:delete',
        'posts:publish',
        'channels:read',
        'channels:manage',
        'schedules:manage',
        'media:read',
        'media:write',
        'webhooks:manage',
        'alerts:manage',
        'events:read',
        'admin',
      ],
    },
    features: { ai_summaries: false },
    defaults: { processing_timeout_seconds: 3600 },
  }
}

/** The inbox's active row (aria-current), optionally checked to show `name`. */
export async function findActiveRow(name?: RegExp) {
  return waitFor(() => getActiveRow(name))
}

export function getActiveRow(name?: RegExp) {
  const row = screen.getByRole('listitem', { current: true })

  if (name && !name.test(row.textContent ?? '')) {
    throw new Error(`active row is "${row.textContent}", not ${name}`)
  }

  return row
}

/** An inbox row by its text, or null. */
export function queryRow(name: RegExp) {
  // Inbox rows only: toasts are list items too.
  return (
    screen
      .queryAllByRole('listitem')
      .find((li) => li.querySelector('[data-item-id]') && name.test(li.textContent ?? '')) ?? null
  )
}

/** The posts a list request asks for by its status filter (one status or several, comma-separated). */
export function byStatus<T extends { status: string }>(posts: T[], call: Call): T[] {
  const want = call.query.get('status')

  return want ? posts.filter((p) => want.split(',').includes(p.status)) : posts
}
