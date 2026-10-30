import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, render, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { LiveUpdates } from '@/components/shell/live-updates'
import { api, type StashEvent } from '@/lib/api'
import { qk } from '@/lib/queries'

const event = (seq: number, type = 'webhook.test') =>
  ({ id: `evt_${seq}`, seq, type, post_id: null, actor: { type: 'api_key' } }) as StashEvent

describe('live updates', () => {
  it('refreshes potentially missed entities when a burst fills the recent-events window', async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } })
    const initial = [event(1)]
    const keys = [
      qk.collections(),
      qk.collection('col_1'),
      qk.channels,
      qk.schedules(),
      qk.post('pst_missing'),
      qk.webhooks,
      qk.alerts,
    ]

    for (const key of keys) {
      qc.setQueryData(key, { cached: true })
    }

    qc.setQueryData(qk.recentEvents, initial)
    const fetchEvents = vi.spyOn(api, 'events').mockResolvedValue({ data: initial })
    const { unmount } = render(
      <QueryClientProvider client={qc}>
        <LiveUpdates />
      </QueryClientProvider>,
    )

    // The events for the changed entities were followed by 20 other events between polls.
    await act(async () => {
      qc.setQueryData(
        qk.recentEvents,
        Array.from({ length: 20 }, (_, i) => event(30 - i)),
      )
    })
    await waitFor(() => {
      for (const key of keys) {
        expect(qc.getQueryState(key)?.isInvalidated).toBe(true)
      }
    })
    unmount()
    qc.clear()
    fetchEvents.mockRestore()
  })

  it.each([
    ['webhook.updated', qk.webhooks],
    ['alert.updated', qk.alerts],
  ])('refreshes %s changes from another tab', async (type, key) => {
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } })
    const initial = [event(1)]

    qc.setQueryData(key, [])
    qc.setQueryData(qk.channels, [])
    qc.setQueryData(qk.recentEvents, initial)
    const fetchEvents = vi.spyOn(api, 'events').mockResolvedValue({ data: initial })
    const { unmount } = render(
      <QueryClientProvider client={qc}>
        <LiveUpdates />
      </QueryClientProvider>,
    )

    await act(async () => {
      qc.setQueryData(qk.recentEvents, [event(2, type), ...initial])
    })
    await waitFor(() => expect(qc.getQueryState(key)?.isInvalidated).toBe(true))
    expect(qc.getQueryState(qk.channels)?.isInvalidated).toBe(false)
    unmount()
    qc.clear()
    fetchEvents.mockRestore()
  })

  it('keeps unrelated caches fresh when the window overlaps the previous poll', async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } })
    const initial = [event(1)]

    qc.setQueryData(qk.channels, [])
    qc.setQueryData(qk.recentEvents, initial)
    const fetchEvents = vi.spyOn(api, 'events').mockResolvedValue({ data: initial })
    const { unmount } = render(
      <QueryClientProvider client={qc}>
        <LiveUpdates />
      </QueryClientProvider>,
    )

    await act(async () => {
      qc.setQueryData(qk.recentEvents, [event(2), ...initial])
    })
    expect(qc.getQueryState(qk.channels)?.isInvalidated).toBe(false)
    unmount()
    qc.clear()
    fetchEvents.mockRestore()
  })
})
