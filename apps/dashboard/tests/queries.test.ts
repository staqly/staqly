import { InfiniteQueryObserver, QueryClient, type InfiniteData } from '@tanstack/react-query'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { EventPage, ItemPage, StashEvent } from '@/lib/api'
import { createRecentIds, qk, refetchItemListsFromStart, refreshEventLists, shareItemPages } from '@/lib/queries'

import { item } from './fixtures'
import { mockApi } from './utils'

const a = item({ id: 'itm_a', title: 'A' })
const b = item({ id: 'itm_b', title: 'B' })
const c = item({ id: 'itm_c', title: 'C' })

function data(...pages: ItemPage[]): InfiniteData<ItemPage> {
  return { pages, pageParams: pages.map((_, i) => (i ? `cur${i}` : '')) }
}

describe('shareItemPages', () => {
  it('returns the previous data when nothing changed', () => {
    const prev = data({ data: [a, b], next_cursor: 'cur1' }, { data: [c] })
    const next = structuredClone(prev)

    expect(shareItemPages(prev, next)).toBe(prev)
  })

  it('keeps item identity when items shift to another page or index', () => {
    const prev = data({ data: [a, b], next_cursor: 'cur1' }, { data: [c] })
    // `a` was approved elsewhere: the refetch moves b and c up.
    const next = structuredClone(data({ data: [b, c] }))
    const out = shareItemPages(prev, next) as InfiniteData<ItemPage>

    expect(out).not.toBe(prev)
    expect(out.pages[0].data[0]).toBe(b)
    expect(out.pages[0].data[1]).toBe(c)
  })

  it('replaces only items whose content changed', () => {
    const prev = data({ data: [a, b] })
    const next = structuredClone(prev)

    next.pages[0].data[1] = { ...next.pages[0].data[1], title: 'B2' }

    const out = shareItemPages(prev, next) as InfiniteData<ItemPage>

    expect(out.pages[0].data[0]).toBe(a)
    expect(out.pages[0].data[1]).not.toBe(b)
    expect(out.pages[0].data[1].title).toBe('B2')
  })

  it('handles a newly appended page', () => {
    const prev = data({ data: [a], next_cursor: 'cur1' })
    const next = data({ data: [a], next_cursor: 'cur1' }, { data: [b] })
    const out = shareItemPages(prev, next) as InfiniteData<ItemPage>

    expect(out.pages[0]).toBe(prev.pages[0])
    expect(out.pages[1].data[0]).toBe(b)
  })
})

describe('refetchItemListsFromStart', () => {
  it('keeps only the first page of every item list, so the refetch is one request per list', async () => {
    const qc = new QueryClient()
    const key = qk.items({ collection_id: 'col_1', limit: 48 })

    qc.setQueryData(key, data({ data: [a], next_cursor: 'cur1' }, { data: [b], next_cursor: 'cur2' }, { data: [c] }))
    refetchItemListsFromStart(qc, 'itm_new')

    const out = qc.getQueryData<InfiniteData<ItemPage>>(key)!

    expect(out.pages).toHaveLength(1)
    expect(out.pageParams).toEqual([''])
    expect(qc.getQueryState(key)?.isInvalidated).toBe(true)
  })
})

describe('createRecentIds', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it('treats an id as changed here only for a while, so a later change to the item counts as foreign', () => {
    vi.useFakeTimers()
    const ids = createRecentIds(1000)

    ids.add('itm_a')
    expect(ids.has('itm_a')).toBe(true)
    expect(ids.has('itm_b')).toBe(false)
    vi.advanceTimersByTime(1001)
    expect(ids.has('itm_a')).toBe(false)
    // Adding again restarts the window.
    ids.add('itm_a')
    expect(ids.has('itm_a')).toBe(true)
  })
})

const ev = (seq: number) => ({ id: `evt_${seq}`, seq, type: 'item.created' }) as StashEvent

describe('refreshEventLists', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('puts new events in front of a loaded activity feed with one request instead of refetching every page', async () => {
    const api = mockApi({ 'GET /v1/events': { data: [ev(12), ev(11), ev(10), ev(9)], next_cursor: 'c1' } })
    const qc = new QueryClient()
    const key = qk.events({ limit: 50 })
    const loaded: InfiniteData<EventPage> = {
      pages: [
        { data: [ev(10), ev(9)], next_cursor: 'c1' },
        { data: [ev(8), ev(7)], next_cursor: 'c2' },
      ],
      pageParams: ['', 'c1'],
    }

    qc.setQueryData(key, loaded)
    const observer = new InfiniteQueryObserver(qc, {
      queryKey: key,
      queryFn: () => Promise.reject(new Error('the feed must not refetch its pages')),
      initialPageParam: '',
      getNextPageParam: (last: EventPage) => last.next_cursor || undefined,
      staleTime: Infinity,
    })
    const unsubscribe = observer.subscribe(() => {})

    await refreshEventLists(qc)
    unsubscribe()

    const out = qc.getQueryData<InfiniteData<EventPage>>(key)!

    expect(api.find('GET', '/v1/events')).toHaveLength(1)
    expect(out.pages.map((p) => p.data.map((e) => e.seq))).toEqual([
      [12, 11, 10, 9],
      [8, 7],
    ])
    expect(out.pages[1]).toBe(loaded.pages[1])
  })
})
