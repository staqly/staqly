import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, render, waitFor } from '@testing-library/react'
import { toast } from 'sonner'
import { describe, expect, it, vi } from 'vitest'

import { insertMedia, mediaKeys, useLiveMedia } from '@/components/media/media-cache'
import { LiveUpdates } from '@/components/shell/live-updates'
import { api, type Media, type MediaLibraryItem, type MediaPage, type Post, type StashEvent } from '@/lib/api'
import { awaitOutcome, progressOf } from '@/lib/publish-outcome'
import { undoToast } from '@/lib/publish-undo'
import { announceWorkspace, qk } from '@/lib/queries'

const event = (seq: number, type = 'webhook.test') =>
  ({ id: `evt_${seq}`, seq, type, post_id: null, actor: { type: 'api_key' } }) as StashEvent

const file = (id: string, over: Partial<MediaLibraryItem> = {}): MediaLibraryItem => ({
  id,
  mime_type: 'image/png',
  size_bytes: 1,
  original_name: `${id}.png`,
  url: '',
  sha256: id,
  created_at: '2026-11-01T10:00:00Z',
  alt_text: '',
  width: 1,
  height: 1,
  duration_ms: 0,
  folder_id: '',
  tags: [],
  uses: 0,
  ...over,
})

const page = (data: MediaLibraryItem[]) => ({ pages: [{ data } as MediaPage], pageParams: [0] })

function MediaView() {
  useLiveMedia()

  return null
}

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

  it.each([
    ['workspace.credits_added', qk.xPosts],
    ['workspace.credits_added', qk.usage],
    ['workspace.plan_changed', qk.billing],
    ['workspace.plan_changed', qk.usage],
    ['workspace.entitlements_changed', qk.addons],
    ['workspace.entitlements_changed', qk.usage],
    ['member.updated', qk.session],
    ['member.removed', qk.members],
    ['invitation.accepted', qk.members],
    ['invitation.accepted', qk.invitations],
  ])('reloads after %s from elsewhere: %j', async (type, key) => {
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } })
    const initial = [event(1)]

    qc.setQueryData(key, { cached: true })
    qc.setQueryData(qk.channels, [])
    qc.setQueryData(qk.recentEvents, initial)
    const fetchEvents = vi.spyOn(api, 'events').mockResolvedValue({ data: initial })
    const { unmount } = render(
      <QueryClientProvider client={qc}>
        <LiveUpdates />
      </QueryClientProvider>,
    )

    await act(async () => {
      qc.setQueryData(qk.recentEvents, [{ ...event(2, type), actor: { type: 'system' } } as StashEvent, ...initial])
    })
    await waitFor(() => expect(qc.getQueryState(key)?.isInvalidated).toBe(true))
    expect(qc.getQueryState(qk.channels)?.isInvalidated).toBe(false)
    unmount()
    qc.clear()
    fetchEvents.mockRestore()
  })

  it.each([
    ['post.responses_updated', qk.postResponses],
    ['post.metrics_updated', qk.postMetrics],
    ['post.feedback_added', qk.postFeedback],
  ])('reloads the post %s names, and no other post', async (type, key) => {
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } })
    const initial = [event(1)]

    qc.setQueryData(key('pst_read'), { cached: true })
    qc.setQueryData(key('pst_other'), { cached: true })
    qc.setQueryData(qk.recentEvents, initial)
    const fetchEvents = vi.spyOn(api, 'events').mockResolvedValue({ data: initial })
    const { unmount } = render(
      <QueryClientProvider client={qc}>
        <LiveUpdates />
      </QueryClientProvider>,
    )

    await act(async () => {
      qc.setQueryData(qk.recentEvents, [
        { ...event(2, type), post_id: 'pst_read', actor: { type: 'system' } } as StashEvent,
        ...initial,
      ])
    })
    await waitFor(() => expect(qc.getQueryState(key('pst_read'))?.isInvalidated).toBe(true))
    expect(qc.getQueryState(key('pst_other'))?.isInvalidated).toBe(false)
    unmount()
    qc.clear()
    fetchEvents.mockRestore()
  })

  it("media views write an agent's changes into the open library without reloading it, and skips this tab's own", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } })
    const initial = [event(1)]
    const library = mediaKeys.library({})

    qc.setQueryData(library, page([file('med_old', { uses: 3 }), file('med_gone')]))
    qc.setQueryData(qk.recentEvents, initial)
    const fetchEvents = vi.spyOn(api, 'events').mockResolvedValue({ data: initial })
    const media = vi.spyOn(api, 'media').mockImplementation(
      async (id) =>
        file(id, {
          original_name: 'renamed.png',
          ...(id === 'med_new' && { created_at: '2026-11-02T10:00:00Z' }),
        }) as Media,
    )
    const { unmount } = render(
      <QueryClientProvider client={qc}>
        <MediaView />
      </QueryClientProvider>,
    )
    const ev = (seq: number, type: string, ids: string[]) =>
      ({ ...event(seq, type), data: { media_ids: ids } }) as StashEvent

    await act(async () => {
      qc.setQueryData(qk.recentEvents, [
        ev(4, 'media.deleted', ['med_gone']),
        ev(3, 'media.updated', ['med_old']),
        ev(2, 'media.created', ['med_new']),
        ...initial,
      ])
    })
    await waitFor(() =>
      expect(
        qc.getQueryData<{ pages: MediaPage[] }>(library)?.pages[0].data.map((m) => [m.id, m.original_name, m.uses]),
      ).toEqual([
        ['med_new', 'renamed.png', 0],
        ['med_old', 'renamed.png', 3],
      ]),
    )
    expect(qc.getQueryState(library)?.isInvalidated).toBe(false)
    expect(media).toHaveBeenCalledTimes(2)

    // This tab's own upload: already in its cache, nothing to read.
    insertMedia(qc, [file('med_mine', { created_at: '2026-11-03T10:00:00Z' })])
    await act(async () => {
      qc.setQueryData(qk.recentEvents, [ev(5, 'media.created', ['med_mine']), ...initial])
    })
    expect(media).toHaveBeenCalledTimes(2)
    unmount()
    qc.clear()
    fetchEvents.mockRestore()
    media.mockRestore()
  })

  it("reloads a post's comments when the AI's reading of them arrives, and no other post's", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } })
    const initial = [event(1)]

    qc.setQueryData(qk.postResponses('pst_read'), { overall: null, channels: [] })
    qc.setQueryData(qk.postResponses('pst_other'), { overall: null, channels: [] })
    qc.setQueryData(qk.recentEvents, initial)
    const fetchEvents = vi.spyOn(api, 'events').mockResolvedValue({ data: initial })
    const { unmount } = render(
      <QueryClientProvider client={qc}>
        <LiveUpdates />
      </QueryClientProvider>,
    )

    await act(async () => {
      qc.setQueryData(qk.recentEvents, [
        { ...event(2, 'post.responses_analyzed'), post_id: 'pst_read', actor: { type: 'system' } } as StashEvent,
        ...initial,
      ])
    })
    await waitFor(() => expect(qc.getQueryState(qk.postResponses('pst_read'))?.isInvalidated).toBe(true))
    expect(qc.getQueryState(qk.postResponses('pst_other'))?.isInvalidated).toBe(false)
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

  it('says when a post this tab published with Publish now went out, once', async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } })
    const initial = [event(1)]
    const success = vi.spyOn(toast, 'success').mockImplementation(() => 1)

    undoToast({ id: 'pst_mine', title: 'Test 1', content: 'x', targets: [{}, {}] } as Post, () => {})
    qc.setQueryData(qk.recentEvents, initial)
    const fetchEvents = vi.spyOn(api, 'events').mockResolvedValue({ data: initial })
    const { unmount } = render(
      <QueryClientProvider client={qc}>
        <LiveUpdates />
      </QueryClientProvider>,
    )
    const published = (seq: number, postId: string) =>
      ({ ...event(seq, 'post.published'), post_id: postId }) as StashEvent

    // A channel reports back: the toast's progress moves, and it isn't over yet.
    await act(async () => {
      qc.setQueryData(qk.recentEvents, [
        { ...event(2, 'post.target_published'), post_id: 'pst_mine' } as StashEvent,
        ...initial,
      ])
    })
    await waitFor(() => expect(progressOf('pst_mine')).toEqual({ done: 1, failed: 0, total: 2 }))
    expect(success).not.toHaveBeenCalled()
    await act(async () => {
      qc.setQueryData(qk.recentEvents, [published(4, 'pst_other'), published(3, 'pst_mine'), ...initial])
    })
    await waitFor(() =>
      expect(success).toHaveBeenCalledWith('Published', {
        id: 'publish:pst_mine',
        description: 'Test 1',
        duration: 5000,
      }),
    )
    expect(progressOf('pst_mine')).toBeUndefined()
    await act(async () => {
      qc.setQueryData(qk.recentEvents, [published(5, 'pst_mine'), published(4, 'pst_other'), ...initial])
    })
    expect(success).toHaveBeenCalledTimes(1)
    unmount()
    qc.clear()
    fetchEvents.mockRestore()
    success.mockRestore()
  })

  it('says Unpublished once every channel being taken down settled', async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } })
    const initial = [event(1)]
    const success = vi.spyOn(toast, 'success').mockImplementation(() => 1)
    const removed = (seq: number) => ({ ...event(seq, 'post.target_removed'), post_id: 'pst_down' }) as StashEvent

    awaitOutcome('pst_down', 'Down', true, 2)
    qc.setQueryData(qk.recentEvents, initial)
    const fetchEvents = vi.spyOn(api, 'events').mockResolvedValue({ data: initial })
    const { unmount } = render(
      <QueryClientProvider client={qc}>
        <LiveUpdates />
      </QueryClientProvider>,
    )

    await act(async () => {
      qc.setQueryData(qk.recentEvents, [removed(2), ...initial])
    })
    expect(success).not.toHaveBeenCalled()
    await act(async () => {
      qc.setQueryData(qk.recentEvents, [removed(3), removed(2), ...initial])
    })
    await waitFor(() =>
      expect(success).toHaveBeenCalledWith('Unpublished', { id: undefined, description: 'Down', duration: 5000 }),
    )
    unmount()
    qc.clear()
    fetchEvents.mockRestore()
    success.mockRestore()
  })
})

// The tabs of one browser share the session: when one switches workspace, the writes of the others would land in
// the new workspace while their screen still shows the old one.
describe('another tab switched workspace', () => {
  const assign = vi.fn<(url: string) => void>()

  function open(workspace: string) {
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } })

    vi.stubGlobal('location', { ...window.location, assign })
    assign.mockReset()
    qc.setQueryData(qk.session, { workspace: { id: workspace } })
    qc.setQueryData(qk.recentEvents, [])
    vi.spyOn(api, 'events').mockResolvedValue({ data: [] })

    return render(
      <QueryClientProvider client={qc}>
        <LiveUpdates />
      </QueryClientProvider>,
    )
  }

  it('this tab opens the new workspace too', async () => {
    const { unmount } = open('ws_1')

    announceWorkspace('ws_2')
    await waitFor(() => expect(assign).toHaveBeenCalledWith('/schedule'))
    unmount()
  })

  it('a tab already in that workspace stays where it is', async () => {
    const { unmount } = open('ws_2')

    announceWorkspace('ws_2')
    await new Promise((r) => setTimeout(r, 50))
    expect(assign).not.toHaveBeenCalled()
    unmount()
  })
})
