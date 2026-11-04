import { describe, expect, it, vi } from 'vitest'

const calls = vi.hoisted(() => ({ init: [] as unknown[], set: [] as unknown[], send: [] as unknown[] }))

vi.mock('react-ga4', () => ({
  default: {
    isInitialized: false,
    initialize: (...a: unknown[]) => calls.init.push(a),
    set: (v: unknown) => calls.set.push(v),
    send: (v: unknown) => calls.send.push(v),
  },
}))

import { startGoogleAnalytics, trackPage } from '@/lib/analytics'

describe('Google Analytics in the app', () => {
  it('sends page views with the path alone: no query (codes, searches) and no fragment (tokens)', async () => {
    trackPage('/posts') // before it starts: nothing
    expect(calls.send).toHaveLength(0)

    window.history.pushState({}, '', '/invite?token=secret#token=stk_local_x')
    startGoogleAnalytics('G-TEST123')
    await vi.waitFor(() => expect(calls.send).toHaveLength(1))

    const [id, opts] = calls.init[0] as [string, { gtagOptions: Record<string, unknown> }]

    expect(id).toBe('G-TEST123')
    expect(opts.gtagOptions).toEqual({ send_page_view: false, page_location: `${window.location.origin}/invite` })

    window.history.pushState({}, '', '/posts?q=launch%20plans')
    trackPage('/posts')

    expect(calls.send[1]).toEqual({
      hitType: 'pageview',
      page: '/posts',
      location: `${window.location.origin}/posts`,
      title: 'staqly',
    })
    expect(JSON.stringify(calls)).not.toMatch(/secret|stk_local|launch/)
  })
})
